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
	result, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || result.Config != nil || result.Status.Enabled || result.Status.Reason != "missing" {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestLoadExpiredMeansOptionalConfigExplicitlyDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsqualify.json")
	config := validConfig(time.Now().Add(-time.Minute))
	writeConfig(t, path, config)
	result, err := Load(path)
	if err != nil || result.Config != nil || result.Status.Enabled || result.Status.Reason != "expired" || result.Status.ExpiresAt != config.ExpiresAt {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestLoadRejectsMalformedExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsqualify.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Load error = %v, want strict existing-config failure", err)
	}
}

func TestValidateOnlyConsumerContract(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{name: "version", edit: func(config *Config) { config.Version = 1 }, want: "version 1"},
		{name: "empty policy", edit: func(config *Config) { config.NameserverPolicy = nil }, want: "nameserver_policy"},
		{name: "invalid expiry", edit: func(config *Config) { config.ExpiresAt = "tomorrow" }, want: "invalid expires_at"},
		{name: "expired", edit: func(config *Config) { config.ExpiresAt = now.Add(-time.Second).Format(time.RFC3339Nano) }, want: "expired"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig(now.Add(time.Minute))
			test.edit(&config)
			if err := ValidateAt(config, now); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateAt error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadAndApplyNameserverPolicyOverlay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsqualify.json")
	config := validConfig(time.Now().Add(time.Minute))
	writeConfig(t, path, config)
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	dns := map[string]any{
		"nameserver-policy":       map[string]any{"geosite:gfw": []string{"https://1.1.1.1/dns-query#DNSProxy"}},
		"proxy-server-nameserver": []string{"https://223.5.5.5/dns-query"},
	}
	if loaded.Config == nil || !loaded.Status.Enabled || loaded.Status.State != "active" {
		t.Fatalf("loaded result = %+v", loaded)
	}
	if err := Apply(map[string]any{"dns": dns}, *loaded.Config); err != nil {
		t.Fatal(err)
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
	if err := Apply(map[string]any{"dns": dns}, config); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("Apply error = %v, want explicit policy conflict", err)
	}
	policy := dns["nameserver-policy"].(map[string]any)
	if len(policy) != 1 {
		t.Fatalf("conflicting overlay partially mutated policy: %#v", policy)
	}
}

func validConfig(expires time.Time) Config {
	server := "https://8.8.8.8/dns-query#DNSProxy"
	return Config{
		Version: Version, ExpiresAt: expires.Format(time.RFC3339Nano),
		NameserverPolicy: map[string][]string{
			"cdn.fastly.steamstatic.com": {server},
			"devstreaming-cdn.apple.com": {server},
		},
	}
}

func writeConfig(t *testing.T, path string, config Config) {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
