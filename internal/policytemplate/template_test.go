package policytemplate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"localclash/internal/localconfig"

	"gopkg.in/yaml.v3"
)

func TestBuildMinimalTemplate(t *testing.T) {
	dir := writeTemplateFixture(t)
	config, summary, err := Build(dir, TemplateMinimal)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ID != TemplateMinimal || config.PolicyTemplate != TemplateMinimal {
		t.Fatalf("template = %+v config = %+v, want minimal", summary, config)
	}
	if len(config.Packs) != 0 || len(config.ProxyGroups) != 0 {
		t.Fatalf("minimal config = %+v, want no packs or proxy groups", config)
	}
}

func TestBuildLocalClashDefaultTemplate(t *testing.T) {
	dir := writeTemplateFixture(t)
	config, summary, err := Build(dir, TemplateLocalClashDefault)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ID != TemplateLocalClashDefault || config.PolicyTemplate != TemplateLocalClashDefault {
		t.Fatalf("template = %+v config = %+v, want localclash default", summary, config)
	}
	if len(config.ProxyGroups) == 0 || len(config.Packs) == 0 {
		t.Fatalf("default config = %+v, want proxy groups and packs", config)
	}
	if config.Packs[0].Source != "v2fly-dlc" || config.Packs[0].Pack != "category-ads-all" || config.Packs[0].Target != "REJECT" {
		t.Fatalf("first pack = %+v, want ads reject first", config.Packs[0])
	}
	group := config.ProxyGroups["AI"]
	if group.Mode != "auto" || group.Match == nil || group.Match.Pattern != ".*" {
		t.Fatalf("AI group = %+v, want auto all-nodes selector", group)
	}
	if got := packTarget(config.Packs, "v2fly-dlc", "openai"); got != "AI" {
		t.Fatalf("openai pack target = %q, want AI", got)
	}
}

func TestBuildPatchSetTemplateMergesPatchesInManifestOrder(t *testing.T) {
	dir := t.TempDir()
	writePolicyTemplateTestFile(t, filepath.Join(dir, "localclash-default.json"), `id: localclash-default
name: localClash Default
description: Patch-set policy.
default: true
config:
  version: 1
  policy_template: localclash-default
patches:
  - id: default.region.v1
    path: localclash-default.d/00-region.json
  - id: default.ai.v1
    path: localclash-default.d/10-ai.json
  - id: default.ai-override.v1
    path: localclash-default.d/20-ai-override.json
`)
	writePolicyTemplateTestFile(t, filepath.Join(dir, "localclash-default.d", "00-region.json"), `id: default.region.v1
config:
  version: 2
  proxy_groups:
    AI:
      mode: auto
      match:
        type: name_regex
        pattern: .*
        min: 1
      reason: first definition
`)
	writePolicyTemplateTestFile(t, filepath.Join(dir, "localclash-default.d", "10-ai.json"), `id: default.ai.v1
config:
  version: 2
  policy_groups:
    ChatGPT:
      mode: manual
      exits:
        - AI
  packs:
    - source: v2fly-dlc
      pack: category-ads-all
      type: geosite
      target: REJECT
    - source: v2fly-dlc
      pack: openai
      type: geosite
      target: ChatGPT
`)
	writePolicyTemplateTestFile(t, filepath.Join(dir, "localclash-default.d", "20-ai-override.json"), `id: default.ai-override.v1
config:
  version: 2
  proxy_groups:
    AI:
      mode: manual
      nodes:
        - SG 01
      reason: override definition
  packs:
    - source: v2fly-dlc
      pack: openai
      type: geosite
      target: AI
`)

	config, summary, err := Build(dir, TemplateLocalClashDefault)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ID != TemplateLocalClashDefault || config.PolicyTemplate != TemplateLocalClashDefault {
		t.Fatalf("template = %+v config = %+v, want localclash default", summary, config)
	}
	if config.Version != localconfig.ConfigSchemaVersion {
		t.Fatalf("version = %d, want current config schema", config.Version)
	}
	group := config.ProxyGroups["AI"]
	if group.Mode != "manual" || len(group.Nodes) != 1 || group.Nodes[0] != "SG 01" {
		t.Fatalf("AI group = %+v, want later patch override", group)
	}
	if len(config.Packs) != 2 {
		t.Fatalf("packs = %+v, want ads plus deduped openai", config.Packs)
	}
	if config.Packs[0].Pack != "category-ads-all" || config.Packs[1].Pack != "openai" {
		t.Fatalf("pack order = %+v, want manifest order with replacement in place", config.Packs)
	}
	if config.Packs[1].Target != "AI" {
		t.Fatalf("openai pack = %+v, want later patch target replacement", config.Packs[1])
	}
}

func TestBuildPatchSetTemplateRejectsPatchIDMismatch(t *testing.T) {
	dir := t.TempDir()
	writePolicyTemplateTestFile(t, filepath.Join(dir, "localclash-default.json"), `id: localclash-default
name: localClash Default
description: Patch-set policy.
patches:
  - id: default.ai.v1
    path: localclash-default.d/10-ai.json
`)
	writePolicyTemplateTestFile(t, filepath.Join(dir, "localclash-default.d", "10-ai.json"), `id: wrong.id
config:
  version: 1
  packs: []
`)

	_, _, err := Build(dir, TemplateLocalClashDefault)
	if err == nil {
		t.Fatal("expected patch id mismatch error")
	}
}

func TestBuildRejectsEmptyTemplate(t *testing.T) {
	dir := t.TempDir()
	writePolicyTemplateTestFile(t, filepath.Join(dir, "localclash-default.json"), `id: localclash-default
name: localClash Default
description: Empty policy.
`)

	_, _, err := Build(dir, TemplateLocalClashDefault)
	if err == nil {
		t.Fatal("expected empty template error")
	}
}

func TestRealLocalClashDefaultTemplateIsLayered(t *testing.T) {
	config, summary, err := Build(filepath.Join("..", "..", DefaultDir), TemplateLocalClashDefault)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ID != TemplateLocalClashDefault || config.Version != localconfig.ConfigSchemaVersion {
		t.Fatalf("template = %+v config version = %d, want current localclash default", summary, config.Version)
	}
	if len(config.ProxyGroups) != 9 || len(config.PolicyGroups) != 26 || len(config.Packs) != 32 || len(config.CustomRules) != 1 {
		t.Fatalf("default template counts: proxy_groups=%d policy_groups=%d packs=%d custom_rules=%d, want 9/26/32/1", len(config.ProxyGroups), len(config.PolicyGroups), len(config.Packs), len(config.CustomRules))
	}
	if _, exists := config.ProxyGroups["STEAM"]; exists {
		t.Fatalf("default template still has flat STEAM proxy group: %+v", config.ProxyGroups["STEAM"])
	}
	if group := config.ProxyGroups["🎯 手动选择"]; group.Mode != "manual" || group.Match == nil || group.Match.Pattern != ".*" {
		t.Fatalf("default template manual selector = %+v, want explicit all-node selector", group)
	}
	if group := config.ProxyGroups["⚡ 自动选择"]; group.Mode != "auto" || group.Match == nil || group.Match.Pattern != ".*" {
		t.Fatalf("default template auto selector = %+v, want explicit all-node selector", group)
	}
	if !config.ProxyGroups["🇭🇰 香港节点"].Optional {
		t.Fatalf("香港节点 group = %+v, want optional region selector", config.ProxyGroups["🇭🇰 香港节点"])
	}
	steam := config.PolicyGroups["🎮 Steam"]
	if steam.Mode != "manual" || len(steam.Exits) == 0 {
		t.Fatalf("Steam policy group = %+v, want business-to-exit selector", steam)
	}
	if _, exists := config.PolicyGroups["🎮 游戏"]; exists {
		t.Fatalf("default template still has old game policy group name")
	}
	wantExitsByGroup := map[string][]string{
		"🎮 Steam":   {"🌐 全球直连", "🎯 手动选择", "⚡ 自动选择", "🇭🇰 香港节点", "🇺🇸 美国节点", "🇯🇵 日本节点", "🇸🇬 新加坡节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点"},
		"🎮 游戏平台":    {"🌐 全球直连", "🎯 手动选择", "⚡ 自动选择", "🇭🇰 香港节点", "🇺🇸 美国节点", "🇯🇵 日本节点", "🇸🇬 新加坡节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点"},
		"🕹 Bahamut": {"🇹🇼 台湾节点", "🎯 手动选择", "🌐 全球直连"},
		"🤖 ChatGPT": {"🎯 手动选择", "⚡ 自动选择", "🇸🇬 新加坡节点", "🇭🇰 香港节点", "🇺🇸 美国节点", "🇯🇵 日本节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点"},
		"🍎 Apple":   {"🌐 全球直连", "🎯 手动选择", "⚡ 自动选择", "🇭🇰 香港节点", "🇺🇸 美国节点", "🇯🇵 日本节点", "🇸🇬 新加坡节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点"},
	}
	for id, wantExits := range wantExitsByGroup {
		group, exists := config.PolicyGroups[id]
		if !exists {
			t.Fatalf("missing policy group %q", id)
		}
		if !reflect.DeepEqual(group.Exits, wantExits) {
			t.Fatalf("policy group %q exits = %#v, want %#v", id, group.Exits, wantExits)
		}
	}
	if config.Packs[0].Source != "v2fly-dlc" || config.Packs[0].Pack != "category-ads-all" || config.Packs[0].Target != "REJECT" {
		t.Fatalf("first pack = %+v, want ads reject first", config.Packs[0])
	}
	if got := packTarget(config.Packs, "v2fly-dlc", "category-games"); got != "🎮 游戏平台" {
		t.Fatalf("game category target = %q, want 🎮 游戏平台", got)
	}
	if got := packTarget(config.Packs, "v2fly-dlc", "telegram"); got != "💬 通信服务" {
		t.Fatalf("telegram target = %q, want 💬 通信服务", got)
	}
	wantExactPacks := map[string]string{
		"category-social-media-!cn": "👥 社交媒体",
		"category-ai-!cn":           "🧠 AI",
		"geolocation-!cn":           "🧭 漏网之鱼",
		"category-games@cn":         "DIRECT",
		"cn":                        "DIRECT",
	}
	for pack, target := range wantExactPacks {
		if got := packTarget(config.Packs, "v2fly-dlc", pack); got != target {
			t.Fatalf("pack %q target = %q, want %q", pack, got, target)
		}
	}
	for _, pack := range []string{"category-social-media-cn", "category-ai-cn", "geolocation-cn"} {
		if hasPack(config.Packs, "v2fly-dlc", pack) {
			t.Fatalf("default template should not include collapsed pack %q", pack)
		}
	}
	if got := customRuleTarget(config.CustomRules, "telegram-ip-ranges"); got != "💬 通信服务" {
		t.Fatalf("telegram IP custom rule target = %q, want 💬 通信服务", got)
	}
	if len(config.CustomRules[0].Rules) != 12 {
		t.Fatalf("telegram IP custom rule count = %d, want 12", len(config.CustomRules[0].Rules))
	}
	if got := config.Packs[len(config.Packs)-2].Target; got != "🧭 漏网之鱼" {
		t.Fatalf("geolocation fallback target = %q, want 🧭 漏网之鱼", got)
	}
	if got := config.Packs[len(config.Packs)-2].Pack; got != "geolocation-!cn" {
		t.Fatalf("geolocation fallback pack = %q, want geolocation-!cn", got)
	}
	if config.FallbackTarget != "🧭 漏网之鱼" {
		t.Fatalf("fallback_target = %q, want 🧭 漏网之鱼", config.FallbackTarget)
	}
}

func TestListTemplatesReadsDiskFiles(t *testing.T) {
	dir := writeTemplateFixture(t)
	templates, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 2 {
		t.Fatalf("templates = %+v, want two", templates)
	}
	if templates[0].Path == "" || templates[1].Path == "" {
		t.Fatalf("templates = %+v, want disk paths", templates)
	}
}

func writeTemplateFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writePolicyTemplateTestFile(t, filepath.Join(dir, "minimal.json"), `id: minimal
name: Minimal
description: Minimal policy.
config:
  version: 1
  policy_template: minimal
  proxy_groups: {}
  packs: []
`)
	writePolicyTemplateTestFile(t, filepath.Join(dir, "localclash-default.json"), `id: localclash-default
name: localClash Default
description: Patch-set default policy.
default: true
config:
  version: 1
  policy_template: localclash-default
patches:
  - id: default.ai.v1
    path: localclash-default.d/10-ai.json
`)
	writePolicyTemplateTestFile(t, filepath.Join(dir, "localclash-default.d", "10-ai.json"), `id: default.ai.v1
config:
  version: 1
  proxy_groups:
    AI:
      mode: auto
      match:
        type: name_regex
        pattern: .*
        min: 1
  packs:
    - source: v2fly-dlc
      pack: category-ads-all
      type: geosite
      target: REJECT
    - source: v2fly-dlc
      pack: openai
      type: geosite
      target: AI
`)
	return dir
}

func writePolicyTemplateTestFile(t *testing.T, path string, content string) {
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

func packTarget(packs []localconfig.Pack, source, name string) string {
	for _, pack := range packs {
		if pack.Source == source && pack.Pack == name {
			return pack.Target
		}
	}
	return ""
}

func hasPack(packs []localconfig.Pack, source, name string) bool {
	for _, pack := range packs {
		if pack.Source == source && pack.Pack == name {
			return true
		}
	}
	return false
}

func customRuleTarget(customRules []localconfig.CustomRule, id string) string {
	for _, rule := range customRules {
		if rule.ID == id {
			return rule.Target
		}
	}
	return ""
}
