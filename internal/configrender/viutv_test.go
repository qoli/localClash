package configrender

import (
	"path/filepath"
	"reflect"
	"testing"

	"localclash/internal/localconfig"
	"localclash/internal/policytemplate"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"
)

func TestRenderDefaultViuTVHongKongRouting(t *testing.T) {
	for _, tc := range []struct {
		name      string
		core      string
		nodes     []string
		wantExits []string
		wantType  string
	}{
		{"meta_hong_kong_available", runtimeprofile.CoreMeta, []string{"HK fixture 01", "HK fixture 02", "JP fixture"}, []string{"🇭🇰 香港节点", "DIRECT"}, "url-test"},
		{"meta_hong_kong_absent", runtimeprofile.CoreMeta, []string{"JP fixture"}, []string{"DIRECT"}, ""},
		{"smart_hong_kong_available", runtimeprofile.CoreSmart, []string{"HK fixture 01", "HK fixture 02", "JP fixture"}, []string{"🇭🇰 香港节点", "DIRECT"}, "smart"},
		{"smart_hong_kong_absent", runtimeprofile.CoreSmart, []string{"JP fixture"}, []string{"DIRECT"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, _, err := policytemplate.Build(filepath.Join("..", "..", policytemplate.DefaultDir), policytemplate.TemplateLocalClashDefault)
			if err != nil {
				t.Fatal(err)
			}
			group := config.PolicyGroups["📺 ViuTV"]
			if group.Mode != "manual" || !reflect.DeepEqual(group.Exits, []string{"🇭🇰 香港节点", "DIRECT"}) {
				t.Fatalf("ViuTV policy = %+v, want shared Hong Kong and direct exits", group)
			}
			sharedHK := config.ProxyGroups["🇭🇰 香港节点"]
			if sharedHK.Mode != "auto" || !sharedHK.Optional {
				t.Fatalf("shared Hong Kong selector changed: %+v", sharedHK)
			}
			if _, exists := config.ProxyGroups["🇭🇰 ViuTV 香港节点"]; exists {
				t.Fatal("ViuTV must reuse the shared Hong Kong selector")
			}
			var viuRules []localconfig.CustomRuleLine
			for _, custom := range config.CustomRules {
				if custom.ID == "viutv-playback-hosts" {
					if custom.Target != "📺 ViuTV" {
						t.Fatalf("ViuTV target = %q", custom.Target)
					}
					viuRules = custom.Rules
				}
			}
			// The suffix covers the site, api.viu.tv and static.viu.tv certificate;
			// shared now.com/nowe.com and the separate Viu.com service stay outside.
			wantRules := []localconfig.CustomRuleLine{
				{Type: "domain_suffix", Value: "viu.tv"},
				{Type: "domain", Value: "api.viu.now.com"},
				{Type: "domain", Value: "cdn01r.now.com"},
				{Type: "domain", Value: "cdn01.now.com"},
				{Type: "domain", Value: "fwp.nowe.com"},
			}
			if !reflect.DeepEqual(viuRules, wantRules) {
				t.Fatalf("ViuTV endpoint coverage = %+v, want %+v", viuRules, wantRules)
			}

			// Keep the real template's groups, custom rules and tail targets;
			// unrelated packs need no network downloads for this priority regression.
			var tailPacks []localconfig.Pack
			var cachedPacks []rules.Pack
			for _, pack := range config.Packs {
				if pack.Source == "v2fly-dlc" && (pack.Pack == "category-entertainment" || pack.Pack == "geolocation-!cn" || pack.Pack == "cn") {
					tailPacks = append(tailPacks, pack)
					cachedPacks = append(cachedPacks, rules.Pack{ID: pack.Pack, Renderable: true, Components: []rules.Component{{ID: "domain", Behavior: "v2fly-dlc"}}})
				}
			}
			config.Packs = tailPacks
			paths := writeRenderFixture(t)
			profilePath := filepath.Join(paths.dir, "localclash-runtime.json")
			if _, err := runtimeprofile.Configure(profilePath, "", tc.core); err != nil {
				t.Fatal(err)
			}
			if err := rules.WritePackIndex(rules.PackIndexPath(paths.cacheDir), map[string]rules.PackCache{
				"v2fly-dlc": {Version: 1, Source: "v2fly-dlc", Adapter: "v2fly-dlc", Renderable: true, Packs: cachedPacks},
			}); err != nil {
				t.Fatal(err)
			}
			var nodes []localconfig.SubscriptionNode
			var proxies []any
			for _, name := range tc.nodes {
				nodes = append(nodes, localconfig.SubscriptionNode{Name: name})
				proxies = append(proxies, map[string]any{"name": name, "type": "ss", "server": "127.0.0.1", "port": 1, "cipher": "aes-128-gcm", "password": "fixture"})
			}
			resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
				Config: config, SubscriptionNodes: nodes, RulesCache: paths.cacheDir,
				CapabilityNodes: map[string][]string{"openai.chatgpt.oauth_statsig.v1": {"JP fixture"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := Render(Options{
				Source: map[string]any{"proxies": proxies}, OutputPath: filepath.Join(paths.dir, "viutv.yaml"),
				Selection: &resolved.Selection, RulesCacheDir: paths.cacheDir,
				RuntimeProfilePath: profilePath,
			})
			if err != nil {
				t.Fatal(err)
			}
			rendered := readTestYAML(t, result.OutputPath)
			viuGroup := proxyGroupFromConfig(t, rendered, "📺 ViuTV")
			if got := testStringSlice(viuGroup["proxies"]); viuGroup["type"] != "select" || !reflect.DeepEqual(got, tc.wantExits) {
				t.Fatalf("rendered ViuTV exits = %+v, want %+v", got, tc.wantExits)
			}
			if proxyGroupNamesFromConfig(rendered)["🇭🇰 ViuTV 香港节点"] {
				t.Fatal("rendered ViuTV must not have a dedicated Hong Kong selector")
			}
			if tc.wantType != "" {
				hk := proxyGroupFromConfig(t, rendered, "🇭🇰 香港节点")
				if got := testStringSlice(hk["proxies"]); hk["type"] != tc.wantType || !reflect.DeepEqual(got, []string{"HK fixture 01", "HK fixture 02"}) {
					t.Fatalf("shared Hong Kong selector = %+v, want %s with both HK fixtures", hk, tc.wantType)
				}
			} else if proxyGroupNamesFromConfig(rendered)["🇭🇰 香港节点"] {
				t.Fatal("empty optional Hong Kong exit must remain omitted")
			}
			renderedRules := testStringSlice(rendered["rules"])
			for _, want := range []string{
				"DOMAIN-SUFFIX,viu.tv,📺 ViuTV", "DOMAIN,api.viu.now.com,📺 ViuTV",
				"DOMAIN,cdn01r.now.com,📺 ViuTV", "DOMAIN,cdn01.now.com,📺 ViuTV", "DOMAIN,fwp.nowe.com,📺 ViuTV",
			} {
				index := indexOf(renderedRules, want)
				for _, later := range []string{"GEOIP,cloudflare,☁️ Cloudflare,no-resolve", "GEOSITE,category-entertainment,🎞 媒体", "GEOSITE,geolocation-!cn,🌍 非中國網站", "GEOSITE,cn,DIRECT"} {
					if other := indexOf(renderedRules, later); index < len(LocalBaselineRuleLines()) || other <= index {
						t.Fatalf("ViuTV rule %q at %d must precede %q at %d after safety baseline", want, index, later, other)
					}
				}
			}
			if renderedRules[0] != "DOMAIN,localhost,DIRECT" || renderedRules[len(renderedRules)-1] != "MATCH,🌐 全球直连" {
				t.Fatalf("safety baseline or default fallback changed: %+v", renderedRules)
			}
			direct := proxyGroupFromConfig(t, rendered, "🌐 全球直连")
			if got := testStringSlice(direct["proxies"]); len(got) == 0 || got[0] != "DIRECT" {
				t.Fatalf("global fallback must remain DIRECT first: %+v", got)
			}
			media := proxyGroupFromConfig(t, rendered, "🎞 媒体")
			if got := testStringSlice(media["proxies"]); len(got) == 0 || got[0] != "⚡ 自动选择" {
				t.Fatalf("broader media selection changed: %+v", got)
			}
		})
	}
}
