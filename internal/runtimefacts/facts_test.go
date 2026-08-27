package runtimefacts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRouterFacts(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "runtime.json")
	data := `{
  "version": 2,
  "mode": "router",
  "core": "meta"
}`
	if err := os.WriteFile(profilePath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.yaml")
	config := `redir-port: 7892
tproxy-port: 7895
ipv6: true
external-controller: 127.0.0.1:19090
dns:
  listen: 0.0.0.0:7874
tun:
  enable: true
  device: utun
  auto-route: false
  auto-redirect: false
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	facts, err := Read(context.Background(), Options{RuntimeProfile: profilePath, RuntimeDir: filepath.Join(dir, "runtime"), ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if facts.SchemaVersion != 1 || facts.ProfileMode != "router" {
		t.Fatalf("identity facts = %+v", facts)
	}
	if facts.DNSPort != 7874 || facts.RedirPort != 7892 || facts.TProxyPort != 7895 {
		t.Fatalf("ports = %+v", facts)
	}
	if !facts.TunEnabled || facts.TunDevice != "utun" || facts.TunAutoRoute || facts.TunAutoRedirect || !facts.IPv6 {
		t.Fatalf("network facts = %+v", facts)
	}
	if facts.ConfigSHA256 == "" {
		t.Fatal("config hash is required")
	}
}

func TestReadRequiresExistingRuntimeProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if _, err := Read(context.Background(), Options{RuntimeProfile: path, ConfigPath: filepath.Join(t.TempDir(), "config.yaml")}); err == nil {
		t.Fatal("missing runtime profile should fail explicitly")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("runtime facts must not create missing profile, stat err = %v", err)
	}
}

func TestReadRequiresGeneratedConfig(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "runtime.json")
	if err := os.WriteFile(profilePath, []byte(`{"version":2,"mode":"router","core":"meta"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "missing.yaml")
	if _, err := Read(context.Background(), Options{RuntimeProfile: profilePath, ConfigPath: configPath}); err == nil {
		t.Fatal("missing generated config should fail explicitly")
	}
}

func TestListenPortRejectsMalformedAddress(t *testing.T) {
	if _, err := listenPort("not-an-address"); err == nil {
		t.Fatal("malformed DNS listen should fail")
	}
}

func TestReadRejectsMalformedRequiredScalar(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "runtime.json")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(profilePath, []byte(`{"version":2,"mode":"router","core":"meta"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("redir-port: not-a-port\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(context.Background(), Options{RuntimeProfile: profilePath, ConfigPath: configPath}); err == nil {
		t.Fatal("malformed redir-port should fail explicitly")
	}
}
