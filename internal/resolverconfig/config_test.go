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
	config, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || config != nil {
		t.Fatalf("config = %+v, error = %v", config, err)
	}
}

func TestLoadRejectsMalformedExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsqualify.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Load error = %v, want strict existing-config failure", err)
	}
}

func TestLoadAndApplyStandaloneConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsqualify.json")
	config := validConfig()
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
		"nameserver-policy":       map[string]any{"geosite:gfw": []string{"https://dns.google/dns-query#DNSProxy"}},
		"proxy-server-nameserver": []string{"https://dns.alidns.com/dns-query", "https://doh.pub/dns-query"},
	}
	if err := Apply(map[string]any{"dns": dns}, *loaded); err != nil {
		t.Fatal(err)
	}
	got := dns["nameserver-policy"].(map[string]any)[ScopeMainlandServices].([]string)
	if len(got) != 1 || got[0] != "119.29.29.29" {
		t.Fatalf("mainland policy = %#v", got)
	}
	if len(dns["proxy-server-nameserver"].([]string)) != 2 {
		t.Fatal("proxy-server-nameserver was changed")
	}
}

func validConfig() Config {
	now := time.Now().Format(time.RFC3339Nano)
	return Config{
		Version: Version, Scope: ScopeMainlandServices,
		Resolver: Resolver{
			CandidateID: "dnspod-udp", Source: "public_provider",
			Transport: "udp", Endpoint: "119.29.29.29",
		},
		Measurement: Measurement{
			ReportSHA256: strings.Repeat("a", 64), ReportFinishedAt: now,
			ResolvPath: "/tmp/resolv.conf.auto", GeneratedAt: now,
		},
	}
}
