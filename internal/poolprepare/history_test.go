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
	history["failed"] = autoObservation{WorstDoHNS: int64(time.Second)}.afterProbe(0)
	history["skipped"] = autoObservation{WorstDoHNS: int64(time.Millisecond)}
	history["removed"] = autoObservation{ConsecutiveFailures: 1}
	history["recovered"] = autoObservation{ConsecutiveFailures: 2}.afterProbe(20 * time.Millisecond)
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
