package poolprepare

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"localclash/internal/poolcontract"
)

func TestAutoCandidatesPrioritizeMatchingDefinitionsFromPreviousRevision(t *testing.T) {
	runtimeDir := t.TempDir()
	previous := writeAutoProvider(t, runtimeDir, map[string]any{
		"name": "old label", "type": "ss", "server": "known.example",
	})
	input, err := poolcontract.NewInput([]map[string]any{
		{"name": "changed", "type": "ss", "server": "other.example"},
		{"name": "new label", "type": "ss", "server": "known.example"},
		{"name": "duplicate", "type": "ss", "server": "known.example"},
		{"name": "unused", "type": "ss", "server": "unused.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if input.Revision == previous.Revision {
		t.Fatal("expected a different subscription revision")
	}
	prior := previousAutoPriority(runtimeDir, input.Revision)
	candidates, preferred, _, err := autoCandidates(input.Proxies, []string{"changed", "new label", "duplicate"}, prior, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := candidateNames(candidates)
	if preferred != 1 || !slices.Equal(names, []string{"new label", "changed"}) {
		t.Fatalf("candidate order = %v, prioritized = %d", names, preferred)
	}
}

func TestPreviousAutoPriorityPrefersSameRevisionOrLatestProvider(t *testing.T) {
	runtimeDir := t.TempDir()
	first := writeAutoProvider(t, runtimeDir, map[string]any{
		"name": "first", "type": "ss", "server": "first.example",
	})
	second := writeAutoProvider(t, runtimeDir, map[string]any{
		"name": "second", "type": "ss", "server": "second.example",
	})
	pool, _ := poolcontract.Lookup(poolcontract.AutoCapability)
	firstPath := filepath.Join(runtimeDir, pool.Path(first.Revision))
	earlier := time.Now().Add(-time.Hour)
	if err := os.Chtimes(firstPath, earlier, earlier); err != nil {
		t.Fatal(err)
	}
	proxies := append(slices.Clone(first.Proxies), second.Proxies...)
	eligible := []string{"first", "second"}
	for _, test := range []struct{ revision, wantFirst string }{
		{first.Revision, "first"},
		{"another-revision", "second"},
	} {
		prior := previousAutoPriority(runtimeDir, test.revision)
		candidates, preferred, _, err := autoCandidates(proxies, eligible, prior, nil)
		if err != nil {
			t.Fatal(err)
		}
		names := candidateNames(candidates)
		if preferred != 1 || names[0] != test.wantFirst {
			t.Fatalf("revision %s: candidate order = %v, prioritized = %d", test.revision, names, preferred)
		}
	}
}

func TestAutoCandidatesRankByLatencyAndConsecutiveFailures(t *testing.T) {
	names := []string{"unknown", "twice failed", "slower", "legacy", "once failed", "faster"}
	proxies := make([]map[string]any, 0, len(names))
	for _, name := range names {
		proxies = append(proxies, map[string]any{"name": name, "server": name + ".example", "type": "ss"})
	}
	unranked, _, _, err := autoCandidates(proxies, names, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	history := map[string]autoObservation{
		unranked[1].fingerprint: {ConsecutiveFailures: 2},
		unranked[2].fingerprint: {WorstDoHNS: int64(time.Second)},
		unranked[4].fingerprint: {WorstDoHNS: int64(time.Millisecond), ConsecutiveFailures: 1},
		unranked[5].fingerprint: {WorstDoHNS: int64(100 * time.Millisecond)},
	}
	previous := map[string]bool{unranked[3].fingerprint: true, unranked[4].fingerprint: true}
	ranked, prioritized, matches, err := autoCandidates(proxies, names, previous, history)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"faster", "slower", "legacy", "unknown", "once failed", "twice failed"}
	if got := candidateNames(ranked); !slices.Equal(got, want) || prioritized != 3 || matches != 4 {
		t.Fatalf("rank = %v, prioritized = %d, matches = %d, want %v, 3, 4", got, prioritized, matches, want)
	}
}

func TestAutoCandidatesKeepHistoryAfterRenameAndDropItAfterDialerChange(t *testing.T) {
	proxy := map[string]any{"name": "old label", "server": "edge.example", "dialer-proxy": "relay"}
	relay := map[string]any{"name": "relay", "server": "first-relay.example"}
	proxies := []map[string]any{proxy, relay}
	first, _, _, err := autoCandidates(proxies, []string{"old label"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	history := map[string]autoObservation{first[0].fingerprint: {WorstDoHNS: int64(time.Millisecond)}}
	proxy["name"] = "new label"
	_, prioritized, matches, err := autoCandidates(proxies, []string{"new label"}, nil, history)
	if err != nil || prioritized != 1 || matches != 1 {
		t.Fatalf("rename: prioritized = %d, matches = %d, err = %v", prioritized, matches, err)
	}
	relay["server"] = "second-relay.example"
	_, prioritized, matches, err = autoCandidates(proxies, []string{"new label"}, nil, history)
	if err != nil || prioritized != 0 || matches != 0 {
		t.Fatalf("changed dialer: prioritized = %d, matches = %d, err = %v", prioritized, matches, err)
	}
}

func candidateNames(candidates []autoCandidate) []string {
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, candidate.name)
	}
	return names
}

func writeAutoProvider(t *testing.T, runtimeDir string, proxy map[string]any) poolcontract.Input {
	t.Helper()
	input, err := poolcontract.NewInput([]map[string]any{proxy})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := poolcontract.Lookup(poolcontract.AutoCapability)
	path := filepath.Join(runtimeDir, pool.Path(input.Revision))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _, err := poolcontract.ProviderData(pool, input, []string{proxy["name"].(string)})
	if err != nil {
		t.Fatal(err)
	}
	if err := poolcontract.WriteFile(path, data); err != nil {
		t.Fatal(err)
	}
	return input
}
