package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderFragmentUsesSelectionAndPackCache(t *testing.T) {
	selection := Selection{
		ProxyGroups: map[string]ProxyGroup{
			"⚡ 自动选择": {Nodes: []string{"JP 01"}, Auto: true},
		},
		EnabledPack: []SelectedPack{
			{Source: "blackmatrix7", Pack: "OpenAI", Target: "⚡ 自动选择"},
		},
	}
	caches := map[string]PackCache{
		"blackmatrix7": {
			Source:  "blackmatrix7",
			Adapter: "blackmatrix7",
			Packs: []Pack{
				{
					ID:         "OpenAI",
					Renderable: true,
					Components: []Component{
						{
							ID:         "OpenAI",
							Behavior:   "classical",
							Format:     "yaml",
							OrderClass: "mixed",
							URL:        "https://example.com/OpenAI.yaml",
							Path:       "./rule-packs/blackmatrix7/OpenAI.yaml",
						},
					},
				},
			},
		},
	}

	fragment, err := RenderFragment(selection, caches, []string{"JP 01"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fragment.RuleProviders) != 1 {
		t.Fatalf("providers = %d, want 1", len(fragment.RuleProviders))
	}
	if got := fragment.Rules[0]; got != "RULE-SET,blackmatrix7_OpenAI,⚡ 自动选择" {
		t.Fatalf("rule = %q, want RULE-SET,blackmatrix7_OpenAI,⚡ 自动选择", got)
	}
}

func TestRenderFragmentRendersV2FlyDLCAsGeoSite(t *testing.T) {
	selection := Selection{
		ProxyGroups: map[string]ProxyGroup{
			"⚡ 自动选择": {Nodes: []string{"JP 01"}, Auto: true},
		},
		EnabledPack: []SelectedPack{
			{Source: "v2fly-dlc", Pack: "google", Target: "⚡ 自动选择"},
		},
	}
	caches := map[string]PackCache{
		"v2fly-dlc": {
			Source: "v2fly-dlc",
			Packs: []Pack{
				{
					ID:         "google",
					Renderable: true,
					Components: []Component{{
						ID:       "domain",
						Behavior: "v2fly-dlc",
						Format:   "text",
						URL:      "https://example.com/google",
						Path:     "./rule-packs/v2fly-dlc/google.txt",
					}},
				},
			},
		},
	}

	fragment, err := RenderFragment(selection, caches, []string{"JP 01"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fragment.RuleProviders) != 0 {
		t.Fatalf("providers = %+v, want none for GEOSITE pack", fragment.RuleProviders)
	}
	if got := fragment.Rules[0]; got != "GEOSITE,google,⚡ 自动选择" {
		t.Fatalf("rule = %q, want GEOSITE google rule", got)
	}
}

func TestRenderFragmentRendersV2FlyDLCGeoSiteAttribute(t *testing.T) {
	selection := Selection{EnabledPack: []SelectedPack{
		{Source: "v2fly-dlc", Pack: "category-games@cn", Target: "DIRECT"},
	}}
	caches := map[string]PackCache{
		"v2fly-dlc": {
			Source: "v2fly-dlc",
			Packs: []Pack{
				{
					ID:         "category-games",
					Renderable: true,
					Components: []Component{{
						ID:       "domain",
						Behavior: "v2fly-dlc",
						Format:   "text",
						URL:      "https://example.com/category-games",
						Path:     "./rule-packs/v2fly-dlc/category-games.txt",
					}},
				},
			},
		},
	}

	fragment, err := RenderFragment(selection, caches)
	if err != nil {
		t.Fatal(err)
	}
	if got := fragment.Rules[0]; got != "GEOSITE,category-games@cn,DIRECT" {
		t.Fatalf("rule = %q, want GEOSITE attribute rule", got)
	}
}

func TestRenderFragmentRejectsMissingV2FlyDLCGeoSiteSelectorBase(t *testing.T) {
	selection := Selection{EnabledPack: []SelectedPack{
		{Source: "v2fly-dlc", Pack: "category-games@cn", Target: "DIRECT"},
	}}
	caches := map[string]PackCache{
		"v2fly-dlc": {
			Source: "v2fly-dlc",
			Packs: []Pack{
				{
					ID:         "category-social-media",
					Renderable: true,
					Components: []Component{{
						ID:       "domain",
						Behavior: "v2fly-dlc",
						Format:   "text",
						URL:      "https://example.com/category-social-media",
						Path:     "./rule-packs/v2fly-dlc/category-social-media.txt",
					}},
				},
			},
		},
	}

	_, err := RenderFragment(selection, caches)
	if err == nil || !strings.Contains(err.Error(), `pack "category-games@cn" not found`) {
		t.Fatalf("error = %v, want missing exact pack", err)
	}
}

func TestSelectionWithProxyGroupParses(t *testing.T) {
	raw := []byte(`
version: 1
proxy_groups:
  AI:
    nodes: [JP Tokyo 01, SG 01, US 01]
    manual: true
    direct: false
enabled_packs:
  - source: sukkaw
    pack: ai
    target: AI
`)
	var selection Selection
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &selection); err != nil {
		t.Fatal(err)
	}
	if !selection.ProxyGroups["AI"].Manual || selection.ProxyGroups["AI"].Auto {
		t.Fatalf("AI proxy group = %+v, want manual only", selection.ProxyGroups["AI"])
	}
}

func TestLoadSourcesIgnoresMacOSMetadataFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "blackmatrix7.json"), []byte(`{
  "id": "blackmatrix7",
  "adapter": "blackmatrix7",
  "url": "https://example.com/index.yaml",
  "raw_base_url": "https://example.com/raw"
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "._blackmatrix7.json"), []byte{0, 5, 'b', 'a', 'd'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.json"), []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy.yaml"), []byte(`
id: blackmatrix7
adapter: blackmatrix7
url: https://example.com/index.yaml
raw_base_url: https://example.com/raw
`), 0o644); err != nil {
		t.Fatal(err)
	}

	sources, err := LoadSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].ID != "blackmatrix7" {
		t.Fatalf("sources = %+v, want only blackmatrix7", sources)
	}
}

func TestLoadPackIndexReadsGobIndexOnly(t *testing.T) {
	dir := t.TempDir()
	if err := WritePackIndex(PackIndexPath(dir), map[string]PackCache{
		"blackmatrix7": {
			Source:  "blackmatrix7",
			Adapter: "blackmatrix7",
			Packs: []Pack{{
				ID:         "OpenAI",
				Name:       "OpenAI",
				Target:     "AI",
				Renderable: true,
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blackmatrix7.yaml"), []byte("not valid cache yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	index, err := LoadPackIndex(PackIndexPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Caches) != 1 {
		t.Fatalf("caches = %+v, want one cache", index.Caches)
	}
	if _, ok := index.Caches["blackmatrix7"]; !ok {
		t.Fatalf("caches = %+v, want blackmatrix7", index.Caches)
	}
}

func TestLoadPackIndexMissingGobIndexHardFailsFromCacheDir(t *testing.T) {
	_, err := LoadPackIndex(PackIndexPath(t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "pack index not found: run localclash rules adapt") {
		t.Fatalf("err = %v, want missing pack index hard fail", err)
	}
}

func TestDirectTreeChildrenReturnsOnlyRequestedDirectoryFiles(t *testing.T) {
	entries := directTreeChildren([]githubTreeEntry{
		{Path: "data/youtube", Type: "blob"},
		{Path: "data/steam", Type: "blob"},
		{Path: "data/nested/child", Type: "blob"},
		{Path: "data/category", Type: "tree"},
		{Path: "README.md", Type: "blob"},
	}, "data")

	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want three direct data children", entries)
	}
	for _, want := range []string{"category", "steam", "youtube"} {
		found := false
		for _, entry := range entries {
			if entry.Name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("entries = %+v, missing %q", entries, want)
		}
	}
}

func TestRenderFragmentMaterializesProxyGroup(t *testing.T) {
	selection := Selection{
		ProxyGroups: map[string]ProxyGroup{
			"AI": {
				Nodes:  []string{"🇯🇵 Tokyo", "SG Singapore", "🇺🇸 US"},
				Manual: true,
				Direct: false,
			},
		},
		EnabledPack: []SelectedPack{
			{Source: "sukkaw", Pack: "ai", Target: "AI"},
			{Source: "blackmatrix7", Pack: "OpenAI", Target: "AI"},
		},
	}
	fragment, err := RenderFragment(selection, testPackCaches(), []string{"🇯🇵 Tokyo", "SG Singapore", "🇺🇸 US"})
	if err != nil {
		t.Fatal(err)
	}
	if got := fragment.Rules[0]; got != "RULE-SET,sukkaw_ai_non_ip,AI" {
		t.Fatalf("first rule = %q, want target AI", got)
	}
	if got := fragment.Rules[1]; got != "RULE-SET,blackmatrix7_OpenAI,AI" {
		t.Fatalf("second rule = %q, want target AI", got)
	}
	groupNames := proxyGroupNames(fragment.ProxyGroups)
	if !groupNames["AI"] {
		t.Fatalf("missing proxy group AI in %+v", groupNames)
	}
	for _, unwanted := range []string{"AI_AUTO", "AI_MANUAL", "JP", "SG", "US"} {
		if groupNames[unwanted] {
			t.Fatalf("%q should not be materialized as a proxy group", unwanted)
		}
	}
}

func TestRenderFragmentMaterializesPolicyGroupOverProxyGroupExits(t *testing.T) {
	selection := Selection{
		ProxyGroups: map[string]ProxyGroup{
			"HK": {
				Nodes:  []string{"HK 01"},
				Manual: true,
			},
			"JP": {
				Nodes: []string{"JP Tokyo"},
				Auto:  true,
			},
		},
		PolicyGroups: map[string]PolicyGroup{
			"Steam": {
				Exits:  []string{"HK", "JP", "DIRECT"},
				Manual: true,
			},
		},
		EnabledPack: []SelectedPack{{Source: "blackmatrix7", Pack: "OpenAI", Target: "Steam"}},
	}

	fragment, err := RenderFragment(selection, testPackCaches(), []string{"HK 01", "JP Tokyo"})
	if err != nil {
		t.Fatal(err)
	}
	if got := fragment.Rules[0]; got != "RULE-SET,blackmatrix7_OpenAI,Steam" {
		t.Fatalf("rule = %q, want policy group target", got)
	}
	if len(fragment.ProxyGroups) != 3 {
		t.Fatalf("proxy groups = %+v, want HK, JP, and Steam", fragment.ProxyGroups)
	}
	groups := map[string]map[string]any{}
	for _, group := range fragment.ProxyGroups {
		groups[group["name"].(string)] = group
	}
	steam := groups["Steam"]
	if steam["type"] != "select" {
		t.Fatalf("Steam group = %+v, want select policy group", steam)
	}
	exits := steam["proxies"].([]string)
	want := []string{"HK", "JP", "DIRECT"}
	if len(exits) != len(want) {
		t.Fatalf("Steam exits = %+v, want %+v", exits, want)
	}
	for i := range want {
		if exits[i] != want[i] {
			t.Fatalf("Steam exits = %+v, want %+v", exits, want)
		}
	}
	if groups["HK"] == nil || groups["JP"] == nil {
		t.Fatalf("leaf exit groups missing from %+v", groups)
	}
}

func TestRenderFragmentSortsPolicyGroupsBeforeRegionGroupsByDisplayName(t *testing.T) {
	selection := Selection{
		ProxyGroups: map[string]ProxyGroup{
			"🇭🇰 香港节点": {
				Nodes:    []string{"HK 01"},
				Auto:     true,
				Optional: true,
			},
			"🌐 全球直连": {
				Direct: true,
			},
		},
		PolicyGroups: map[string]PolicyGroup{
			"🎮 Steam": {
				Exits:  []string{"🌐 全球直连", "🇭🇰 香港节点"},
				Manual: true,
			},
			"🧠 AI": {
				Exits:  []string{"🌐 全球直连", "🇭🇰 香港节点"},
				Manual: true,
			},
		},
		EnabledPack: []SelectedPack{
			{Source: "blackmatrix7", Pack: "OpenAI", Target: "🎮 Steam"},
			{Source: "sukkaw", Pack: "ai", Target: "🧠 AI"},
		},
	}

	fragment, err := RenderFragment(selection, testPackCaches(), []string{"HK 01"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, group := range fragment.ProxyGroups {
		names = append(names, group["name"].(string))
	}
	want := []string{"🧠 AI", "🎮 Steam", "🌐 全球直连", "🇭🇰 香港节点"}
	if len(names) != len(want) {
		t.Fatalf("proxy group names = %+v, want %+v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("proxy group names = %+v, want %+v", names, want)
		}
	}
	if len(fragment.BaseManualChoices) != 1 || fragment.BaseManualChoices[0] != "🇭🇰 香港节点" {
		t.Fatalf("base manual choices = %+v, want region group", fragment.BaseManualChoices)
	}
}

func TestRenderFragmentSkipsEmptyOptionalPolicyExit(t *testing.T) {
	selection := Selection{
		ProxyGroups: map[string]ProxyGroup{
			"HK": {
				Nodes: []string{"HK 01"},
				Auto:  true,
			},
			"KR": {
				Auto:     true,
				Optional: true,
			},
		},
		PolicyGroups: map[string]PolicyGroup{
			"Steam": {
				Exits:  []string{"HK", "KR", "DIRECT"},
				Manual: true,
			},
		},
		EnabledPack: []SelectedPack{{Source: "blackmatrix7", Pack: "OpenAI", Target: "Steam"}},
	}

	fragment, err := RenderFragment(selection, testPackCaches(), []string{"HK 01"})
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]map[string]any{}
	for _, group := range fragment.ProxyGroups {
		groups[group["name"].(string)] = group
	}
	if groups["KR"] != nil {
		t.Fatalf("empty optional KR group should not be materialized: %+v", fragment.ProxyGroups)
	}
	steam := groups["Steam"]
	if steam == nil {
		t.Fatalf("missing Steam policy group in %+v", fragment.ProxyGroups)
	}
	exits := steam["proxies"].([]string)
	want := []string{"HK", "DIRECT"}
	if len(exits) != len(want) {
		t.Fatalf("Steam exits = %+v, want %+v", exits, want)
	}
	for i := range want {
		if exits[i] != want[i] {
			t.Fatalf("Steam exits = %+v, want %+v", exits, want)
		}
	}
}

func TestRenderFragmentMaterializesSmartProxyGroup(t *testing.T) {
	selection := Selection{
		ProxyGroups: map[string]ProxyGroup{
			"AI": {
				Nodes: []string{"SG Singapore", "JP Tokyo"},
				Smart: true,
			},
		},
		EnabledPack: []SelectedPack{{Source: "sukkaw", Pack: "ai", Target: "AI"}},
	}
	fragment, err := RenderFragment(selection, testPackCaches(), []string{"SG Singapore", "JP Tokyo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fragment.ProxyGroups) != 1 {
		t.Fatalf("proxy groups = %+v, want one smart group", fragment.ProxyGroups)
	}
	group := fragment.ProxyGroups[0]
	if group["type"] != "smart" || group["uselightgbm"] != true || group["prefer-asn"] != true {
		t.Fatalf("smart group = %+v, want smart defaults", group)
	}
}

func TestRenderFragmentRendersCustomRulesBeforePacks(t *testing.T) {
	selection := Selection{
		ProxyGroups: map[string]ProxyGroup{
			"TempLine": {
				Nodes:  []string{"SG Singapore"},
				Manual: true,
			},
		},
		CustomRules: []CustomRule{
			{
				ID:     "huggingface_temp",
				Target: "TempLine",
				Rules: []CustomRuleLine{
					{Type: "domain_suffix", Value: "huggingface.co"},
				},
			},
		},
		EnabledPack: []SelectedPack{
			{Source: "sukkaw", Pack: "ai", Target: "TempLine"},
		},
	}
	fragment, err := RenderFragment(selection, testPackCaches(), []string{"SG Singapore"})
	if err != nil {
		t.Fatal(err)
	}
	if got := fragment.Rules[0]; got != "DOMAIN-SUFFIX,huggingface.co,TempLine" {
		t.Fatalf("first rule = %q, want custom rule before packs", got)
	}
	if got := fragment.Rules[1]; got != "RULE-SET,sukkaw_ai_non_ip,TempLine" {
		t.Fatalf("second rule = %q, want pack after custom rule", got)
	}
	if !proxyGroupNames(fragment.ProxyGroups)["TempLine"] {
		t.Fatalf("missing proxy group TempLine in %+v", fragment.ProxyGroups)
	}
}

func TestRenderFragmentRejectsConflictingProxyGroupModes(t *testing.T) {
	selection := Selection{
		ProxyGroups: map[string]ProxyGroup{
			"AI": {
				Nodes:  []string{"🇯🇵 Tokyo"},
				Auto:   true,
				Manual: true,
			},
		},
		EnabledPack: []SelectedPack{
			{Source: "sukkaw", Pack: "ai", Target: "AI"},
		},
	}
	if _, err := RenderFragment(selection, testPackCaches(), []string{"🇯🇵 Tokyo"}); err == nil {
		t.Fatal("expected conflicting proxy group mode error")
	}
}

func TestRenderFragmentRejectsUnknownTarget(t *testing.T) {
	selection := Selection{EnabledPack: []SelectedPack{
		{Source: "sukkaw", Pack: "ai", Target: "MISSING"},
	}}
	if _, err := RenderFragment(selection, testPackCaches()); err == nil {
		t.Fatal("expected unknown target error")
	}
}

func TestRenderFragmentRejectsLegacyProxyAlias(t *testing.T) {
	selection := Selection{EnabledPack: []SelectedPack{
		{Source: "sukkaw", Pack: "ai", Target: "PROXY"},
	}}
	if _, err := RenderFragment(selection, testPackCaches()); err == nil || !strings.Contains(err.Error(), `unknown pack target "PROXY"`) {
		t.Fatalf("error = %v, want legacy PROXY alias rejected", err)
	}
}

func TestRenderFragmentRejectsMissingProxyGroupNode(t *testing.T) {
	selection := Selection{
		ProxyGroups: map[string]ProxyGroup{
			"AI": {
				Nodes:  []string{"🇯🇵 Tokyo"},
				Manual: true,
			},
		},
		EnabledPack: []SelectedPack{
			{Source: "sukkaw", Pack: "ai", Target: "AI"},
		},
	}
	if _, err := RenderFragment(selection, testPackCaches(), []string{"HK 01"}); err == nil {
		t.Fatal("expected missing proxy group node error")
	}
}

func TestValidateSourceRequiresAdapterFields(t *testing.T) {
	if err := validateSource(Source{ID: "sukkaw", Adapter: "sukkaw", URL: "https://github.com/SukkaW/Surge"}); err == nil {
		t.Fatal("expected missing base_url error")
	}
	if err := validateSource(Source{ID: "blackmatrix7", Adapter: "blackmatrix7", URL: "https://github.com/blackmatrix7/ios_rule_script/tree/master/rule/Clash"}); err == nil {
		t.Fatal("expected missing raw_base_url error")
	}
	if err := validateSource(Source{ID: "syncnext", Adapter: "syncnext", URL: "https://github.com/qoli/SyncnextClash"}); err == nil {
		t.Fatal("expected missing raw_base_url error")
	}
}

func TestGitHubAPIContentsURL(t *testing.T) {
	got, err := githubAPIContentsURL("https://github.com/blackmatrix7/ios_rule_script/tree/master/rule/Clash")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://api.github.com/repos/blackmatrix7/ios_rule_script/contents/rule/Clash?ref=master"
	if got != want {
		t.Fatalf("api url = %q, want %q", got, want)
	}
}

func TestGitHubTreeAPIURL(t *testing.T) {
	got, repoPath, err := githubTreeAPIURL("https://github.com/v2fly/domain-list-community/tree/master/data")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.github.com/repos/v2fly/domain-list-community/git/trees/master?recursive=1" {
		t.Fatalf("api url = %q", got)
	}
	if repoPath != "data" {
		t.Fatalf("repoPath = %q, want data", repoPath)
	}
}

func TestAdaptSyncnextBuildsAppMaintenancePacks(t *testing.T) {
	cache := adaptSyncnext(Source{
		ID:         "syncnext",
		Adapter:    "syncnext",
		URL:        "https://github.com/qoli/SyncnextClash",
		RawBaseURL: "https://raw.githubusercontent.com/qoli/SyncnextClash/main",
	})
	if !cache.Renderable || len(cache.Packs) != 2 {
		t.Fatalf("cache = %+v, want two renderable packs", cache)
	}
	if cache.Packs[0].ID != "SyncnextProxy" || cache.Packs[0].Target != "⚡ 自动选择" {
		t.Fatalf("first pack = %+v, want SyncnextProxy target ⚡ 自动选择", cache.Packs[0])
	}
	if cache.Packs[1].ID != "SyncnextUnbreak" || cache.Packs[1].Target != "DIRECT" {
		t.Fatalf("second pack = %+v, want SyncnextUnbreak target DIRECT", cache.Packs[1])
	}
	if got := cache.Packs[0].Components[0].URL; got != "https://raw.githubusercontent.com/qoli/SyncnextClash/main/proxy-classical.yaml" {
		t.Fatalf("proxy URL = %q", got)
	}
	if got := cache.Packs[1].Components[0].URL; got != "https://raw.githubusercontent.com/qoli/SyncnextClash/main/Unbreak-classical.yaml" {
		t.Fatalf("unbreak URL = %q", got)
	}
}

func TestSortComponentsUsesRulesetOrder(t *testing.T) {
	components := []Component{
		{ID: "ip", OrderClass: "ip"},
		{ID: "domainset", OrderClass: "domainset"},
		{ID: "non_ip", OrderClass: "non_ip"},
	}
	sortComponents(components)
	if got := components[0].OrderClass; got != "domainset" {
		t.Fatalf("first = %q, want domainset", got)
	}
	if got := components[1].OrderClass; got != "non_ip" {
		t.Fatalf("second = %q, want non_ip", got)
	}
	if got := components[2].OrderClass; got != "ip" {
		t.Fatalf("third = %q, want ip", got)
	}
}

func testPackCaches() map[string]PackCache {
	return map[string]PackCache{
		"sukkaw": {
			Source: "sukkaw",
			Packs: []Pack{
				{
					ID:         "ai",
					Renderable: true,
					Components: []Component{
						{
							ID:         "non_ip",
							Behavior:   "classical",
							Format:     "text",
							OrderClass: "non_ip",
							URL:        "https://example.com/ai.txt",
							Path:       "./rule-packs/sukkaw/ai_non_ip.txt",
						},
					},
				},
			},
		},
		"blackmatrix7": {
			Source: "blackmatrix7",
			Packs: []Pack{
				{
					ID:         "OpenAI",
					Renderable: true,
					Components: []Component{
						{
							ID:         "OpenAI",
							Behavior:   "classical",
							Format:     "yaml",
							OrderClass: "mixed",
							URL:        "https://example.com/OpenAI.yaml",
							Path:       "./rule-packs/blackmatrix7/OpenAI.yaml",
						},
					},
				},
			},
		},
	}
}

func proxyGroupNames(groups []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, group := range groups {
		if name, ok := group["name"].(string); ok {
			out[name] = true
		}
	}
	return out
}
