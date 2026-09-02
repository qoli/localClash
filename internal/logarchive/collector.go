package logarchive

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"localclash/internal/mihomoapi"
)

var querySecret = regexp.MustCompile(`(?i)(https?://[^\s?]+)\?[^\s]+`)
var namedSecret = regexp.MustCompile(`(?i)(authorization|password|passwd|token|secret)([=: ]+)([^\s,;]+)`)
var authSecret = regexp.MustCompile(`(?i)(authorization[=: ]+)(?:(?:bearer|basic)\s+)?[^\s,;]+`)
var urlCredentials = regexp.MustCompile(`(?i)(https?://)[^\s/@]+@`)

func redact(s string) string {
	s = authSecret.ReplaceAllString(s, "${1}<redacted>")
	s = urlCredentials.ReplaceAllString(s, "${1}<redacted>@")
	s = querySecret.ReplaceAllString(s, "${1}?<redacted>")
	return namedSecret.ReplaceAllString(s, "${1}${2}<redacted>")
}

// Run is an independent collector. Storage errors terminate only this process.
// Source disconnections reconnect every five seconds and are explicitly marked.
// The upstream stream is lossy and has no sequence numbers: complete coverage
// cannot be inferred, even when this collector reports no local drops.
func Run(ctx context.Context, config, dir string) error {
	if dir == "" {
		return errors.New("log archive directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("log archive must be a real directory")
	}
	unlock, err := lockArchive(dir)
	if err != nil {
		return err
	}
	defer unlock()
	a, err := openArchive(dir, time.Now())
	if err != nil {
		return err
	}
	defer a.close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan Record, 1024)
	var dropped atomic.Uint64
	emit := func(r Record) {
		select {
		case queue <- r:
		default:
			dropped.Add(1)
		}
	}
	if err := a.append(Record{ReceivedAt: time.Now().UTC(), Kind: "gap", Reason: "collector_started_prior_coverage_unknown"}); err != nil {
		return err
	}
	go collect(ctx, config, emit)
	ticker := time.NewTicker(SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := a.prune(time.Now()); err != nil {
				return err
			}
		case r := <-queue:
			// Timestamp on receipt, not disk-write time. Old queued data is never
			// relabeled as fresh after a long pause.
			if r.ReceivedAt.Before(time.Now().Add(-Window + 2*time.Minute)) {
				dropped.Add(1)
				continue
			}
			if err := a.append(r); err != nil {
				return err
			}
		}
		if n := dropped.Swap(0); n > 0 {
			if err := a.append(Record{ReceivedAt: time.Now().UTC(), Kind: "gap", Reason: "collector_queue_overflow", Count: n}); err != nil {
				return err
			}
		}
		if a.evicted > 0 {
			n := a.evicted
			a.evicted = 0
			if err := a.append(Record{ReceivedAt: time.Now().UTC(), Kind: "gap", Reason: "capacity_evicted_segments", Count: n}); err != nil {
				return err
			}
		}
	}
}

func collect(ctx context.Context, config string, emit func(Record)) {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	unavailable := false
	for ctx.Err() == nil {
		err := stream(ctx, client, config, func(r Record) {
			if unavailable {
				emit(Record{ReceivedAt: time.Now().UTC(), Kind: "state", Reason: "stream_reconnected"})
				unavailable = false
			}
			emit(r)
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil && !unavailable {
			emit(Record{ReceivedAt: time.Now().UTC(), Kind: "gap", Reason: "stream_disconnected_or_unavailable", Message: redact(err.Error())})
			unavailable = true
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func stream(ctx context.Context, httpClient *http.Client, config string, emit func(Record)) error {
	c, err := mihomoapi.NewFromConfig(config)
	if err != nil {
		return err
	}
	u := url.URL{Scheme: "http", Host: c.Controller, Path: "/logs", RawQuery: "level=debug"}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("log stream HTTP %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 16<<10)
	for scanner.Scan() {
		var line struct {
			Type    string `json:"type"`
			Payload string `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return errors.New("malformed controller log frame")
		}
		if line.Type == "" || line.Payload == "" {
			return errors.New("controller log frame lacks type or payload")
		}
		if c.Secret != "" {
			line.Payload = strings.ReplaceAll(line.Payload, c.Secret, "<redacted>")
		}
		emit(Record{ReceivedAt: time.Now().UTC(), Kind: "log", Level: line.Type, Message: redact(line.Payload)})
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}
