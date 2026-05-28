package rules

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPackRulesFetchesProviderAndReturnsSamples(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("DOMAIN-SUFFIX,openai.com\nDOMAIN-SUFFIX,chatgpt.com\nIP-CIDR,1.1.1.1/32,no-resolve\n"))
	}))
	t.Cleanup(server.Close)
	cacheDir, providerCache := writePackRulesCache(t, server.URL)

	result, err := ReadPackRules(context.Background(), PackRulesReadOptions{
		CacheDir:      cacheDir,
		ProviderCache: providerCache,
		Source:        "sukkaw",
		Pack:          "ai",
		Limit:         2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Pack.Source != "sukkaw" || result.Pack.Pack != "ai" || result.Summary.RuleCount != 3 || result.Summary.DomainSuffixCount != 2 || result.Summary.IPCIDRCount != 1 {
		t.Fatalf("result = %+v, want parsed sukkaw_ai summary", result)
	}
	component := result.Components[0]
	if !component.Available || component.RuleCount != 3 || len(component.RulesSample) != 2 || !component.Truncated {
		t.Fatalf("component = %+v, want limited available sample", component)
	}
	if len(component.DomainsSample) != 2 || component.DomainsSample[0] != "openai.com" {
		t.Fatalf("domains sample = %+v, want openai.com", component.DomainsSample)
	}
}

func TestPrefetchAndQueryPackRulesUseLocalProviderCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload:\n  - DOMAIN-SUFFIX,huggingface.co\n  - DOMAIN,api.openai.com\n"))
	}))
	t.Cleanup(server.Close)
	cacheDir, providerCache := writePackRulesCache(t, server.URL)

	before, err := QueryPackRules(context.Background(), PackRulesQueryOptions{
		CacheDir:      cacheDir,
		ProviderCache: providerCache,
		Query:         "huggingface.co",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Matches) != 0 || before.CacheComplete || before.UncachedPacks != 1 {
		t.Fatalf("before query = %+v, want no local cache and incomplete cache", before)
	}

	prefetch, err := PrefetchPackRules(context.Background(), PackRulesPrefetchOptions{
		CacheDir:      cacheDir,
		ProviderCache: providerCache,
		Packs:         []PackSelector{{Source: "sukkaw", Pack: "ai"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prefetch.Summary.AvailableCount != 1 || prefetch.Summary.ErrorCount != 0 {
		t.Fatalf("prefetch = %+v, want one fetched component", prefetch)
	}

	after, err := QueryPackRules(context.Background(), PackRulesQueryOptions{
		CacheDir:      cacheDir,
		ProviderCache: providerCache,
		Query:         "models.huggingface.co",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Matches) != 1 || after.Matches[0].Source != "sukkaw" || after.Matches[0].Pack != "ai" || after.Matches[0].Kind != "domain_suffix" {
		t.Fatalf("after query = %+v, want huggingface suffix match", after)
	}
	if !after.CacheComplete || after.SearchedCachedPacks != 1 {
		t.Fatalf("after query cache = %+v, want complete local cache", after)
	}
}

func TestV2FlyDLCPackRulesAreQueryableWithoutBeingRenderable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`
# Main domain
openai.com
chatgpt.com
full:openaiapi-site.azureedge.net
keyword:oai
regexp:^chatgpt-\S+\.example\.com$
include:category-ai-!cn # comment
full:o33249.ingest.sentry.io @ads
`))
	}))
	t.Cleanup(server.Close)
	cacheDir, providerCache := writeV2FlyDLCPackRulesCache(t, server.URL)

	read, err := ReadPackRules(context.Background(), PackRulesReadOptions{
		CacheDir:      cacheDir,
		ProviderCache: providerCache,
		Source:        "v2fly-dlc",
		Pack:          "openai",
		Limit:         10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !read.Pack.Renderable || read.Summary.RuleCount != 7 || read.Summary.DomainSuffixCount != 2 || read.Summary.DomainCount != 2 || read.Summary.KeywordCount != 1 {
		t.Fatalf("read = %+v, want queryable renderable v2fly rules", read)
	}
	if read.Pack.Type != PackTypeGeoSite || read.Pack.RenderStrategy != RenderStrategyGeoSite {
		t.Fatalf("read pack = %+v, want geosite render strategy", read.Pack)
	}
	if read.Backend.Type != PackTypeGeoSite || read.Backend.QuerySource != QuerySourceRawDLC {
		t.Fatalf("read backend = %+v, want raw DLC geosite backend", read.Backend)
	}
	if got := read.Components[0].ID; got != "domain" {
		t.Fatalf("component id = %q, want domain", got)
	}

	query, err := QueryPackRules(context.Background(), PackRulesQueryOptions{
		CacheDir:      cacheDir,
		ProviderCache: providerCache,
		Source:        "v2fly-dlc",
		Query:         "api.openai.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(query.Matches) != 1 || query.Matches[0].Source != "v2fly-dlc" || query.Matches[0].Pack != "openai" || query.Matches[0].Kind != "domain_suffix" {
		t.Fatalf("query = %+v, want openai suffix match", query)
	}
	if query.Backend.Type != PackTypeGeoSite || query.Backend.RenderStrategy != RenderStrategyGeoSite {
		t.Fatalf("query backend = %+v, want geosite backend", query.Backend)
	}
	if query.Matches[0].Type != PackTypeGeoSite || query.Matches[0].RenderStrategy != RenderStrategyGeoSite {
		t.Fatalf("query match = %+v, want geosite match metadata", query.Matches[0])
	}

	regexQuery, err := QueryPackRules(context.Background(), PackRulesQueryOptions{
		CacheDir:      cacheDir,
		ProviderCache: providerCache,
		Source:        "v2fly-dlc",
		Query:         "chatgpt-web.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(regexQuery.Matches) != 1 || regexQuery.Matches[0].Kind != "domain_regex" {
		t.Fatalf("regex query = %+v, want v2fly regexp match", regexQuery)
	}
}

func TestPrefetchPackRulesRequiresExplicitScope(t *testing.T) {
	cacheDir, providerCache := writePackRulesCache(t, "https://example.com")

	if _, err := PrefetchPackRules(context.Background(), PackRulesPrefetchOptions{CacheDir: cacheDir, ProviderCache: providerCache}); err == nil {
		t.Fatal("expected prefetch without packs or filters to fail")
	}
}

func TestQueryPackRulesRejectsApproximateSource(t *testing.T) {
	cacheDir, providerCache := writeV2FlyDLCPackRulesCache(t, "https://example.com")

	_, err := QueryPackRules(context.Background(), PackRulesQueryOptions{
		CacheDir:      cacheDir,
		ProviderCache: providerCache,
		Source:        "v2fly",
		Query:         "google",
	})
	if err == nil || !strings.Contains(err.Error(), `did you mean "v2fly-dlc"`) {
		t.Fatalf("err = %v, want exact source hint", err)
	}
}

func writePackRulesCache(t *testing.T, baseURL string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "packs")
	providerCache := filepath.Join(dir, "provider-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCatalogTestCache(t, cacheDir, PackCache{
		Version:    1,
		Source:     "sukkaw",
		Adapter:    "sukkaw",
		Renderable: true,
		Packs: []Pack{
			{
				ID:         "ai",
				Name:       "ai",
				Target:     "AI",
				Renderable: true,
				Components: []Component{
					{
						ID:         "non_ip",
						Behavior:   "classical",
						Format:     "yaml",
						OrderClass: "non_ip",
						URL:        baseURL + "/ai.yaml",
						Path:       "./rule-packs/sukkaw/ai_non_ip.txt",
					},
				},
			},
		},
	})
	return cacheDir, providerCache
}

func writeV2FlyDLCPackRulesCache(t *testing.T, baseURL string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "packs")
	providerCache := filepath.Join(dir, "provider-cache")
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
				ID:         "openai",
				Name:       "openai",
				Renderable: true,
				Reason:     "queryable raw DLC rules and renderable as GEOSITE",
				Components: []Component{
					{
						ID:         "domain",
						Behavior:   "v2fly-dlc",
						Format:     "text",
						OrderClass: "domain",
						URL:        baseURL + "/openai",
						Path:       "./rule-packs/v2fly-dlc/openai.txt",
					},
				},
			},
		},
	})
	return cacheDir, providerCache
}
