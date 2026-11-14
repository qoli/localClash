package poolprepare

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
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
	youtube := &youtubeObservation{HTTPStatus: http.StatusNoContent}
	history["recovered"] = autoObservation{ConsecutiveFailures: 2}.afterProbe(measurement{
		worstDoH: 20 * time.Millisecond, youtube: youtube,
	})
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
		"recovered": {WorstDoHNS: int64(20 * time.Millisecond), YouTube: youtube},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("read history = %+v, %v, want %+v", got, err, want)
	}
}

func TestAutoHistoryPreservesLastEnhancedObservationDuringBasicPreparation(t *testing.T) {
	runtimeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(autoHistoryPath(runtimeDir)), 0o700); err != nil {
		t.Fatal(err)
	}
	enhanced := &enhancedObservation{CheckedAt: time.Now().UTC(), DurationMS: 123,
		Checks: []networkObservation{{Probe: "smtp_starttls", Target: smtpProbeAddress, Error: "timeout"}}}
	base := measurement{worstDoH: time.Millisecond, youtube: &youtubeObservation{HTTPStatus: http.StatusNoContent}}
	failed := base
	failed.enhanced = enhanced
	observed := (autoObservation{}).afterProbe(failed)
	if observed.ConsecutiveFailures != 1 {
		t.Fatalf("enhanced failure history = %+v", observed)
	}
	observed = observed.afterProbe(base)
	if observed.ConsecutiveFailures != 0 || observed.Enhanced != enhanced {
		t.Fatalf("basic preparation lost the previous enhanced observation: %+v", observed)
	}
	if err := writeAutoHistory(runtimeDir, []autoCandidate{{fingerprint: "node"}}, map[string]autoObservation{"node": observed}); err != nil {
		t.Fatal(err)
	}
	saved, err := readAutoHistory(runtimeDir)
	if err != nil || !reflect.DeepEqual(saved["node"].Enhanced, enhanced) {
		t.Fatalf("enhanced history round trip = %+v, %v", saved, err)
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
	if observed.ConsecutiveFailures != 1 {
		t.Fatalf("YouTube timed out with healthy DoH, got %d consecutive failures, want 1", observed.ConsecutiveFailures)
	}
	history["candidate"] = observed.afterProbe(measurement{})
	if err := writeAutoHistory(runtimeDir, []autoCandidate{{fingerprint: "candidate"}}, history); err != nil {
		t.Fatal(err)
	}
	saved, err := readAutoHistory(runtimeDir)
	got := saved["candidate"]
	if err != nil || got.WorstDoHNS != (20*time.Millisecond).Nanoseconds() || got.ConsecutiveFailures != 2 ||
		got.YouTube == nil || *got.YouTube != youtube {
		t.Fatalf("history after a skipped YouTube check = %+v, YouTube %+v, error %v", got, got.YouTube, err)
	}
}
