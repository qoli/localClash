package autoavailable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"localclash/internal/capability"
)

const (
	ProfileID                   = "network.connectivity.g204.v1"
	SnapshotVersion             = 1
	ConsecutiveFailureThreshold = 1
)

var (
	ErrQualificationCollapse = errors.New("automatic connectivity qualification collapsed")
	ErrNoQualifiedCandidates = errors.New("automatic connectivity qualification produced no candidates")
)

type Candidate struct {
	Name                string
	EndpointFingerprint string
	Aliases             []string
	PreflightError      string
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

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type NodeState struct {
	Name                string   `json:"name"`
	Aliases             []string `json:"aliases,omitempty"`
	Available           bool     `json:"available"`
	ObservedAvailable   bool     `json:"observed_available"`
	ConsecutiveFailures int      `json:"consecutive_failures"`
	Attempts            int      `json:"attempts"`
	HTTPStatus          int      `json:"http_status,omitempty"`
	Error               string   `json:"error,omitempty"`
	CheckedAt           string   `json:"checked_at"`
	LastAvailableAt     string   `json:"last_available_at,omitempty"`
	DurationMS          int64    `json:"duration_ms"`
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
	Resolver             Resolver
	Now                  func() time.Time
}

type candidateBuildStats struct {
	HelperExcluded int
	Deduplicated   int
}

func Rebuild(ctx context.Context, proxies []map[string]any, prober Prober, opts Options) (Result, error) {
	started := time.Now()
	if prober == nil {
		return Result{}, errors.New("automatic connectivity prober is required")
	}
	if strings.TrimSpace(opts.SnapshotPath) == "" {
		return Result{}, errors.New("automatic connectivity snapshot path is required")
	}
	if opts.Resolver == nil {
		opts.Resolver = net.DefaultResolver
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	candidates, buildStats, err := buildCandidates(ctx, proxies, opts.Resolver)
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
	previous, previousExists, err := readSnapshot(previousPath)
	if err != nil {
		return Result{}, err
	}
	probeCandidates := make([]Candidate, 0, len(candidates))
	observations := make([]Observation, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.PreflightError == "" {
			probeCandidates = append(probeCandidates, candidate)
			continue
		}
		observations = append(observations, Observation{
			EndpointFingerprint: candidate.EndpointFingerprint,
			Available:           false,
			Error:               candidate.PreflightError,
		})
	}
	if len(probeCandidates) > 0 {
		probed, err := prober.Probe(ctx, proxies, probeCandidates)
		if err != nil {
			return Result{}, fmt.Errorf("probe automatic connectivity: %w", err)
		}
		if len(probed) != len(probeCandidates) {
			return Result{}, fmt.Errorf("automatic connectivity probe returned %d observations for %d candidates", len(probed), len(probeCandidates))
		}
		observations = append(observations, probed...)
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
	previouslyQualified := 0
	for _, candidate := range candidates {
		observation, ok := observed[candidate.EndpointFingerprint]
		if !ok {
			return Result{}, fmt.Errorf("automatic connectivity probe omitted endpoint fingerprint %q", candidate.EndpointFingerprint)
		}
		previousState := previous.Nodes[candidate.EndpointFingerprint]
		if previousExists && previousState.Available {
			previouslyQualified++
		}
		state := NodeState{
			Name: candidate.Name, Aliases: append([]string{}, candidate.Aliases...), Available: observation.Available,
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
	if len(qualified) == 0 {
		if previouslyQualified > 0 {
			return Result{}, fmt.Errorf("%w: %d previously-qualified endpoints failed", ErrQualificationCollapse, previouslyQualified)
		}
		return Result{}, ErrNoQualifiedCandidates
	}
	snapshot.Qualified = append([]string{}, qualified...)
	if err := writeSnapshot(opts.SnapshotPath, snapshot); err != nil {
		return Result{}, err
	}
	return Result{
		Profile: ProfileID, SnapshotPath: opts.SnapshotPath, Candidates: len(proxies), Probed: len(probeCandidates),
		DeduplicatedCount: buildStats.Deduplicated, HelperExcludedCount: buildStats.HelperExcluded,
		Qualified: qualified, QualifiedCount: len(qualified),
		ObservedQualifiedCount: observedQualified, RetainedCount: retained, UnavailableCount: len(candidates) - len(qualified),
		DurationMS: time.Since(started).Milliseconds(),
	}, nil
}

func buildCandidates(ctx context.Context, proxies []map[string]any, resolver Resolver) ([]Candidate, candidateBuildStats, error) {
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
	representatives := map[string]int{}
	candidates := make([]Candidate, 0, len(proxies))
	resolvedHosts := map[string][]string{}
	for _, proxy := range proxies {
		name := strings.TrimSpace(stringValue(proxy["name"]))
		if referencedDialers[name] {
			stats.HelperExcluded++
			continue
		}
		endpoint, err := effectiveEndpoint(ctx, name, byName, resolver, resolvedHosts)
		if err != nil {
			var resolutionErr *endpointResolutionError
			if !errors.As(err, &resolutionErr) {
				return nil, stats, err
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, stats, ctxErr
			}
			endpoint = resolutionErr.Identity
		}
		sum := sha256.Sum256([]byte(endpoint))
		fingerprint := hex.EncodeToString(sum[:])
		if index, exists := representatives[fingerprint]; exists {
			candidates[index].Aliases = append(candidates[index].Aliases, name)
			stats.Deduplicated++
			continue
		}
		representatives[fingerprint] = len(candidates)
		candidate := Candidate{Name: name, EndpointFingerprint: fingerprint}
		if err != nil {
			candidate.PreflightError = err.Error()
		}
		candidates = append(candidates, candidate)
	}
	return candidates, stats, nil
}

type endpointResolutionError struct {
	Identity string
	Cause    error
}

func (e *endpointResolutionError) Error() string {
	return e.Cause.Error()
}

func (e *endpointResolutionError) Unwrap() error {
	return e.Cause
}

func effectiveEndpoint(ctx context.Context, name string, byName map[string]map[string]any, resolver Resolver, resolvedHosts map[string][]string) (string, error) {
	seen := map[string]bool{}
	current := name
	for {
		if seen[current] {
			return "", fmt.Errorf("automatic connectivity candidate %q has a dialer-proxy cycle at %q", name, current)
		}
		seen[current] = true
		proxy, ok := byName[current]
		if !ok {
			return "", fmt.Errorf("automatic connectivity candidate %q references unknown dialer-proxy %q", name, current)
		}
		if dialer := strings.TrimSpace(stringValue(proxy["dialer-proxy"])); dialer != "" {
			current = dialer
			continue
		}
		server := strings.TrimSpace(stringValue(proxy["server"]))
		if server == "" {
			return "", fmt.Errorf("automatic connectivity candidate %q first hop %q has no server", name, current)
		}
		port, err := intValue(proxy["port"])
		if err != nil || port < 1 || port > 65535 {
			return "", fmt.Errorf("automatic connectivity candidate %q first hop %q has invalid port", name, current)
		}
		addresses := []string{}
		if ip := net.ParseIP(server); ip != nil {
			addresses = append(addresses, ip.String())
		} else {
			cacheKey := strings.ToLower(server)
			identity := "unresolved:" + cacheKey + ":" + strconv.Itoa(port)
			addresses = append(addresses, resolvedHosts[cacheKey]...)
			if len(addresses) == 0 {
				resolved, err := resolver.LookupIPAddr(ctx, server)
				if err != nil {
					return "", &endpointResolutionError{Identity: identity, Cause: fmt.Errorf("resolve automatic connectivity candidate %q first hop %q: %w", name, server, err)}
				}
				seenAddresses := map[string]bool{}
				for _, address := range resolved {
					if address.IP == nil {
						continue
					}
					text := address.IP.String()
					if !seenAddresses[text] {
						seenAddresses[text] = true
						addresses = append(addresses, text)
					}
				}
				if len(addresses) > 0 {
					resolvedHosts[cacheKey] = append([]string{}, addresses...)
				}
			}
			if len(addresses) == 0 {
				return "", &endpointResolutionError{Identity: identity, Cause: fmt.Errorf("automatic connectivity candidate %q first hop %q resolved no IP addresses", name, server)}
			}
		}
		sort.Strings(addresses)
		return strings.Join(addresses, ",") + ":" + strconv.Itoa(port), nil
	}
}

func intValue(value any) (int, error) {
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case uint64:
		return int(typed), nil
	case float64:
		if typed != float64(int(typed)) {
			return 0, errors.New("not an integer")
		}
		return int(typed), nil
	default:
		return 0, errors.New("not numeric")
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
	if snapshot.Version != SnapshotVersion {
		return Snapshot{}, false, fmt.Errorf("automatic connectivity snapshot version must be %d, got %d", SnapshotVersion, snapshot.Version)
	}
	if snapshot.Profile != ProfileID {
		return Snapshot{}, false, fmt.Errorf("automatic connectivity snapshot profile must be %q, got %q", ProfileID, snapshot.Profile)
	}
	if snapshot.Nodes == nil || snapshot.Qualified == nil {
		return Snapshot{}, false, errors.New("automatic connectivity snapshot nodes and qualified nodes are required")
	}
	if len(snapshot.Qualified) == 0 {
		return Snapshot{}, false, errors.New("automatic connectivity snapshot requires at least one qualified node")
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
