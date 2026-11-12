package poolprepare

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAutoHistoryKeepsSkippedObservationsAndDropsRemovedCandidates(t *testing.T) {
	runtimeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(autoHistoryPath(runtimeDir)), 0o700); err != nil {
		t.Fatal(err)
	}
	history, err := readAutoHistory(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	history["failed"] = autoObservation{WorstDoHNS: int64(time.Second)}.afterProbe(measurement{})
	history["skipped"] = autoObservation{WorstDoHNS: int64(time.Millisecond)}
	history["removed"] = autoObservation{ConsecutiveFailures: 1}
	history["recovered"] = autoObservation{ConsecutiveFailures: 2}.afterProbe(measurement{worstDoH: 20 * time.Millisecond})
	candidates := []autoCandidate{
		{fingerprint: "failed"}, {fingerprint: "skipped"},
		{fingerprint: "recovered"}, {fingerprint: "unmeasured"},
	}
	if err := writeAutoHistory(runtimeDir, candidates, history); err != nil {
		t.Fatal(err)
	}
	got, err := readAutoHistory(runtimeDir)
	want := map[string]autoObservation{
		"failed":    {WorstDoHNS: int64(time.Second), ConsecutiveFailures: 1},
		"skipped":   {WorstDoHNS: int64(time.Millisecond)},
		"recovered": {WorstDoHNS: int64(20 * time.Millisecond)},
	}
	if err != nil || !maps.Equal(got, want) {
		t.Fatalf("read history = %+v, %v, want %+v", got, err, want)
	}
}

func TestAutoHistoryRecordsLatestYouTubeAttemptWithLegacyHistory(t *testing.T) {
	runtimeDir := t.TempDir()
	path := autoHistoryPath(runtimeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"nodes":{"candidate":{"worst_doh_ns":1000000}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	history, err := readAutoHistory(runtimeDir)
	if err != nil || history["candidate"].WorstDoHNS != time.Millisecond.Nanoseconds() || history["candidate"].YouTube != nil {
		t.Fatalf("legacy history = %+v, %v", history, err)
	}
	youtube := youtubeObservation{
		Node: "example", CheckedAt: time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC), DurationMS: 5000, Error: "timeout",
	}
	observed := history["candidate"].afterProbe(measurement{worstDoH: 20 * time.Millisecond, youtube: &youtube})
	history["candidate"] = observed.afterProbe(measurement{})
	if err := writeAutoHistory(runtimeDir, []autoCandidate{{fingerprint: "candidate"}}, history); err != nil {
		t.Fatal(err)
	}
	saved, err := readAutoHistory(runtimeDir)
	got := saved["candidate"]
	if err != nil || got.WorstDoHNS != (20*time.Millisecond).Nanoseconds() || got.ConsecutiveFailures != 1 ||
		got.YouTube == nil || *got.YouTube != youtube {
		t.Fatalf("history after a skipped YouTube check = %+v, YouTube %+v, error %v", got, got.YouTube, err)
	}
}
