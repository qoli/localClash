package runtimeprofile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStatusForMissingFileInitializesV2RuntimeSelectorWithoutWorkingProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultPath)
	status, err := StatusFor(path)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists {
		t.Fatal("missing runtime selector should be initialized")
	}
	if status.Mode != ModeNormal || status.Core != CoreMeta || status.RuntimeSource != RuntimeSourceBuiltin {
		t.Fatalf("status = %+v, want normal/meta builtin", status)
	}
	if status.CorePath != MetaCorePath {
		t.Fatalf("core path = %q, want %q", status.CorePath, MetaCorePath)
	}
	if status.Summary["mixed-port"] != 7890 {
		t.Fatalf("summary mixed-port = %v, want 7890", status.Summary["mixed-port"])
	}
	if _, err := os.Stat(filepath.Join(dir, "profiles")); !os.IsNotExist(err) {
		t.Fatalf("V2 must not create profiles directory, stat err=%v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"version": 2`, `"mode": "normal"`, `"core": "meta"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("runtime selector missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{`"profiles"`, `"cores"`, `"mihomo"`} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("runtime selector should not contain %q:\n%s", unwanted, text)
		}
	}
}

func TestConfigureWritesModeAndCore(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)

	status, err := Configure(path, ModeRouter, CoreSmart)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists {
		t.Fatal("configured runtime selector should exist")
	}
	if status.Mode != ModeRouter || status.Core != CoreSmart || status.CorePath != SmartCorePath {
		t.Fatalf("status = %+v, want router smart", status)
	}
	if status.RuntimeSource != RuntimeSourceBuiltin {
		t.Fatalf("runtime source = %q, want builtin", status.RuntimeSource)
	}
	if !status.RouterTakeoverRequired {
		t.Fatal("router profile should require router takeover after run_runtime")
	}
	if status.SmartGroupDefault.URL != "https://cp.cloudflare.com/generate_204" || status.SmartGroupDefault.Interval != 600 {
		t.Fatalf("smart defaults = %+v, want OpenClash-style health check defaults", status.SmartGroupDefault)
	}

	file, exists, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || file.Version != 2 || file.Mode != ModeRouter || file.Core != CoreSmart {
		t.Fatalf("loaded file = %+v exists=%v", file, exists)
	}
}

func TestDefaultNormalProfileCanResolveV2FlyGeoSitePacks(t *testing.T) {
	file := DefaultFile()
	mihomo := file.Profiles[ModeNormal].Mihomo

	if mihomo["geodata-mode"] != true || mihomo["geodata-loader"] != "memconservative" || mihomo["geo-auto-update"] != true || mihomo["geo-update-interval"] != 24 || mihomo["etag-support"] != true {
		t.Fatalf("normal geodata = mode %v loader %v auto %v interval %v etag %v, want enabled memconservative auto update",
			mihomo["geodata-mode"], mihomo["geodata-loader"], mihomo["geo-auto-update"], mihomo["geo-update-interval"], mihomo["etag-support"])
	}
	geoxURL := mihomo["geox-url"].(map[string]any)
	assertGitHubProxyGeoxURL(t, geoxURL, "normal")
}

func TestDefaultRouterProfileMatchesRouterReferencePreferences(t *testing.T) {
	file := DefaultFile()
	profile := file.Profiles[ModeRouter]
	mihomo := profile.Mihomo

	for key, want := range map[string]any{
		"mixed-port":          7893,
		"redir-port":          7892,
		"tproxy-port":         7895,
		"allow-lan":           true,
		"bind-address":        "*",
		"external-controller": "0.0.0.0:9090",
		"ipv6":                true,
		"geodata-mode":        true,
		"geodata-loader":      "memconservative",
		"geo-auto-update":     true,
		"geo-update-interval": 24,
		"etag-support":        true,
		"tcp-concurrent":      true,
		"unified-delay":       true,
		"find-process-mode":   "off",
		"keep-alive-interval": 15,
		"keep-alive-idle":     600,
	} {
		if got := mihomo[key]; got != want {
			t.Fatalf("router mihomo[%q] = %v, want %v", key, got, want)
		}
	}
	geoxURL := mihomo["geox-url"].(map[string]any)
	assertGitHubProxyGeoxURL(t, geoxURL, "router")

	assertMainlandReachableDNS(t, mihomo, "0.0.0.0:7874", "router")
	if _, ok := mihomo["interface-name"]; ok {
		t.Fatalf("router default must not pin Ronnie's WAN device: %+v", mihomo["interface-name"])
	}
	tun := mihomo["tun"].(map[string]any)
	if tun["stack"] != "mixed" || tun["device"] != "utun" || tun["auto-route"] != false || tun["auto-redirect"] != false {
		t.Fatalf("router tun = %+v, want mixed utun without Mihomo auto-route/auto-redirect", tun)
	}
	sniffer := mihomo["sniffer"].(map[string]any)
	if sniffer["enable"] != true || sniffer["override-destination"] != true || sniffer["force-dns-mapping"] != true || sniffer["parse-pure-ip"] != true {
		t.Fatalf("router sniffer = %+v, want enabled DNS mapping and pure-IP parsing", sniffer)
	}
	if _, ok := profile.Deploy["openclash-conflict"]; ok {
		t.Fatalf("router deploy must not contain OpenClash conflict policy: %+v", profile.Deploy)
	}
	if _, ok := profile.Deploy["wan-interface"]; ok {
		t.Fatalf("router deploy must not pin Ronnie's WAN interface: %+v", profile.Deploy)
	}
}

func assertGitHubProxyGeoxURL(t *testing.T, geoxURL map[string]any, profile string) {
	t.Helper()
	for _, key := range []string{"geoip", "geosite", "mmdb", "asn"} {
		value := fmt.Sprint(geoxURL[key])
		if !strings.HasPrefix(value, "https://gh-proxy.com/https://") {
			t.Fatalf("%s geox-url[%s] = %q, want gh-proxy.com HTTPS mirror", profile, key, value)
		}
		if strings.Contains(value, "testingcf.jsdelivr.net") {
			t.Fatalf("%s geox-url[%s] = %q, should not use testingcf mirror", profile, key, value)
		}
	}
	if !strings.Contains(fmt.Sprint(geoxURL["geosite"]), "Loyalsoldier/v2ray-rules-dat") || !strings.Contains(fmt.Sprint(geoxURL["geosite"]), "geosite.dat") {
		t.Fatalf("%s geox-url = %+v, want Loyalsoldier geosite.dat", profile, geoxURL)
	}
}

func TestUserProfileReplacesBuiltinWithoutBackfill(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultPath)
	writeRuntimeProfileTestFile(t, path, `version: 2
mode: router
core: meta
`)
	writeRuntimeProfileTestFile(t, filepath.Join(dir, UserPath), `mixed-port: 9000
allow-lan: true
`)

	_, profile, exists, err := ActiveProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("runtime selector should exist")
	}
	if profile.Mihomo["mixed-port"] != 9000 {
		t.Fatalf("mixed-port = %v, want user value 9000", profile.Mihomo["mixed-port"])
	}
	for _, key := range []string{"dns", "tun", "sniffer", "external-controller"} {
		if _, ok := profile.Mihomo[key]; ok {
			t.Fatalf("%s should not be backfilled into user profile: %+v", key, profile.Mihomo)
		}
	}

	status, err := StatusFor(path)
	if err != nil {
		t.Fatal(err)
	}
	if status.RuntimeSource != RuntimeSourceUser || !status.UserProfileExists {
		t.Fatalf("status = %+v, want user runtime source", status)
	}
}

func TestUserProfileRejectsLocalClashOwnedDynamicKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultPath)
	writeRuntimeProfileTestFile(t, path, `version: 2
mode: normal
core: meta
`)
	writeRuntimeProfileTestFile(t, filepath.Join(dir, UserPath), `rules:
  - MATCH,DIRECT
`)

	_, _, _, err := ActiveProfile(path)
	if err == nil || !strings.Contains(err.Error(), `rules`) || !strings.Contains(err.Error(), UserPath) {
		t.Fatalf("ActiveProfile error = %v, want banned key with user profile path", err)
	}
}

func TestUserProfileAllowsMihomoMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultPath)
	writeRuntimeProfileTestFile(t, path, `version: 2
mode: router
core: meta
`)
	writeRuntimeProfileTestFile(t, filepath.Join(dir, UserPath), `mode: global
mixed-port: 9001
`)

	_, profile, _, err := ActiveProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Mihomo["mode"] != "global" || profile.Mihomo["mixed-port"] != 9001 {
		t.Fatalf("profile = %+v, want user Mihomo mode and port", profile.Mihomo)
	}
}

func TestBuildConfigInjectsOnlyLocalClashDynamicKeys(t *testing.T) {
	profile := Profile{Mihomo: map[string]any{
		"mixed-port": 9000,
		"mode":       "global",
	}}
	dynamic := map[string]any{
		"proxies":        []any{map[string]any{"name": "HK"}},
		"proxy-groups":   []any{map[string]any{"name": "AUTO"}},
		"rule-providers": map[string]any{},
		"rules":          []string{"MATCH,DIRECT"},
		"x-localclash":   map[string]any{"version": 1},
		"dns":            map[string]any{"enable": true},
	}

	config := BuildConfig(profile, dynamic)

	if config["mixed-port"] != 9000 || config["mode"] != "global" {
		t.Fatalf("runtime keys = %+v, want user base preserved", config)
	}
	if _, ok := config["dns"]; ok {
		t.Fatalf("non-dynamic DNS must not be injected from dynamic fragment: %+v", config["dns"])
	}
	for _, key := range []string{"proxies", "proxy-groups", "rule-providers", "rules", "x-localclash"} {
		if _, ok := config[key]; !ok {
			t.Fatalf("missing injected dynamic key %q in %+v", key, config)
		}
	}
}

func assertMainlandReachableDNS(t *testing.T, mihomo map[string]any, listen string, label string) {
	t.Helper()
	dns, ok := mihomo["dns"].(map[string]any)
	if !ok {
		t.Fatalf("%s profile must define DNS defaults", label)
	}
	if dns["enhanced-mode"] != "redir-host" || dns["listen"] != listen || dns["respect-rules"] != true {
		t.Fatalf("%s dns = %+v, want redir-host dns on %s with respect-rules", label, dns, listen)
	}
	for _, key := range []string{"nameserver", "proxy-server-nameserver", "direct-nameserver", "default-nameserver"} {
		if strings.Contains(fmt.Sprint(dns[key]), "127.0.0.1:5335") {
			t.Fatalf("%s dns %s = %+v, must not depend on Ronnie's local mosDNS", label, key, dns[key])
		}
	}
	for key, want := range map[string][]any{
		"nameserver":              {"https://223.5.5.5/dns-query"},
		"proxy-server-nameserver": {"https://223.5.5.5/dns-query"},
		"direct-nameserver":       {"https://223.5.5.5/dns-query"},
		"default-nameserver":      {"https://223.5.5.5/dns-query"},
	} {
		if !reflect.DeepEqual(dns[key], want) {
			t.Fatalf("%s dns %s = %+v, want mainland-reachable defaults %+v", label, key, dns[key], want)
		}
	}
	dnsText := fmt.Sprint(dns)
	for _, forbidden := range []string{"tls://1.1.1.1", "tls://8.8.8.8", "1.1.1.1:853", "8.8.8.8:853"} {
		if strings.Contains(dnsText, forbidden) {
			t.Fatalf("%s dns must not ship foreign DoT default %q: %+v", label, forbidden, dns)
		}
	}
	globalResolvers := []any{"https://8.8.8.8/dns-query#DNSProxy"}
	if !reflect.DeepEqual(dns["fallback"], globalResolvers) {
		t.Fatalf("%s dns fallback = %+v, want global DoH through DNSProxy %+v", label, dns["fallback"], globalResolvers)
	}
	if dns["fallback-lazy-query"] != true {
		t.Fatalf("%s dns fallback-lazy-query = %+v, want explicit lazy fallback", label, dns["fallback-lazy-query"])
	}
	policy, ok := dns["nameserver-policy"].(map[string]any)
	gfwResolvers := []any{"https://1.1.1.1/dns-query#DNSProxy"}
	if !ok || !reflect.DeepEqual(policy["geosite:gfw"], gfwResolvers) {
		t.Fatalf("%s dns nameserver-policy = %+v, want geosite:gfw through DNSProxy DoH", label, dns["nameserver-policy"])
	}
	filter, ok := dns["fallback-filter"].(map[string]any)
	if !ok || filter["geoip"] != false || filter["geoip-code"] != nil || filter["geosite"] != nil {
		t.Fatalf("%s dns fallback-filter = %+v, want explicit CIDR-only filtering", label, dns["fallback-filter"])
	}
}

func writeRuntimeProfileTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
