package configrender

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localclash/internal/configmeta"
	"localclash/internal/resolverconfig"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"
	"localclash/internal/smartpolicy"

	"gopkg.in/yaml.v3"
)

func TestRenderRuleSpecsSupportsDomainSuffix(t *testing.T) {
	rules, err := renderRuleSpecs([]ruleSpec{
		{DomainSuffix: "local", Target: "DIRECT"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := rules[0]; got != "DOMAIN-SUFFIX,local,DIRECT" {
		t.Fatalf("rule = %q, want DOMAIN-SUFFIX,local,DIRECT", got)
	}
}

func TestRenderRuleSpecsSupportsGeoIPNoResolve(t *testing.T) {
	rules, err := renderRuleSpecs([]ruleSpec{
		{GeoIP: "telegram", Target: "DIRECT", NoResolve: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := rules[0]; got != "GEOIP,telegram,DIRECT,no-resolve" {
		t.Fatalf("rule = %q, want GEOIP,telegram,DIRECT,no-resolve", got)
	}
}

func TestParseOverlayRuleLineSupportsGeoSite(t *testing.T) {
	rule, ok := parseOverlayRuleLine("GEOSITE,google,Global")
	if !ok {
		t.Fatal("expected GEOSITE overlay rule to parse")
	}
	if rule.Type != "GEOSITE" || rule.Value != "google" || rule.Target != "Global" || rule.Provider != "" {
		t.Fatalf("rule = %+v, want GEOSITE google Global", rule)
	}
}

func TestParseOverlayRuleLineSupportsGeoIP(t *testing.T) {
	rule, ok := parseOverlayRuleLine("GEOIP,telegram,Global,no-resolve")
	if !ok {
		t.Fatal("expected GEOIP overlay rule to parse")
	}
	if rule.Type != "GEOIP" || rule.Value != "telegram" || rule.Target != "Global" || rule.Provider != "" {
		t.Fatalf("rule = %+v, want GEOIP telegram Global", rule)
	}
}

func TestParseOverlayRuleLineSupportsANDTransportRule(t *testing.T) {
	rule, ok := parseOverlayRuleLine("AND,((NETWORK,UDP),(DST-PORT,443)),🚦 QUIC")
	if !ok {
		t.Fatal("expected AND overlay rule to parse")
	}
	if rule.Type != "AND" || rule.Value != "((NETWORK,UDP),(DST-PORT,443))" || rule.Target != "🚦 QUIC" {
		t.Fatalf("rule = %+v, want AND UDP/443 QUIC", rule)
	}
}

func TestProxyNamesDeduplicates(t *testing.T) {
	names, err := proxyNames([]any{
		map[string]any{"name": "HK"},
		map[string]any{"name": "HK"},
		map[string]any{"name": "JP"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("len(names) = %d, want 2", len(names))
	}
}

func TestBuildOrderedRulesUsesLocalBaselineFragmentAndDirectFallback(t *testing.T) {
	fragment := &rules.Fragment{Rules: []string{"DOMAIN-SUFFIX,example.com,⚡ 自动选择"}}
	got, err := buildOrderedRules(fragment, "")
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "DOMAIN,localhost,DIRECT" {
		t.Fatalf("first rule = %q, want local baseline", got[0])
	}
	if indexOf(got, "DOMAIN-SUFFIX,example.com,⚡ 自动选择") <= 0 {
		t.Fatalf("rules = %+v, want fragment after local baseline", got)
	}
	if got[len(got)-1] != "MATCH,DIRECT" {
		t.Fatalf("last rule = %q, want DIRECT fallback", got[len(got)-1])
	}
}

func TestBuildOrderedRulesUsesConfiguredFallback(t *testing.T) {
	got, err := buildOrderedRules(nil, "Catchall")
	if err != nil {
		t.Fatal(err)
	}
	if got[len(got)-1] != "MATCH,Catchall" {
		t.Fatalf("last rule = %q, want configured fallback", got[len(got)-1])
	}
}

func TestRenderWithoutPacksSelectionPreservesBaseConfig(t *testing.T) {
	paths := writeRenderFixture(t)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "base.yaml"),
		RuntimeProfilePath: filepath.Join(paths.dir, "localclash-runtime.json"),
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuleCount != len(LocalBaselineRuleLines())+1 {
		t.Fatalf("RuleCount = %d, want baseline + direct fallback", result.RuleCount)
	}
	config := readTestYAML(t, result.OutputPath)
	if _, ok := config["rule-providers"].(map[string]any)["sukkaw_ai_non_ip"]; ok {
		t.Fatal("base config should not include pack rule-provider")
	}
	if proxyGroupNamesFromConfig(config)["AI"] {
		t.Fatal("base config should not include AI proxy-group")
	}
	metadata := config[configmeta.Key].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	if overlay["modifiable"] != true {
		t.Fatalf("overlay modifiable = %v, want true", overlay["modifiable"])
	}
	if len(overlay["packs"].([]any)) != 0 {
		t.Fatalf("overlay packs = %v, want empty", overlay["packs"])
	}
	assertNoSensitiveConfigMetadata(t, metadata)
}

func TestRenderWithPacksSelectionIncludesProxyGroupFragment(t *testing.T) {
	paths := writeRenderFixture(t)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "with-packs.gob"),
		PacksSelectionPath: paths.selection,
		RulesCacheDir:      paths.cacheDir,
		RuntimeProfilePath: filepath.Join(paths.dir, "localclash-runtime.json"),
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)

	groups := proxyGroupNamesFromConfig(config)
	if !groups["AI"] {
		t.Fatal("missing proxy-group AI")
	}
	for _, unwanted := range []string{"AI_AUTO", "AI_MANUAL", "JP", "SG", "US"} {
		if groups[unwanted] {
			t.Fatalf("%q should not be materialized", unwanted)
		}
	}

	providers := config["rule-providers"].(map[string]any)
	for _, want := range []string{"sukkaw_ai_non_ip", "blackmatrix7_OpenAI"} {
		if _, ok := providers[want]; !ok {
			t.Fatalf("missing rule-provider %q", want)
		}
	}

	rules := testStringSlice(config["rules"])
	packIndex := indexOf(rules, "RULE-SET,sukkaw_ai_non_ip,AI")
	fallbackIndex := indexOf(rules, "MATCH,DIRECT")
	if packIndex < 0 {
		t.Fatal("missing sukkaw AI rule")
	}
	if fallbackIndex < 0 {
		t.Fatal("missing DIRECT fallback rule")
	}
	if packIndex > fallbackIndex {
		t.Fatalf("pack rule index %d should be before DIRECT fallback index %d", packIndex, fallbackIndex)
	}

	metadata := config["x-localclash"].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	packs := overlay["packs"].([]any)
	if len(packs) != 2 {
		t.Fatalf("overlay packs = %d, want 2", len(packs))
	}
	proxyGroups := overlay["proxy_groups"].([]any)
	if len(proxyGroups) != 1 {
		t.Fatalf("overlay proxy groups = %d, want 1", len(proxyGroups))
	}
	if got := proxyGroups[0].(map[string]any)["mode"]; got != "manual" {
		t.Fatalf("proxy group mode = %v, want manual", got)
	}
	assertNoSensitiveConfigMetadata(t, metadata)
}

func TestRenderUsesSelectionFallbackTarget(t *testing.T) {
	paths := writeRenderFixture(t)
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  Catchall:
    nodes: ["🇯🇵日本01 | JP"]
    manual: true
fallback_target: Catchall
required_targets: [Catchall]
enabled_packs: []
`)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "fallback.yaml"),
		PacksSelectionPath: paths.selection,
		RulesCacheDir:      paths.cacheDir,
		RuntimeProfilePath: filepath.Join(paths.dir, "localclash-runtime.json"),
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)
	rules := testStringSlice(config["rules"])
	if got := rules[len(rules)-1]; got != "MATCH,Catchall" {
		t.Fatalf("last rule = %q, want configured fallback", got)
	}
	if !proxyGroupNamesFromConfig(config)["Catchall"] {
		t.Fatal("missing fallback proxy group")
	}
}

func TestRenderDefaultOverlayUsesBaseManualAutoSelectors(t *testing.T) {
	paths := writeRenderFixture(t)
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  "🎯 手动选择":
    nodes: ["🇯🇵日本01 | JP"]
    manual: true
  "⚡ 自动选择":
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
  "🌐 全球直连":
    direct: true
  "🇭🇰 香港节点":
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
    optional: true
policy_groups:
  "🎮 Steam":
    exits: ["⚡ 自动选择", "🎯 手动选择", "🌐 全球直连", "🇭🇰 香港节点"]
    manual: true
enabled_packs:
  - source: blackmatrix7
    pack: OpenAI
    target: "🎮 Steam"
`)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "default-overlay.yaml"),
		PacksSelectionPath: paths.selection,
		RulesCacheDir:      paths.cacheDir,
		RuntimeProfilePath: filepath.Join(paths.dir, "localclash-runtime.json"),
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)
	groups := proxyGroupNamesFromConfig(config)
	for _, want := range []string{"⚡ 自动选择", "🎯 手动选择", "🎮 Steam", "🇭🇰 香港节点"} {
		if !groups[want] {
			t.Fatalf("missing proxy-group %q in %+v", want, groups)
		}
	}
	for _, unwanted := range []string{"AUTO", "MANUAL", "PROXY"} {
		if groups[unwanted] {
			t.Fatalf("proxy-group %q should not be emitted for default overlay", unwanted)
		}
	}
	steam := proxyGroupFromConfig(t, config, "🎮 Steam")
	wantSteam := []string{"⚡ 自动选择", "🎯 手动选择", "🌐 全球直连", "🇭🇰 香港节点"}
	if got := steam["proxies"].([]any); len(got) != len(wantSteam) {
		t.Fatalf("Steam proxies = %+v, want %+v", got, wantSteam)
	} else {
		for i, want := range wantSteam {
			if got[i] != want {
				t.Fatalf("Steam proxies = %+v, want %+v", got, wantSteam)
			}
		}
	}
	manual := proxyGroupFromConfig(t, config, "🎯 手动选择")
	wantManualPrefix := []string{"🇯🇵日本01 | JP"}
	if got := manual["proxies"].([]any); len(got) < len(wantManualPrefix) {
		t.Fatalf("manual proxies = %+v, want prefix %+v", got, wantManualPrefix)
	} else {
		for i, want := range wantManualPrefix {
			if got[i] != want {
				t.Fatalf("manual proxies = %+v, want prefix %+v", got, wantManualPrefix)
			}
		}
	}
	order := proxyGroupOrderFromConfig(config)
	if len(order) < 2 || order[0] != "🎯 手动选择" || order[1] != "⚡ 自动选择" {
		t.Fatalf("proxy group order = %+v, want manual and auto selectors first", order)
	}
	if indexOf(order, "🇭🇰 香港节点") < indexOf(order, "🎯 手动选择") {
		t.Fatalf("region group order = %+v, want region groups after non-region groups", order)
	}
}

func TestRenderOmitsLegacyAppleGroupWithoutSelection(t *testing.T) {
	paths := writeRenderFixture(t)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "without-legacy-apple.yaml"),
		RuntimeProfilePath: filepath.Join(paths.dir, "localclash-runtime.json"),
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)
	if proxyGroupNamesFromConfig(config)["Apple"] {
		t.Fatal("legacy Apple proxy-group should not be generated without groups.apple")
	}
	if _, ok := config["rule-providers"].(map[string]any)["apple"]; ok {
		t.Fatal("legacy apple rule-provider should not be generated without apple provider")
	}
	for _, rule := range testStringSlice(config["rules"]) {
		if strings.Contains(rule, "RULE-SET,apple,") {
			t.Fatalf("legacy apple rule should not be generated: %s", rule)
		}
	}
}

func TestRenderWithPacksSelectionRejectsMissingProxyGroupNode(t *testing.T) {
	paths := writeRenderFixture(t)
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  AI:
    nodes: ["🇯🇵日本01 | JP"]
    manual: true
enabled_packs:
  - source: sukkaw
    pack: ai
    target: AI
`)

	_, err := Render(Options{
		SourcePath:         paths.subscriptionNoJP,
		OutputPath:         filepath.Join(paths.dir, "empty-candidates.yaml"),
		PacksSelectionPath: paths.selection,
		RulesCacheDir:      paths.cacheDir,
		RuntimeProfilePath: filepath.Join(paths.dir, "localclash-runtime.json"),
		Force:              true,
	})
	if err == nil {
		t.Fatal("expected empty candidate error")
	}
}

func TestRenderAppliesActiveRuntimeProfile(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  "⚡ 自动选择":
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
policy_groups:
  DNSProxy:
    exits: ["⚡ 自动选择"]
    manual: true
enabled_packs: []
`)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "router.yaml"),
		PacksSelectionPath: paths.selection,
		RuntimeProfilePath: profilePath,
		RulesCacheDir:      paths.cacheDir,
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeMode != runtimeprofile.ModeRouter {
		t.Fatalf("runtime mode = %q, want %q", result.RuntimeMode, runtimeprofile.ModeRouter)
	}
	config := readTestYAML(t, result.OutputPath)
	if config["mixed-port"] != 7893 {
		t.Fatalf("mixed-port = %v, want router preset port", config["mixed-port"])
	}
	if config["allow-lan"] != true {
		t.Fatalf("allow-lan = %v, want true", config["allow-lan"])
	}
	listeners, ok := config["listeners"].([]any)
	if !ok || len(listeners) != 1 {
		t.Fatalf("listeners = %#v, want one DNSProxy measurement listener", config["listeners"])
	}
	listener, ok := listeners[0].(map[string]any)
	if !ok || listener["name"] != "dnsproxy-local" || listener["type"] != "http" || listener["listen"] != "127.0.0.1" || listener["port"] != 7894 || listener["proxy"] != "DNSProxy" {
		t.Fatalf("DNSProxy measurement listener = %#v", listeners[0])
	}
	if _, ok := config["proxies"].([]any); !ok {
		t.Fatalf("proxies should remain generated dynamic config, got %T", config["proxies"])
	}
	dns := config["dns"].(map[string]any)
	if dns["listen"] != "0.0.0.0:7874" {
		t.Fatalf("dns.listen = %v, want router DNS listen", dns["listen"])
	}
	groups := proxyGroupNamesFromConfig(config)
	if !groups["DNSProxy"] {
		t.Fatalf("router runtime DNSProxy group was not materialized: %+v", groups)
	}
}

func TestRenderBuiltinRouterDoesNotInjectWANResolvers(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  "⚡ 自动选择":
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
policy_groups:
  DNSProxy:
    exits: ["⚡ 自动选择"]
    manual: true
enabled_packs: []
`)
	result, err := Render(Options{
		SourcePath: paths.subscription, OutputPath: filepath.Join(paths.dir, "optimized.yaml"),
		PacksSelectionPath: paths.selection, RulesCacheDir: paths.cacheDir,
		RuntimeProfilePath: profilePath, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)
	dns := config["dns"].(map[string]any)
	policy := dns["nameserver-policy"].(map[string]any)
	if _, exists := policy["geosite:cn"]; exists {
		t.Fatalf("builtin router unexpectedly injected broad mainland DNS authority: %#v", policy)
	}
	if got := dns["proxy-server-nameserver"].([]any); len(got) != 2 || got[0] != "https://223.5.5.5/dns-query" || got[1] != "https://doh.pub/dns-query" {
		t.Fatalf("proxy-server-nameserver changed: %#v", got)
	}
	if result.ResolverStatus.Enabled || result.ResolverStatus.Reason != "missing" {
		t.Fatalf("missing resolver status = %+v", result.ResolverStatus)
	}
}

func TestRenderBuiltinRouterKeepsExplicitFilteredFallback(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  "⚡ 自动选择":
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
policy_groups:
  DNSProxy:
    exits: ["⚡ 自动选择"]
    manual: true
enabled_packs: []
`)
	result, err := Render(Options{
		SourcePath: paths.subscription, OutputPath: filepath.Join(paths.dir, "fallback.yaml"),
		PacksSelectionPath: paths.selection, RulesCacheDir: paths.cacheDir,
		RuntimeProfilePath: profilePath,
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)
	dns := config["dns"].(map[string]any)
	if got := dns["fallback"].([]any); len(got) != 2 || got[0] != "https://8.8.8.8/dns-query#DNSProxy" || got[1] != "https://1.1.1.1/dns-query#DNSProxy" {
		t.Fatalf("fallback = %#v", got)
	}
	if dns["fallback-lazy-query"] != false {
		t.Fatalf("fallback-lazy-query = %#v", dns["fallback-lazy-query"])
	}
	if dns["cache-algorithm"] != "arc" {
		t.Fatalf("cache-algorithm = %#v", dns["cache-algorithm"])
	}
	filter := dns["fallback-filter"].(map[string]any)
	if filter["geoip"] != true || filter["geoip-code"] != "CN" || filter["geosite"] != nil {
		t.Fatalf("fallback-filter = %#v, want GeoIP CN and CIDR classification without deprecated geosite", filter)
	}
}

func TestRenderRejectsMalformedResolverOverlayAndKeepsSecureBaseline(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  "⚡ 自动选择":
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
policy_groups:
  DNSProxy:
    exits: ["⚡ 自动选择"]
    manual: true
enabled_packs: []
`)
	configPath := resolverconfig.DefaultPath(profilePath)
	writeFile(t, configPath, `{"version":2,"unknown":true}`)
	outputPath := filepath.Join(paths.dir, "rejected-baseline.yaml")
	result, err := Render(Options{
		SourcePath: paths.subscription, OutputPath: outputPath,
		PacksSelectionPath: paths.selection, RulesCacheDir: paths.cacheDir,
		RuntimeProfilePath: profilePath, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResolverStatus.Enabled || result.ResolverStatus.State != "disabled" || result.ResolverStatus.Reason != "rejected" || !strings.Contains(result.ResolverStatus.Detail, "unknown field") {
		t.Fatalf("rejected resolver status = %+v", result.ResolverStatus)
	}
	config := readTestYAML(t, outputPath)
	policy := config["dns"].(map[string]any)["nameserver-policy"].(map[string]any)
	if _, exists := policy["devstreaming-cdn.apple.com"]; exists {
		t.Fatalf("rejected overlay mutated secure baseline: %#v", policy)
	}
}

func TestRenderUnacceptableResolverOverlayUsesVisibleSecureBaseline(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  "⚡ 自动选择":
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
policy_groups:
  DNSProxy:
    exits: ["⚡ 自动选择"]
    manual: true
enabled_packs: []
`)
	writeFile(t, resolverconfig.DefaultPath(profilePath), `{
  "version": 2,
  "scope": {"type":"domains"},
  "resolver": {},
  "ecs": {},
  "measurement": {}
}`)
	outputPath := filepath.Join(paths.dir, "legacy-baseline.yaml")
	result, err := Render(Options{
		SourcePath: paths.subscription, OutputPath: outputPath,
		PacksSelectionPath: paths.selection, RulesCacheDir: paths.cacheDir,
		RuntimeProfilePath: profilePath, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResolverStatus.Enabled || result.ResolverStatus.State != "disabled" || result.ResolverStatus.Reason != "rejected" || !strings.Contains(result.ResolverStatus.Detail, "unknown field") {
		t.Fatalf("rejected resolver status = %+v", result.ResolverStatus)
	}
	config := readTestYAML(t, outputPath)
	policy := config["dns"].(map[string]any)["nameserver-policy"].(map[string]any)
	if _, exists := policy["devstreaming-cdn.apple.com"]; exists {
		t.Fatalf("rejected resolver policy remained active: %#v", policy)
	}
}

func TestRenderIgnoresProducerMetadata(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  "⚡ 自动选择":
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
policy_groups:
  DNSProxy:
    exits: ["⚡ 自动选择"]
    manual: true
enabled_packs: []
`)
	domains := []string{"cdn.fastly.steamstatic.com", "devstreaming-cdn.apple.com"}
	writeFile(t, resolverconfig.DefaultPath(profilePath), `{
  "version": 2,
  "expires_at": "2000-01-01T00:00:00Z",
  "nameserver_policy": {
    "cdn.fastly.steamstatic.com": ["https://8.8.8.8/dns-query#DNSProxy"],
    "devstreaming-cdn.apple.com": ["https://8.8.8.8/dns-query#DNSProxy"]
  }
}`)
	outputPath := filepath.Join(paths.dir, "ignored-producer-metadata.yaml")
	result, err := Render(Options{
		SourcePath: paths.subscription, OutputPath: outputPath,
		PacksSelectionPath: paths.selection, RulesCacheDir: paths.cacheDir,
		RuntimeProfilePath: profilePath, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ResolverStatus.Enabled || result.ResolverStatus.State != "active" || result.ResolverStatus.PolicyCount != len(domains) {
		t.Fatalf("resolver result = %+v", result)
	}
	config := readTestYAML(t, outputPath)
	policy := config["dns"].(map[string]any)["nameserver-policy"].(map[string]any)
	for _, domain := range domains {
		if _, exists := policy[domain]; !exists {
			t.Fatalf("ignored producer metadata disabled ECS policy %q: %#v", domain, policy)
		}
	}
}

func TestRenderConsumesQualifiedECSResolverConfig(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  "⚡ 自动选择":
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
policy_groups:
  DNSProxy:
    exits: ["⚡ 自动选择"]
    manual: true
enabled_packs: []
`)
	configPath := resolverconfig.DefaultPath(profilePath)
	domains := []string{"cdn.fastly.steamstatic.com", "devstreaming-cdn.apple.com"}
	writeFile(t, configPath, `{
  "version": 2,
  "nameserver_policy": {
    "cdn.fastly.steamstatic.com": ["https://8.8.8.8/dns-query#DNSProxy"],
    "devstreaming-cdn.apple.com": ["https://8.8.8.8/dns-query#DNSProxy"]
  }
}`)
	result, err := Render(Options{
		SourcePath: paths.subscription, OutputPath: filepath.Join(paths.dir, "dnsqualify.yaml"),
		PacksSelectionPath: paths.selection, RulesCacheDir: paths.cacheDir,
		RuntimeProfilePath: profilePath, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ResolverStatus.Enabled || result.ResolverStatus.State != "active" || result.ResolverStatus.PolicyCount != len(domains) {
		t.Fatalf("active resolver status = %+v", result.ResolverStatus)
	}
	config := readTestYAML(t, result.OutputPath)
	policy := config["dns"].(map[string]any)["nameserver-policy"].(map[string]any)
	want := "https://8.8.8.8/dns-query#DNSProxy"
	for _, domain := range domains {
		got := policy[domain].([]any)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("dnsqualify policy[%q] = %#v", domain, got)
		}
	}
}

func TestRenderUserProfileOmittedDNSDoesNotBackfillBuiltinDNS(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(paths.dir, runtimeprofile.UserPath), `mixed-port: 9000
allow-lan: true
`)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "user-no-dns.yaml"),
		RuntimeProfilePath: profilePath,
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)
	if config["mixed-port"] != 9000 {
		t.Fatalf("mixed-port = %v, want user runtime base", config["mixed-port"])
	}
	if _, ok := config["dns"]; ok {
		t.Fatalf("dns should not be backfilled from builtin router template: %+v", config["dns"])
	}
	if _, ok := config["tun"]; ok {
		t.Fatalf("tun should not be backfilled from builtin router template: %+v", config["tun"])
	}
	if _, ok := config["proxies"].([]any); !ok {
		t.Fatalf("proxies should still be injected dynamic config, got %T", config["proxies"])
	}
}

func TestRenderUserProfileEmptyDNSDoesNotBackfillBuiltinDNS(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(paths.dir, runtimeprofile.UserPath), `mixed-port: 9000
dns: {}
`)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "user-empty-dns.yaml"),
		RuntimeProfilePath: profilePath,
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)
	dns := config["dns"].(map[string]any)
	if len(dns) != 0 {
		t.Fatalf("empty user dns should remain empty without template backfill: %+v", dns)
	}
}

func TestRenderUserProfileMosDNSSample(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(paths.dir, runtimeprofile.UserPath), `mixed-port: 7893
allow-lan: true
dns:
  enable: true
  listen: 0.0.0.0:7874
  nameserver:
    - tcp://127.0.0.1:5335
`)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "user-mosdns.yaml"),
		RuntimeProfilePath: profilePath,
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)
	dns := config["dns"].(map[string]any)
	nameservers := dns["nameserver"].([]any)
	if len(nameservers) != 1 || nameservers[0] != "tcp://127.0.0.1:5335" {
		t.Fatalf("dns.nameserver = %+v, want mosDNS tcp upstream", nameservers)
	}
}

func TestRenderRejectsUserProfileOwnedDynamicKey(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(paths.dir, runtimeprofile.UserPath), `rules:
  - MATCH,DIRECT
`)

	_, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "bad-user.yaml"),
		RuntimeProfilePath: profilePath,
		Force:              true,
	})
	if err == nil || !strings.Contains(err.Error(), "rules") || !strings.Contains(err.Error(), runtimeprofile.UserPath) {
		t.Fatalf("Render error = %v, want banned user profile key", err)
	}
}

func TestRenderIgnoresSubscriptionRuntimeLayerKeys(t *testing.T) {
	paths := writeRenderFixture(t)
	writeFile(t, paths.subscription, `hosts:
  router.local: 192.168.1.1
dns:
  enable: true
  listen: 127.0.0.1:5353
proxies:
  - name: "🇯🇵日本01 | JP"
    type: ss
    server: example.com
    port: 443
    cipher: none
    password: test
`)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "subscription-runtime-ignored.yaml"),
		RuntimeProfilePath: filepath.Join(paths.dir, "localclash-runtime.json"),
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)
	if _, ok := config["hosts"]; ok {
		t.Fatalf("subscription hosts should not be copied into runtime config: %+v", config["hosts"])
	}
	if _, ok := config["dns"]; ok {
		t.Fatalf("subscription dns should not be copied into runtime config: %+v", config["dns"])
	}
}

func TestRenderSmartCoreMaterializesAutoGroupsAsSmart(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profilePath, "", runtimeprofile.CoreSmart); err != nil {
		t.Fatal(err)
	}
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  AI:
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
    smart_priority:
      - label: US
        pattern: "(美国|US)"
        factor: 5
      - label: Other
        pattern: ".*"
        factor: 1
  SmartOnly:
    nodes: ["🇯🇵日本01 | JP"]
    smart: true
enabled_packs:
  - source: sukkaw
    pack: ai
    target: AI
  - source: sukkaw
    pack: ai
    target: SmartOnly
`)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "smart.yaml"),
		PacksSelectionPath: paths.selection,
		RulesCacheDir:      paths.cacheDir,
		RuntimeProfilePath: profilePath,
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Core != runtimeprofile.CoreSmart {
		t.Fatalf("core = %q, want smart", result.Core)
	}
	config := readTestYAML(t, result.OutputPath)
	for _, name := range []string{"AI", "SmartOnly"} {
		group := proxyGroupFromConfig(t, config, name)
		if group["type"] != "smart" {
			t.Fatalf("%s type = %v, want smart", name, group["type"])
		}
		wantURL := "https://cp.cloudflare.com/generate_204"
		wantInterval := 60
		if name == "SmartOnly" {
			wantInterval = 600
		}
		if group["url"] != wantURL || group["interval"] != wantInterval {
			t.Fatalf("%s smart health-check fields = %+v, want %s/%d", name, group, wantURL, wantInterval)
		}
		if group["tolerance"] != nil {
			t.Fatalf("%s kept url-test tolerance: %+v", name, group)
		}
		if group["uselightgbm"] != true || group["prefer-asn"] != true {
			t.Fatalf("%s smart options = %+v, want defaults", name, group)
		}
		if name == "AI" {
			if got := group["policy-priority"]; got != `(美国|US):5;.*:1` {
				t.Fatalf("AI policy-priority = %v, want group-scoped order", got)
			}
		} else if group["policy-priority"] != nil {
			t.Fatalf("%s inherited AI policy-priority: %+v", name, group)
		}
	}
	if config["lgbm-auto-update"] != true || config["lgbm-update-interval"] != 72 {
		t.Fatalf("smart lgbm update = auto %v interval %v, want enabled 72h", config["lgbm-auto-update"], config["lgbm-update-interval"])
	}
	if got := fmt.Sprint(config["lgbm-url"]); got != "https://gh-proxy.com/https://github.com/vernesong/mihomo/releases/download/LightGBM-Model/Model-middle.bin" {
		t.Fatalf("lgbm-url = %q, want mirrored Model-middle.bin URL", got)
	}
	profile := config["profile"].(map[string]any)
	if profile["smart-collector-size"] != 100 {
		t.Fatalf("profile.smart-collector-size = %v, want 100", profile["smart-collector-size"])
	}
	metadata := config["x-localclash"].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	proxyGroups := overlay["proxy_groups"].([]any)
	if got := proxyGroups[0].(map[string]any)["mode"]; got != "auto" {
		t.Fatalf("metadata proxy group mode = %v, want original auto intent", got)
	}

	if _, err := runtimeprofile.Configure(profilePath, "", runtimeprofile.CoreMeta); err != nil {
		t.Fatal(err)
	}
	metaResult, err := Render(Options{
		SourcePath:         paths.subscription,
		OutputPath:         filepath.Join(paths.dir, "meta.yaml"),
		PacksSelectionPath: paths.selection,
		RulesCacheDir:      paths.cacheDir,
		RuntimeProfilePath: profilePath,
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	meta := readTestYAML(t, metaResult.OutputPath)
	ai := proxyGroupFromConfig(t, meta, "AI")
	if ai["type"] != "url-test" || ai["policy-priority"] != nil {
		t.Fatalf("Meta AI group = %+v, want url-test without Smart-only priority", ai)
	}
}

func TestApplySmartCoreProxyGroupsRejectsInvalidDirectSelection(t *testing.T) {
	config := map[string]any{"proxy-groups": []map[string]any{{"name": "AI", "type": "url-test"}}}
	selection := &rules.Selection{ProxyGroups: map[string]rules.ProxyGroup{
		"AI": {Auto: true, SmartPriority: []smartpolicy.Rule{{Label: "US", Pattern: "US;JP", Factor: 1}}},
	}}
	if err := applySmartCoreProxyGroups(config, runtimeprofile.SmartOptions{}, selection); err == nil {
		t.Fatal("applySmartCoreProxyGroups() error = nil")
	}
}

func TestMergeRuleProvidersRejectsConflict(t *testing.T) {
	base := map[string]any{"applications": map[string]any{}}
	err := mergeRuleProviders(base, map[string]map[string]any{"applications": {}})
	if err == nil {
		t.Fatal("expected provider conflict error")
	}
}

type renderFixturePaths struct {
	dir              string
	subscription     string
	subscriptionNoJP string
	cacheDir         string
	selection        string
}

func writeRenderFixture(t *testing.T) renderFixturePaths {
	t.Helper()
	dir := t.TempDir()
	paths := renderFixturePaths{
		dir:              dir,
		subscription:     filepath.Join(dir, "subscription.gob"),
		subscriptionNoJP: filepath.Join(dir, "subscription-no-jp.gob"),
		cacheDir:         filepath.Join(dir, "packs"),
		selection:        filepath.Join(dir, "packs.gob"),
	}
	writeFile(t, paths.subscription, `proxies:
  - name: "🇯🇵日本01 | JP"
    type: ss
    server: example.com
    port: 443
    cipher: none
    password: test
`)
	writeFile(t, paths.subscriptionNoJP, `proxies:
  - name: "HK 01"
    type: ss
    server: example.com
    port: 443
    cipher: none
    password: test
`)
	if err := os.MkdirAll(paths.cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRenderPackIndex(t, paths.cacheDir)
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  AI:
    nodes: ["🇯🇵日本01 | JP"]
    manual: true
    direct: false
enabled_packs:
  - source: sukkaw
    pack: ai
    target: AI
  - source: blackmatrix7
    pack: OpenAI
    target: AI
`)
	return paths
}

func writeRenderPackIndex(t *testing.T, cacheDir string) {
	t.Helper()
	if err := rules.WritePackIndex(rules.PackIndexPath(cacheDir), map[string]rules.PackCache{
		"sukkaw": {
			Version:    1,
			Source:     "sukkaw",
			Adapter:    "sukkaw",
			Renderable: true,
			Packs: []rules.Pack{{
				ID:         "ai",
				Renderable: true,
				Components: []rules.Component{{
					ID:         "non_ip",
					Behavior:   "classical",
					Format:     "text",
					OrderClass: "non_ip",
					URL:        "https://ruleset.skk.moe/Clash/non_ip/ai.txt",
					Path:       "./rule-packs/sukkaw/ai_non_ip.txt",
				}},
			}},
		},
		"blackmatrix7": {
			Version:    1,
			Source:     "blackmatrix7",
			Adapter:    "blackmatrix7",
			Renderable: true,
			Packs: []rules.Pack{{
				ID:         "OpenAI",
				Renderable: true,
				Components: []rules.Component{{
					ID:         "OpenAI",
					Behavior:   "classical",
					Format:     "yaml",
					OrderClass: "mixed",
					URL:        "https://example.com/OpenAI.yaml",
					Path:       "./rule-packs/blackmatrix7/OpenAI.yaml",
				}},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var data []byte
	var err error
	switch filepath.Ext(path) {
	case ".json":
		var doc any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatal(err)
		}
		data, err = json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
	case ".gob":
		gob.Register(map[string]any{})
		gob.Register([]any{})
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := doc["proxies"]; ok {
			err = gob.NewEncoder(file).Encode(struct {
				Version int
				Data    map[string]any
				Raw     []byte
			}{Version: 1, Data: doc, Raw: []byte(content)})
		} else {
			var selection rules.Selection
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &selection); err != nil {
				t.Fatal(err)
			}
			err = gob.NewEncoder(file).Encode(selection)
		}
		closeErr := file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		return
	default:
		data = []byte(content)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	err = yaml.Unmarshal(data, &out)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func proxyGroupNamesFromConfig(config map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, raw := range config["proxy-groups"].([]any) {
		group := raw.(map[string]any)
		out[group["name"].(string)] = true
	}
	return out
}

func proxyGroupOrderFromConfig(config map[string]any) []string {
	var out []string
	for _, raw := range config["proxy-groups"].([]any) {
		group := raw.(map[string]any)
		out = append(out, group["name"].(string))
	}
	return out
}

func proxyGroupFromConfig(t *testing.T, config map[string]any, name string) map[string]any {
	t.Helper()
	for _, raw := range config["proxy-groups"].([]any) {
		group := raw.(map[string]any)
		if group["name"] == name {
			return group
		}
	}
	t.Fatalf("missing proxy-group %q", name)
	return nil
}

func testStringSlice(raw any) []string {
	values := raw.([]any)
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.(string))
	}
	return out
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}

func assertNoSensitiveConfigMetadata(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, banned := range []string{"example.com", "password", "server", "cipher", "test"} {
		if strings.Contains(text, banned) {
			t.Fatalf("metadata leaked %q in %s", banned, text)
		}
	}
}
