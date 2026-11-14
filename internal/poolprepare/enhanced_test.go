package poolprepare

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"localclash/internal/capability"
)

func ubuntuRelease(suite string) string {
	return "-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA512\n\nOrigin: Ubuntu\nSuite: " + suite +
		"\nSHA256:\n checksum 123 file\n-----BEGIN PGP SIGNATURE-----\nsignature\n-----END PGP SIGNATURE-----\n"
}

func TestEnhancedHTTPChecksValidateContent(t *testing.T) {
	var thumbnail bytes.Buffer
	if err := jpeg.Encode(&thumbnail, image.NewRGBA(image.Rect(0, 0, 92, 138)), nil); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		body string
		read func(io.Reader) error
		pass bool
	}{
		{"ubuntu", ubuntuRelease("noble-updates"), func(r io.Reader) error { return readUbuntuRelease(r, "noble-updates") }, true},
		{"wrong suite", ubuntuRelease("noble"), func(r io.Reader) error { return readUbuntuRelease(r, "noble-updates") }, false},
		{"truncated index", strings.TrimSuffix(ubuntuRelease("noble-security"), "-----END PGP SIGNATURE-----\n"), func(r io.Reader) error { return readUbuntuRelease(r, "noble-security") }, false},
		{"portal index", "<html>Sign in</html>", func(r io.Reader) error { return readUbuntuRelease(r, "noble-updates") }, false},
		{"thumbnail", thumbnail.String(), readTMDBImage, true},
		{"truncated thumbnail", thumbnail.String()[:thumbnail.Len()/2], readTMDBImage, false},
		{"portal image", "<html>Sign in</html>", readTMDBImage, false},
		{"microsoft", "Microsoft Connect Test", readMicrosoftConnect, true},
		{"microsoft extra content", "Microsoft Connect Test login", readMicrosoftConnect, false},
		{"portal microsoft", "<html>Sign in</html>", readMicrosoftConnect, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.read(strings.NewReader(test.body)); (err == nil) != test.pass {
				t.Fatalf("content validation = %v, want pass=%t", err, test.pass)
			}
		})
	}
	for _, status := range []int{http.StatusNoContent, http.StatusFound, http.StatusForbidden} {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("Microsoft Connect Test"))}, nil
		})}
		got, err := probeContent(t.Context(), client, microsoftProbeURL, readMicrosoftConnect)
		if err == nil || got != status {
			t.Fatalf("HTTP %d = %d, %v, want rejection", status, got, err)
		}
	}
}

func TestEnhancedProbesRunConcurrentlyAndAffectQualification(t *testing.T) {
	var thumbnail bytes.Buffer
	if err := jpeg.Encode(&thumbnail, image.NewRGBA(image.Rect(0, 0, 92, 138)), nil); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 5)
	release := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		started <- req.URL.String()
		<-release
		var body string
		switch req.URL.String() {
		case ubuntuUpdatesURL:
			body = ubuntuRelease("noble-updates")
		case ubuntuSecurityURL:
			body = ubuntuRelease("noble-security")
		case tmdbImageURL:
			body = thumbnail.String()
		case microsoftProbeURL:
			body = "Microsoft Connect Test"
		default:
			t.Errorf("unexpected HTTP target %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	done := make(chan enhancedObservation, 1)
	go func() {
		done <- measureEnhanced(t.Context(), client, func(ctx context.Context, address string) (net.Conn, error) {
			started <- address
			<-release
			return nil, errors.New("blocked SMTP")
		})
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	seen := make(map[string]bool)
	for range 5 {
		select {
		case target := <-started:
			seen[target] = true
		case <-ctx.Done():
			close(release)
			t.Fatal("enhanced probes did not all start concurrently")
		}
	}
	close(release)
	observation := <-done
	if len(seen) != 5 || observation.CheckedAt.IsZero() || observation.passed() {
		t.Fatalf("observations = %+v, targets = %v", observation, seen)
	}
	summary := &capability.EnhancedProbeSummary{Checks: make(map[string]capability.NetworkProbeSummary)}
	observation.record(summary)
	if summary.Probed != 1 || summary.Passed != 0 || summary.Checks["smtp_starttls"].Passed != 0 || summary.Checks["tmdb_image"].Passed != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	measured := measurement{worstDoH: time.Millisecond, youtube: &youtubeObservation{HTTPStatus: http.StatusNoContent}}
	if !measured.qualified() {
		t.Fatal("base qualification failed before optional checks")
	}
	measured.enhanced = &observation
	if measured.qualified() {
		t.Fatal("a failed enhanced check admitted the node")
	}
	observation.Checks[0].Passed = true
	if !measured.qualified() {
		t.Fatal("a node passing all enhanced checks was rejected")
	}
}

func TestSMTPRequiresGreetingSTARTTLSAndVerifiedCertificate(t *testing.T) {
	certificateServer := httptest.NewTLSServer(http.NotFoundHandler())
	defer certificateServer.Close()
	trustedTLS := certificateServer.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	trustedTLS.ServerName = "example.com"
	for _, mode := range []string{"success", "reject greeting", "no STARTTLS", "untrusted certificate"} {
		t.Run(mode, func(t *testing.T) {
			commands := make(chan []string, 1)
			dial := func(context.Context, string) (net.Conn, error) {
				client, server := net.Pipe()
				go func() {
					defer server.Close()
					var received []string
					defer func() { commands <- received }()
					if mode == "reject greeting" {
						fmt.Fprint(server, "554 unavailable\r\n")
						return
					}
					fmt.Fprint(server, "220 smtp.example ready\r\n")
					reader := bufio.NewReader(server)
					for {
						line, err := reader.ReadString('\n')
						if err != nil {
							return
						}
						command := strings.TrimSpace(line)
						received = append(received, command)
						switch command {
						case "EHLO localhost":
							if len(received) == 1 && mode != "no STARTTLS" {
								fmt.Fprint(server, "250-smtp.example\r\n250 STARTTLS\r\n")
							} else {
								fmt.Fprint(server, "250 smtp.example\r\n")
							}
						case "STARTTLS":
							fmt.Fprint(server, "220 begin TLS\r\n")
							encrypted := tls.Server(server, certificateServer.TLS)
							if err := encrypted.Handshake(); err != nil {
								return
							}
							server = encrypted
							reader = bufio.NewReader(server)
						case "QUIT":
							fmt.Fprint(server, "221 bye\r\n")
							return
						default:
							t.Errorf("unexpected SMTP command %q", command)
							return
						}
					}
				}()
				return client, nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			config := trustedTLS
			if mode == "untrusted certificate" {
				config = nil
			}
			err := probeSMTP(ctx, dial, "example.com:587", config)
			if (err == nil) != (mode == "success") {
				t.Fatalf("SMTP %s = %v", mode, err)
			}
			got := <-commands
			if mode == "success" && !reflect.DeepEqual(got, []string{"EHLO localhost", "STARTTLS", "EHLO localhost", "QUIT"}) {
				t.Fatalf("SMTP commands = %v", got)
			}
		})
	}
}

func TestSMTPStalledGreetingHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	connected := make(chan struct{})
	dial := func(context.Context, string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			close(connected)
			io.Copy(io.Discard, server)
		}()
		return client, nil
	}
	done := make(chan error, 1)
	go func() { done <- probeSMTP(ctx, dial, smtpProbeAddress, nil) }()
	<-connected
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled SMTP probe succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP greeting kept waiting after cancellation")
	}
}
