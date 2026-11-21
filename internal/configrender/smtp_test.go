package configrender

import (
	"path/filepath"
	"slices"
	"testing"

	"localclash/internal/poolcontract"
	"localclash/internal/rules"
)

func TestSMTPGroupRemainsWhenPoolDisabled(t *testing.T) {
	paths := writeRenderFixture(t)
	pool, _ := poolcontract.Lookup(poolcontract.SMTPCapability)
	selection := rules.Selection{
		Version:     1,
		CustomRules: []rules.CustomRule{{ID: "submission", Target: "SMTP", Rules: []rules.CustomRuleLine{{Type: "domain_tcp", Value: "smtp.gmail.com:587"}}}},
		ProxyGroups: map[string]rules.ProxyGroup{
			"Auto":       {Auto: true, PoolCapability: poolcontract.AutoCapability},
			"SMTP exits": {Manual: true, Optional: true, PoolCapability: pool.Capability},
		},
		PolicyGroups: map[string]rules.PolicyGroup{"SMTP": {Manual: true, Exits: []string{"Auto", "DIRECT", "SMTP exits"}}},
	}
	for _, enabled := range []bool{false, true} {
		for _, enhanced := range []bool{false, true} {
			opts := Options{SourcePath: paths.subscription, Selection: &selection, OutputPath: filepath.Join(paths.dir, "out.yaml"), RulesCacheDir: paths.cacheDir, PoolsEnabled: &enabled, EnhancedFilter: &enhanced, Force: true}
			if _, err := Render(opts); err != nil {
				t.Fatal(err)
			}
			doc := readTestYAML(t, opts.OutputPath)
			choices := testStringSlice(proxyGroupFromConfig(t, doc, "SMTP")["proxies"])
			want := []string{"Auto", "DIRECT"}
			if enabled {
				want = append(want, "SMTP exits")
			}
			providers, _ := doc["proxy-providers"].(map[string]any)
			_, hasProvider := providers[pool.Provider]
			if !slices.Equal(choices, want) || hasProvider != enabled || proxyGroupNamesFromConfig(doc)["SMTP exits"] != enabled {
				t.Fatalf("enabled=%v enhanced=%v choices=%v provider=%v", enabled, enhanced, choices, hasProvider)
			}
			if !slices.Contains(testStringSlice(doc["rules"]), "AND,((DOMAIN,smtp.gmail.com),(NETWORK,TCP),(DST-PORT,587)),SMTP") {
				t.Fatal("missing SMTP rule")
			}
		}
	}
}
