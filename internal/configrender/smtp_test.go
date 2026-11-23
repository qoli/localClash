package configrender

import (
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"localclash/internal/poolcontract"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"
)

func TestSMTPPoolRendering(t *testing.T) {
	paths := writeRenderFixture(t)
	pool, _ := poolcontract.Lookup(poolcontract.SMTPCapability)
	selection := rules.Selection{
		Version:     1,
		CustomRules: []rules.CustomRule{{ID: "submission", Target: "SMTP", Rules: []rules.CustomRuleLine{{Type: "domain_tcp", Value: "smtp.gmail.com:587"}}}},
		ProxyGroups: map[string]rules.ProxyGroup{
			"Auto":       {Auto: true, PoolCapability: poolcontract.AutoCapability},
			"SMTP exits": {Auto: true, Optional: true, PoolCapability: pool.Capability},
		},
		PolicyGroups: map[string]rules.PolicyGroup{"SMTP": {Manual: true, Exits: []string{"Auto", "DIRECT", "SMTP exits"}}},
	}
	for _, tc := range []struct {
		name, core, groupType string
		enabled, enhanced     bool
	}{
		{name: "disabled", core: runtimeprofile.CoreMeta},
		{name: "disabled_enhanced", core: runtimeprofile.CoreMeta, enhanced: true},
		{name: "meta", core: runtimeprofile.CoreMeta, groupType: "url-test", enabled: true},
		{name: "smart_enhanced", core: runtimeprofile.CoreSmart, groupType: "smart", enabled: true, enhanced: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profilePath := filepath.Join(t.TempDir(), "localclash-runtime.json")
			if _, err := runtimeprofile.Configure(profilePath, "", tc.core); err != nil {
				t.Fatal(err)
			}
			opts := Options{
				SourcePath: paths.subscription, Selection: &selection, RulesCacheDir: paths.cacheDir,
				OutputPath: filepath.Join(t.TempDir(), "out.yaml"), RuntimeProfilePath: profilePath,
				PoolsEnabled: &tc.enabled, EnhancedFilter: &tc.enhanced,
			}
			if _, err := Render(opts); err != nil {
				t.Fatal(err)
			}
			doc := readTestYAML(t, opts.OutputPath)
			choices := testStringSlice(proxyGroupFromConfig(t, doc, "SMTP")["proxies"])
			want := []string{"Auto", "DIRECT"}
			if tc.enabled {
				want = append(want, "SMTP exits")
			}
			providers, _ := doc["proxy-providers"].(map[string]any)
			provider, hasProvider := providers[pool.Provider]
			if !slices.Equal(choices, want) || hasProvider != tc.enabled || proxyGroupNamesFromConfig(doc)["SMTP exits"] != tc.enabled {
				t.Fatalf("SMTP choices=%v provider=%v, want choices=%v provider=%v", choices, hasProvider, want, tc.enabled)
			}
			if !slices.Contains(testStringSlice(doc["rules"]), "AND,((DOMAIN,smtp.gmail.com),(NETWORK,TCP),(DST-PORT,587)),SMTP") {
				t.Fatal("missing SMTP rule")
			}
			if !tc.enabled {
				return
			}
			group := proxyGroupFromConfig(t, doc, "SMTP exits")
			if group["type"] != tc.groupType || !slices.Equal(testStringSlice(group["use"]), []string{pool.Provider}) || group["proxies"] != nil || group["empty-fallback"] != "REJECT" {
				t.Fatalf("SMTP pool group = %+v", group)
			}
			check, _ := provider.(map[string]any)["health-check"].(map[string]any)
			if check["enable"] != true || check["lazy"] != false || check["url"] != poolcontract.AutoHTTPSProbeURL || check["interval"] != 60 ||
				check["expected-status"] != strconv.Itoa(poolcontract.AutoHTTPSProbeStatus) || check["expected-status"] != group["expected-status"] || check["url"] != group["url"] || check["interval"] != group["interval"] {
				t.Fatalf("SMTP provider health check = %+v, group = %+v", check, group)
			}
		})
	}
}
