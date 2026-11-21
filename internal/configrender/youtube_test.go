package configrender

import (
	"path/filepath"
	"slices"
	"testing"

	"localclash/internal/poolcontract"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"
)

func TestYouTubeBulkRenderGateAndPickingPolicy(t *testing.T) {
	paths := writeRenderFixture(t)
	profile := filepath.Join(paths.dir, runtimeprofile.DefaultPath)
	selection := rules.Selection{Version: 1, RequiredTargets: []string{"YouTube"}, ProxyGroups: map[string]rules.ProxyGroup{"Bulk": {Auto: true, Optional: true, PoolCapability: poolcontract.BulkCapability}}, PolicyGroups: map[string]rules.PolicyGroup{"YouTube": {Manual: true, Exits: []string{"DIRECT", "Bulk"}}}}
	for _, core := range []string{"meta", "smart"} {
		if _, err := runtimeprofile.Configure(profile, "", core); err != nil {
			t.Fatal(err)
		}
		for _, settings := range [][2]bool{{false, true}, {true, false}, {true, true}, {true, false}} {
			if err := runtimeprofile.SetPoolOptions(profile, settings[0], settings[1]); err != nil {
				t.Fatal(err)
			}
			opts := Options{SourcePath: paths.subscription, RuntimeProfilePath: profile, OutputPath: filepath.Join(paths.dir, "out.yaml"), Selection: &selection, RulesCacheDir: paths.cacheDir, Force: true}
			if _, err := Render(opts); err != nil {
				t.Fatal(err)
			}
			doc := readTestYAML(t, opts.OutputPath)
			exits := testStringSlice(proxyGroupFromConfig(t, doc, "YouTube")["proxies"])
			visible := settings[0] && settings[1]
			if slices.Contains(exits, "Bulk") != visible || proxyGroupNamesFromConfig(doc)["Bulk"] != visible || exits[0] != "DIRECT" {
				t.Fatal(core, settings, exits)
			}
			if visible {
				g := proxyGroupFromConfig(t, doc, "Bulk")
				if core == "meta" && g["type"] != "fallback" || core == "smart" && (g["type"] != "smart" || g["policy-priority"] == nil) {
					t.Fatalf("wrong selection: %v", g)
				}
				pool, _ := poolcontract.Lookup(poolcontract.BulkCapability)
				p := doc["proxy-providers"].(map[string]any)[pool.Provider].(map[string]any)
				if len(p["payload"].([]any)) == 0 {
					t.Fatal("empty bootstrap")
				}
			}
		}
	}
}
