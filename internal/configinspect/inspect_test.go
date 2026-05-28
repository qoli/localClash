package configinspect

import (
	"encoding/gob"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localclash/internal/rules"

	"gopkg.in/yaml.v3"
)

func TestInspectBaseReturnsBaseSummary(t *testing.T) {
	path := writeInspectConfig(t, true, true)

	result, err := InspectBase(Options{ConfigPath: path, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Layer != "base" || result.Modifiable {
		t.Fatalf("layer/modifiable = %s/%v, want base/false", result.Layer, result.Modifiable)
	}
	if result.Summary.ProxiesCount != 2 {
		t.Fatalf("proxies count = %d, want 2", result.Summary.ProxiesCount)
	}
	if len(result.Summary.ProxyGroups) != 1 || result.Summary.ProxyGroups[0].Name != "PROXY" {
		t.Fatalf("proxy groups = %+v, want limited PROXY", result.Summary.ProxyGroups)
	}
	if len(result.Summary.RuleProviders) != 1 {
		t.Fatalf("rule providers = %+v, want limit 1", result.Summary.RuleProviders)
	}
	if result.Summary.RulesCount != 1 || len(result.Summary.RulesSample) != 1 {
		t.Fatalf("rules summary = count %d sample %+v, want count 1 sample 1", result.Summary.RulesCount, result.Summary.RulesSample)
	}
	if result.Summary.RulesSample[0] != "RULE-SET,applications,DIRECT" {
		t.Fatalf("rules sample = %+v, want base applications rule only", result.Summary.RulesSample)
	}
	assertInspectNoSensitiveLeak(t, result)
	assertInspectJSONExcludes(t, result, "rules_sample", "RULE-SET,", "blackmatrix7_OpenAI", `"name":"applications"`)
}

func TestInspectBaseMissingConfigReturnsClearError(t *testing.T) {
	_, err := InspectBase(Options{ConfigPath: filepath.Join(t.TempDir(), "missing.yaml")})
	if err == nil {
		t.Fatal("expected missing config error")
	}
	if !strings.Contains(err.Error(), "run config_render first") {
		t.Fatalf("error = %q, want config_render hint", err.Error())
	}
}

func TestInspectOverlayWithMetadataReturnsOverlay(t *testing.T) {
	path := writeInspectConfig(t, true, true)

	result, err := InspectOverlay(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if result.Layer != "overlay" || !result.Modifiable {
		t.Fatalf("layer/modifiable = %s/%v, want overlay/true", result.Layer, result.Modifiable)
	}
	if !result.MetadataPresent || !result.OverlayPresent {
		t.Fatalf("metadata/overlay present = %v/%v, want true/true", result.MetadataPresent, result.OverlayPresent)
	}
	if len(result.Packs) != 1 || result.Packs[0].Source != "blackmatrix7" || result.Packs[0].Pack != "OpenAI" {
		t.Fatalf("packs = %+v, want OpenAI pack", result.Packs)
	}
	if len(result.ProxyGroups) != 1 || result.ProxyGroups[0].ID != "AI" {
		t.Fatalf("proxy groups = %+v, want AI", result.ProxyGroups)
	}
	if len(result.RuleProviders) != 1 || result.RuleProviders[0].Name != "blackmatrix7_OpenAI" {
		t.Fatalf("providers = %+v, want blackmatrix7_OpenAI", result.RuleProviders)
	}
	if len(result.Rules) != 1 || result.Rules[0].Target != "AI" {
		t.Fatalf("rules = %+v, want AI target", result.Rules)
	}
	assertInspectNoSensitiveLeak(t, result)
	assertInspectJSONExcludes(t, result, "blackmatrix7_OpenAI", `"provider":`, `"name":`)
}

func TestInspectOverlayWithoutMetadataDoesNotGuess(t *testing.T) {
	path := writeInspectConfig(t, false, false)

	result, err := InspectOverlay(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if result.MetadataPresent || result.OverlayPresent {
		t.Fatalf("metadata/overlay present = %v/%v, want false/false", result.MetadataPresent, result.OverlayPresent)
	}
	if len(result.Packs) != 0 || len(result.RuleProviders) != 0 || len(result.Rules) != 0 {
		t.Fatalf("overlay result = %+v, want empty arrays", result)
	}
}

func TestInspectOverlayWithEmptyMetadataReturnsNoOverlay(t *testing.T) {
	path := writeInspectConfig(t, true, false)

	result, err := InspectOverlay(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if !result.MetadataPresent || result.OverlayPresent {
		t.Fatalf("metadata/overlay present = %v/%v, want true/false", result.MetadataPresent, result.OverlayPresent)
	}
	if len(result.Packs) != 0 || len(result.ProxyGroups) != 0 || len(result.RuleProviders) != 0 || len(result.Rules) != 0 {
		t.Fatalf("overlay arrays = %+v, want empty", result)
	}
	assertInspectNoSensitiveLeak(t, result)
}

func TestInspectIntentMissingConfigReturnsEmptyIntent(t *testing.T) {
	result, err := InspectIntent(IntentOptions{ConfigPath: filepath.Join(t.TempDir(), "localclash.json")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Exists || result.Valid || result.Resolved {
		t.Fatalf("intent state = exists %v valid %v resolved %v, want all false", result.Exists, result.Valid, result.Resolved)
	}
	if len(result.ProxyGroups) != 0 || len(result.CustomRules) != 0 || len(result.Packs) != 0 {
		t.Fatalf("intent arrays = %+v, want empty", result)
	}
}

func TestInspectIntentReturnsResolvedDurableIntent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "localclash.json")
	subscriptionPath := filepath.Join(dir, "subscription.gob")
	rulesCache := filepath.Join(dir, ".runtime", "rules", "packs")
	writeInspectFile(t, configPath, `version: 1
proxy_groups:
  AI:
    mode: manual
    match:
      type: name_regex
      pattern: SG
      min: 1
    reason: Use Singapore-labelled nodes for AI.
    boundary: name_based_hint_only
custom_rules:
  - id: huggingface_temp
    target: AI
    reason: Route Hugging Face through the AI group.
    rules:
      - type: domain_suffix
        value: huggingface.co
packs:
  - source: blackmatrix7
    pack: OpenAI
    target: AI
    reason: Route OpenAI domains through AI.
`)
	writeInspectFile(t, subscriptionPath, `proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`)
	if err := rules.WritePackIndex(rules.PackIndexPath(rulesCache), map[string]rules.PackCache{
		"blackmatrix7": {
			Version:    1,
			Source:     "blackmatrix7",
			Adapter:    "blackmatrix7",
			Renderable: true,
			Packs: []rules.Pack{{
				ID:         "OpenAI",
				Name:       "OpenAI",
				Target:     "AI",
				Renderable: true,
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := InspectIntent(IntentOptions{ConfigPath: configPath, Subscription: subscriptionPath, RulesCache: rulesCache})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Exists || !result.Valid || !result.Resolved {
		t.Fatalf("intent state = exists %v valid %v resolved %v, want true", result.Exists, result.Valid, result.Resolved)
	}
	if len(result.ProxyGroups) != 1 || result.ProxyGroups[0].ID != "AI" || result.ProxyGroups[0].Status != "resolved" {
		t.Fatalf("proxy groups = %+v, want resolved AI", result.ProxyGroups)
	}
	if len(result.ProxyGroups[0].SelectedNodes) != 1 || result.ProxyGroups[0].SelectedNodes[0] != "SG 01" {
		t.Fatalf("selected nodes = %+v, want SG 01", result.ProxyGroups[0].SelectedNodes)
	}
	if len(result.CustomRules) != 1 || result.CustomRules[0].RuleCount != 1 || result.CustomRules[0].Target != "AI" {
		t.Fatalf("custom rules = %+v, want one AI rule", result.CustomRules)
	}
	if len(result.Packs) != 1 || result.Packs[0].Source != "blackmatrix7" || result.Packs[0].Pack != "OpenAI" || result.Packs[0].Status != "resolved" {
		t.Fatalf("packs = %+v, want resolved OpenAI pack", result.Packs)
	}
	assertInspectNoSensitiveLeak(t, result)
}

func TestInspectIntentReportsResolveErrorWithoutLosingRawGroups(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "localclash.json")
	subscriptionPath := filepath.Join(dir, "subscription.gob")
	writeInspectFile(t, configPath, `version: 1
proxy_groups:
  TempLine:
    mode: manual
    nodes:
      - Missing Node
    reason: User explicitly chose this line.
`)
	writeInspectFile(t, subscriptionPath, `proxies:
  - name: SG 01
    type: ss
`)

	result, err := InspectIntent(IntentOptions{ConfigPath: configPath, Subscription: subscriptionPath})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Exists || !result.Valid || result.Resolved || result.ResolveError == "" {
		t.Fatalf("intent state = %+v, want valid unresolved with resolve_error", result)
	}
	if len(result.ProxyGroups) != 1 || result.ProxyGroups[0].ID != "TempLine" || result.ProxyGroups[0].NodeCount != 1 {
		t.Fatalf("proxy groups = %+v, want raw TempLine", result.ProxyGroups)
	}
	if result.ProxyGroups[0].Nodes[0] != "Missing Node" {
		t.Fatalf("nodes = %+v, want missing raw node preserved", result.ProxyGroups[0].Nodes)
	}
}

func assertInspectJSONExcludes(t *testing.T, value any, forbidden ...string) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("inspect JSON is not serializable: %v", err)
	}
	text := string(data)
	for _, needle := range forbidden {
		if strings.Contains(text, needle) {
			t.Fatalf("inspect JSON contains %q: %s", needle, text)
		}
	}
}

func writeInspectConfig(t *testing.T, metadata bool, overlay bool) string {
	t.Helper()
	content := `
mode: rule
external-controller: 127.0.0.1:9090
external-ui: ui/zashboard
proxies:
  - name: JP 01
    type: ss
    server: jp.example.com
    password: secret
  - name: SG 01
    type: trojan
    server: sg.example.com
    password: secret2
proxy-groups:
  - name: PROXY
    type: select
    proxies: [JP 01, SG 01]
  - name: ⚡ 自动选择
    type: url-test
    proxies: [JP 01, SG 01]
rule-providers:
  applications:
    type: http
    behavior: classical
    url: https://example.com/applications.yaml
  blackmatrix7_OpenAI:
    type: http
    behavior: classical
    url: https://example.com/OpenAI.yaml
rules:
  - RULE-SET,applications,DIRECT
  - RULE-SET,blackmatrix7_OpenAI,AI
`
	if metadata {
		content += `
x-localclash:
  version: 1
  base:
    modifiable: false
    description: localClash generated base config
  overlay:
    modifiable: true
    packs:
`
		if overlay {
			content += `      - source: blackmatrix7
        pack: OpenAI
        target: AI
    proxy_groups:
      - id: AI
        mode: manual
        nodes: [SG 01, JP Tokyo 01, US 01]
    rule_providers:
      - name: blackmatrix7_OpenAI
        behavior: classical
        type: http
    rules:
      - type: RULE-SET
        provider: blackmatrix7_OpenAI
        target: AI
`
		} else {
			content += `    proxy_groups: []
    rule_providers: []
    rules: []
`
		}
		content += `    insertion: after local safety baseline, before DIRECT fallback
`
	}
	path := filepath.Join(t.TempDir(), "mihomo.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeInspectFile(t *testing.T, path string, content string) {
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

func assertInspectNoSensitiveLeak(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, banned := range []string{"jp.example.com", "sg.example.com", "secret", "secret2", "password", "server"} {
		if strings.Contains(text, banned) {
			t.Fatalf("inspection leaked %q in %s", banned, text)
		}
	}
}
