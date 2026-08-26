package resolverconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadMissingMeansOptionalConfigDisabled(t *testing.T) {
	status := ApplyOptional(filepath.Join(t.TempDir(), "missing.json"), validMihomo())
	if status.Enabled || status.State != "disabled" || status.Reason != "missing" {
		t.Fatalf("status = %+v", status)
	}
}

func TestLoadExpiredMeansOptionalConfigExplicitlyDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsqualify.json")
	config := validConfig(time.Now().Add(-time.Minute))
	writeConfig(t, path, config)
	status := ApplyOptional(path, validMihomo())
	if status.Enabled || status.State != "disabled" || status.Reason != "expired" || status.ExpiresAt != config.ExpiresAt {
		t.Fatalf("status = %+v", status)
	}
}

func TestApplyOptionalRejectsUnacceptableOverlayWithoutMutatingBaseline(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{
			name:   "unrecognized object version 1",
			config: `{"version":1,"scope":"geosite:cn","resolver":{},"measurement":{}}`,
		},
		{
			name:   "unrecognized object version 2",
			config: `{"version":2,"scope":{},"resolver":{},"ecs":{},"measurement":{}}`,
		},
		{
			name:   "unknown current field",
			config: `{"version":2,"unknown":true}`,
		},
		{
			name:   "mixed current and legacy fields",
			config: `{"version":2,"expires_at":"2099-01-01T00:00:00Z","nameserver_policy":{"example.com":["https://8.8.8.8/dns-query#DNSProxy"]},"scope":{}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dnsqualify.json")
			if err := os.WriteFile(path, []byte(test.config), 0o600); err != nil {
				t.Fatal(err)
			}
			mihomo := validMihomo()
			status := ApplyOptional(path, mihomo)
			if status.Enabled || status.State != "disabled" || status.Reason != "rejected" || status.Detail == "" {
				t.Fatalf("status = %+v", status)
			}
			policy := mihomo["dns"].(map[string]any)["nameserver-policy"].(map[string]any)
			if len(policy) != 1 {
				t.Fatalf("rejected overlay mutated baseline: %#v", policy)
			}
		})
	}
}

func TestValidateOnlyConsumerContract(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		edit func(*config)
		want string
	}{
		{name: "version", edit: func(config *config) { config.Version = 1 }, want: "version 1"},
		{name: "empty policy", edit: func(config *config) { config.NameserverPolicy = nil }, want: "nameserver_policy"},
		{name: "invalid expiry", edit: func(config *config) { config.ExpiresAt = "tomorrow" }, want: "invalid expires_at"},
		{name: "expired", edit: func(config *config) { config.ExpiresAt = now.Add(-time.Second).Format(time.RFC3339Nano) }, want: "expired"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig(now.Add(time.Minute))
			test.edit(&config)
			if err := validateAt(config, now); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateAt error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadAndApplyNameserverPolicyOverlay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsqualify.json")
	config := validConfig(time.Now().Add(time.Minute))
	writeConfig(t, path, config)
	dns := map[string]any{
		"nameserver-policy":       map[string]any{"geosite:gfw": []string{"https://1.1.1.1/dns-query#DNSProxy"}},
		"proxy-server-nameserver": []string{"https://223.5.5.5/dns-query"},
	}
	status := ApplyOptional(path, map[string]any{"dns": dns})
	if !status.Enabled || status.State != "active" || status.PolicyCount != len(config.NameserverPolicy) {
		t.Fatalf("status = %+v", status)
	}
	policy := dns["nameserver-policy"].(map[string]any)
	for domain, want := range config.NameserverPolicy {
		got := policy[domain].([]string)
		if len(got) != 1 || got[0] != want[0] {
			t.Fatalf("policy[%q] = %#v, want %#v", domain, got, want)
		}
	}
	if len(dns["proxy-server-nameserver"].([]string)) != 1 {
		t.Fatal("proxy-server-nameserver was changed")
	}
}

func TestApplyRejectsPolicyConflictWithoutPartialMutation(t *testing.T) {
	config := validConfig(time.Now().Add(time.Minute))
	conflict := "devstreaming-cdn.apple.com"
	dns := map[string]any{"nameserver-policy": map[string]any{
		conflict: []string{"https://existing.example/dns-query"},
	}}
	if err := apply(map[string]any{"dns": dns}, config); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("apply error = %v, want explicit policy conflict", err)
	}
	policy := dns["nameserver-policy"].(map[string]any)
	if len(policy) != 1 {
		t.Fatalf("conflicting overlay partially mutated policy: %#v", policy)
	}
}

func TestApplyOptionalRejectsPolicyConflictAndKeepsBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsqualify.json")
	candidate := validConfig(time.Now().Add(time.Minute))
	writeConfig(t, path, candidate)
	conflict := "devstreaming-cdn.apple.com"
	mihomo := map[string]any{"dns": map[string]any{"nameserver-policy": map[string]any{
		conflict: []string{"https://existing.example/dns-query"},
	}}}
	status := ApplyOptional(path, mihomo)
	if status.Enabled || status.State != "disabled" || status.Reason != "rejected" || !strings.Contains(status.Detail, "conflicts") {
		t.Fatalf("status = %+v", status)
	}
	policy := mihomo["dns"].(map[string]any)["nameserver-policy"].(map[string]any)
	if len(policy) != 1 {
		t.Fatalf("rejected conflict partially mutated baseline: %#v", policy)
	}
}

func validConfig(expires time.Time) config {
	server := "https://8.8.8.8/dns-query#DNSProxy"
	return config{
		Version: version, ExpiresAt: expires.Format(time.RFC3339Nano),
		NameserverPolicy: map[string][]string{
			"cdn.fastly.steamstatic.com": {server},
			"devstreaming-cdn.apple.com": {server},
		},
	}
}

func writeConfig(t *testing.T, path string, config config) {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func validMihomo() map[string]any {
	return map[string]any{"dns": map[string]any{"nameserver-policy": map[string]any{
		"geosite:gfw": []string{"https://1.1.1.1/dns-query#DNSProxy"},
	}}}
}
