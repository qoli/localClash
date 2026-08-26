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
	now := time.Now()
	path := filepath.Join(t.TempDir(), "dnsqualify.json")
	config := validConfig(now)
	config.Measurement.ReportFinishedAt = now.Add(-3 * time.Minute).Format(time.RFC3339Nano)
	config.Measurement.GeneratedAt = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	config.Measurement.ExpiresAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Load(path)
	if err != nil || result.Config != nil || result.Status.Enabled || result.Status.Reason != "expired" || result.Status.ExpiresAt != config.Measurement.ExpiresAt {
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

func TestValidateRejectsLegacyExpiredAndUnverifiableECS(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{name: "legacy", edit: func(config *Config) { config.Version = 1 }, want: "version 1"},
		{name: "expired", edit: func(config *Config) {
			config.Measurement.ReportFinishedAt = now.Add(-3 * time.Minute).Format(time.RFC3339Nano)
			config.Measurement.GeneratedAt = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
			config.Measurement.ExpiresAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
		}, want: "expired"},
		{name: "private ECS", edit: func(config *Config) { config.ECS.Prefix = "192.168.1.0/24" }, want: "publicly routable"},
		{name: "documentation ECS", edit: func(config *Config) { config.ECS.Prefix = "203.0.113.0/24" }, want: "publicly routable"},
		{name: "missing ECS interface", edit: func(config *Config) { config.ECS.Interface = "" }, want: "interface provenance"},
		{name: "too specific ECS", edit: func(config *Config) { config.ECS.Prefix = "114.114.114.1/32" }, want: "no more than 24"},
		{name: "scope hash", edit: func(config *Config) { config.Scope.DomainSHA256 = strings.Repeat("b", 64) }, want: "domain_sha256"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig(now)
			test.edit(&config)
			if err := ValidateAt(config, now); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateAt error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadAndApplyQualifiedECSConfig(t *testing.T) {
	now := time.Now()
	path := filepath.Join(t.TempDir(), "dnsqualify.json")
	config := validConfig(now)
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
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
	want := "https://8.8.8.8/dns-query#DNSProxy&ecs=114.114.114.0/24&ecs-override=true"
	for _, domain := range config.Scope.Domains {
		got := policy[domain].([]string)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("policy[%q] = %#v, want %q", domain, got, want)
		}
	}
	if len(dns["proxy-server-nameserver"].([]string)) != 1 {
		t.Fatal("proxy-server-nameserver was changed")
	}
}

func TestApplyRejectsPolicyConflict(t *testing.T) {
	now := time.Now()
	config := validConfig(now)
	dns := map[string]any{"nameserver-policy": map[string]any{
		config.Scope.Domains[0]: []string{"https://existing.example/dns-query"},
	}}
	if err := Apply(map[string]any{"dns": dns}, config); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("Apply error = %v, want explicit policy conflict", err)
	}
}

func validConfig(now time.Time) Config {
	domains := []string{"cdn.fastly.steamstatic.com", "devstreaming-cdn.apple.com"}
	hash, _ := CanonicalDomainSHA256(domains)
	return Config{
		Version: Version,
		Scope:   Scope{Type: ScopeTypeDomains, ID: "mainland-known-services-v2", Domains: domains, DomainSHA256: hash},
		Resolver: Resolver{
			CandidateID: "google-doh-wan-ecs", Source: "global_encrypted_ecs",
			Transport: "doh", Endpoint: "https://8.8.8.8/dns-query", Proxy: DNSProxyGroup,
		},
		ECS: ECS{Prefix: "114.114.114.0/24", Source: ECSSTUNMainland, Interface: "wan", Server: MainlandSTUNServer, ServerIP: "106.12.251.193"},
		Measurement: Measurement{
			ReportSHA256: strings.Repeat("a", 64), ReportFinishedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
			ResolvPath: "/tmp/resolv.conf.auto", GeneratedAt: now.Format(time.RFC3339Nano),
			ExpiresAt: now.Add(30 * time.Minute).Format(time.RFC3339Nano),
		},
	}
}
