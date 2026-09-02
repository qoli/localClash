package logarchive

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func records(t *testing.T, dir string) []Record {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "mihomo-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []Record
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		s := bufio.NewScanner(f)
		for s.Scan() {
			var r Record
			if err := json.Unmarshal(s.Bytes(), &r); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		if err := s.Err(); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	return out
}

func TestSliding48HoursNotCalendarDays(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 3, 0, 30, 0, 0, time.UTC)
	a, err := openArchive(dir, now.Add(-49*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	for _, age := range []time.Duration{49 * time.Hour, 47 * time.Hour, 2 * time.Hour} {
		if err := a.append(Record{ReceivedAt: now.Add(-age), Kind: "log", Message: age.String()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.prune(now); err != nil {
		t.Fatal(err)
	}
	got := records(t, dir)
	if len(got) != 2 {
		t.Fatalf("records=%+v", got)
	}
	for _, r := range got {
		if r.ReceivedAt.Before(now.Add(-Window)) {
			t.Fatal("retained expired record")
		}
	}
	// Expiry still runs without a new source event.
	if err := a.prune(now.Add(49 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(records(t, dir)) != 0 {
		t.Fatal("idle archive did not expire")
	}
}

func TestCapacityAndPrivateFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	a, err := openArchive(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	a.budget = 8192
	for i := 0; i < 10; i++ {
		if err := a.append(Record{ReceivedAt: now.Add(time.Duration(i) * time.Minute), Kind: "log", Message: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if a.used() > a.budget || a.evicted == 0 {
		t.Fatalf("used=%d evicted=%d", a.used(), a.evicted)
	}
	for _, s := range a.segments {
		info, err := os.Stat(s.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("mode=%o", info.Mode().Perm())
		}
	}
	info, _ := os.Stat(dir)
	if info.Mode().Perm() != 0700 {
		t.Fatalf("dir mode=%o", info.Mode().Perm())
	}
}

func TestRejectUnsafeAndMalformedArchive(t *testing.T) {
	for _, mode := range []string{"symlink", "permissions", "name", "size"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "mihomo-20260903T0000Z-123.jsonl")
			switch mode {
			case "symlink":
				if err := os.Symlink("/etc/hosts", path); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				os.WriteFile(path, []byte("x"), 0644)
			case "name":
				os.WriteFile(filepath.Join(dir, "mihomo-invalid.jsonl"), nil, 0600)
			case "size":
				os.WriteFile(path, nil, 0600)
				os.Truncate(path, segmentLimit+1)
			}
			if a, err := openArchive(dir, time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC)); err == nil {
				a.close()
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestStreamPersistsExistingDebugWithoutMutatingConfig(t *testing.T) {
	seen := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/logs" || r.URL.Query().Get("level") != "debug" || r.Header.Get("Authorization") != "Bearer private" {
			t.Errorf("unexpected log request: %s", r.URL)
		}
		fmt.Fprintln(w, `{"type":"debug","payload":"[SmartTiming] trace=abc https://example.test/path?token=private"}`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	dir := t.TempDir()
	config := filepath.Join(dir, "config.yaml")
	content := []byte("external-controller: " + strings.TrimPrefix(server.URL, "http://") + "\nsecret: private\n")
	if err := os.WriteFile(config, content, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer close(seen)
		err := stream(ctx, server.Client(), config, func(r Record) {
			if r.Level != "debug" || strings.Contains(r.Message, "token=private") {
				t.Errorf("record=%+v", r)
			}
			cancel()
		})
		if err == nil {
			t.Error("expected stream ending")
		}
	}()
	select {
	case <-seen:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not stop")
	}
	got, _ := os.ReadFile(config)
	if string(got) != string(content) {
		t.Fatal("collector mutated config")
	}
}

func TestCollectorNoControllerAndCancellation(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, filepath.Join(dir, "missing.yaml"), filepath.Join(dir, "history")) }()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			t.Fatal("missing disconnect marker")
		case <-time.After(20 * time.Millisecond):
		}
		got := records(t, filepath.Join(dir, "history"))
		found := false
		for _, r := range got {
			if r.Reason == "stream_disconnected_or_unavailable" {
				found = true
			}
		}
		if found {
			break
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("collector cancellation blocked")
	}
}

func TestArchiveLockRejectsSecondCollector(t *testing.T) {
	dir := t.TempDir()
	release, err := lockArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if release2, err := lockArchive(dir); err == nil {
		release2()
		t.Fatal("second writer acquired lock")
	}
}

func TestRedaction(t *testing.T) {
	for _, input := range []string{
		"Authorization: Bearer sensitive-value", "authorization=Basic sensitive-value",
		"password=sensitive-value", "https://user:sensitive-value@example.test/path",
		"https://example.test/path?key=sensitive-value",
	} {
		if got := redact(input); strings.Contains(got, "sensitive-value") {
			t.Fatalf("credential survived redaction: %q", got)
		}
	}
}

func TestArchiveReopenAndSegmentRotation(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	a, err := openArchive(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if err := a.append(Record{ReceivedAt: now, Kind: "log", Message: strings.Repeat("x", 16000)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.segments) < 2 {
		t.Fatal("segments did not rotate")
	}
	for _, s := range a.segments {
		if s.size > segmentLimit {
			t.Fatal("segment exceeded limit")
		}
	}
	if err := a.close(); err != nil {
		t.Fatal(err)
	}
	a, err = openArchive(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	if len(records(t, dir)) != 40 {
		t.Fatal("reopen lost persisted records")
	}
	if err := a.prune(now.Add(49 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(records(t, dir)) != 0 {
		t.Fatal("reopened records did not expire")
	}
}

func TestExpiryHasHeadroomForNextSweep(t *testing.T) {
	for _, seconds := range []int{0, 29, 59} {
		now := time.Date(2026, 9, 3, 12, 0, seconds, 0, time.UTC)
		dir := t.TempDir()
		a, err := openArchive(dir, now.Add(-Window))
		if err != nil {
			t.Fatal(err)
		}
		for _, age := range []time.Duration{Window, Window - time.Second, Window - time.Minute, Window - 2*time.Minute} {
			if err := a.append(Record{ReceivedAt: now.Add(-age), Kind: "log"}); err != nil {
				t.Fatal(err)
			}
		}
		if err := a.prune(now); err != nil {
			t.Fatal(err)
		}
		for _, r := range records(t, dir) {
			if r.ReceivedAt.Before(now.Add(SweepInterval).Add(-Window)) {
				t.Fatal("record would exceed 48h before next sweep")
			}
		}
		a.close()
	}
}

func TestAppendWriteErrorIsReturned(t *testing.T) {
	a, err := openArchive(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	r := Record{ReceivedAt: time.Now(), Kind: "log"}
	if err := a.append(r); err != nil {
		t.Fatal(err)
	}
	if err := a.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.append(r); err == nil {
		t.Fatal("write failure was hidden")
	}
}

func TestCollectorReconnectPersistsGap(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintln(w, `{"type":"debug","payload":"[SmartTiming] trace=test proxy-connect=12ms"}`)
		w.(http.Flusher).Flush()
		if calls > 1 {
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	config := filepath.Join(dir, "config.yaml")
	content := []byte("external-controller: " + strings.TrimPrefix(server.URL, "http://") + "\n")
	if err := os.WriteFile(config, content, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := Run(ctx, config, filepath.Join(dir, "history")); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, r := range records(t, filepath.Join(dir, "history")) {
		seen[r.Kind+":"+r.Reason]++
	}
	if seen["log:"] < 2 || seen["gap:stream_disconnected_or_unavailable"] != 1 || seen["state:stream_reconnected"] != 1 {
		t.Fatalf("records: %v", seen)
	}
	got, _ := os.ReadFile(config)
	if string(got) != string(content) {
		t.Fatal("config changed")
	}
}

func TestStorageErrorAndUnsafeLockFailExplicitly(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), "missing.yaml", blocked); err == nil {
		t.Fatal("storage failure was hidden")
	}
	if err := os.Symlink(blocked, filepath.Join(dir, "collector.lock")); err != nil {
		t.Fatal(err)
	}
	if release, err := lockArchive(dir); err == nil {
		release()
		t.Fatal("symlink lock accepted")
	}
}
