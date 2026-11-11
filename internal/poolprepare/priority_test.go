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
	names, preferred, err := autoCandidates(input.Proxies, []string{"changed", "new label", "duplicate"}, prior)
	if err != nil {
		t.Fatal(err)
	}
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
		names, preferred, err := autoCandidates(proxies, eligible, prior)
		if err != nil {
			t.Fatal(err)
		}
		if preferred != 1 || names[0] != test.wantFirst {
			t.Fatalf("revision %s: candidate order = %v, prioritized = %d", test.revision, names, preferred)
		}
	}
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
