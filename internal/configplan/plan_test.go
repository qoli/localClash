package configplan

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localclash/internal/localconfig"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"

	"gopkg.in/yaml.v3"
)

func TestRenderBuiltInTargetPlanWritesArtifacts(t *testing.T) {
	paths := writePlanFixture(t)
	generated := filepath.Join(paths.dir, "generated", "mihomo.yaml")
	writeFile(t, generated, "sentinel: keep\n")

	result, err := Render(context.Background(), Options{
		PlanName:     "gaming-direct",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{
				{Source: "blackmatrix7", Pack: "Steam", Target: "DIRECT"},
				{Source: "blackmatrix7", Pack: "Epic", Target: "DIRECT"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PlanID != "gaming-direct-20260516-130000" {
		t.Fatalf("plan id = %q", result.PlanID)
	}
	assertFileExists(t, result.Output)
	assertFileExists(t, result.SummaryPath)
	if got := readFile(t, generated); got != "sentinel: keep\n" {
		t.Fatalf("generated config was overwritten: %q", got)
	}
	if result.Changes.RuleProvidersAdded != 2 || result.Changes.ProxyGroupsAdded != 0 || result.Changes.RulesAdded != 2 {
		t.Fatalf("changes = %+v, want 2 providers, 0 groups, 2 rules", result.Changes)
	}
	if len(result.Overlay.Packs) != 2 || result.Overlay.Packs[0].Type != rules.PackTypeRuleProvider {
		t.Fatalf("overlay packs = %+v, want rule-provider type metadata", result.Overlay.Packs)
	}
	config := readYAMLMap(t, result.Output)
	metadata := config["x-localclash"].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	if len(overlay["packs"].([]any)) != 2 {
		t.Fatalf("overlay packs = %v, want 2", overlay["packs"])
	}
	firstPack := overlay["packs"].([]any)[0].(map[string]any)
	if firstPack["type"] != rules.PackTypeRuleProvider {
		t.Fatalf("metadata pack = %+v, want rule-provider type", firstPack)
	}
	if len(overlay["proxy_groups"].([]any)) != 0 {
		t.Fatalf("overlay proxy_groups = %v, want empty", overlay["proxy_groups"])
	}
	assertMetadataHasNoSensitiveFields(t, metadata)
	assertSummaryJSON(t, result.SummaryPath)
}

func TestRenderPlanIDDoesNotOverwriteExistingPlan(t *testing.T) {
	paths := writePlanFixture(t)
	firstPlanDir := filepath.Join(paths.planDir, "gaming-direct-20260516-130000")
	writeFile(t, filepath.Join(firstPlanDir, "summary.json"), `{"sentinel":true}`)

	result, err := Render(context.Background(), Options{
		PlanName:     "gaming-direct",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{
				{Source: "blackmatrix7", Pack: "Steam", Target: "DIRECT"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PlanID != "gaming-direct-20260516-130000-2" {
		t.Fatalf("plan id = %q, want collision suffix", result.PlanID)
	}
	if got := readFile(t, filepath.Join(firstPlanDir, "summary.json")); got != "{\n  \"sentinel\": true\n}" {
		t.Fatalf("existing plan was overwritten: %q", got)
	}
}

func TestRenderEnabledRulePackPlanWritesDurableIntent(t *testing.T) {
	paths := writePlanFixture(t)
	writeFile(t, filepath.Join(paths.dir, "rule-packs", "ads.json"), `{
  "id": "ads",
  "name": "Ads",
  "version": 1,
  "default_target": "REJECT",
  "target_options": ["REJECT", "DIRECT"],
  "rules": [
    {"domain_suffix": "doubleclick.net"},
    {"domain_suffix": "googlesyndication.com"}
  ]
}`)

	result, err := Render(context.Background(), Options{
		PlanName:     "ads-reject",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			EnabledRulePacks: []OverlayRulePackIntent{{ID: "ads"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Overlay.EnabledRulePacks) != 1 || result.Overlay.EnabledRulePacks[0].RuleCount != 2 {
		t.Fatalf("enabled rule packs = %+v, want ads with two rules", result.Overlay.EnabledRulePacks)
	}
	if result.Changes.RulePacksAdded != 1 || result.Changes.RulesAdded != 2 {
		t.Fatalf("changes = %+v, want one rule pack and two rules", result.Changes)
	}
	candidate := readFile(t, result.ConfigPath)
	if !strings.Contains(candidate, `"enabled_rule_packs"`) || !strings.Contains(candidate, `"ads"`) {
		t.Fatalf("candidate localclash.json missing enabled_rule_packs:\n%s", candidate)
	}
	config := readYAMLMap(t, result.Output)
	rules := config["rules"].([]any)
	found := false
	for _, rule := range rules {
		if rule == "DOMAIN-SUFFIX,doubleclick.net,REJECT" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("rendered rules = %+v, missing local rule pack rule", rules)
	}
	metadata := config["x-localclash"].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	packs := overlay["packs"].([]any)
	if len(packs) != 1 || packs[0].(map[string]any)["source"] != "local" {
		t.Fatalf("metadata packs = %+v, want local rule pack", packs)
	}
}

func TestRenderProxyGroupPlan(t *testing.T) {
	paths := writePlanFixture(t)

	result, err := Render(context.Background(), Options{
		PlanName:     "ai-sg-jp-us",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{
				{Source: "blackmatrix7", Pack: "OpenAI", Target: "AI"},
				{Source: "sukkaw", Pack: "ai", Target: "AI"},
			},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "AI", Nodes: []string{"SG 01", "JP Tokyo 01", "US 01"}, Mode: "manual"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Overlay.ProxyGroups) != 1 {
		t.Fatalf("proxy groups = %+v, want one", result.Overlay.ProxyGroups)
	}
	if result.Overlay.ProxyGroups[0].NodeCount != 3 {
		t.Fatalf("proxy group node count = %d, want 3", result.Overlay.ProxyGroups[0].NodeCount)
	}
	if result.Changes.RuleProvidersAdded != 2 || result.Changes.ProxyGroupsAdded != 1 || result.Changes.RulesAdded != 2 {
		t.Fatalf("changes = %+v, want 2 providers, 1 group, 2 rules", result.Changes)
	}
	config := readYAMLMap(t, result.Output)
	if !proxyGroupNames(config)["AI"] {
		t.Fatal("candidate config missing AI proxy group")
	}
	metadata := config["x-localclash"].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	proxyGroups := overlay["proxy_groups"].([]any)
	if got := proxyGroups[0].(map[string]any)["mode"]; got != "manual" {
		t.Fatalf("proxy group mode = %v, want manual", got)
	}
	assertMetadataHasNoSensitiveFields(t, metadata)
}

func TestRenderPolicyGroupPlan(t *testing.T) {
	paths := writePlanFixture(t)

	result, err := Render(context.Background(), Options{
		PlanName:     "steam-exits",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{
				{Source: "blackmatrix7", Pack: "Steam", Target: "Steam"},
			},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "SG", Nodes: []string{"SG 01"}, Mode: "manual"},
				{ID: "JP", Nodes: []string{"JP Tokyo 01"}, Mode: "auto"},
				{ID: "全球直连", Mode: "direct"},
			},
			PolicyGroups: []OverlayPolicyGroupIntent{
				{ID: "Steam", Mode: "manual", Exits: []string{"SG", "JP", "全球直连"}, Reason: "Steam traffic should pick an exit group."},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Overlay.PolicyGroups) != 1 || result.Overlay.PolicyGroups[0].ID != "Steam" {
		t.Fatalf("policy groups = %+v, want Steam", result.Overlay.PolicyGroups)
	}
	if result.Changes.ProxyGroupsAdded != 3 || result.Changes.PolicyGroupsAdded != 1 || result.Changes.RulesAdded != 1 {
		t.Fatalf("changes = %+v, want 3 proxy groups, 1 policy group, 1 rule", result.Changes)
	}
	config := readYAMLMap(t, result.Output)
	names := proxyGroupNames(config)
	for _, want := range []string{"SG", "JP", "全球直连", "Steam"} {
		if !names[want] {
			t.Fatalf("candidate config missing proxy group %q in %+v", want, names)
		}
	}
	metadata := config["x-localclash"].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	policyGroups := overlay["policy_groups"].([]any)
	if len(policyGroups) != 1 || policyGroups[0].(map[string]any)["id"] != "Steam" {
		t.Fatalf("metadata policy_groups = %+v, want Steam", policyGroups)
	}
	candidate, err := localconfig.Load(result.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Version != localconfig.ConfigSchemaVersion {
		t.Fatalf("candidate localclash version = %d, want current schema", candidate.Version)
	}
}

func TestRenderPatchCanReuseExistingProxyGroups(t *testing.T) {
	paths := writePlanFixture(t)
	existingConfig := filepath.Join(paths.dir, "localclash.json")
	writeFile(t, existingConfig, `version: 2
policy_template: localclash-default
proxy_groups:
  HK:
    mode: auto
    nodes:
      - SG 01
  JP:
    mode: auto
    nodes:
      - JP Tokyo 01
  US:
    mode: auto
    nodes:
      - US 01
packs:
  - source: blackmatrix7
    pack: Epic
    target: DIRECT
`)

	result, err := Render(context.Background(), Options{
		PlanName:     "steam-existing-exits",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		ConfigPath:   existingConfig,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			PolicyGroups: []OverlayPolicyGroupIntent{
				{ID: "Steam", Mode: "manual", Exits: []string{"HK", "JP", "US"}, Reason: "Steam can choose existing regional exits."},
			},
			CustomRules: []OverlayCustomRuleIntent{
				{
					ID:     "steam_domains",
					Target: "Steam",
					Rules: []localconfig.CustomRuleLine{
						{Type: "domain_suffix", Value: "steampowered.com"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Overlay.ProxyGroups) != 0 {
		t.Fatalf("requested overlay proxy groups = %+v, want none", result.Overlay.ProxyGroups)
	}
	if len(result.Overlay.PolicyGroups) != 1 || result.Overlay.PolicyGroups[0].ID != "Steam" {
		t.Fatalf("requested overlay policy groups = %+v, want Steam only", result.Overlay.PolicyGroups)
	}
	if result.Changes.ProxyGroupsAdded != 0 || result.Changes.PolicyGroupsAdded != 1 || result.Changes.RulesAdded != 1 {
		t.Fatalf("changes = %+v, want one policy group and one custom rule line", result.Changes)
	}
	candidate, err := localconfig.Load(result.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.PolicyTemplate != "localclash-default" {
		t.Fatalf("policy template = %q, want localclash-default", candidate.PolicyTemplate)
	}
	for _, want := range []string{"HK", "JP", "US"} {
		if _, exists := candidate.ProxyGroups[want]; !exists {
			t.Fatalf("candidate missing preserved proxy group %q", want)
		}
	}
	if _, exists := candidate.PolicyGroups["Steam"]; !exists {
		t.Fatalf("candidate policy groups = %+v, want Steam", candidate.PolicyGroups)
	}
	if len(candidate.Packs) != 1 || candidate.Packs[0].Source != "blackmatrix7" || candidate.Packs[0].Pack != "Epic" {
		t.Fatalf("candidate packs = %+v, want preserved Epic pack", candidate.Packs)
	}
	config := readFile(t, result.Output)
	for _, want := range []string{"DOMAIN-SUFFIX,steampowered.com,Steam", "name: Steam", "- HK", "- JP", "- US"} {
		if !strings.Contains(config, want) {
			t.Fatalf("candidate config missing %q:\n%s", want, config)
		}
	}
}

func TestRenderExternalRuleProviderPlan(t *testing.T) {
	paths := writePlanFixture(t)

	result, err := Render(context.Background(), Options{
		PlanName:     "us-proxy",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			RuleProviders: []OverlayRuleProviderIntent{
				{
					ID:       "US-Proxy",
					Target:   "⚡ 自动选择",
					Type:     "http",
					Behavior: "classical",
					Format:   "yaml",
					Path:     "./rule_provider/US-Proxy.yaml",
					URL:      "https://raw.githubusercontent.com/qoli/clash_yaml/refs/heads/main/us_proxy.yaml",
					Interval: 86400,
					Reason:   "User supplied qoli US proxy rule-provider.",
				},
			},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "⚡ 自动选择", Nodes: []string{"US 01"}, Mode: "auto"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changes.RuleProvidersAdded != 1 || result.Changes.RulesAdded != 1 {
		t.Fatalf("changes = %+v, want one provider and one rule", result.Changes)
	}
	if len(result.Overlay.RuleProviders) != 1 || result.Overlay.RuleProviders[0].ID != "US-Proxy" {
		t.Fatalf("overlay rule providers = %+v, want US-Proxy", result.Overlay.RuleProviders)
	}
	config := readYAMLMap(t, result.Output)
	providers := config["rule-providers"].(map[string]any)
	provider := providers["US-Proxy"].(map[string]any)
	if provider["url"] != "https://raw.githubusercontent.com/qoli/clash_yaml/refs/heads/main/us_proxy.yaml" {
		t.Fatalf("provider = %+v, want qoli url", provider)
	}
	rules := config["rules"].([]any)
	if !containsRule(rules, "RULE-SET,US-Proxy,⚡ 自动选择") {
		t.Fatalf("rules missing external provider rule: %+v", rules)
	}
	metadata := config["x-localclash"].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	if len(overlay["rule_providers"].([]any)) != 1 {
		t.Fatalf("metadata rule providers = %+v, want one", overlay["rule_providers"])
	}
}

func TestRenderMihomoTestUsesRuntimeProfileCore(t *testing.T) {
	paths := writePlanFixture(t)
	profilePath := filepath.Join(paths.dir, runtimeprofile.DefaultPath)
	if _, err := runtimeprofile.Configure(profilePath, "", runtimeprofile.CoreSmart); err != nil {
		t.Fatal(err)
	}
	corePath := filepath.Join(paths.dir, runtimeprofile.SmartCorePath)
	argsPath := filepath.Join(paths.dir, "smart-core.args")
	writeExecutable(t, corePath, "#!/bin/sh\nprintf '%s\\n' \"$0 $*\" > '"+argsPath+"'\nexit 0\n")

	result, err := Render(context.Background(), Options{
		PlanName:           "smart-core-test",
		Subscription:       paths.subscription,
		RulesCache:         paths.cacheDir,
		RuntimeProfilePath: profilePath,
		OutputDir:          paths.planDir,
		Test:               true,
		Now:                fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{Source: "blackmatrix7", Pack: "Steam", Target: "DIRECT"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || !result.MihomoTest.Passed {
		t.Fatalf("result = %+v, want passing smart core test", result.MihomoTest)
	}
	if got := readFile(t, argsPath); !strings.Contains(got, runtimeprofile.SmartCorePath) || !strings.Contains(got, "-t") {
		t.Fatalf("smart core args = %q, want smart core config test", got)
	}
}

func TestRunMihomoTestFailureIncludesErrorMetadata(t *testing.T) {
	dir := t.TempDir()
	corePath := filepath.Join(dir, "mihomo")
	writeExecutable(t, corePath, "#!/bin/sh\nfor n in 1 2 3 4 5 6 7 8 9 10; do echo line-$n; done\nexit 42\n")
	configPath := filepath.Join(dir, "mihomo.yaml")
	writeFile(t, configPath, "mode: rule\n")

	result := runMihomoTest(context.Background(), Options{
		CorePath: corePath,
		WorkDir:  dir,
	}, configPath)

	if !result.Enabled || result.Passed {
		t.Fatalf("mihomo test = %+v, want enabled failure", result)
	}
	if result.ExitCode != 42 || result.Error == "" || result.DurationMS < 0 {
		t.Fatalf("mihomo test metadata = %+v, want exit code, error, and duration", result)
	}
	outputLines := strings.Split(result.Output, "\n")
	if len(outputLines) < 2 || outputLines[0] != "line-3" || outputLines[len(outputLines)-2] != "line-10" {
		t.Fatalf("output = %q, want compact tail", result.Output)
	}
	if !strings.Contains(result.Output, "error:") {
		t.Fatalf("output = %q, want appended command error", result.Output)
	}
}

func TestRunMihomoTestUsesIsolatedRuntimeWithoutCache(t *testing.T) {
	dir := t.TempDir()
	sourceWorkDir := filepath.Join(dir, "runtime")
	writeFile(t, filepath.Join(sourceWorkDir, "Model.bin"), "model")
	writeFile(t, filepath.Join(sourceWorkDir, "geoip.dat"), "geoip")
	writeFile(t, filepath.Join(sourceWorkDir, "cache.db"), "live-cache")
	writeFile(t, filepath.Join(sourceWorkDir, "rule-packs", "OpenAI.yaml"), "payload")
	argsPath := filepath.Join(dir, "mihomo.args")
	corePath := filepath.Join(dir, "mihomo")
	writeExecutable(t, corePath, fmt.Sprintf(`#!/bin/sh
work_dir=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-d" ]; then
    shift
    work_dir="$1"
  fi
  shift
done
printf '%%s\n' "$work_dir" > %[1]q
if [ "$work_dir" = %[2]q ]; then
  echo "used live runtime dir" >&2
  exit 40
fi
if [ -e "$work_dir/cache.db" ]; then
  echo "copied cache.db" >&2
  exit 41
fi
if [ ! -f "$work_dir/Model.bin" ] || [ ! -f "$work_dir/geoip.dat" ] || [ ! -f "$work_dir/rule-packs/OpenAI.yaml" ]; then
  echo "missing validation artifacts" >&2
  exit 42
fi
echo "configuration file test is successful"
`, argsPath, sourceWorkDir))
	configPath := filepath.Join(dir, "mihomo.yaml")
	writeFile(t, configPath, "mode: rule\n")

	result := runMihomoTest(context.Background(), Options{
		CorePath: corePath,
		WorkDir:  sourceWorkDir,
	}, configPath)

	if !result.Enabled || !result.Passed || !result.Isolated {
		t.Fatalf("mihomo test = %+v, want passing isolated validation", result)
	}
	if result.SourceWorkDir != sourceWorkDir || result.WorkDir == "" || result.WorkDir == sourceWorkDir {
		t.Fatalf("mihomo test dirs = %+v, want isolated work dir from source %q", result, sourceWorkDir)
	}
	if _, err := os.Stat(result.WorkDir); !os.IsNotExist(err) {
		t.Fatalf("isolated work dir still exists after cleanup: %s err=%v", result.WorkDir, err)
	}
	if got := strings.TrimSpace(readFile(t, argsPath)); got == "" || got == sourceWorkDir {
		t.Fatalf("recorded work dir = %q, want isolated dir", got)
	}
}

func TestMihomoTestFailureNextActionsWarnBeforeBypass(t *testing.T) {
	actions := strings.Join(mihomoTestFailureNextActions(MihomoTestResult{TimedOut: true}), "\n")
	for _, want := range []string{"Do not apply this patch", "timed out", "test=false", "explicitly accepts"} {
		if !strings.Contains(actions, want) {
			t.Fatalf("actions = %q, missing %q", actions, want)
		}
	}
}

func TestRenderProxyGroupMatchPlanWritesCandidateLocalClashConfig(t *testing.T) {
	paths := writePlanFixture(t)

	result, err := Render(context.Background(), Options{
		PlanName:     "ai-by-regex",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{Source: "blackmatrix7", Pack: "OpenAI", Target: "AI", Reason: "Route AI rules to selected Singapore-labelled nodes."}},
			ProxyGroups: []OverlayProxyGroupIntent{
				{
					ID:       "AI",
					Mode:     "manual",
					Match:    &localconfig.Match{Type: "name_regex", Pattern: "SG", Min: 1, Max: 1},
					Reason:   "Use nodes whose names indicate Singapore.",
					Boundary: "name_based_hint_only",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ConfigPath == "" {
		t.Fatal("result missing candidate localclash config path")
	}
	config, err := localconfig.Load(result.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	group := config.ProxyGroups["AI"]
	if group.Match == nil || group.Match.Pattern != "SG" {
		t.Fatalf("AI match = %+v, want SG selector", group.Match)
	}
	if len(group.SelectedNodes) != 1 || group.SelectedNodes[0] != "SG 01" {
		t.Fatalf("selected nodes = %+v, want SG 01", group.SelectedNodes)
	}
	if result.Overlay.ProxyGroups[0].Match == nil || result.Overlay.ProxyGroups[0].Boundary != "name_based_hint_only" {
		t.Fatalf("overlay summary = %+v, want match and boundary", result.Overlay.ProxyGroups[0])
	}
}

func TestApplyPlanWritesSelectionAndGeneratedConfig(t *testing.T) {
	paths := writePlanFixture(t)
	generated := filepath.Join(paths.dir, "generated", "mihomo.yaml")
	selectionPath := filepath.Join(paths.dir, "localclash-packs.gob")
	writeFile(t, generated, "sentinel: old generated\n")

	plan, err := Render(context.Background(), Options{
		PlanName:     "ai-sg",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{Source: "blackmatrix7", Pack: "OpenAI", Target: "AI"}},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "AI", Nodes: []string{"SG 01"}, Mode: "manual"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), ApplyOptions{
		PlanID:        plan.PlanID,
		PlansDir:      paths.planDir,
		Subscription:  paths.subscription,
		RulesCache:    paths.cacheDir,
		SelectionPath: selectionPath,
		OutputPath:    generated,
		Test:          false,
		Now:           fixedPlanTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || !result.Valid {
		t.Fatalf("apply result = %+v, want applied valid plan", result)
	}
	if !result.Transaction.Prepared || !result.Transaction.Atomic || len(result.Transaction.Targets) != 3 {
		t.Fatalf("transaction = %+v, want atomic three-target commit", result.Transaction)
	}
	if len(result.Backups) != 2 {
		t.Fatalf("backups = %+v, want selection and generated backups", result.Backups)
	}
	selection, err := rules.LoadSelection(selectionPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.EnabledPack) != 1 || selection.EnabledPack[0].Source != "blackmatrix7" || selection.EnabledPack[0].Pack != "OpenAI" || selection.EnabledPack[0].Target != "AI" {
		t.Fatalf("selection enabled packs = %+v", selection.EnabledPack)
	}
	if got := selection.ProxyGroups["AI"].Nodes; len(got) != 1 || got[0] != "SG 01" {
		t.Fatalf("AI nodes = %+v, want SG 01", got)
	}
	config := readYAMLMap(t, generated)
	if !proxyGroupNames(config)["AI"] {
		t.Fatal("generated config missing applied AI proxy group")
	}
	if strings.Contains(readFile(t, generated), "sentinel") {
		t.Fatalf("generated config was not replaced: %s", readFile(t, generated))
	}
}

func TestApplyPlanRollsBackWhenAtomicCommitFails(t *testing.T) {
	paths := writePlanFixture(t)
	activeConfig := filepath.Join(paths.dir, "localclash.json")
	selectionPath := filepath.Join(paths.dir, "localclash-packs.gob")
	generated := filepath.Join(paths.dir, "generated", "mihomo.yaml")
	writeFile(t, activeConfig, `version: 1
proxy_groups:
  OLD:
    mode: direct
packs: []
`)
	writeFile(t, selectionPath, `version: 1
proxy_groups:
  OLD:
    direct: true
enabled_packs: []
`)
	writeFile(t, generated, "sentinel: old generated\n")

	plan, err := Render(context.Background(), Options{
		PlanName:     "ai-sg",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		ConfigPath:   activeConfig,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{Source: "blackmatrix7", Pack: "OpenAI", Target: "AI"}},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "AI", Nodes: []string{"SG 01"}, Mode: "manual"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	originalRename := renameFile
	renameFile = func(oldPath, newPath string) error {
		if filepath.Base(newPath) == "localclash-packs.gob" {
			return fmt.Errorf("injected selection commit failure")
		}
		return originalRename(oldPath, newPath)
	}
	t.Cleanup(func() { renameFile = originalRename })

	result, err := Apply(context.Background(), ApplyOptions{
		PlanID:        plan.PlanID,
		PlansDir:      paths.planDir,
		ConfigPath:    activeConfig,
		Subscription:  paths.subscription,
		RulesCache:    paths.cacheDir,
		SelectionPath: selectionPath,
		OutputPath:    generated,
		Test:          false,
		Now:           fixedPlanTime(),
	})
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("error = %v, want rollback commit error", err)
	}
	if result.Applied || result.Error == "" || result.Transaction.Prepared != true || result.Transaction.Atomic != true {
		t.Fatalf("result = %+v, want structured rollback failure", result)
	}
	if len(result.Backups) == 0 || len(result.NextActions) == 0 {
		t.Fatalf("result = %+v, want backups and recovery next actions", result)
	}
	active, err := localconfig.Load(activeConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := active.ProxyGroups["OLD"]; !ok {
		t.Fatalf("active config = %+v, want OLD after rollback", active.ProxyGroups)
	}
	if _, ok := active.ProxyGroups["AI"]; ok {
		t.Fatalf("active config = %+v, AI should not be committed after rollback", active.ProxyGroups)
	}
	if got := readFile(t, generated); got != "sentinel: old generated\n" {
		t.Fatalf("generated = %q, want old generated after rollback", got)
	}
	selection, err := rules.LoadSelection(selectionPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := selection.ProxyGroups["OLD"]; !ok {
		t.Fatalf("selection = %+v, want OLD after rollback", selection.ProxyGroups)
	}
}

func TestApplyPlanReturnsStructuredResultWhenBackupFails(t *testing.T) {
	paths := writePlanFixture(t)
	generated := filepath.Join(paths.dir, "generated", "mihomo.yaml")
	selectionPath := filepath.Join(paths.dir, "localclash-packs.gob")
	writeFile(t, generated, "sentinel: old generated\n")
	plan, err := Render(context.Background(), Options{
		PlanName:     "ai-sg",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{Source: "blackmatrix7", Pack: "OpenAI", Target: "AI"}},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "AI", Nodes: []string{"SG 01"}, Mode: "manual"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(paths.dir, "backup-blocker")
	writeFile(t, blocker, "not a directory\n")

	result, err := Apply(context.Background(), ApplyOptions{
		PlanID:        plan.PlanID,
		PlansDir:      paths.planDir,
		Subscription:  paths.subscription,
		RulesCache:    paths.cacheDir,
		SelectionPath: selectionPath,
		OutputPath:    generated,
		BackupDir:     blocker,
		Test:          false,
		Now:           fixedPlanTime(),
	})
	if err == nil {
		t.Fatal("Apply error = nil, want backup failure")
	}
	if result.PlanID != plan.PlanID || result.Error == "" || len(result.NextActions) == 0 {
		t.Fatalf("result = %+v, want structured backup failure", result)
	}
	if result.Applied || result.Transaction.Prepared {
		t.Fatalf("result = %+v, backup failure should not prepare or apply transaction", result)
	}
	if got := readFile(t, generated); got != "sentinel: old generated\n" {
		t.Fatalf("generated = %q, want unchanged generated config", got)
	}
}

func TestApplyPlanRunsMihomoTestEvenWhenCreateSkippedIt(t *testing.T) {
	paths := writePlanFixture(t)
	corePath := filepath.Join(paths.dir, "mihomo")
	argsPath := filepath.Join(paths.dir, "apply-core.args")
	writeExecutable(t, corePath, fmt.Sprintf("#!/bin/sh\nprintf '%%s\n' \"$0 $*\" > '%s'\nexit 0\n", argsPath))
	plan, err := Render(context.Background(), Options{
		PlanName:     "ai-sg",
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{Source: "blackmatrix7", Pack: "OpenAI", Target: "AI"}},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "AI", Nodes: []string{"SG 01"}, Mode: "manual"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), ApplyOptions{
		PlanID:        plan.PlanID,
		PlansDir:      paths.planDir,
		Subscription:  paths.subscription,
		RulesCache:    paths.cacheDir,
		SelectionPath: filepath.Join(paths.dir, "localclash-packs.gob"),
		OutputPath:    filepath.Join(paths.dir, "generated", "mihomo.yaml"),
		CorePath:      corePath,
		WorkDir:       paths.dir,
		Test:          true,
		Now:           fixedPlanTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.MihomoTest.Enabled || !result.MihomoTest.Passed {
		t.Fatalf("apply mihomo test = %+v, want enabled passing test", result.MihomoTest)
	}
	if got := readFile(t, argsPath); !strings.Contains(got, "-t") {
		t.Fatalf("core args = %q, want mihomo config test", got)
	}
	if !result.Applied || !result.Valid {
		t.Fatalf("apply result = %+v, want applied valid plan", result)
	}
}

func TestRenderUnknownPackIDReturnsError(t *testing.T) {
	paths := writePlanFixture(t)

	_, err := Render(context.Background(), Options{
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{Source: "blackmatrix7", Pack: "missing_pack", Target: "DIRECT"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "missing_pack") {
		t.Fatalf("error = %v, want missing pack error", err)
	}
}

func TestOverlayPackIntentRejectsLegacyPackID(t *testing.T) {
	var pack OverlayPackIntent
	err := json.Unmarshal([]byte(`{"id":"v2fly_dlc_geolocation__cn","target":"DIRECT"}`), &pack)
	if err == nil || !strings.Contains(err.Error(), "packs[].id is no longer supported; use packs[].source and packs[].pack from packs_list") ||
		!strings.Contains(err.Error(), "Composite renderer/provider names are not MCP pack selectors") {
		t.Fatalf("error = %v, want legacy pack id rejection", err)
	}
}

func TestRenderRejectsMismatchedPackType(t *testing.T) {
	paths := writePlanFixture(t)

	_, err := Render(context.Background(), Options{
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{Source: "blackmatrix7", Pack: "OpenAI", Type: rules.PackTypeGeoSite, Target: "DIRECT"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `is type "rule_provider"`) || !strings.Contains(err.Error(), `declared "geosite"`) {
		t.Fatalf("error = %v, want pack type mismatch", err)
	}
}

func TestRenderMissingProxyGroupReturnsError(t *testing.T) {
	paths := writePlanFixture(t)

	_, err := Render(context.Background(), Options{
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{Source: "blackmatrix7", Pack: "OpenAI", Target: "AI"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "requires a matching proxy group") {
		t.Fatalf("error = %v, want missing proxy group error", err)
	}
}

func TestRenderUnknownProxyGroupNodeReturnsError(t *testing.T) {
	paths := writePlanFixture(t)

	_, err := Render(context.Background(), Options{
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{Source: "blackmatrix7", Pack: "OpenAI", Target: "AI"}},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "AI", Nodes: []string{"MISSING"}, Mode: "manual"},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown subscription node") {
		t.Fatalf("error = %v, want unknown subscription node error", err)
	}
}

func TestRenderDuplicateProxyGroupNodesAreDeduplicated(t *testing.T) {
	paths := writePlanFixture(t)

	result, err := Render(context.Background(), Options{
		Subscription: paths.subscription,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{Source: "blackmatrix7", Pack: "OpenAI", Target: "AI"}},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "AI", Nodes: []string{"SG 01", "SG 01"}, Mode: "manual"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", result.Warnings)
	}
	if result.Overlay.ProxyGroups[0].NodeCount != 1 {
		t.Fatalf("node count = %d, want deduplicated SG node only", result.Overlay.ProxyGroups[0].NodeCount)
	}
}

type planFixturePaths struct {
	dir          string
	subscription string
	cacheDir     string
	planDir      string
}

func writePlanFixture(t *testing.T) planFixturePaths {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	paths := planFixturePaths{
		dir:          dir,
		subscription: filepath.Join(dir, "subscription.gob"),
		cacheDir:     filepath.Join(dir, ".runtime", "rules", "packs"),
		planDir:      filepath.Join(dir, ".runtime", "plans"),
	}
	writeFile(t, paths.subscription, `proxies:
  - name: "SG 01"
    type: ss
    server: sg.example.com
    port: 443
    password: secret
  - name: "JP Tokyo 01"
    type: trojan
    server: jp.example.com
    password: secret
  - name: "US 01"
    type: vmess
    server: us.example.com
    uuid: secret
`)
	writeFile(t, filepath.Join(dir, "localclash-packs.gob"), `version: 1
proxy_groups: {}
enabled_packs: []
`)
	if err := os.MkdirAll(paths.cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlanPackIndex(t, paths.cacheDir)
	return paths
}

func writePlanPackIndex(t *testing.T, cacheDir string) {
	t.Helper()
	if err := rules.WritePackIndex(rules.PackIndexPath(cacheDir), map[string]rules.PackCache{
		"blackmatrix7": {
			Version:    1,
			Source:     "blackmatrix7",
			Adapter:    "blackmatrix7",
			Renderable: true,
			Packs: []rules.Pack{
				planPack("Epic", "DIRECT"),
				planPack("OpenAI", "AI"),
				planPack("Steam", "DIRECT"),
			},
		},
		"sukkaw": {
			Version:    1,
			Source:     "sukkaw",
			Adapter:    "sukkaw",
			Renderable: true,
			Packs: []rules.Pack{{
				ID:         "ai",
				Name:       "AI",
				Target:     "AI",
				Renderable: true,
				Components: []rules.Component{{
					ID:         "non_ip",
					Behavior:   "classical",
					Format:     "text",
					OrderClass: "non_ip",
					URL:        "https://ruleset.skk.moe/Clash/non_ip/ai.txt",
					Path:       "./rule-packs/sukkaw/ai_non_ip.txt",
				}},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func planPack(id, target string) rules.Pack {
	return rules.Pack{
		ID:         id,
		Name:       id,
		Target:     target,
		Renderable: true,
		Components: []rules.Component{{
			ID:         id,
			Behavior:   "classical",
			Format:     "yaml",
			OrderClass: "mixed",
			URL:        "https://example.com/" + id + ".yaml",
			Path:       "./rule-packs/blackmatrix7/" + id + ".yaml",
		}},
	}
}

func fixedPlanTime() time.Time {
	return time.Date(2026, 5, 16, 13, 0, 0, 0, time.UTC)
}

func writeFile(t *testing.T, path string, content string) {
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
		if _, ok := doc["proxies"]; ok {
			err = gob.NewEncoder(file).Encode(struct {
				Version int
				Data    map[string]any
				Raw     []byte
			}{Version: 1, Data: doc, Raw: []byte(content)})
		} else {
			var selection rules.Selection
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &selection); err != nil {
				t.Fatal(err)
			}
			err = gob.NewEncoder(file).Encode(selection)
		}
		closeErr := file.Close()
		if err != nil {
			t.Fatal(err)
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

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	writeFile(t, path, content)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func assertSummaryJSON(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.PlanID == "" || result.Output == "" {
		t.Fatalf("summary result = %+v, want plan id and output", result)
	}
}

func readYAMLMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func proxyGroupNames(config map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, raw := range config["proxy-groups"].([]any) {
		group := raw.(map[string]any)
		out[group["name"].(string)] = true
	}
	return out
}

func containsRule(rules []any, want string) bool {
	for _, raw := range rules {
		if raw == want {
			return true
		}
	}
	return false
}

func assertMetadataHasNoSensitiveFields(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, banned := range []string{"sg.example.com", "jp.example.com", "us.example.com", "password", "server", "secret", "uuid"} {
		if strings.Contains(text, banned) {
			t.Fatalf("metadata leaked %q in %s", banned, text)
		}
	}
}
