package autoavailable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"localclash/internal/capability"
)

const (
	ProfileID                   = "network.connectivity.g204.v1"
	SnapshotVersion             = 2
	ConsecutiveFailureThreshold = 1
)

type Candidate struct {
	Name                string
	EndpointFingerprint string
}

type Observation struct {
	EndpointFingerprint string        `json:"endpoint_fingerprint"`
	Available           bool          `json:"available"`
	Attempts            int           `json:"attempts"`
	Duration            time.Duration `json:"-"`
	HTTPStatus          int           `json:"http_status,omitempty"`
	Error               string        `json:"error,omitempty"`
}

type Prober interface {
	Probe(context.Context, []map[string]any, []Candidate) ([]Observation, error)
}

type NodeState struct {
	Name                string `json:"name"`
	Available           bool   `json:"available"`
	ObservedAvailable   bool   `json:"observed_available"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	Attempts            int    `json:"attempts"`
	HTTPStatus          int    `json:"http_status,omitempty"`
	Error               string `json:"error,omitempty"`
	CheckedAt           string `json:"checked_at"`
	LastAvailableAt     string `json:"last_available_at,omitempty"`
	DurationMS          int64  `json:"duration_ms"`
}

type Snapshot struct {
	Version   int                  `json:"version"`
	Profile   string               `json:"profile"`
	UpdatedAt string               `json:"updated_at"`
	Qualified []string             `json:"qualified"`
	Nodes     map[string]NodeState `json:"nodes"`
}

type Result = capability.Result

type Options struct {
	SnapshotPath         string
	PreviousSnapshotPath string
	Now                  func() time.Time
}

type candidateBuildStats struct {
	HelperExcluded int
}

func Rebuild(ctx context.Context, proxies []map[string]any, prober Prober, opts Options) (Result, error) {
	started := time.Now()
	if prober == nil {
		return Result{}, errors.New("automatic connectivity prober is required")
	}
	if strings.TrimSpace(opts.SnapshotPath) == "" {
		return Result{}, errors.New("automatic connectivity snapshot path is required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	candidates, buildStats, err := buildCandidates(proxies)
	if err != nil {
		return Result{}, err
	}
	if len(candidates) == 0 {
		return Result{}, errors.New("automatic connectivity qualification requires at least one subscription proxy")
	}
	previousPath := strings.TrimSpace(opts.PreviousSnapshotPath)
	if previousPath == "" {
		previousPath = opts.SnapshotPath
	}
	previous, _, err := readSnapshot(previousPath)
	if err != nil {
		return Result{}, err
	}
	observations, err := prober.Probe(ctx, proxies, candidates)
	if err != nil {
		return Result{}, fmt.Errorf("probe automatic connectivity: %w", err)
	}
	if len(observations) != len(candidates) {
		return Result{}, fmt.Errorf("automatic connectivity probe returned %d observations for %d candidates", len(observations), len(candidates))
	}
	observed := make(map[string]Observation, len(observations))
	known := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		known[candidate.EndpointFingerprint] = true
	}
	for _, observation := range observations {
		if !known[observation.EndpointFingerprint] {
			return Result{}, fmt.Errorf("automatic connectivity probe returned unknown endpoint fingerprint %q", observation.EndpointFingerprint)
		}
		if _, duplicate := observed[observation.EndpointFingerprint]; duplicate {
			return Result{}, fmt.Errorf("automatic connectivity probe returned duplicate endpoint fingerprint %q", observation.EndpointFingerprint)
		}
		observed[observation.EndpointFingerprint] = observation
	}

	now := opts.Now().UTC()
	snapshot := Snapshot{Version: SnapshotVersion, Profile: ProfileID, UpdatedAt: now.Format(time.RFC3339Nano), Nodes: make(map[string]NodeState, len(candidates))}
	qualified := make([]string, 0, len(candidates))
	observedQualified := 0
	retained := 0
	for _, candidate := range candidates {
		observation, ok := observed[candidate.EndpointFingerprint]
		if !ok {
			return Result{}, fmt.Errorf("automatic connectivity probe omitted endpoint fingerprint %q", candidate.EndpointFingerprint)
		}
		previousState := previous.Nodes[candidate.EndpointFingerprint]
		state := NodeState{
			Name: candidate.Name, Available: observation.Available,
			ObservedAvailable: observation.Available, Attempts: observation.Attempts, HTTPStatus: observation.HTTPStatus,
			Error: observation.Error, CheckedAt: now.Format(time.RFC3339Nano), DurationMS: observation.Duration.Milliseconds(),
		}
		if observation.Available {
			state.LastAvailableAt = now.Format(time.RFC3339Nano)
			observedQualified++
		} else {
			state.ConsecutiveFailures = previousState.ConsecutiveFailures + 1
			state.LastAvailableAt = previousState.LastAvailableAt
			if state.LastAvailableAt == "" && previousState.Available {
				state.LastAvailableAt = previousState.CheckedAt
			}
			if previousState.Available && state.ConsecutiveFailures < ConsecutiveFailureThreshold {
				state.Available = true
				retained++
			}
		}
		snapshot.Nodes[candidate.EndpointFingerprint] = state
		if state.Available {
			qualified = append(qualified, candidate.Name)
		}
	}
	snapshot.Qualified = append([]string{}, qualified...)
	if err := writeSnapshot(opts.SnapshotPath, snapshot); err != nil {
		return Result{}, err
	}
	return Result{
		Profile: ProfileID, SnapshotPath: opts.SnapshotPath, Candidates: len(proxies), Probed: len(candidates),
		HelperExcludedCount: buildStats.HelperExcluded,
		Qualified:           qualified, QualifiedCount: len(qualified),
		ObservedQualifiedCount: observedQualified, RetainedCount: retained, UnavailableCount: len(candidates) - len(qualified),
		DurationMS: time.Since(started).Milliseconds(),
	}, nil
}

func buildCandidates(proxies []map[string]any) ([]Candidate, candidateBuildStats, error) {
	byName := make(map[string]map[string]any, len(proxies))
	referencedDialers := map[string]bool{}
	stats := candidateBuildStats{}
	for index, proxy := range proxies {
		name := strings.TrimSpace(stringValue(proxy["name"]))
		if name == "" {
			return nil, stats, fmt.Errorf("automatic connectivity candidate %d has no name", index)
		}
		if _, duplicate := byName[name]; duplicate {
			return nil, stats, fmt.Errorf("automatic connectivity contains duplicate proxy name %q", name)
		}
		byName[name] = proxy
		if dialer := strings.TrimSpace(stringValue(proxy["dialer-proxy"])); dialer != "" {
			referencedDialers[dialer] = true
		}
	}
	candidates := make([]Candidate, 0, len(proxies))
	for _, proxy := range proxies {
		name := strings.TrimSpace(stringValue(proxy["name"]))
		if referencedDialers[name] {
			stats.HelperExcluded++
			continue
		}
		if err := validateDialerChain(name, byName); err != nil {
			return nil, stats, err
		}
		definition, err := json.Marshal(proxy)
		if err != nil {
			return nil, stats, fmt.Errorf("fingerprint automatic connectivity candidate %q: %w", name, err)
		}
		sum := sha256.Sum256(definition)
		fingerprint := hex.EncodeToString(sum[:])
		candidates = append(candidates, Candidate{Name: name, EndpointFingerprint: fingerprint})
	}
	return candidates, stats, nil
}

func validateDialerChain(name string, byName map[string]map[string]any) error {
	seen := map[string]bool{}
	current := name
	for {
		if seen[current] {
			return fmt.Errorf("automatic connectivity candidate %q has a dialer-proxy cycle at %q", name, current)
		}
		seen[current] = true
		proxy, ok := byName[current]
		if !ok {
			return fmt.Errorf("automatic connectivity candidate %q references unknown dialer-proxy %q", name, current)
		}
		if dialer := strings.TrimSpace(stringValue(proxy["dialer-proxy"])); dialer != "" {
			current = dialer
			continue
		}
		return nil
	}
}

func readSnapshot(path string) (Snapshot, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, false, nil
		}
		return Snapshot{}, false, fmt.Errorf("read automatic connectivity snapshot: %w", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, false, fmt.Errorf("decode automatic connectivity snapshot: %w", err)
	}
	if snapshot.Version > 0 && snapshot.Version < SnapshotVersion {
		return Snapshot{}, false, nil
	}
	if snapshot.Version != SnapshotVersion {
		return Snapshot{}, false, fmt.Errorf("automatic connectivity snapshot version must be %d, got %d", SnapshotVersion, snapshot.Version)
	}
	if snapshot.Profile != ProfileID {
		return Snapshot{}, false, fmt.Errorf("automatic connectivity snapshot profile must be %q, got %q", ProfileID, snapshot.Profile)
	}
	if snapshot.Nodes == nil || snapshot.Qualified == nil {
		return Snapshot{}, false, errors.New("automatic connectivity snapshot nodes and qualified nodes are required")
	}
	seen := map[string]bool{}
	for _, name := range snapshot.Qualified {
		name = strings.TrimSpace(name)
		if name == "" {
			return Snapshot{}, false, errors.New("automatic connectivity snapshot contains an empty qualified node")
		}
		if seen[name] {
			return Snapshot{}, false, fmt.Errorf("automatic connectivity snapshot contains duplicate qualified node %q", name)
		}
		seen[name] = true
	}
	return snapshot, true, nil
}

func LoadQualified(path string) ([]string, error) {
	snapshot, exists, err := readSnapshot(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("automatic connectivity snapshot %q is unavailable; run subscription refresh", path)
	}
	return append([]string{}, snapshot.Qualified...), nil
}

func writeSnapshot(path string, snapshot Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create automatic connectivity snapshot directory: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode automatic connectivity snapshot: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".auto-connectivity-*.tmp")
	if err != nil {
		return fmt.Errorf("create automatic connectivity snapshot temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure automatic connectivity snapshot temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write automatic connectivity snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close automatic connectivity snapshot: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish automatic connectivity snapshot: %w", err)
	}
	return nil
}

func QualifiedByProfile(result Result) map[string][]string {
	return capability.QualifiedByProfile(result)
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
