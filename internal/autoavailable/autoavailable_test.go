package autoavailable

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeProber struct {
	available map[string]bool
	seen      *[]string
}

func (p fakeProber) Probe(_ context.Context, _ []map[string]any, candidates []Candidate) ([]Observation, error) {
	out := make([]Observation, 0, len(candidates))
	for _, candidate := range candidates {
		if p.seen != nil {
			*p.seen = append(*p.seen, candidate.Name)
		}
		available := p.available[candidate.Name]
		observation := Observation{EndpointFingerprint: candidate.EndpointFingerprint, Available: available, Attempts: 1, HTTPStatus: 204, Duration: time.Millisecond}
		if !available {
			observation.HTTPStatus = 0
			observation.Error = "unavailable"
		}
		out = append(out, observation)
	}
	return out, nil
}

func TestBuildCandidatesKeepsEveryProxyDefinition(t *testing.T) {
	proxies := []map[string]any{
		{"name": "[01] US04", "type": "ss", "server": "same.example", "port": 22320, "password": "first"},
		{"name": "[02] US04", "type": "ss", "server": "same.example", "port": 22320, "password": "second"},
		{"name": "[03] US04", "type": "trojan", "server": "same.example", "port": 22320, "password": "third"},
	}
	candidates, stats, err := buildCandidates(proxies)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 || stats.HelperExcluded != 0 {
		t.Fatalf("candidates = %+v stats=%+v, want every proxy definition", candidates, stats)
	}
	if candidates[0].Name != "[01] US04" || candidates[1].Name != "[02] US04" || candidates[2].Name != "[03] US04" {
		t.Fatalf("candidate order = %+v, want subscription order", candidates)
	}
	if candidates[0].EndpointFingerprint == candidates[1].EndpointFingerprint || candidates[1].EndpointFingerprint == candidates[2].EndpointFingerprint {
		t.Fatalf("fingerprints = %+v, want protocol and credentials to remain distinct", candidates)
	}
}

func TestBuildCandidatesExcludesDialerHelperButKeepsDefinitionForProbe(t *testing.T) {
	proxies := []map[string]any{
		{"name": "outer", "server": "entry.example", "port": 443},
		{"name": "HK exit", "server": "inner.example", "port": 8443, "dialer-proxy": "outer"},
	}
	candidates, stats, err := buildCandidates(proxies)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || stats.HelperExcluded != 1 || candidates[0].Name != "HK exit" {
		t.Fatalf("candidates = %+v, want selectable exit instead of referenced helper", candidates)
	}
}

func TestRebuildProbesAndPublishesEveryPassingProxyDefinition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-available.json")
	proxies := []map[string]any{
		{"name": "[01] US04", "server": "first.example", "port": 22320},
		{"name": "[02] US04", "server": "second.example", "port": 22320},
		{"name": "HK01", "server": "hk.example", "port": 22311},
	}
	seen := []string{}
	result, err := Rebuild(context.Background(), proxies, fakeProber{available: map[string]bool{"[01] US04": true, "[02] US04": true}, seen: &seen}, Options{
		SnapshotPath: path,
		Now:          func() time.Time { return time.Unix(100, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, []string{"[01] US04", "[02] US04", "HK01"}) {
		t.Fatalf("probed candidates = %v, want every selectable definition", seen)
	}
	if !reflect.DeepEqual(result.Qualified, []string{"[01] US04", "[02] US04"}) || result.Candidates != 3 || result.Probed != 3 || result.DeduplicatedCount != 0 {
		t.Fatalf("result = %+v", result)
	}
	qualified, err := LoadQualified(path)
	if err != nil || !reflect.DeepEqual(qualified, result.Qualified) {
		t.Fatalf("qualified = %v err=%v", qualified, err)
	}
}

func TestRebuildDelegatesHostnameResolutionToMihomoProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-available.json")
	proxies := []map[string]any{
		{"name": "broken", "server": "missing.example", "port": 443},
		{"name": "healthy", "server": "healthy.example", "port": 443},
	}
	seen := []string{}
	result, err := Rebuild(context.Background(), proxies, fakeProber{available: map[string]bool{"healthy": true}, seen: &seen}, Options{
		SnapshotPath: path,
		Now:          func() time.Time { return time.Unix(100, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, []string{"broken", "healthy"}) {
		t.Fatalf("probed candidates = %v, want Mihomo to evaluate every candidate", seen)
	}
	if result.Probed != 2 || result.UnavailableCount != 1 || !reflect.DeepEqual(result.Qualified, []string{"healthy"}) {
		t.Fatalf("result = %+v", result)
	}
}

func TestRebuildPublishesEmptyResultWhenNoCandidatePasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-available.json")
	result, err := Rebuild(context.Background(), []map[string]any{{"name": "US01", "server": "192.0.2.10", "port": 443}}, fakeProber{}, Options{SnapshotPath: path})
	if err != nil || result.QualifiedCount != 0 || result.UnavailableCount != 1 {
		t.Fatalf("result = %+v error = %v, want explicit empty capability", result, err)
	}
	if qualified, loadErr := LoadQualified(path); loadErr != nil || len(qualified) != 0 {
		t.Fatalf("qualified = %v error = %v, want published empty capability", qualified, loadErr)
	}
}

func TestLoadQualifiedAcceptsExplicitEmptySnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-available.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"profile":"network.connectivity.g204.v1","updated_at":"now","qualified":[],"nodes":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if qualified, err := LoadQualified(path); err != nil || len(qualified) != 0 {
		t.Fatalf("qualified = %v error = %v, want explicit empty capability", qualified, err)
	}
}

func TestLoadQualifiedRejectsLegacyDeduplicatedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-available.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"profile":"network.connectivity.g204.v1","updated_at":"old","qualified":["representative"],"nodes":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if qualified, err := LoadQualified(path); err == nil || qualified != nil {
		t.Fatalf("qualified=%v error=%v, want legacy deduplicated snapshot rejected", qualified, err)
	}
}

func TestRebuildRemovesPreviouslyAvailableEndpointAfterOneFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-available.json")
	proxies := []map[string]any{{"name": "US01", "server": "192.0.2.10", "port": 443}}
	if _, err := Rebuild(context.Background(), proxies, fakeProber{available: map[string]bool{"US01": true}}, Options{SnapshotPath: path}); err != nil {
		t.Fatal(err)
	}
	result, err := Rebuild(context.Background(), proxies, fakeProber{}, Options{SnapshotPath: path})
	if err != nil || result.QualifiedCount != 0 || result.ObservedQualifiedCount != 0 || result.UnavailableCount != 1 {
		t.Fatalf("result = %+v error = %v, want explicit empty capability after first failure", result, err)
	}
}
