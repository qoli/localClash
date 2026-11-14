package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"localclash/internal/poolcontract"
)

func TestSchedulerRecordsEnhancedModeAndPreservesLegacyStatus(t *testing.T) {
	for _, enhanced := range []bool{false, true} {
		workDir := t.TempDir()
		path := filepath.Join(workDir, poolcontract.Directory, "schedule.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"last_run_at":"2026-11-12T08:00:00Z","qualified":{"network.doh.qualified.v1":5}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		args := []string{"schedule", "--runtime", workDir, "--period", "5m"}
		if enhanced {
			args = append(args, "--enhanced-filter")
		}
		if err := run(ctx, args); !errors.Is(err, context.Canceled) {
			t.Fatalf("schedule = %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var status scheduleStatus
		if err := json.Unmarshal(data, &status); err != nil {
			t.Fatal(err)
		}
		if status.EnhancedFilter != enhanced || status.LastRunAt == nil || status.NextRunAt == nil || status.Qualified[poolcontract.AutoCapability] != 5 {
			t.Fatalf("enhanced=%t, status = %+v", enhanced, status)
		}
	}
}

func TestPrepareDoesNotAcceptEnhancedFilterFlag(t *testing.T) {
	if err := run(t.Context(), []string{"prepare", "--enhanced-filter"}); err == nil {
		t.Fatal("prepare accepted enhanced filtering")
	}
}

func TestNextRun(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// The next day begins daylight saving time, 23 hours after today's 08:00.
	now := time.Date(2026, time.March, 7, 8, 0, 0, 0, location)
	for period, elapsed := range map[string]time.Duration{
		"5m": 5 * time.Minute, "15m": 15 * time.Minute, "30m": 30 * time.Minute,
		"1h": time.Hour, "daily-08": 23 * time.Hour,
	} {
		got, err := nextRun(now, period)
		if err != nil || !got.Equal(now.Add(elapsed)) {
			t.Fatalf("nextRun(%q) = %v, %v; want %v", period, got, err, now.Add(elapsed))
		}
	}
	before := now.Add(-time.Second)
	if got, err := nextRun(before, "daily-08"); err != nil || !got.Equal(now) {
		t.Fatalf("next daily run before 08:00 = %v, %v; want %v", got, err, now)
	}
	if _, err := nextRun(now, "10m"); err == nil {
		t.Fatal("unknown period was accepted")
	}
}
