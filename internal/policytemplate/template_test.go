package policytemplate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"localclash/internal/localconfig"
	"localclash/internal/rules"
	"localclash/internal/smartpolicy"

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

func TestRealMinimalTemplateDescribesOptionalG204WithoutChangingSmartPriority(t *testing.T) {
	config, _, err := Build(filepath.Join("..", "..", DefaultDir), TemplateMinimal)
	if err != nil {
		t.Fatal(err)
	}
	auto := config.ProxyGroups["⚡ 自动选择"]
	if auto.Reason != "面向 Dashboard 的免维护自动出口；默认使用完整可选订阅节点，可在订阅设置中启用逐节点 generate_204 资格筛选。" {
		t.Fatalf("minimal automatic reason = %q", auto.Reason)
	}
	if auto.Boundary != "all_selectable_subscription_nodes_with_optional_g204_filter" {
		t.Fatalf("minimal automatic boundary = %q", auto.Boundary)
	}
	if len(auto.SmartPriority) != 6 || auto.SmartPriority[0].Label != "HK" || auto.SmartPriority[0].Factor != 6 || auto.SmartPriority[5].Label != "Other" || auto.SmartPriority[5].Factor != 1 {
		t.Fatalf("minimal automatic Smart priority changed: %+v", auto.SmartPriority)
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
	if len(config.ProxyGroups) != 9 || len(config.PolicyGroups) != 31 || len(config.Packs) != 35 || len(config.TransportRules) != 1 || len(config.CustomRules) != 3 {
		t.Fatalf("default template counts: proxy_groups=%d policy_groups=%d packs=%d transport_rules=%d custom_rules=%d, want 9/31/35/1/3", len(config.ProxyGroups), len(config.PolicyGroups), len(config.Packs), len(config.TransportRules), len(config.CustomRules))
	}
	if got := packTarget(config.Packs, "v2fly-dlc", "category-pt"); got != "🧲 BT/PT 下载" {
		t.Fatalf("default template category-pt target = %q, want 🧲 BT/PT 下载", got)
	}
	if _, exists := config.ProxyGroups["STEAM"]; exists {
		t.Fatalf("default template still has flat STEAM proxy group: %+v", config.ProxyGroups["STEAM"])
	}
	if group := config.ProxyGroups["🎯 手动选择"]; group.Mode != "manual" || group.Match == nil || group.Match.Pattern != ".*" {
		t.Fatalf("default template manual selector = %+v, want explicit all-node selector", group)
	}
	auto := config.ProxyGroups["⚡ 自动选择"]
	wantAutoPriority := []smartpolicy.Rule{
		{Label: "HK", Pattern: `(🇭🇰|香港|(^|[^A-Za-z])HK([^A-Za-z]|$)|Hong.?Kong)`, Factor: 6},
		{Label: "JP", Pattern: `(🇯🇵|日本|(^|[^A-Za-z])JP([^A-Za-z]|$)|Japan)`, Factor: 5},
		{Label: "SG", Pattern: `(🇸🇬|新加坡|狮城|獅城|(^|[^A-Za-z])SG([^A-Za-z]|$)|Singapore)`, Factor: 4},
		{Label: "TW", Pattern: `(🇹🇼|台湾|台灣|(^|[^A-Za-z])TW([^A-Za-z]|$)|Taiwan)`, Factor: 3},
		{Label: "US", Pattern: `(🇺🇸|美国|美國|(^|[^A-Za-z])US([^A-Za-z]|$)|United.?States|America)`, Factor: 2},
		{Label: "Other", Pattern: `.*`, Factor: 1},
	}
	if auto.Mode != "auto" || auto.Capability != "network.connectivity.g204.v1" || auto.Match != nil || !reflect.DeepEqual(auto.SmartPriority, wantAutoPriority) {
		t.Fatalf("default template auto selector = %+v, want optional-g204 Smart selector", auto)
	}
	if auto.Reason != "面向 Dashboard 的免维护自动出口；默认使用完整可选订阅节点，可在订阅设置中启用逐节点 generate_204 资格筛选。" {
		t.Fatalf("default template automatic reason = %q", auto.Reason)
	}
	if auto.Boundary != "all_selectable_subscription_nodes_with_optional_g204_filter" {
		t.Fatalf("default template automatic boundary = %q", auto.Boundary)
	}
	if !config.ProxyGroups["🇭🇰 香港节点"].Optional {
		t.Fatalf("香港节点 group = %+v, want optional region selector", config.ProxyGroups["🇭🇰 香港节点"])
	}
	chatGPTAvailable := config.ProxyGroups["ChatGPT-available"]
	if chatGPTAvailable.Mode != "auto" || chatGPTAvailable.Capability != "openai.chatgpt.statsig.v1" || !chatGPTAvailable.Optional {
		t.Fatalf("ChatGPT-available group = %+v, want optional ChatGPT capability auto group", chatGPTAvailable)
	}
	wantPriority := []smartpolicy.Rule{
		{Label: "US", Pattern: `(🇺🇸|美国|美國|(^|[^A-Za-z])US([^A-Za-z]|$)|United.?States|America)`, Factor: 5},
		{Label: "JP", Pattern: `(🇯🇵|日本|(^|[^A-Za-z])JP([^A-Za-z]|$)|Japan)`, Factor: 4},
		{Label: "SG", Pattern: `(🇸🇬|新加坡|狮城|獅城|(^|[^A-Za-z])SG([^A-Za-z]|$)|Singapore)`, Factor: 3},
		{Label: "TW", Pattern: `(🇹🇼|台湾|台灣|(^|[^A-Za-z])TW([^A-Za-z]|$)|Taiwan)`, Factor: 2},
		{Label: "Other", Pattern: `.*`, Factor: 1},
	}
	if !reflect.DeepEqual(chatGPTAvailable.SmartPriority, wantPriority) {
		t.Fatalf("ChatGPT-available smart priority = %#v, want %#v", chatGPTAvailable.SmartPriority, wantPriority)
	}
	chatGPT := config.PolicyGroups["🤖 ChatGPT"]
	if len(chatGPT.Exits) == 0 || chatGPT.Exits[len(chatGPT.Exits)-1] != "ChatGPT-available" {
		t.Fatalf("ChatGPT policy exits = %+v, want ChatGPT-available last", chatGPT.Exits)
	}
	globalDirect := config.PolicyGroups["🌐 全球直连"]
	wantGlobalDirectExits := []string{"DIRECT", "⚡ 自动选择", "🇭🇰 香港节点", "🇺🇸 美国节点", "🇯🇵 日本节点", "🇸🇬 新加坡节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点"}
	if globalDirect.Mode != "manual" || !reflect.DeepEqual(globalDirect.Exits, wantGlobalDirectExits) {
		t.Fatalf("全球直连 policy group = %+v, want default DIRECT with switchable exits %#v", globalDirect, wantGlobalDirectExits)
	}
	proxyGroups := make(map[string]rules.ProxyGroup, len(config.ProxyGroups))
	for id := range config.ProxyGroups {
		proxyGroups[id] = rules.ProxyGroup{}
	}
	policyGroups := make(map[string]rules.PolicyGroup, len(config.PolicyGroups))
	for id, group := range config.PolicyGroups {
		policyGroups[id] = rules.PolicyGroup{Exits: append([]string{}, group.Exits...)}
	}
	if err := rules.ValidatePolicyGroupGraph(proxyGroups, policyGroups); err != nil {
		t.Fatalf("default template policy graph is invalid: %v", err)
	}
	steam := config.PolicyGroups["🎮 Steam"]
	if steam.Mode != "manual" || len(steam.Exits) == 0 {
		t.Fatalf("Steam policy group = %+v, want business-to-exit selector", steam)
	}
	if _, exists := config.PolicyGroups["🎮 游戏"]; exists {
		t.Fatalf("default template still has old game policy group name")
	}
	quic := config.PolicyGroups["🚦 QUIC"]
	wantQUICExits := []string{"REJECT", "🎯 手动选择", "⚡ 自动选择", "🇭🇰 香港节点", "🇯🇵 日本节点", "🇺🇸 美国节点", "DIRECT"}
	if quic.Mode != "manual" || !reflect.DeepEqual(quic.Exits, wantQUICExits) {
		t.Fatalf("QUIC policy group = %+v, want exact default-reject candidates %#v", quic, wantQUICExits)
	}
	if got := config.TransportRules[0]; got.ID != "quic-udp-443-main" || got.Network != "UDP" || got.DstPort != 443 || got.Target != "🚦 QUIC" {
		t.Fatalf("transport rule = %+v, want QUIC UDP/443 target", got)
	}
	wantExitsByGroup := map[string][]string{
		"🎮 Steam":    {"⚡ 自动选择", "🎯 手动选择", "🌐 全球直连", "🇭🇰 香港节点", "🇺🇸 美国节点", "🇯🇵 日本节点", "🇸🇬 新加坡节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点"},
		"🎮 游戏平台":     {"🌐 全球直连", "🎯 手动选择", "⚡ 自动选择", "🇭🇰 香港节点", "🇺🇸 美国节点", "🇯🇵 日本节点", "🇸🇬 新加坡节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点"},
		"🕹 Bahamut":  {"🇹🇼 台湾节点", "🎯 手动选择", "🌐 全球直连"},
		"🤖 ChatGPT":  {"🇺🇸 美国节点", "🇯🇵 日本节点", "🇸🇬 新加坡节点", "🎯 手动选择", "⚡ 自动选择", "🇹🇼 台湾节点", "🇰🇷 韩国节点", "ChatGPT-available"},
		"🧠 AI":       {"⚡ 自动选择", "🎯 手动选择", "🇸🇬 新加坡节点", "🇭🇰 香港节点", "🇺🇸 美国节点", "🇯🇵 日本节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点", "🌐 全球直连"},
		"📥 大模型下载":    {"🌐 全球直连", "⚡ 自动选择", "🇺🇸 美国节点", "🇭🇰 香港节点", "🇯🇵 日本节点", "🇸🇬 新加坡节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点"},
		"🍎 Apple":    {"🌐 全球直连", "🎯 手动选择", "⚡ 自动选择", "🇭🇰 香港节点", "🇺🇸 美国节点", "🇯🇵 日本节点", "🇸🇬 新加坡节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点"},
		"🧲 BT/PT 下载": {"🌐 全球直连", "⚡ 自动选择", "🎯 手动选择", "🇭🇰 香港节点", "🇯🇵 日本节点", "🇺🇸 美国节点", "🇸🇬 新加坡节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点"},
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
	autoFirstGroups := []string{
		"💬 通信服务", "👥 社交媒体",
		"🎮 Steam", "🧠 AI", "💻 GitHub",
		"📺 YouTube", "📺 Apple TV", "📬 Google FCM", "🔎 Google", "🎵 TikTok",
		"🎬 Netflix", "🏰 Disney", "🎞 HBO", "🎥 Prime Video", "📺 Emby", "🎧 Spotify",
		"🎞 媒体", "🛒 电商", "🌍 非中國網站",
		"☁️ Cloudflare",
	}
	for _, id := range autoFirstGroups {
		group := config.PolicyGroups[id]
		if len(group.Exits) < 2 || group.Exits[0] != "⚡ 自动选择" || group.Exits[1] != "🎯 手动选择" {
			t.Fatalf("policy group %q exits = %#v, want auto then manual defaults", id, group.Exits)
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
		"category-public-tracker":   "🧲 BT/PT 下载",
		"category-pt":               "🧲 BT/PT 下载",
		"category-social-media-!cn": "👥 社交媒体",
		"category-ai-!cn":           "🧠 AI",
		"geolocation-!cn":           "🌍 非中國網站",
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
	if got := packTarget(config.Packs, "syncnext", "SyncnextUnbreak"); got != "DIRECT" {
		t.Fatalf("SyncnextUnbreak target = %q, want DIRECT", got)
	}
	if got := packTarget(config.Packs, "syncnext", "SyncnextProxy"); got != "⚡ 自动选择" {
		t.Fatalf("SyncnextProxy target = %q, want ⚡ 自动选择", got)
	}
	if syncnextProxy, chinaDirect := packIndex(config.Packs, "syncnext", "SyncnextProxy"), packIndex(config.Packs, "v2fly-dlc", "cn"); syncnextProxy < 0 || chinaDirect < 0 || syncnextProxy >= chinaDirect {
		t.Fatalf("SyncnextProxy index=%d and cn index=%d, want SyncnextProxy before broad China direct routing", syncnextProxy, chinaDirect)
	}
	telegramRule := customRuleByID(config.CustomRules, "telegram-geoip")
	if telegramRule == nil || telegramRule.Target != "💬 通信服务" {
		t.Fatalf("telegram GEOIP custom rule = %+v, want 💬 通信服务", telegramRule)
	}
	if len(telegramRule.Rules) != 1 || telegramRule.Rules[0].Type != "geoip" || telegramRule.Rules[0].Value != "telegram" || !telegramRule.Rules[0].NoResolve {
		t.Fatalf("telegram GEOIP custom rule lines = %+v, want geoip telegram no-resolve", telegramRule.Rules)
	}
	cloudflareRule := customRuleByID(config.CustomRules, "cloudflare-geoip")
	if cloudflareRule == nil || cloudflareRule.Target != "☁️ Cloudflare" {
		t.Fatalf("Cloudflare GEOIP custom rule = %+v, want ☁️ Cloudflare", cloudflareRule)
	}
	if len(cloudflareRule.Rules) != 1 || cloudflareRule.Rules[0].Type != "geoip" || cloudflareRule.Rules[0].Value != "cloudflare" || !cloudflareRule.Rules[0].NoResolve {
		t.Fatalf("Cloudflare GEOIP custom rule lines = %+v, want geoip cloudflare no-resolve", cloudflareRule.Rules)
	}
	if hasPack(config.Packs, "v2fly-dlc", "cloudflare") {
		t.Fatal("default template must not add GEOSITE,cloudflare")
	}
	modelDownloadRule := customRuleByID(config.CustomRules, "large-model-download-hosts")
	if modelDownloadRule == nil || modelDownloadRule.Target != "📥 大模型下载" {
		t.Fatalf("large-model-download-hosts custom rule = %+v, want 📥 大模型下载", modelDownloadRule)
	}
	wantModelDownloadDomains := []string{
		"cas-server.xethub.hf.co",
		"cas-server.xethub-eu.hf.co",
		"transfer.xethub.hf.co",
		"transfer.xethub-eu.hf.co",
		"us.aws.cdn.hf.co",
		"us.gcp.cdn.hf.co",
		"cdn-lfs-us-1.hf.co",
		"cdn-lfs-eu-1.hf.co",
		"cdn-lfs-cn-1.modelscope.cn",
		"cdn-lfs-ap-1.modelscope.ai",
		"registry.ollama.ai",
	}
	if len(modelDownloadRule.Rules) != len(wantModelDownloadDomains) {
		t.Fatalf("large-model-download-hosts rules = %+v, want %d exact domains", modelDownloadRule.Rules, len(wantModelDownloadDomains))
	}
	for i, wantDomain := range wantModelDownloadDomains {
		if got := modelDownloadRule.Rules[i]; got.Type != "domain" || got.Value != wantDomain || got.NoResolve {
			t.Fatalf("large-model-download-hosts rule[%d] = %+v, want exact domain %q", i, got, wantDomain)
		}
	}
	if got := config.Packs[len(config.Packs)-2].Target; got != "🌍 非中國網站" {
		t.Fatalf("geolocation non-China target = %q, want 🌍 非中國網站", got)
	}
	if got := config.Packs[len(config.Packs)-2].Pack; got != "geolocation-!cn" {
		t.Fatalf("geolocation fallback pack = %q, want geolocation-!cn", got)
	}
	if config.FallbackTarget != "🌐 全球直连" {
		t.Fatalf("fallback_target = %q, want switchable global direct policy", config.FallbackTarget)
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

func packIndex(packs []localconfig.Pack, source, name string) int {
	for i, pack := range packs {
		if pack.Source == source && pack.Pack == name {
			return i
		}
	}
	return -1
}

func customRuleByID(customRules []localconfig.CustomRule, id string) *localconfig.CustomRule {
	for i := range customRules {
		if customRules[i].ID == id {
			return &customRules[i]
		}
	}
	return nil
}
