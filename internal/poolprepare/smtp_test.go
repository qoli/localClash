package poolprepare

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

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
