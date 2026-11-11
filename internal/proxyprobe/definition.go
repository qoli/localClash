package proxyprobe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
)

// SelectableNames excludes proxies that only serve as dialer helpers.
func SelectableNames(proxies []map[string]any) ([]string, error) {
	byName := make(map[string]bool, len(proxies))
	referencedDialers := make(map[string]bool)
	for index, proxy := range proxies {
		name := strings.TrimSpace(proxyString(proxy, "name"))
		if name == "" {
			return nil, fmt.Errorf("subscription proxy %d has no name", index)
		}
		if byName[name] {
			return nil, fmt.Errorf("subscription contains duplicate proxy name %q", name)
		}
		byName[name] = true
		if dialer := strings.TrimSpace(proxyString(proxy, "dialer-proxy")); dialer != "" {
			referencedDialers[dialer] = true
		}
	}
	eligible := make([]string, 0, len(proxies))
	for _, proxy := range proxies {
		name := strings.TrimSpace(proxyString(proxy, "name"))
		if !referencedDialers[name] {
			eligible = append(eligible, name)
		}
	}
	return eligible, nil
}

// RequiredDefinitions keeps the requested exits and the dialer definitions
// needed by their chains, in subscription order.
func RequiredDefinitions(proxies []map[string]any, names []string) []map[string]any {
	byName := make(map[string]map[string]any, len(proxies))
	for _, proxy := range proxies {
		byName[strings.TrimSpace(proxyString(proxy, "name"))] = proxy
	}
	needed := make(map[string]bool, len(names))
	for _, candidate := range names {
		for name := strings.TrimSpace(candidate); name != "" && !needed[name]; {
			if name == "GLOBAL" {
				// Mihomo builds GLOBAL from the entire configuration.
				return proxies
			}
			proxy := byName[name]
			if proxy == nil {
				break
			}
			needed[name] = true
			name = strings.TrimSpace(proxyString(proxy, "dialer-proxy"))
		}
	}
	required := make([]map[string]any, 0, len(needed))
	for _, proxy := range proxies {
		if needed[strings.TrimSpace(proxyString(proxy, "name"))] {
			required = append(required, proxy)
		}
	}
	return required
}

// Fingerprint identifies equivalent proxy definitions with different names.
func Fingerprint(proxy map[string]any) (string, error) {
	copy := maps.Clone(proxy)
	delete(copy, "name")
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func proxyString(proxy map[string]any, key string) string {
	value, _ := proxy[key].(string)
	return value
}
