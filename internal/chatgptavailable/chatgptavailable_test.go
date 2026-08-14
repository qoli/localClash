package chatgptavailable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeProber struct {
	observations map[string]Observation
	err          error
}

func (p fakeProber) Probe(_ context.Context, candidates []Candidate) ([]Observation, error) {
	if p.err != nil {
		return nil, p.err
	}
	out := make([]Observation, 0, len(candidates))
	for _, candidate := range candidates {
		observation := p.observations[candidate.Name]
		observation.Fingerprint = candidate.Fingerprint
		out = append(out, observation)
	}
	return out, nil
}

func TestRebuildPublishesOnlyQualifiedNodes(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "chatgpt.json")
	proxies := []map[string]any{
		{"name": "US 01", "type": "ss", "server": "us.example.com", "password": "secret-us"},
		{"name": "JP 01", "type": "trojan", "server": "jp.example.com", "password": "secret-jp"},
	}
	result, err := Rebuild(context.Background(), proxies, fakeProber{observations: map[string]Observation{
		"US 01": {Available: true, Attempts: 1, IOSEligibility: eligibilityEligible, IOSEligibilityStatus: 403, AndroidEligibility: eligibilityEligible, AndroidEligibilityStatus: 403, Duration: 120 * time.Millisecond},
		"JP 01": {Available: false, Attempts: 3, IOSEligibility: eligibilityEligible, IOSEligibilityStatus: 403, AndroidEligibility: eligibilityUnexpectedResponse, AndroidEligibilityStatus: 403, Error: "unexpected Android response", Duration: 2 * time.Second},
	}}, Options{
		SnapshotPath: snapshotPath,
		Now:          func() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Qualified, []string{"US 01"}) {
		t.Fatalf("qualified = %+v, want US 01", result.Qualified)
	}
	if result.Candidates != 2 || result.Probed != 2 || result.UnavailableCount != 1 {
		t.Fatalf("result counts = %+v", result)
	}
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-us", "secret-jp", "us.example.com", "jp.example.com"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("snapshot leaked %q: %s", secret, data)
		}
	}
	if !strings.Contains(string(data), `"profile": "openai.chatgpt.mobile.v1"`) {
		t.Fatalf("snapshot missing profile: %s", data)
	}
	if !strings.Contains(string(data), `"version": 4`) || !strings.Contains(string(data), `"qualified": [`) || !strings.Contains(string(data), `"ios_ip_eligibility": "eligible"`) || !strings.Contains(string(data), `"android_ip_eligibility": "eligible"`) {
		t.Fatalf("snapshot missing v4 qualified nodes and dual-platform eligibility evidence: %s", data)
	}
	qualified, err := LoadQualified(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(qualified, []string{"US 01"}) {
		t.Fatalf("loaded qualified nodes = %+v, want US 01", qualified)
	}
}

func TestRebuildRejectsTotalCollapseAndPreservesSnapshot(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "chatgpt.json")
	proxies := []map[string]any{{"name": "US 01", "type": "ss", "server": "us.example.com", "password": "secret"}}
	available := fakeProber{observations: map[string]Observation{
		"US 01": {Available: true, Attempts: 1, IOSEligibility: eligibilityEligible, AndroidEligibility: eligibilityEligible},
	}}
	if _, err := Rebuild(context.Background(), proxies, available, Options{SnapshotPath: snapshotPath}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := fakeProber{observations: map[string]Observation{
		"US 01": {Available: false, Attempts: 3, Error: "timeout"},
	}}
	for attempt := 1; attempt < ConsecutiveFailureThreshold; attempt++ {
		result, rebuildErr := Rebuild(context.Background(), proxies, unavailable, Options{SnapshotPath: snapshotPath})
		if rebuildErr != nil {
			t.Fatalf("transient failure %d: %v", attempt, rebuildErr)
		}
		if result.QualifiedCount != 1 || result.RetainedCount != 1 || result.ObservedQualifiedCount != 0 {
			t.Fatalf("transient failure %d result = %+v, want retained qualification", attempt, result)
		}
	}
	before, err = os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Rebuild(context.Background(), proxies, unavailable, Options{SnapshotPath: snapshotPath})
	if !errors.Is(err, ErrQualificationCollapse) {
		t.Fatalf("error = %v, want qualification collapse", err)
	}
	after, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("collapse replaced snapshot\nbefore: %s\nafter: %s", before, after)
	}
}

func TestRebuildDoesNotAdmitNewUnavailableCandidate(t *testing.T) {
	snapshotPath := filepath.Join(t.TempDir(), "chatgpt.json")
	result, err := Rebuild(context.Background(), []map[string]any{{"name": "US 01", "type": "ss"}}, fakeProber{observations: map[string]Observation{
		"US 01": {Available: false, Attempts: 3, Error: "timeout"},
	}}, Options{SnapshotPath: snapshotPath})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualifiedCount != 0 || result.RetainedCount != 0 || result.ObservedQualifiedCount != 0 {
		t.Fatalf("result = %+v, want unavailable new candidate excluded", result)
	}
}

func TestRebuildEligibilityRejectionBypassesFailureHysteresis(t *testing.T) {
	snapshotPath := filepath.Join(t.TempDir(), "chatgpt.json")
	proxies := []map[string]any{{"name": "HK 01", "type": "vless", "server": "hk.example.com"}}
	available := fakeProber{observations: map[string]Observation{
		"HK 01": {
			Available: true, Attempts: 1, IOSEligibility: eligibilityEligible,
			IOSEligibilityStatus: 403, AndroidEligibility: eligibilityEligible, AndroidEligibilityStatus: 403,
		},
	}}
	if _, err := Rebuild(context.Background(), proxies, available, Options{SnapshotPath: snapshotPath}); err != nil {
		t.Fatal(err)
	}
	rejected := fakeProber{observations: map[string]Observation{
		"HK 01": {
			EligibilityRejected: true, Attempts: 1, IOSEligibility: eligibilityEligible,
			IOSEligibilityStatus: 403, AndroidEligibility: eligibilityDisallowedISP, AndroidEligibilityStatus: 403,
			Error: "OpenAI rejected the exit ISP",
		},
	}}
	_, err := Rebuild(context.Background(), proxies, rejected, Options{SnapshotPath: snapshotPath})
	if !errors.Is(err, ErrQualificationCollapse) {
		t.Fatalf("error = %v, want immediate collapse for explicit eligibility rejection", err)
	}
}

func TestRebuildNetworkFailureStillUsesFailureHysteresis(t *testing.T) {
	snapshotPath := filepath.Join(t.TempDir(), "chatgpt.json")
	proxies := []map[string]any{{"name": "US 01", "type": "vless", "server": "us.example.com"}}
	available := fakeProber{observations: map[string]Observation{
		"US 01": {Available: true, Attempts: 1, IOSEligibility: eligibilityEligible, IOSEligibilityStatus: 403, AndroidEligibility: eligibilityEligible, AndroidEligibilityStatus: 403},
	}}
	if _, err := Rebuild(context.Background(), proxies, available, Options{SnapshotPath: snapshotPath}); err != nil {
		t.Fatal(err)
	}
	networkFailure := fakeProber{observations: map[string]Observation{
		"US 01": {Attempts: 3, IOSEligibility: eligibilityTransportFailure, AndroidEligibility: eligibilityTransportFailure, Error: "connection reset by peer"},
	}}
	result, err := Rebuild(context.Background(), proxies, networkFailure, Options{SnapshotPath: snapshotPath})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualifiedCount != 1 || result.RetainedCount != 1 {
		t.Fatalf("result = %+v, want transient network failure retained", result)
	}
}

func TestRebuildDoesNotPublishWhenProbeInfrastructureFails(t *testing.T) {
	snapshotPath := filepath.Join(t.TempDir(), "chatgpt.json")
	_, err := Rebuild(context.Background(), []map[string]any{{"name": "US 01", "type": "ss"}}, fakeProber{err: errors.New("core failed")}, Options{SnapshotPath: snapshotPath})
	if err == nil || !strings.Contains(err.Error(), "core failed") {
		t.Fatalf("error = %v, want core failure", err)
	}
	if _, statErr := os.Stat(snapshotPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("snapshot exists after probe failure: %v", statErr)
	}
}

func TestBuildCandidatesDeduplicatesEquivalentProxyDefinitions(t *testing.T) {
	proxies := []map[string]any{
		{"name": "US 01", "type": "ss", "server": "same.example", "password": "secret"},
		{"name": "US copy", "type": "ss", "server": "same.example", "password": "secret"},
	}
	candidates, names, err := buildCandidates(proxies)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if got := names[candidates[0].Fingerprint]; !reflect.DeepEqual(got, []string{"US 01", "US copy"}) {
		t.Fatalf("deduplicated names = %+v", got)
	}
}

func TestReadSnapshotTreatsLegacyQualificationsAsAbsent(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "chatgpt.json")
			legacy := fmt.Sprintf(`{
  "version": %d,
  "profile": "openai.chatgpt.mobile.v1",
  "updated_at": "2026-08-14T00:00:00Z",
  "nodes": {
    "old": {"name":"HK 01","available":true,"observed_available":true,"attempts":1,"checked_at":"2026-08-14T00:00:00Z","duration_ms":1}
  }
}`, version)
			if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
				t.Fatal(err)
			}
			snapshot, exists, err := readSnapshot(path)
			if err != nil {
				t.Fatal(err)
			}
			if exists || snapshot.Nodes != nil {
				t.Fatalf("legacy snapshot = %+v, exists=%v; want absent migration baseline", snapshot, exists)
			}
		})
	}
}

func TestLoadQualifiedRequiresCurrentSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := LoadQualified(path)
	if err == nil || !strings.Contains(err.Error(), "run subscription refresh") {
		t.Fatalf("error = %v, want explicit missing snapshot instruction", err)
	}
}

func TestLoadQualifiedRejectsMalformedCurrentSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chatgpt.json")
	data := `{
  "version": 4,
  "profile": "openai.chatgpt.mobile.v1",
  "updated_at": "2026-08-15T00:00:00Z",
  "qualified": ["US 01", "US 01"],
  "nodes": {}
}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadQualified(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate qualified node") {
		t.Fatalf("error = %v, want malformed snapshot rejection", err)
	}
}
