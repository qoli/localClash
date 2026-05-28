package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListPacksReturnsSummaries(t *testing.T) {
	cacheDir := writeCatalogTestCaches(t)

	result, err := ListPacks(PackListOptions{CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || result.Returned != 3 {
		t.Fatalf("counts = %d/%d, want 3/3", result.Returned, result.Total)
	}
	if result.Packs[0].Source != "blackmatrix7" || result.Packs[0].Pack != "GitHub" {
		t.Fatalf("first pack = %s/%s, want blackmatrix7/GitHub", result.Packs[0].Source, result.Packs[0].Pack)
	}
	if result.Packs[0].ProviderCount != 1 || result.Packs[0].RuleCount != 1 {
		t.Fatalf("counts = %+v, want provider/rule count 1", result.Packs[0])
	}
	if result.Packs[0].Type != PackTypeRuleProvider || result.Packs[0].RenderStrategy != RenderStrategyRuleSet {
		t.Fatalf("pack backend = %+v, want rule-provider RULE-SET", result.Packs[0])
	}
	if result.Packs[0].RenderRuleTemplate != "RULE-SET,blackmatrix7_GitHub,<target>" {
		t.Fatalf("render template = %q, want RULE-SET template", result.Packs[0].RenderRuleTemplate)
	}
	if len(result.Guidance) == 0 || len(result.NextActions) == 0 {
		t.Fatalf("result = %+v, want guidance and next actions", result)
	}
	if result.Packs[0].TargetMeaning == "" {
		t.Fatalf("pack summary = %+v, want target meaning", result.Packs[0])
	}
	if result.Packs[0].ToolArgs.PacksGet.Source != "blackmatrix7" || result.Packs[0].ToolArgs.PacksGet.Pack != "GitHub" {
		t.Fatalf("tool args = %+v, want copyable source/pack args", result.Packs[0].ToolArgs)
	}
	if result.Packs[0].ToolArgs.ConfigPatchCreatePack == nil || result.Packs[0].ToolArgs.ConfigPatchCreatePack.Target != "⚡ 自动选择" {
		t.Fatalf("tool args = %+v, want config_patch_create pack args", result.Packs[0].ToolArgs)
	}
	assertPublicPackJSONExcludes(t, result, "render_rule_template", "RULE-SET,", "blackmatrix7_GitHub")
}

func TestListPacksFiltersNameCaseInsensitive(t *testing.T) {
	cacheDir := writeCatalogTestCaches(t)

	result, err := ListPacks(PackListOptions{CacheDir: cacheDir, Name: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Packs[0].Source != "blackmatrix7" || result.Packs[0].Pack != "OpenAI" {
		t.Fatalf("result = %+v, want blackmatrix7/OpenAI", result)
	}
}

func TestListPacksFiltersSourceExact(t *testing.T) {
	cacheDir := writeCatalogTestCaches(t)

	result, err := ListPacks(PackListOptions{CacheDir: cacheDir, Source: "sukkaw"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Packs[0].Source != "sukkaw" {
		t.Fatalf("result = %+v, want only sukkaw", result)
	}
}

func TestListPacksFiltersTargetExact(t *testing.T) {
	cacheDir := writeCatalogTestCaches(t)

	result, err := ListPacks(PackListOptions{CacheDir: cacheDir, Target: "AI"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Packs[0].Source != "blackmatrix7" || result.Packs[0].Pack != "OpenAI" {
		t.Fatalf("result = %+v, want AI OpenAI pack", result)
	}
}

func TestListPacksLimit(t *testing.T) {
	cacheDir := writeCatalogTestCaches(t)

	result, err := ListPacks(PackListOptions{CacheDir: cacheDir, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || result.Returned != 2 || len(result.Packs) != 2 || !result.Truncated {
		t.Fatalf("result = %+v, want 2 returned from 3 total truncated packs", result)
	}
}

func TestGetPackReturnsDetail(t *testing.T) {
	cacheDir := writeCatalogTestCaches(t)

	result, err := GetPack(PackGetOptions{CacheDir: cacheDir, Source: "blackmatrix7", Pack: "OpenAI"})
	if err != nil {
		t.Fatal(err)
	}
	pack := result.Pack
	if pack.Source != "blackmatrix7" || pack.Pack != "OpenAI" || pack.Target != "AI" {
		t.Fatalf("pack = %+v, want OpenAI target AI", pack)
	}
	if pack.TargetMeaning == "" {
		t.Fatalf("pack = %+v, want target meaning", pack)
	}
	if len(pack.Providers) != 1 || pack.Providers[0].Name != "blackmatrix7_OpenAI" {
		t.Fatalf("providers = %+v, want blackmatrix7_OpenAI", pack.Providers)
	}
	if len(pack.Rules) != 1 || pack.Rules[0] != "RULE-SET,blackmatrix7_OpenAI,AI" {
		t.Fatalf("rules = %+v, want RULE-SET target AI", pack.Rules)
	}
	if pack.Backend.Type != PackTypeRuleProvider || pack.Backend.QuerySource != QuerySourceProviderCache {
		t.Fatalf("backend = %+v, want provider cache backend", pack.Backend)
	}
	if pack.ToolArgs.PackRulesRead.Source != "blackmatrix7" || pack.ToolArgs.PackRulesRead.Pack != "OpenAI" {
		t.Fatalf("tool args = %+v, want copyable source/pack args", pack.ToolArgs)
	}
	assertPublicPackJSONExcludes(t, result, "render_rule_template", "RULE-SET,", "blackmatrix7_OpenAI", `"rules":`)
}

func TestGetPackReturnsGeoSiteBackend(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "packs")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCatalogTestCache(t, cacheDir, PackCache{
		Version:    1,
		Source:     "v2fly-dlc",
		Adapter:    "v2fly-dlc",
		Renderable: false,
		Packs: []Pack{
			{
				ID:         "google",
				Name:       "google",
				Target:     "⚡ 自动选择",
				Renderable: true,
				Components: []Component{
					{
						ID:         "domain",
						Behavior:   "v2fly-dlc",
						Format:     "text",
						OrderClass: "domain",
						URL:        "https://example.com/google",
						Path:       "./rule-packs/v2fly-dlc/google.txt",
					},
				},
			},
		},
	})

	result, err := GetPack(PackGetOptions{CacheDir: cacheDir, Source: "v2fly-dlc", Pack: "google"})
	if err != nil {
		t.Fatal(err)
	}
	pack := result.Pack
	if pack.Type != PackTypeGeoSite || pack.RenderStrategy != RenderStrategyGeoSite {
		t.Fatalf("pack = %+v, want geosite backend fields", pack)
	}
	if pack.Backend.Type != PackTypeGeoSite || pack.Backend.QuerySource != QuerySourceRawDLC || pack.Backend.DataFile != GeoSiteDataFileDLC {
		t.Fatalf("backend = %+v, want raw DLC geosite backend", pack.Backend)
	}
	if pack.RenderRuleTemplate != "GEOSITE,google,⚡ 自动选择" {
		t.Fatalf("render template = %q, want GEOSITE template", pack.RenderRuleTemplate)
	}
	if len(pack.Rules) != 1 || pack.Rules[0] != "GEOSITE,google,⚡ 自动选择" {
		t.Fatalf("rules = %+v, want GEOSITE rule", pack.Rules)
	}
}

func TestResolvePackRefAcceptsGeoSiteSelectorWhenBasePackExists(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "packs")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCatalogTestCache(t, cacheDir, PackCache{
		Version:    1,
		Source:     "v2fly-dlc",
		Adapter:    "v2fly-dlc",
		Renderable: false,
		Packs: []Pack{
			{
				ID:         "category-games",
				Name:       "category-games",
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
	})

	index, err := LoadPackIndex(PackIndexPath(cacheDir))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := index.ResolvePackRef("v2fly-dlc", "category-games@cn")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Pack != "category-games@cn" || ref.RenderRuleTemplate != "GEOSITE,category-games@cn,<target>" {
		t.Fatalf("ref = %+v, want selector geosite ref", ref)
	}
	result, err := GetPack(PackGetOptions{CacheDir: cacheDir, Source: "v2fly-dlc", Pack: "category-games@cn"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Pack.Pack != "category-games@cn" || result.Pack.RenderRuleTemplate != "GEOSITE,category-games@cn,<target>" {
		t.Fatalf("pack detail = %+v, want selector geosite detail", result.Pack)
	}
}

func TestResolvePackRefRejectsGeoSiteSelectorForRuleProviderBase(t *testing.T) {
	cacheDir := writeCatalogTestCaches(t)

	index, err := LoadPackIndex(PackIndexPath(cacheDir))
	if err != nil {
		t.Fatal(err)
	}
	_, err = index.ResolvePackRef("blackmatrix7", "OpenAI@cn")
	if err == nil || !strings.Contains(err.Error(), `base pack "OpenAI" is type "rule_provider"`) {
		t.Fatalf("error = %v, want non-geosite selector rejection", err)
	}
}

func TestGetPackUnknownExactPackReturnsError(t *testing.T) {
	cacheDir := writeCatalogTestCaches(t)

	if _, err := GetPack(PackGetOptions{CacheDir: cacheDir, Source: "blackmatrix7", Pack: "missing_pack"}); err == nil {
		t.Fatal("expected unknown pack error")
	}
}

func TestResolvePackRefAcceptsExactSourcePackOnly(t *testing.T) {
	cacheDir := writeCatalogTestCaches(t)

	index, err := LoadPackIndex(PackIndexPath(cacheDir))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := index.ResolvePackRef("blackmatrix7", "OpenAI")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Source != "blackmatrix7" || ref.Pack != "OpenAI" {
		t.Fatalf("ref = %+v, want blackmatrix7/OpenAI", ref)
	}

	writeCatalogTestCache(t, cacheDir, PackCache{
		Version:    1,
		Source:     "sukkaw",
		Adapter:    "sukkaw",
		Renderable: true,
		Packs: []Pack{
			{
				ID:         "ai",
				Name:       "AI",
				Target:     "AI",
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
	})
	index, err = LoadPackIndex(PackIndexPath(cacheDir))
	if err != nil {
		t.Fatal(err)
	}
	ref, err = index.ResolvePackRef("sukkaw", "ai")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Source != "sukkaw" || ref.Pack != "ai" {
		t.Fatalf("ref = %+v, want source pack ref for sukkaw ai", ref)
	}
	if _, err := index.ResolvePackRef("sukkaw", "ai_non_ip"); err == nil {
		t.Fatal("expected provider component alias to be rejected")
	}
}

func TestResolvePackRefKeepsBangCNSeparateFromCN(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "packs")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pairs := []string{
		"geolocation-!cn", "geolocation-cn",
		"category-ai-!cn", "category-ai-cn",
		"category-social-media-!cn", "category-social-media-cn",
	}
	cache := PackCache{Version: 1, Source: "v2fly-dlc", Adapter: "v2fly-dlc", Renderable: true}
	for _, pack := range pairs {
		cache.Packs = append(cache.Packs, Pack{
			ID:         pack,
			Name:       pack,
			Renderable: true,
			Components: []Component{{ID: "domain", Behavior: "v2fly-dlc", Format: "text", URL: "https://example.com/" + pack, Path: "./rule-packs/v2fly-dlc/" + pack + ".txt"}},
		})
	}
	writeCatalogTestCache(t, cacheDir, cache)

	index, err := LoadPackIndex(PackIndexPath(cacheDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, pack := range pairs {
		ref, err := index.ResolvePackRef("v2fly-dlc", pack)
		if err != nil {
			t.Fatalf("ResolvePackRef(%q) returned error: %v", pack, err)
		}
		if ref.Pack != pack {
			t.Fatalf("ResolvePackRef(%q) = %+v, want exact pack", pack, ref)
		}
	}
}

func assertPublicPackJSONExcludes(t *testing.T, value any, forbidden ...string) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("pack JSON is not serializable: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"tool_args"`) {
		t.Fatalf("pack JSON = %s, want tool_args", text)
	}
	for _, needle := range forbidden {
		if strings.Contains(text, needle) {
			t.Fatalf("pack JSON contains %q: %s", needle, text)
		}
	}
}

func writeCatalogTestCaches(t *testing.T) string {
	t.Helper()
	cacheDir := filepath.Join(t.TempDir(), "packs")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caches := []PackCache{
		{
			Version:    1,
			Source:     "blackmatrix7",
			Adapter:    "blackmatrix7",
			Renderable: true,
			Packs: []Pack{
				testCatalogPack("GitHub", "⚡ 自动选择"),
				testCatalogPack("OpenAI", "AI"),
			},
		},
		{
			Version:    1,
			Source:     "sukkaw",
			Adapter:    "sukkaw",
			Renderable: true,
			Packs: []Pack{
				testCatalogPack("direct", "DIRECT"),
			},
		},
	}
	for _, cache := range caches {
		writeCatalogTestCache(t, cacheDir, cache)
	}
	return cacheDir
}

func writeCatalogTestCache(t *testing.T, cacheDir string, cache PackCache) {
	t.Helper()
	caches := map[string]PackCache{}
	if _, err := os.Stat(PackIndexPath(cacheDir)); err == nil {
		index, err := LoadPackIndex(PackIndexPath(cacheDir))
		if err != nil {
			t.Fatal(err)
		}
		caches = copyPackCaches(index.Caches)
	}
	caches[cache.Source] = cache
	if err := WritePackIndex(PackIndexPath(cacheDir), caches); err != nil {
		t.Fatal(err)
	}
}

func testCatalogPack(id, target string) Pack {
	return Pack{
		ID:         id,
		Name:       id,
		Target:     target,
		Renderable: true,
		Components: []Component{
			{
				ID:         id,
				Behavior:   "classical",
				Format:     "yaml",
				OrderClass: "mixed",
				URL:        "https://example.com/" + id + ".yaml",
				Path:       "./rule-packs/test/" + id + ".yaml",
			},
		},
	}
}
