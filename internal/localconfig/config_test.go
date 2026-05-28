package localconfig

import (
	"encoding/gob"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"localclash/internal/rules"

	"gopkg.in/yaml.v3"
)

func TestResolveNameRegexUsesSourceArtifactsAndMergeNames(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "localclash-subscriptions.json")
	runtimeDir := filepath.Join(dir, ".runtime", "subscriptions")
	writeTestFile(t, configPath, `sources:
  - id: main
  - id: backup
`)
	writeTestFile(t, filepath.Join(runtimeDir, "main.gob"), `proxies:
  - name: HK 01
    type: ss
  - name: SG 01
    type: ss
`)
	writeTestFile(t, filepath.Join(runtimeDir, "backup.gob"), `proxies:
  - name: HK 01
    type: ss
`)
	rulesCache := filepath.Join(dir, "rules")
	writeTestPackCache(t, rulesCache, "blackmatrix7", "blackmatrix7", testRulePack("Steam", "DIRECT"))

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"SteamHK": {
					Mode:  "manual",
					Match: &Match{Type: "name_regex", Pattern: "HK", SourceIDs: []string{"main"}, Min: 1},
				},
				"MainSG": {
					Mode:  "manual",
					Match: &Match{Type: "name_regex", Pattern: "SG", SourceIDs: []string{"main"}, Min: 1},
				},
			},
			Packs: []Pack{{Source: "blackmatrix7", Pack: "Steam", Target: "SteamHK"}},
		},
		SubscriptionPath:    filepath.Join(dir, "subscription.gob"),
		SubscriptionConfig:  configPath,
		SubscriptionRuntime: runtimeDir,
		RulesCache:          rulesCache,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	got := resolved.Config.ProxyGroups["SteamHK"].SelectedNodes
	want := []string{"[main] HK 01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected nodes = %#v, want %#v", got, want)
	}
	got = resolved.Config.ProxyGroups["MainSG"].SelectedNodes
	want = []string{"[main] SG 01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected nodes = %#v, want %#v", got, want)
	}
}

func TestLoadRejectsLegacyPackID(t *testing.T) {
	var config Config
	err := json.Unmarshal([]byte(`{"version":3,"packs":[{"id":"v2fly_dlc_geolocation__cn","target":"DIRECT"}]}`), &config)
	if err == nil || !strings.Contains(err.Error(), "packs[].id is no longer supported; use packs[].source and packs[].pack") {
		t.Fatalf("error = %v, want legacy pack id rejection", err)
	}
}

func TestResolveFallbackTarget(t *testing.T) {
	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			FallbackTarget: "Catchall",
			ProxyGroups: map[string]ProxyGroup{
				"Catchall": {Mode: "direct"},
			},
		},
		SubscriptionNodes: []SubscriptionNode{{Name: "SG 01"}},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolved.Selection.FallbackTarget != "Catchall" || resolved.Config.FallbackTarget != "Catchall" {
		t.Fatalf("fallback = selection %q config %q, want Catchall", resolved.Selection.FallbackTarget, resolved.Config.FallbackTarget)
	}
}

func TestLoadSubscriptionNodesDoesNotFallbackWhenConfiguredSourceArtifactIsMissing(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "localclash-subscriptions.json")
	writeTestFile(t, configPath, `sources:
  - id: main
`)
	merged := filepath.Join(dir, "subscription.gob")
	writeTestFile(t, merged, `proxies:
  - name: Merged Only
    type: ss
`)

	_, err := LoadSubscriptionNodes(SubscriptionNodeOptions{
		SubscriptionPath:    merged,
		SubscriptionConfig:  configPath,
		SubscriptionRuntime: filepath.Join(dir, ".runtime", "subscriptions"),
	})
	if err == nil || !strings.Contains(err.Error(), "main.gob") {
		t.Fatalf("error = %v, want missing source artifact error", err)
	}
}

func TestResolveEnabledLocalRulePacks(t *testing.T) {
	dir := t.TempDir()
	rulePacksDir := filepath.Join(dir, "rule-packs")
	writeTestFile(t, filepath.Join(rulePacksDir, "ai.json"), `{
  "id": "ai",
  "name": "AI services",
  "version": 1,
  "default_target": "DIRECT",
  "target_options": ["DIRECT", "AI"],
  "rules": [
    {"domain_suffix": "openai.com"},
    {"ip_cidr": "1.1.1.0/24", "no_resolve": true}
  ]
}`)

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			EnabledRulePacks: []RulePackSelection{{ID: "ai", Reason: "local AI routing"}},
		},
		SubscriptionNodes: []SubscriptionNode{{Name: "SG 01"}},
		LocalRulePacksDir: rulePacksDir,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(resolved.RulePacks) != 1 || resolved.RulePacks[0].Target != "DIRECT" || resolved.RulePacks[0].RuleCount != 2 {
		t.Fatalf("rule packs = %+v, want resolved local rule pack", resolved.RulePacks)
	}
	if len(resolved.Selection.LocalRulePacks) != 1 || resolved.Selection.LocalRulePacks[0].ID != "ai" {
		t.Fatalf("selection local rule packs = %+v, want ai metadata", resolved.Selection.LocalRulePacks)
	}
	if len(resolved.Selection.CustomRules) != 1 || resolved.Selection.CustomRules[0].ID != "rule_pack:ai" {
		t.Fatalf("selection custom rules = %+v, want rendered rule pack custom rule", resolved.Selection.CustomRules)
	}
	fragment, err := rules.RenderFragment(resolved.Selection, nil, []string{"SG 01"})
	if err != nil {
		t.Fatalf("RenderFragment returned error: %v", err)
	}
	want := []string{"DOMAIN-SUFFIX,openai.com,DIRECT", "IP-CIDR,1.1.1.0/24,DIRECT,no-resolve"}
	if !reflect.DeepEqual(fragment.Rules, want) {
		t.Fatalf("fragment rules = %#v, want %#v", fragment.Rules, want)
	}
}

func TestResolveEnabledRulePacksFromLocalFiles(t *testing.T) {
	dir := t.TempDir()
	rulePacksDir := filepath.Join(dir, "rule-packs")
	writeTestFile(t, filepath.Join(rulePacksDir, "ai.json"), `{
  "id": "ai",
  "name": "AI Services",
  "version": 1,
  "default_target": "AI",
  "target_options": ["AI", "DIRECT"],
  "rules": [
    {"domain_suffix": "openai.com"},
    {"type": "domain", "value": "chatgpt.com"}
  ]
}`)

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"AI": {Mode: "direct"},
			},
			EnabledRulePacks: []RulePackSelection{{ID: "ai"}},
		},
		SubscriptionNodes: []SubscriptionNode{{Name: "JP 01", Type: "ss"}},
		LocalRulePacksDir: rulePacksDir,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(resolved.RulePacks) != 1 || resolved.RulePacks[0].RuleCount != 2 || resolved.RulePacks[0].Target != "AI" {
		t.Fatalf("rule packs = %+v, want resolved ai pack", resolved.RulePacks)
	}
	if len(resolved.Selection.LocalRulePacks) != 1 || resolved.Selection.LocalRulePacks[0].ID != "ai" {
		t.Fatalf("selection local packs = %+v, want ai", resolved.Selection.LocalRulePacks)
	}
	fragment, err := rules.RenderFragment(resolved.Selection, map[string]rules.PackCache{}, []string{"JP 01"})
	if err != nil {
		t.Fatalf("RenderFragment returned error: %v", err)
	}
	if len(fragment.Rules) < 2 || fragment.Rules[0] != "DOMAIN-SUFFIX,openai.com,AI" || fragment.Rules[1] != "DOMAIN,chatgpt.com,AI" {
		t.Fatalf("fragment rules = %+v, want local rule pack rules", fragment.Rules)
	}
}

func TestResolveEnabledRulePacksRejectsUnsupportedTargetOption(t *testing.T) {
	dir := t.TempDir()
	rulePacksDir := filepath.Join(dir, "rule-packs")
	writeTestFile(t, filepath.Join(rulePacksDir, "ai.json"), `{
  "id": "ai",
  "version": 1,
  "target_options": ["DIRECT"],
  "rules": [{"domain_suffix": "openai.com"}]
}`)

	_, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"AI": {Mode: "direct"},
			},
			EnabledRulePacks: []RulePackSelection{{ID: "ai", Target: "AI"}},
		},
		SubscriptionNodes: []SubscriptionNode{{Name: "JP 01", Type: "ss"}},
		LocalRulePacksDir: rulePacksDir,
	})
	if err == nil || !strings.Contains(err.Error(), "not listed in target_options") {
		t.Fatalf("error = %v, want target_options error", err)
	}
}

func TestResolveNameRegexEnforcesMin(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.gob")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: SG 01
    type: ss
`)

	_, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"SteamHK": {Mode: "manual", Match: &Match{Type: "name_regex", Pattern: "HK", Min: 1}},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	if err == nil {
		t.Fatal("Resolve succeeded, want min-match error")
	}
}

func TestResolveExactNodesSupportsExplicitHumanChoice(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.gob")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: HK 01
    type: ss
  - name: HK Dedicated
    type: ss
`)

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"SteamHK": {Mode: "manual", Nodes: []string{"HK Dedicated"}},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	got := resolved.Config.ProxyGroups["SteamHK"].SelectedNodes
	want := []string{"HK Dedicated"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected nodes = %#v, want %#v", got, want)
	}
}

func TestResolveProxyGroupSupportsSmartMode(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.gob")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: SG 01
    type: ss
`)

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"AI": {Mode: "smart", Nodes: []string{"SG 01"}},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !resolved.Selection.ProxyGroups["AI"].Smart {
		t.Fatalf("selection proxy group = %+v, want smart", resolved.Selection.ProxyGroups["AI"])
	}
}

func TestResolveProxyGroupSupportsDirectMode(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.gob")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: SG 01
    type: ss
`)

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"全球直连": {Mode: "direct"},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !resolved.Selection.ProxyGroups["全球直连"].Direct {
		t.Fatalf("selection proxy group = %+v, want direct", resolved.Selection.ProxyGroups["全球直连"])
	}
	if got := resolved.ProxyGroups[0].NodeCount; got != 0 {
		t.Fatalf("direct group node count = %d, want 0", got)
	}
}

func TestResolvePolicyGroupTargetsProxyGroupExits(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.gob")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: HK 01
    type: ss
  - name: JP Tokyo 01
    type: ss
`)
	rulesCache := filepath.Join(dir, "rules")
	writeTestPackCache(t, rulesCache, "blackmatrix7", "blackmatrix7", testRulePack("Steam", "DIRECT"))

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"HK": {Mode: "manual", Nodes: []string{"HK 01"}},
				"JP": {Mode: "auto", Nodes: []string{"JP Tokyo 01"}},
			},
			PolicyGroups: map[string]PolicyGroup{
				"Steam": {Mode: "manual", Exits: []string{"HK", "JP", "DIRECT"}},
			},
			Packs: []Pack{{Source: "blackmatrix7", Pack: "Steam", Target: "Steam"}},
		},
		SubscriptionPath: subscriptionPath,
		RulesCache:       rulesCache,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	exits := resolved.Selection.PolicyGroups["Steam"].Exits
	want := []string{"HK", "JP", "DIRECT"}
	if !reflect.DeepEqual(exits, want) {
		t.Fatalf("policy exits = %#v, want %#v", exits, want)
	}
	if len(resolved.PolicyGroups) != 1 || resolved.PolicyGroups[0].ExitCount != 3 {
		t.Fatalf("resolved policy groups = %+v, want one group with three exits", resolved.PolicyGroups)
	}
}

func TestResolveRejectsLowercaseTerminalPolicyExit(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.gob")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: HK 01
    type: ss
`)
	_, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"HK": {Mode: "manual", Nodes: []string{"HK 01"}},
			},
			PolicyGroups: map[string]PolicyGroup{
				"Steam": {Mode: "manual", Exits: []string{"HK", "direct"}},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	if err == nil || !strings.Contains(err.Error(), `policy group "Steam" exit "direct" requires a terminal action or matching proxy group`) {
		t.Fatalf("error = %v, want lowercase terminal exit rejected", err)
	}
}

func TestResolveOptionalProxyGroupCanBeEmpty(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.gob")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: HK 01
    type: ss
`)
	rulesCache := filepath.Join(dir, "rules")
	writeTestPackCache(t, rulesCache, "v2fly-dlc", "v2fly-dlc", rules.Pack{
		ID:         "steam",
		Renderable: true,
		Components: []rules.Component{{
			ID:         "domain",
			Behavior:   "v2fly-dlc",
			Format:     "text",
			OrderClass: "domain",
			URL:        "https://example.com/steam",
			Path:       "./rule-packs/v2fly-dlc/steam.txt",
		}},
	})

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"香港节点": {Mode: "auto", Match: &Match{Type: "name_regex", Pattern: "HK"}},
				"韩国节点": {Mode: "auto", Match: &Match{Type: "name_regex", Pattern: "KR"}, Optional: true},
			},
			PolicyGroups: map[string]PolicyGroup{
				"Steam": {Mode: "manual", Exits: []string{"香港节点", "韩国节点", "DIRECT"}},
			},
			Packs: []Pack{{Source: "v2fly-dlc", Pack: "steam", Target: "Steam"}},
		},
		SubscriptionPath: subscriptionPath,
		RulesCache:       rulesCache,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got := resolved.Selection.ProxyGroups["韩国节点"].Nodes; len(got) != 0 {
		t.Fatalf("optional group nodes = %#v, want empty", got)
	}
	if !resolved.Selection.ProxyGroups["韩国节点"].Optional {
		t.Fatalf("optional flag was not preserved: %+v", resolved.Selection.ProxyGroups["韩国节点"])
	}
}

func TestResolveExactNodesReportsMissingNodes(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.gob")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: HK 01
    type: ss
`)

	_, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"SteamHK": {Mode: "manual", Nodes: []string{"HK Dedicated", "HK Backup"}},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	var missing *MissingNodesError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v, want MissingNodesError", err)
	}
	want := []string{"HK Dedicated", "HK Backup"}
	if missing.GroupID != "SteamHK" || !reflect.DeepEqual(missing.Nodes, want) {
		t.Fatalf("missing = %+v, want group SteamHK nodes %#v", missing, want)
	}
}

func TestResolveProxyGroupRejectsAmbiguousMatchAndNodes(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.gob")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: HK 01
    type: ss
`)

	_, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"SteamHK": {
					Mode:  "manual",
					Match: &Match{Type: "name_regex", Pattern: "HK"},
					Nodes: []string{"HK 01"},
				},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	if err == nil {
		t.Fatal("Resolve succeeded, want ambiguous match/nodes error")
	}
}

func TestResolveEmitsStageTimings(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "localclash-subscriptions.json")
	runtimeDir := filepath.Join(dir, ".runtime", "subscriptions")
	writeTestFile(t, configPath, `sources:
  - id: main
`)
	writeTestFile(t, filepath.Join(runtimeDir, "main.gob"), `proxies:
  - name: HK 01
    type: ss
`)
	rulesCache := filepath.Join(dir, "rules")
	writeTestPackCache(t, rulesCache, "blackmatrix7", "blackmatrix7", testRulePack("Steam", "DIRECT"))

	var events []StageEvent
	_, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"香港节点": {Mode: "manual", Match: &Match{Type: "name_regex", Pattern: "HK"}},
			},
			PolicyGroups: map[string]PolicyGroup{
				"Steam": {Mode: "manual", Exits: []string{"香港节点", "DIRECT"}},
			},
			Packs: []Pack{{Source: "blackmatrix7", Pack: "Steam", Target: "Steam"}},
		},
		SubscriptionPath:    filepath.Join(dir, "subscription.gob"),
		SubscriptionConfig:  configPath,
		SubscriptionRuntime: runtimeDir,
		RulesCache:          rulesCache,
		OnStage: func(event StageEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	assertStageEvent(t, events, "load_subscription_nodes", "done")
	assertStageEvent(t, events, "load_subscription_nodes.read_subscription_source_artifact", "done")
	assertStageEvent(t, events, "resolve_proxy_group", "done")
	assertStageEvent(t, events, "resolve_policy_group", "done")
	assertStageEvent(t, events, "resolve_pack", "done")
	assertStageEvent(t, events, "resolve_enabled_rule_packs", "done")
	assertStageEvent(t, events, "resolve_rule_providers", "done")
}

func TestResolveEmitsComplexityCounters(t *testing.T) {
	var events []StageEvent
	_, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"HK": {Mode: "manual", Match: &Match{Type: "name_regex", Pattern: "HK", Min: 1}},
				"JP": {Mode: "manual", Match: &Match{Type: "name_regex", Pattern: "JP", Min: 1}},
			},
		},
		SubscriptionNodes: []SubscriptionNode{
			{Name: "HK 01", SourceID: "S-1"},
			{Name: "HK 02", SourceID: "S-1"},
			{Name: "JP 01", SourceID: "S-1"},
		},
		OnStage: func(event StageEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	event := findStageEvent(t, events, "resolve_proxy_groups", "done")
	assertStageField(t, event, "regex_compiles", 2)
	assertStageField(t, event, "match_node_scans", 6)
	assertStageField(t, event, "regex_match_attempts", 6)
	assertStageField(t, event, "selected_node_appends", 3)
}

func assertStageEvent(t *testing.T, events []StageEvent, stage, event string) {
	t.Helper()
	_ = findStageEvent(t, events, stage, event)
}

func findStageEvent(t *testing.T, events []StageEvent, stage, event string) StageEvent {
	t.Helper()
	for _, got := range events {
		if got.Stage == stage && got.Event == event {
			return got
		}
	}
	names := make([]string, 0, len(events))
	for _, got := range events {
		names = append(names, got.Stage+":"+got.Event)
	}
	t.Fatalf("missing stage event %s:%s in %s", stage, event, strings.Join(names, ", "))
	return StageEvent{}
}

func assertStageField(t *testing.T, event StageEvent, key string, want int) {
	t.Helper()
	got, ok := event.Fields[key].(int)
	if !ok {
		t.Fatalf("%s field = %#v, want int %d in %+v", key, event.Fields[key], want, event.Fields)
	}
	if got != want {
		t.Fatalf("%s = %d, want %d in %+v", key, got, want, event.Fields)
	}
}

func testRulePack(id, target string) rules.Pack {
	return rules.Pack{
		ID:         id,
		Name:       id,
		Target:     target,
		Renderable: true,
		Components: []rules.Component{{
			ID:         id,
			Behavior:   "classical",
			Format:     "yaml",
			OrderClass: "mixed",
			URL:        "https://example.com/" + id + ".yaml",
			Path:       "./rule-packs/test/" + id + ".yaml",
		}},
	}
}

func writeTestPackCache(t *testing.T, dir, source, adapter string, packs ...rules.Pack) {
	t.Helper()
	if err := rules.WritePackIndex(rules.PackIndexPath(dir), map[string]rules.PackCache{
		source: {
			Version:    1,
			Source:     source,
			Adapter:    adapter,
			Renderable: true,
			Packs:      packs,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
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
		encodeErr := gob.NewEncoder(file).Encode(struct {
			Version int
			Data    map[string]any
			Raw     []byte
		}{Version: 1, Data: doc, Raw: []byte(content)})
		closeErr := file.Close()
		if encodeErr != nil {
			t.Fatal(encodeErr)
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
