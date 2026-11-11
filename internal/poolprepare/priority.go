package poolprepare

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"localclash/internal/poolcontract"
	"localclash/internal/proxyprobe"
)

type autoCandidate struct {
	name        string
	fingerprint string
	previous    bool
}

func autoCandidates(proxies []map[string]any, eligible []string, previous map[string]bool, history map[string]autoObservation) ([]autoCandidate, int, int, error) {
	selectable := make(map[string]bool, len(eligible))
	for _, name := range eligible {
		selectable[name] = true
	}
	definitions := make(map[string]map[string]any, len(proxies))
	for _, proxy := range proxies {
		name, _ := proxy["name"].(string)
		definitions[strings.TrimSpace(name)] = proxy
	}
	seen := make(map[string]bool, len(eligible))
	helperFingerprints := make(map[string]string)
	candidates := make([]autoCandidate, 0, len(eligible))
	prioritized, matches := 0, 0
	for _, proxy := range proxies {
		rawName, _ := proxy["name"].(string)
		name := strings.TrimSpace(rawName)
		if !selectable[name] {
			continue
		}
		definition, err := proxyprobe.Fingerprint(proxy)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("fingerprint automatic pool proxy %q: %w", name, err)
		}
		if seen[definition] {
			continue
		}
		seen[definition] = true
		fingerprint, err := autoConnectionFingerprint(definition, proxy, definitions, helperFingerprints)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("fingerprint dialer chain for automatic pool proxy %q: %w", name, err)
		}
		observation, observed := history[fingerprint]
		if observed {
			matches++
		}
		if observation.ConsecutiveFailures == 0 && (observation.WorstDoHNS > 0 || previous[definition]) {
			prioritized++
		}
		candidates = append(candidates, autoCandidate{name: name, fingerprint: fingerprint, previous: previous[definition]})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		oldA, oldB := history[a.fingerprint], history[b.fingerprint]
		tierA, tierB := autoPriority(a, oldA), autoPriority(b, oldB)
		if tierA != tierB {
			return tierA < tierB
		}
		if oldA.ConsecutiveFailures != oldB.ConsecutiveFailures {
			return oldA.ConsecutiveFailures < oldB.ConsecutiveFailures
		}
		if oldA.WorstDoHNS == 0 || oldB.WorstDoHNS == 0 {
			return oldA.WorstDoHNS > oldB.WorstDoHNS
		}
		return oldA.WorstDoHNS < oldB.WorstDoHNS
	})
	return candidates, prioritized, matches, nil
}

func autoPriority(candidate autoCandidate, observation autoObservation) int {
	switch {
	case observation.ConsecutiveFailures > 0:
		return 3
	case observation.WorstDoHNS > 0:
		return 0
	case candidate.previous:
		return 1
	default:
		return 2
	}
}

func autoConnectionFingerprint(leaf string, proxy map[string]any, definitions map[string]map[string]any, helpers map[string]string) (string, error) {
	dialer, _ := proxy["dialer-proxy"].(string)
	name := strings.TrimSpace(dialer)
	if definitions[name] == nil {
		return leaf, nil
	}
	// Definition fingerprints have fixed length. Including the helpers keeps
	// latency recorded for an old chain from ranking a changed chain.
	digest := sha256.New()
	_, _ = io.WriteString(digest, leaf)
	visited := make(map[string]bool)
	for !visited[name] {
		helper := definitions[name]
		if helper == nil {
			break
		}
		visited[name] = true
		fingerprint, found := helpers[name]
		if !found {
			var err error
			fingerprint, err = proxyprobe.Fingerprint(helper)
			if err != nil {
				return "", err
			}
			helpers[name] = fingerprint
		}
		_, _ = io.WriteString(digest, fingerprint)
		dialer, _ := helper["dialer-proxy"].(string)
		name = strings.TrimSpace(dialer)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// previousAutoPriority uses saved pool members as scheduling hints. Every
// candidate is still checked with its current definition and dialer chain.
func previousAutoPriority(runtimeDir, revision string) map[string]bool {
	if runtimeDir == "" {
		return nil
	}
	pool, _ := poolcontract.Lookup(poolcontract.AutoCapability)
	if current := providerFingerprints(filepath.Join(runtimeDir, pool.Path(revision))); len(current) > 0 {
		return current
	}

	entries, err := os.ReadDir(filepath.Join(runtimeDir, poolcontract.Directory))
	if err != nil {
		return nil
	}
	var previous map[string]bool
	var updated time.Time
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == revision {
			continue
		}
		path := filepath.Join(runtimeDir, pool.Path(entry.Name()))
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().After(updated) {
			continue
		}
		if fingerprints := providerFingerprints(path); len(fingerprints) > 0 {
			previous = fingerprints
			updated = info.ModTime()
		}
	}
	return previous
}

func providerFingerprints(path string) map[string]bool {
	proxies, err := poolcontract.ReadProviderProxies(path)
	if err != nil {
		return nil
	}
	fingerprints := make(map[string]bool, len(proxies))
	for _, proxy := range proxies {
		if proxy["name"] == poolcontract.RejectName {
			continue
		}
		if fingerprint, err := proxyprobe.Fingerprint(proxy); err == nil {
			fingerprints[fingerprint] = true
		}
	}
	return fingerprints
}
