package chatgptavailable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ProfileID                   = "openai.chatgpt.mobile.v1"
	SnapshotVersion             = 3
	ConsecutiveFailureThreshold = 3
)

var ErrQualificationCollapse = errors.New("ChatGPT capability qualification collapsed")

type Candidate struct {
	Name        string
	Fingerprint string
	Proxy       map[string]any
	Definitions []map[string]any
}

type Observation struct {
	Fingerprint              string        `json:"fingerprint"`
	Available                bool          `json:"available"`
	EligibilityRejected      bool          `json:"eligibility_rejected,omitempty"`
	Attempts                 int           `json:"attempts"`
	Duration                 time.Duration `json:"-"`
	IOSEligibility           string        `json:"ios_ip_eligibility,omitempty"`
	IOSEligibilityStatus     int           `json:"ios_ip_eligibility_http_status,omitempty"`
	AndroidEligibility       string        `json:"android_ip_eligibility,omitempty"`
	AndroidEligibilityStatus int           `json:"android_ip_eligibility_http_status,omitempty"`
	Error                    string        `json:"error,omitempty"`
}

type Prober interface {
	Probe(context.Context, []Candidate) ([]Observation, error)
}

type NodeState struct {
	Name                     string `json:"name"`
	Available                bool   `json:"available"`
	ObservedAvailable        bool   `json:"observed_available"`
	EligibilityRejected      bool   `json:"eligibility_rejected,omitempty"`
	ConsecutiveFailures      int    `json:"consecutive_failures"`
	Attempts                 int    `json:"attempts"`
	IOSEligibility           string `json:"ios_ip_eligibility,omitempty"`
	IOSEligibilityStatus     int    `json:"ios_ip_eligibility_http_status,omitempty"`
	AndroidEligibility       string `json:"android_ip_eligibility,omitempty"`
	AndroidEligibilityStatus int    `json:"android_ip_eligibility_http_status,omitempty"`
	Error                    string `json:"error,omitempty"`
	CheckedAt                string `json:"checked_at"`
	LastAvailableAt          string `json:"last_available_at,omitempty"`
	DurationMS               int64  `json:"duration_ms"`
}

type Snapshot struct {
	Version   int                  `json:"version"`
	Profile   string               `json:"profile"`
	UpdatedAt string               `json:"updated_at"`
	Nodes     map[string]NodeState `json:"nodes"`
}

type Result struct {
	Profile                string   `json:"profile"`
	SnapshotPath           string   `json:"snapshot_path"`
	Candidates             int      `json:"candidates"`
	Probed                 int      `json:"probed"`
	Qualified              []string `json:"qualified"`
	QualifiedCount         int      `json:"qualified_count"`
	ObservedQualifiedCount int      `json:"observed_qualified_count"`
	RetainedCount          int      `json:"retained_count"`
	UnavailableCount       int      `json:"unavailable_count"`
	DurationMS             int64    `json:"duration_ms"`
}

type Options struct {
	SnapshotPath string
	Now          func() time.Time
}

func Rebuild(ctx context.Context, proxies []map[string]any, prober Prober, opts Options) (Result, error) {
	started := time.Now()
	if prober == nil {
		return Result{}, errors.New("ChatGPT capability prober is required")
	}
	if strings.TrimSpace(opts.SnapshotPath) == "" {
		return Result{}, errors.New("ChatGPT capability snapshot path is required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	candidates, byFingerprint, err := buildCandidates(proxies)
	if err != nil {
		return Result{}, err
	}
	if len(candidates) == 0 {
		return Result{}, errors.New("ChatGPT capability qualification requires at least one subscription proxy")
	}

	previous, previousExists, err := readSnapshot(opts.SnapshotPath)
	if err != nil {
		return Result{}, err
	}
	observations, err := prober.Probe(ctx, candidates)
	if err != nil {
		return Result{}, fmt.Errorf("probe ChatGPT capability: %w", err)
	}
	if len(observations) != len(candidates) {
		return Result{}, fmt.Errorf("probe ChatGPT capability returned %d observations for %d candidates", len(observations), len(candidates))
	}

	observed := make(map[string]Observation, len(observations))
	for _, observation := range observations {
		if _, ok := byFingerprint[observation.Fingerprint]; !ok {
			return Result{}, fmt.Errorf("probe ChatGPT capability returned unknown fingerprint %q", observation.Fingerprint)
		}
		if _, duplicate := observed[observation.Fingerprint]; duplicate {
			return Result{}, fmt.Errorf("probe ChatGPT capability returned duplicate fingerprint %q", observation.Fingerprint)
		}
		observed[observation.Fingerprint] = observation
	}

	now := opts.Now().UTC()
	snapshot := Snapshot{
		Version:   SnapshotVersion,
		Profile:   ProfileID,
		UpdatedAt: now.Format(time.RFC3339Nano),
		Nodes:     make(map[string]NodeState, len(candidates)),
	}
	qualifiedSet := map[string]bool{}
	observedQualifiedSet := map[string]bool{}
	retainedSet := map[string]bool{}
	retainedPreviouslyQualified := 0
	for _, candidate := range candidates {
		observation, ok := observed[candidate.Fingerprint]
		if !ok {
			return Result{}, fmt.Errorf("probe ChatGPT capability omitted fingerprint %q", candidate.Fingerprint)
		}
		previousState := previous.Nodes[candidate.Fingerprint]
		if previousExists && previousState.Available {
			retainedPreviouslyQualified++
		}
		state := NodeState{
			Name:                     candidate.Name,
			Available:                observation.Available,
			ObservedAvailable:        observation.Available,
			EligibilityRejected:      observation.EligibilityRejected,
			Attempts:                 observation.Attempts,
			IOSEligibility:           observation.IOSEligibility,
			IOSEligibilityStatus:     observation.IOSEligibilityStatus,
			AndroidEligibility:       observation.AndroidEligibility,
			AndroidEligibilityStatus: observation.AndroidEligibilityStatus,
			Error:                    observation.Error,
			CheckedAt:                now.Format(time.RFC3339Nano),
			DurationMS:               observation.Duration.Milliseconds(),
		}
		if observation.Available {
			state.LastAvailableAt = now.Format(time.RFC3339Nano)
			for _, name := range byFingerprint[candidate.Fingerprint] {
				observedQualifiedSet[name] = true
			}
		} else {
			state.ConsecutiveFailures = previousState.ConsecutiveFailures + 1
			state.LastAvailableAt = previousState.LastAvailableAt
			if state.LastAvailableAt == "" && previousState.Available {
				state.LastAvailableAt = previousState.CheckedAt
			}
			if !observation.EligibilityRejected && previousState.Available && state.ConsecutiveFailures < ConsecutiveFailureThreshold {
				state.Available = true
				for _, name := range byFingerprint[candidate.Fingerprint] {
					retainedSet[name] = true
				}
			}
		}
		snapshot.Nodes[candidate.Fingerprint] = state
		if state.Available {
			for _, name := range byFingerprint[candidate.Fingerprint] {
				qualifiedSet[name] = true
			}
		}
	}

	qualified := make([]string, 0, len(qualifiedSet))
	for _, proxy := range proxies {
		name := strings.TrimSpace(stringValue(proxy["name"]))
		if qualifiedSet[name] {
			qualified = append(qualified, name)
		}
	}
	if len(qualified) == 0 && retainedPreviouslyQualified > 0 {
		return Result{}, fmt.Errorf("%w: %d retained previously-qualified candidates all failed; snapshot was not replaced", ErrQualificationCollapse, retainedPreviouslyQualified)
	}
	if err := writeSnapshot(opts.SnapshotPath, snapshot); err != nil {
		return Result{}, err
	}

	return Result{
		Profile:                ProfileID,
		SnapshotPath:           opts.SnapshotPath,
		Candidates:             len(proxies),
		Probed:                 len(candidates),
		Qualified:              qualified,
		QualifiedCount:         len(qualified),
		ObservedQualifiedCount: len(observedQualifiedSet),
		RetainedCount:          len(retainedSet),
		UnavailableCount:       len(proxies) - len(qualified),
		DurationMS:             time.Since(started).Milliseconds(),
	}, nil
}

func buildCandidates(proxies []map[string]any) ([]Candidate, map[string][]string, error) {
	byFingerprint := map[string][]string{}
	representatives := map[string]Candidate{}
	order := make([]string, 0, len(proxies))
	for i, proxy := range proxies {
		name := strings.TrimSpace(stringValue(proxy["name"]))
		if name == "" {
			return nil, nil, fmt.Errorf("ChatGPT capability candidate %d has no name", i)
		}
		fingerprint, err := proxyFingerprint(proxy)
		if err != nil {
			return nil, nil, fmt.Errorf("fingerprint ChatGPT capability candidate %q: %w", name, err)
		}
		byFingerprint[fingerprint] = append(byFingerprint[fingerprint], name)
		if candidate, exists := representatives[fingerprint]; exists {
			candidate.Definitions = append(candidate.Definitions, cloneMap(proxy))
			representatives[fingerprint] = candidate
			continue
		}
		definition := cloneMap(proxy)
		representatives[fingerprint] = Candidate{
			Name:        name,
			Fingerprint: fingerprint,
			Proxy:       definition,
			Definitions: []map[string]any{definition},
		}
		order = append(order, fingerprint)
	}
	candidates := make([]Candidate, 0, len(order))
	for _, fingerprint := range order {
		candidates = append(candidates, representatives[fingerprint])
	}
	return candidates, byFingerprint, nil
}

func proxyFingerprint(proxy map[string]any) (string, error) {
	copy := cloneMap(proxy)
	delete(copy, "name")
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func readSnapshot(path string) (Snapshot, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, false, nil
		}
		return Snapshot{}, false, fmt.Errorf("read ChatGPT capability snapshot: %w", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, false, fmt.Errorf("decode ChatGPT capability snapshot: %w", err)
	}
	if snapshot.Profile == ProfileID && snapshot.Version > 0 && snapshot.Version < SnapshotVersion {
		return Snapshot{}, false, nil
	}
	if snapshot.Version != SnapshotVersion {
		return Snapshot{}, false, fmt.Errorf("ChatGPT capability snapshot version must be %d, got %d", SnapshotVersion, snapshot.Version)
	}
	if snapshot.Profile != ProfileID {
		return Snapshot{}, false, fmt.Errorf("ChatGPT capability snapshot profile must be %q, got %q", ProfileID, snapshot.Profile)
	}
	if snapshot.Nodes == nil {
		return Snapshot{}, false, errors.New("ChatGPT capability snapshot nodes are required")
	}
	return snapshot, true, nil
}

func writeSnapshot(path string, snapshot Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create ChatGPT capability snapshot directory: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode ChatGPT capability snapshot: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".chatgpt-capability-*.tmp")
	if err != nil {
		return fmt.Errorf("create ChatGPT capability snapshot temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure ChatGPT capability snapshot temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write ChatGPT capability snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close ChatGPT capability snapshot: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish ChatGPT capability snapshot: %w", err)
	}
	return nil
}

func cloneMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func QualifiedByProfile(result Result) map[string][]string {
	return map[string][]string{result.Profile: append([]string{}, result.Qualified...)}
}

func Profiles(configured []string) []string {
	seen := map[string]bool{}
	for _, profile := range configured {
		profile = strings.TrimSpace(profile)
		if profile != "" {
			seen[profile] = true
		}
	}
	out := make([]string, 0, len(seen))
	for profile := range seen {
		out = append(out, profile)
	}
	sort.Strings(out)
	return out
}
