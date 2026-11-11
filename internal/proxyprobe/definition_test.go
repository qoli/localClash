package proxyprobe

import (
	"slices"
	"testing"
)

func TestRequiredDefinitionsKeepsDialerChainAndSourceOrder(t *testing.T) {
	proxies := []map[string]any{
		{"name": "exit", "dialer-proxy": "relay"},
		{"name": "unrelated"},
		{"name": "entry", "dialer-proxy": "DIRECT"},
		{"name": "relay", "dialer-proxy": "entry"},
	}
	required := RequiredDefinitions(proxies, []string{"exit"})
	names := make([]string, 0, len(required))
	for _, proxy := range required {
		names = append(names, proxyString(proxy, "name"))
	}
	if !slices.Equal(names, []string{"exit", "entry", "relay"}) {
		t.Fatalf("definitions = %v", names)
	}
	proxies[3]["dialer-proxy"] = "GLOBAL"
	if got := RequiredDefinitions(proxies, []string{"exit"}); len(got) != len(proxies) {
		t.Fatalf("GLOBAL requires all definitions, got %v", got)
	}
}
