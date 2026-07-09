package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localclash/internal/appinit"
)

func TestWatchdogTruncatesOversizedMihomoLog(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, ".runtime", "mihomo")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(runtimeDir, "mihomo.log")
	if err := os.WriteFile(logPath, []byte(strings.Repeat("x", 32)), 0o644); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithState(appinit.RuntimeState{Paths: appinit.RuntimePaths{MihomoRuntimeDir: runtimeDir}})

	server.checkMihomoLogSize(10)

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("mihomo log size = %d, want 0", info.Size())
	}
	eventPath := filepath.Join(dir, ".runtime", "logs", "watchdog.jsonl")
	eventData, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatalf("read watchdog event log: %v", err)
	}
	event := string(eventData)
	for _, want := range []string{`"event":"watchdog_mihomo_log"`, `"action":"truncate"`, `"old_size":32`, `"max_bytes":10`, `"result":"ok"`} {
		if !strings.Contains(event, want) {
			t.Fatalf("watchdog event = %s, want %s", event, want)
		}
	}
}

func TestWatchdogSkipsSmallMissingAndNonRegularMihomoLog(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, ".runtime", "mihomo")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithState(appinit.RuntimeState{Paths: appinit.RuntimePaths{MihomoRuntimeDir: runtimeDir}})

	server.checkMihomoLogSize(10)
	if _, err := os.Stat(filepath.Join(runtimeDir, "mihomo.log")); !os.IsNotExist(err) {
		t.Fatalf("missing log stat err = %v, want not exist", err)
	}

	logPath := filepath.Join(runtimeDir, "mihomo.log")
	if err := os.WriteFile(logPath, []byte("small"), 0o644); err != nil {
		t.Fatal(err)
	}
	server.checkMihomoLogSize(10)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "small" {
		t.Fatalf("small log = %q, want unchanged", string(data))
	}

	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath, 0o755); err != nil {
		t.Fatal(err)
	}
	server.checkMihomoLogSize(10)

	eventPath := filepath.Join(dir, ".runtime", "logs", "watchdog.jsonl")
	eventData, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatalf("read watchdog event log: %v", err)
	}
	if !strings.Contains(string(eventData), `"skip":"non_regular_file"`) {
		t.Fatalf("watchdog event = %s, want non_regular_file skip", string(eventData))
	}
}

func TestWatchdogLoopRunsInitialCheckAndStops(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, ".runtime", "mihomo")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(runtimeDir, "mihomo.log")
	if err := os.WriteFile(logPath, []byte(strings.Repeat("x", 32)), 0o644); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithState(appinit.RuntimeState{Paths: appinit.RuntimePaths{MihomoRuntimeDir: runtimeDir}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		server.watchdogLoop(ctx, watchdogOptions{Interval: time.Hour, MihomoLogMaxBytes: 10})
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for {
		info, err := os.Stat(logPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("watchdog did not run initial check")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog loop did not stop")
	}
}
