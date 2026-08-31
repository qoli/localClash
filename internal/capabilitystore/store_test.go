package capabilitystore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPromoteValidatesEveryCandidateBeforeMutatingPromotedSnapshots(t *testing.T) {
	dir := t.TempDir()
	transaction := filepath.Join(dir, "transaction")
	if err := os.MkdirAll(transaction, 0o755); err != nil {
		t.Fatal(err)
	}
	firstPromoted := filepath.Join(dir, "first.json")
	secondPromoted := filepath.Join(dir, "second.json")
	firstCandidate := filepath.Join(transaction, "first.json")
	secondCandidate := filepath.Join(transaction, "second.json")
	for path, content := range map[string]string{
		firstPromoted: "old-first", secondPromoted: "old-second",
		firstCandidate: "new-first", secondCandidate: "invalid-second",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Promote([]Item{
		{ID: "first", Candidate: firstCandidate, Promoted: firstPromoted, Validate: func(string) error { return nil }},
		{ID: "second", Candidate: secondCandidate, Promoted: secondPromoted, Validate: func(string) error { return errors.New("invalid snapshot") }},
	}, transaction)
	if err == nil {
		t.Fatal("promotion unexpectedly accepted an invalid candidate")
	}
	for path, want := range map[string]string{firstPromoted: "old-first", secondPromoted: "old-second"} {
		data, readErr := os.ReadFile(path)
		if readErr != nil || string(data) != want {
			t.Fatalf("promoted snapshot %q = %q err=%v, want %q", path, data, readErr, want)
		}
	}
}
