package poolprepare

import (
	"os"
	"path/filepath"
	"time"

	"localclash/internal/poolcontract"
	"localclash/internal/proxyprobe"
)

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
