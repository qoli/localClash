package autoavailable

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeResolver map[string][]string

func (r fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	values, ok := r[host]
	if !ok {
		return nil, errors.New("missing resolver fixture")
	}
	out := make([]net.IPAddr, 0, len(values))
	for _, value := range values {
		out = append(out, net.IPAddr{IP: net.ParseIP(value)})
	}
	return out, nil
}

type fakeProber struct {
	available map[string]bool
}

func (p fakeProber) Probe(_ context.Context, _ []map[string]any, candidates []Candidate) ([]Observation, error) {
	out := make([]Observation, 0, len(candidates))
	for _, candidate := range candidates {
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

func TestBuildCandidatesDeduplicatesResolvedEndpointFirstWins(t *testing.T) {
	proxies := []map[string]any{
		{"name": "[01] US04", "server": "first.example", "port": 22320},
		{"name": "[02] US04", "server": "second.example", "port": 22320},
		{"name": "JP01", "server": "jp.example", "port": 22317},
	}
	candidates, stats, err := buildCandidates(context.Background(), proxies, fakeResolver{
		"first.example": {"192.0.2.10"}, "second.example": {"192.0.2.10"}, "jp.example": {"192.0.2.20"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || stats.Deduplicated != 1 || stats.HelperExcluded != 0 || candidates[0].Name != "[01] US04" || !reflect.DeepEqual(candidates[0].Aliases, []string{"[02] US04"}) || candidates[1].Name != "JP01" {
		t.Fatalf("candidates = %+v, want first US endpoint and distinct JP endpoint", candidates)
	}
}

func TestBuildCandidatesExcludesDialerHelperButKeepsDefinitionForProbe(t *testing.T) {
	proxies := []map[string]any{
		{"name": "outer", "server": "entry.example", "port": 443},
		{"name": "HK exit", "server": "inner.example", "port": 8443, "dialer-proxy": "outer"},
	}
	candidates, stats, err := buildCandidates(context.Background(), proxies, fakeResolver{"entry.example": {"192.0.2.30"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || stats.HelperExcluded != 1 || stats.Deduplicated != 0 || candidates[0].Name != "HK exit" {
		t.Fatalf("candidates = %+v, want selectable exit instead of referenced helper", candidates)
	}
}

func TestRebuildPublishesOnlyEndpointRepresentativesThatPassG204(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-available.json")
	proxies := []map[string]any{
		{"name": "[01] US04", "server": "first.example", "port": 22320},
		{"name": "[02] US04", "server": "second.example", "port": 22320},
		{"name": "HK01", "server": "hk.example", "port": 22311},
	}
	result, err := Rebuild(context.Background(), proxies, fakeProber{available: map[string]bool{"[01] US04": true}}, Options{
		SnapshotPath: path,
		Resolver:     fakeResolver{"first.example": {"192.0.2.10"}, "second.example": {"192.0.2.10"}, "hk.example": {"192.0.2.20"}},
		Now:          func() time.Time { return time.Unix(100, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Qualified, []string{"[01] US04"}) || result.Candidates != 3 || result.Probed != 2 || result.DeduplicatedCount != 1 {
		t.Fatalf("result = %+v", result)
	}
	qualified, err := LoadQualified(path)
	if err != nil || !reflect.DeepEqual(qualified, result.Qualified) {
		t.Fatalf("qualified = %v err=%v", qualified, err)
	}
}

func TestRebuildFailsExplicitlyWhenNoCandidatePasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-available.json")
	_, err := Rebuild(context.Background(), []map[string]any{{"name": "US01", "server": "192.0.2.10", "port": 443}}, fakeProber{}, Options{SnapshotPath: path})
	if !errors.Is(err, ErrNoQualifiedCandidates) {
		t.Fatalf("error = %v, want ErrNoQualifiedCandidates", err)
	}
	if _, loadErr := LoadQualified(path); loadErr == nil {
		t.Fatal("failed qualification unexpectedly published a snapshot")
	}
}

func TestLoadQualifiedRejectsExplicitEmptySnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-available.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"profile":"network.connectivity.g204.v1","updated_at":"now","qualified":[],"nodes":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQualified(path); err == nil {
		t.Fatal("empty automatic connectivity snapshot was accepted")
	}
}

func TestRebuildRemovesPreviouslyAvailableEndpointAfterOneFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-available.json")
	proxies := []map[string]any{{"name": "US01", "server": "192.0.2.10", "port": 443}}
	if _, err := Rebuild(context.Background(), proxies, fakeProber{available: map[string]bool{"US01": true}}, Options{SnapshotPath: path}); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(context.Background(), proxies, fakeProber{}, Options{SnapshotPath: path}); !errors.Is(err, ErrQualificationCollapse) {
		t.Fatalf("first failure error = %v, want collapse", err)
	}
}
