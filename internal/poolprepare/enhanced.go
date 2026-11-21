package poolprepare

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"localclash/internal/capability"
)

const (
	enhancedProbeTimeout = 10 * time.Second
	ubuntuUpdatesURL     = "http://archive.ubuntu.com/ubuntu/dists/noble-updates/InRelease"
	ubuntuSecurityURL    = "http://security.ubuntu.com/ubuntu/dists/noble-security/InRelease"
	tmdbImageURL         = "https://image.tmdb.org/t/p/w92/1E5baAaEse26fej7uHcjOgEE2t2.jpg"
	microsoftProbeURL    = "http://www.msftconnecttest.com/connecttest.txt"
)

type networkObservation struct {
	Probe      string `json:"probe"`
	Target     string `json:"target"`
	Passed     bool   `json:"passed"`
	DurationMS int64  `json:"duration_ms"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Error      string `json:"error,omitempty"`
}

type enhancedObservation struct {
	CheckedAt  time.Time            `json:"checked_at"`
	DurationMS int64                `json:"duration_ms"`
	Checks     []networkObservation `json:"checks"`
}

func (observation enhancedObservation) passed() bool {
	if len(observation.Checks) == 0 {
		return false
	}
	for _, check := range observation.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func (observation enhancedObservation) record(summary *capability.EnhancedProbeSummary) {
	summary.Probed++
	if observation.passed() {
		summary.Passed++
	}
	for _, check := range observation.Checks {
		counts := summary.Checks[check.Probe]
		counts.Probed++
		if check.Passed {
			counts.Passed++
		}
		summary.Checks[check.Probe] = counts
	}
}

func measureEnhanced(ctx context.Context, client *http.Client) enhancedObservation {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, enhancedProbeTimeout)
	defer cancel()
	// Keep the candidate transport while giving the deeper checks their own budget.
	httpClient := *client
	httpClient.Timeout = enhancedProbeTimeout
	checks := []struct {
		name   string
		target string
		probe  func() (int, error)
	}{
		{"ubuntu_updates", ubuntuUpdatesURL, func() (int, error) {
			return probeContent(ctx, &httpClient, ubuntuUpdatesURL, func(body io.Reader) error { return readUbuntuRelease(body, "noble-updates") })
		}},
		{"ubuntu_security", ubuntuSecurityURL, func() (int, error) {
			return probeContent(ctx, &httpClient, ubuntuSecurityURL, func(body io.Reader) error { return readUbuntuRelease(body, "noble-security") })
		}},
		{"tmdb_image", tmdbImageURL, func() (int, error) { return probeContent(ctx, &httpClient, tmdbImageURL, readTMDBImage) }},
		{"microsoft_connect", microsoftProbeURL, func() (int, error) { return probeContent(ctx, &httpClient, microsoftProbeURL, readMicrosoftConnect) }},
	}
	observation := enhancedObservation{Checks: make([]networkObservation, len(checks))}
	var jobs sync.WaitGroup
	for index, check := range checks {
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			started := time.Now()
			status, err := check.probe()
			result := networkObservation{Probe: check.name, Target: check.target, Passed: err == nil,
				DurationMS: time.Since(started).Milliseconds(), HTTPStatus: status}
			if err != nil {
				result.Error = probeError(err)
			}
			observation.Checks[index] = result
		}()
	}
	jobs.Wait()
	observation.CheckedAt = time.Now().UTC()
	observation.DurationMS = time.Since(start).Milliseconds()
	return observation
}

func probeContent(ctx context.Context, client *http.Client, target string, read func(io.Reader) error) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("unexpected HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, read(resp.Body)
}

func readUbuntuRelease(body io.Reader, suite string) error {
	scanner := bufio.NewScanner(body)
	if !scanner.Scan() || scanner.Text() != "-----BEGIN PGP SIGNED MESSAGE-----" {
		return errors.New("invalid Ubuntu InRelease header")
	}
	var origin, matchedSuite, hashes, signature bool
	for scanner.Scan() {
		switch scanner.Text() {
		case "Origin: Ubuntu":
			origin = true
		case "Suite: " + suite:
			matchedSuite = true
		case "SHA256:":
			hashes = true
		case "-----END PGP SIGNATURE-----":
			signature = true
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !origin || !matchedSuite || !hashes || !signature {
		return errors.New("incomplete Ubuntu InRelease metadata")
	}
	return nil
}

func readTMDBImage(body io.Reader) error {
	thumbnail, err := jpeg.Decode(body)
	if err != nil {
		return err
	}
	if thumbnail.Bounds().Dx() != 92 {
		return errors.New("unexpected TMDB thumbnail size")
	}
	return nil
}

func readMicrosoftConnect(body io.Reader) error {
	const expected = "Microsoft Connect Test"
	data, err := io.ReadAll(io.LimitReader(body, int64(len(expected)+1)))
	if err != nil {
		return err
	}
	if string(data) != expected {
		return errors.New("unexpected Microsoft connectivity content")
	}
	return nil
}

func probeError(err error) string {
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return "timeout"
	}
	// Keep diagnostics free of candidate proxy definitions and credentials.
	return "request_or_content_error"
}
