package configrender

import (
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"localclash/internal/configmeta"
	rulespkg "localclash/internal/rules"
	"localclash/internal/runtimeprofile"

	"gopkg.in/yaml.v3"
)

type StageEvent struct {
	Stage      string         `json:"stage"`
	Event      string         `json:"event"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Error      string         `json:"error,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type Options struct {
	SourcePath         string
	Source             map[string]any `json:"-"`
	OutputPath         string
	PacksSelectionPath string
	Selection          *rulespkg.Selection `json:"-"`
	RulesCacheDir      string
	RuntimeProfilePath string
	Force              bool
	OnStage            func(StageEvent) `json:"-"`
}

type Result struct {
	OutputPath  string
	RuntimeMode string
	Core        string
	ProxyCount  int
	RuleCount   int
}

type ruleSpec struct {
	Domain       string `json:"domain,omitempty"`
	DomainSuffix string `json:"domain_suffix,omitempty"`
	IPCIDR       string `json:"ip_cidr,omitempty"`
	IPCIDR6      string `json:"ip_cidr6,omitempty"`
	GeoIP        string `json:"geoip,omitempty"`
	Match        bool   `json:"match,omitempty"`
	Target       string `json:"target"`
	NoResolve    bool   `json:"no_resolve,omitempty"`
}

var localBaselineRules = []ruleSpec{
	{Domain: "localhost", Target: rulespkg.TerminalDirect},
	{DomainSuffix: "localhost", Target: rulespkg.TerminalDirect},
	{DomainSuffix: "local", Target: rulespkg.TerminalDirect},
	{DomainSuffix: "lan", Target: rulespkg.TerminalDirect},
	{DomainSuffix: "home.arpa", Target: rulespkg.TerminalDirect},
	{IPCIDR: "127.0.0.0/8", Target: rulespkg.TerminalDirect, NoResolve: true},
	{IPCIDR: "10.0.0.0/8", Target: rulespkg.TerminalDirect, NoResolve: true},
	{IPCIDR: "172.16.0.0/12", Target: rulespkg.TerminalDirect, NoResolve: true},
	{IPCIDR: "192.168.0.0/16", Target: rulespkg.TerminalDirect, NoResolve: true},
	{IPCIDR: "169.254.0.0/16", Target: rulespkg.TerminalDirect, NoResolve: true},
	{IPCIDR6: "::1/128", Target: rulespkg.TerminalDirect, NoResolve: true},
	{IPCIDR6: "fc00::/7", Target: rulespkg.TerminalDirect, NoResolve: true},
	{IPCIDR6: "fe80::/10", Target: rulespkg.TerminalDirect, NoResolve: true},
}

func LocalBaselineRuleLines() []string {
	rules, err := renderRuleSpecs(localBaselineRules)
	if err != nil {
		return nil
	}
	return append([]string{}, rules...)
}

func Render(opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	stage := configRenderStageEmitter(opts.OnStage)

	finish := stage("ensure_output", map[string]any{"output": opts.OutputPath, "force": opts.Force})
	if err := ensureOutput(opts.OutputPath, opts.Force); err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, nil)

	finish = stage("read_subscription", map[string]any{"path": opts.SourcePath})
	source := opts.Source
	var err error
	if source == nil {
		source, err = readGobMap(opts.SourcePath)
		if err != nil {
			finish(err, nil)
			return Result{}, err
		}
	}
	finish(nil, map[string]any{"source": renderInputSource(source, opts.Source)})

	finish = stage("read_runtime_profile", map[string]any{"path": opts.RuntimeProfilePath})
	runtimeFile, profile, _, err := runtimeprofile.ActiveProfile(opts.RuntimeProfilePath)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"runtime_mode": runtimeFile.Mode, "core": runtimeFile.Core})

	finish = stage("read_proxies", nil)
	proxies, err := readProxies(source)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	proxyNames, err := proxyNames(proxies)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"proxy_count": len(proxyNames)})
	requiredTargets := dnsProxyGroupReferences(source["dns"], profile.Mihomo["dns"])

	var fragment *rulespkg.Fragment
	var selection *rulespkg.Selection
	if opts.Selection != nil || opts.PacksSelectionPath != "" {
		finish = stage("render_pack_selection", map[string]any{"selection": opts.PacksSelectionPath, "rules_cache": opts.RulesCacheDir})
		if opts.Selection != nil {
			selection = opts.Selection
		} else {
			loadedSelection, err := rulespkg.LoadSelection(opts.PacksSelectionPath)
			if err != nil {
				finish(err, nil)
				return Result{}, err
			}
			selection = &loadedSelection
		}
		selection = selectionWithRequiredTargets(selection, requiredTargets)
		renderedFragment, renderStats, err := rulespkg.RenderSelectionWithStats(*selection, opts.RulesCacheDir, proxyNames)
		if err != nil {
			finish(err, nil)
			return Result{}, err
		}
		fragment = &renderedFragment
		finish(nil, mergeRenderFields(map[string]any{
			"selection_source":    renderSelectionSource(opts.Selection),
			"rule_provider_count": len(renderedFragment.RuleProviders),
			"rule_count":          len(renderedFragment.Rules),
		}, renderStats.Fields()))
	}

	finish = stage("build_runtime_config", nil)
	fallbackTarget := rulespkg.TerminalDirect
	if selection != nil && strings.TrimSpace(selection.FallbackTarget) != "" {
		fallbackTarget = strings.TrimSpace(selection.FallbackTarget)
	}
	rendered, err := buildRuntimeConfig(source, proxies, fragment, fallbackTarget)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, nil)

	if runtimeFile.Core == runtimeprofile.CoreSmart {
		finish = stage("apply_smart_core_groups", nil)
		applySmartCoreProxyGroups(rendered, runtimeFile.Smart)
		finish(nil, nil)
	}
	finish = stage("apply_runtime_profile", map[string]any{"runtime_mode": runtimeFile.Mode, "core": runtimeFile.Core})
	runtimeprofile.ApplyToConfig(rendered, profile)
	finish(nil, nil)

	finish = stage("validate_dns_proxy_groups", map[string]any{"required_targets": len(requiredTargets)})
	if err := validateDNSProxyGroupReferences(rendered); err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, nil)

	rendered[configmeta.Key] = buildLocalClashMetadata(selection, fragment)

	finish = stage("write_output", map[string]any{"output": opts.OutputPath})
	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		finish(err, nil)
		return Result{}, err
	}
	data, err := yaml.Marshal(rendered)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	if err := os.WriteFile(opts.OutputPath, data, 0o644); err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"bytes": len(data)})

	return Result{
		OutputPath:  opts.OutputPath,
		RuntimeMode: runtimeFile.Mode,
		Core:        runtimeFile.Core,
		ProxyCount:  len(proxyNames),
		RuleCount:   len(rendered["rules"].([]string)),
	}, nil
}

func configRenderStageEmitter(callback func(StageEvent)) func(string, map[string]any) func(error, map[string]any) {
	return func(stage string, fields map[string]any) func(error, map[string]any) {
		if callback == nil {
			return func(error, map[string]any) {}
		}
		started := time.Now()
		callback(StageEvent{Stage: stage, Event: "started", Fields: fields})
		return func(err error, doneFields map[string]any) {
			event := StageEvent{
				Stage:      stage,
				Event:      "done",
				DurationMS: time.Since(started).Milliseconds(),
				Fields:     doneFields,
			}
			if err != nil {
				event.Event = "error"
				event.Error = err.Error()
			}
			callback(event)
		}
	}
}

func renderInputSource(source, provided map[string]any) string {
	if provided != nil {
		return "provided"
	}
	return "disk"
}

func renderSelectionSource(selection *rulespkg.Selection) string {
	if selection != nil {
		return "provided"
	}
	return "disk"
}

func mergeRenderFields(base map[string]any, extra map[string]any) map[string]any {
	for key, value := range extra {
		base[key] = value
	}
	return base
}

func selectionWithRequiredTargets(selection *rulespkg.Selection, targets []string) *rulespkg.Selection {
	if selection == nil || len(targets) == 0 {
		return selection
	}
	cloned := *selection
	required := append([]string{}, cloned.RequiredTargets...)
	for _, target := range targets {
		required = appendUnique(required, target)
	}
	cloned.RequiredTargets = required
	return &cloned
}

func dnsProxyGroupReferences(values ...any) []string {
	seen := map[string]bool{}
	var refs []string
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case string:
			ref := dnsProxyGroupReference(typed)
			if ref == "" || seen[ref] {
				return
			}
			seen[ref] = true
			refs = append(refs, ref)
		case []string:
			for _, item := range typed {
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]string:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	for _, value := range values {
		walk(value)
	}
	sort.Strings(refs)
	return refs
}

func dnsProxyGroupReference(value string) string {
	index := strings.LastIndex(value, "#")
	if index < 0 || index == len(value)-1 {
		return ""
	}
	ref := strings.TrimSpace(value[index+1:])
	if cut := strings.IndexAny(ref, "&?"); cut >= 0 {
		ref = strings.TrimSpace(ref[:cut])
	}
	return ref
}

func validateDNSProxyGroupReferences(config map[string]any) error {
	refs := dnsProxyGroupReferences(config["dns"])
	if len(refs) == 0 {
		return nil
	}
	groups := renderedProxyGroupNames(config["proxy-groups"])
	for _, ref := range refs {
		if !groups[ref] {
			return fmt.Errorf("dns references proxy group %q but rendered config does not define it", ref)
		}
	}
	return nil
}

func renderedProxyGroupNames(raw any) map[string]bool {
	out := map[string]bool{}
	switch groups := raw.(type) {
	case []map[string]any:
		for _, group := range groups {
			name := stringValue(group["name"])
			if name != "" {
				out[name] = true
			}
		}
	case []any:
		for _, item := range groups {
			group, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := stringValue(group["name"])
			if name != "" {
				out[name] = true
			}
		}
	}
	return out
}

func buildLocalClashMetadata(selection *rulespkg.Selection, fragment *rulespkg.Fragment) configmeta.Metadata {
	metadata := configmeta.Metadata{
		Version: 1,
		Base: configmeta.BaseMetadata{
			Modifiable:  false,
			Description: "localClash generated base config",
		},
		Overlay: configmeta.OverlayMetadata{
			Modifiable:    true,
			Packs:         []configmeta.OverlayPack{},
			ProxyGroups:   []configmeta.OverlayProxyGroup{},
			PolicyGroups:  []configmeta.OverlayPolicyGroup{},
			RuleProviders: []configmeta.OverlayRuleProvider{},
			Rules:         []configmeta.OverlayRule{},
			Insertion:     "after local safety baseline, before configured fallback",
		},
	}
	if selection != nil {
		usedProxyGroups := map[string]bool{}
		usedPolicyGroups := map[string]bool{}
		markTarget := func(target string) {
			if _, ok := selection.PolicyGroups[target]; ok {
				usedPolicyGroups[target] = true
				for _, exit := range selection.PolicyGroups[target].Exits {
					group, ok := selection.ProxyGroups[exit]
					if ok && !(group.Optional && len(group.Nodes) == 0 && !group.Direct) {
						usedProxyGroups[exit] = true
					}
				}
				return
			}
			if _, ok := selection.ProxyGroups[target]; ok {
				usedProxyGroups[target] = true
			}
		}
		for _, enabled := range selection.EnabledPack {
			metadata.Overlay.Packs = append(metadata.Overlay.Packs, configmeta.OverlayPack{
				Source: enabled.Source,
				Pack:   enabled.Pack,
				Type:   packTypeFromFragment(fragment, enabled),
				Target: enabled.Target,
			})
			markTarget(enabled.Target)
		}
		for _, enabled := range selection.LocalRulePacks {
			metadata.Overlay.Packs = append(metadata.Overlay.Packs, configmeta.OverlayPack{
				Source: "local",
				Pack:   enabled.ID,
				Type:   "local_rule_pack",
				Target: enabled.Target,
			})
			markTarget(enabled.Target)
		}
		for _, custom := range selection.CustomRules {
			markTarget(custom.Target)
		}
		for _, provider := range selection.RuleProviders {
			markTarget(provider.Target)
		}
		for _, target := range selection.RequiredTargets {
			markTarget(target)
		}
		proxyGroupIDs := make([]string, 0, len(usedProxyGroups))
		for id := range usedProxyGroups {
			proxyGroupIDs = append(proxyGroupIDs, id)
		}
		sort.Strings(proxyGroupIDs)
		for _, id := range proxyGroupIDs {
			group := selection.ProxyGroups[id]
			metadata.Overlay.ProxyGroups = append(metadata.Overlay.ProxyGroups, configmeta.OverlayProxyGroup{
				ID:    id,
				Mode:  proxyGroupMode(group),
				Nodes: append([]string(nil), group.Nodes...),
			})
		}
		policyGroupIDs := make([]string, 0, len(usedPolicyGroups))
		for id := range usedPolicyGroups {
			policyGroupIDs = append(policyGroupIDs, id)
		}
		sort.Strings(policyGroupIDs)
		for _, id := range policyGroupIDs {
			group := selection.PolicyGroups[id]
			metadata.Overlay.PolicyGroups = append(metadata.Overlay.PolicyGroups, configmeta.OverlayPolicyGroup{
				ID:    id,
				Mode:  policyGroupMode(group),
				Exits: append([]string(nil), group.Exits...),
			})
		}
	}
	if fragment != nil {
		providerNames := make([]string, 0, len(fragment.RuleProviders))
		for name := range fragment.RuleProviders {
			providerNames = append(providerNames, name)
		}
		sort.Strings(providerNames)
		for _, name := range providerNames {
			provider := fragment.RuleProviders[name]
			metadata.Overlay.RuleProviders = append(metadata.Overlay.RuleProviders, configmeta.OverlayRuleProvider{
				Name:     name,
				Type:     stringValue(provider["type"]),
				Behavior: stringValue(provider["behavior"]),
			})
		}
		for _, line := range fragment.Rules {
			rule, ok := parseOverlayRuleLine(line)
			if ok {
				metadata.Overlay.Rules = append(metadata.Overlay.Rules, rule)
			}
		}
	}
	return metadata
}

func packTypeFromFragment(fragment *rulespkg.Fragment, enabled rulespkg.SelectedPack) string {
	if fragment == nil {
		return ""
	}
	for _, rule := range fragment.Rules {
		parts := strings.Split(rule, ",")
		if len(parts) >= 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "GEOSITE") && strings.TrimSpace(parts[1]) == enabled.Pack {
			return rulespkg.PackTypeGeoSite
		}
	}
	return rulespkg.PackTypeRuleProvider
}

func proxyGroupMode(group rulespkg.ProxyGroup) string {
	switch {
	case group.Auto:
		return "auto"
	case group.Smart:
		return "smart"
	case group.Manual:
		return "manual"
	case group.Direct:
		return "direct"
	default:
		return ""
	}
}

func policyGroupMode(group rulespkg.PolicyGroup) string {
	switch {
	case group.Auto:
		return "auto"
	case group.Smart:
		return "smart"
	case group.Manual:
		return "manual"
	default:
		return ""
	}
}

func parseOverlayRuleLine(line string) (configmeta.OverlayRule, bool) {
	parts := strings.Split(line, ",")
	if len(parts) < 3 {
		return configmeta.OverlayRule{}, false
	}
	switch parts[0] {
	case "RULE-SET":
		return configmeta.OverlayRule{Type: parts[0], Provider: parts[1], Target: parts[2]}, true
	case "DOMAIN", "DOMAIN-SUFFIX", "GEOSITE", "IP-CIDR", "IP-CIDR6":
		return configmeta.OverlayRule{Type: parts[0], Value: parts[1], Target: parts[2]}, true
	default:
		return configmeta.OverlayRule{}, false
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func normalizeOptions(opts Options) Options {
	if opts.SourcePath == "" {
		opts.SourcePath = "subscription.gob"
	}
	if opts.OutputPath == "" {
		opts.OutputPath = "generated/mihomo.yaml"
	}
	if opts.RulesCacheDir == "" {
		opts.RulesCacheDir = ".runtime/rules/packs"
	}
	if opts.RuntimeProfilePath == "" {
		opts.RuntimeProfilePath = runtimeprofile.DefaultPath
	}
	return opts
}

func ensureOutput(path string, force bool) error {
	if strings.TrimSpace(path) == "" || path == "." || path == string(filepath.Separator) {
		return fmt.Errorf("output path %q is not a file path", path)
	}
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("output path %q is a directory", path)
		}
		if !force {
			return fmt.Errorf("output path %q already exists; pass --force to overwrite", path)
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func readGobMap(path string) (map[string]any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gob.Register(map[string]any{})
	gob.Register([]any{})
	var artifact struct {
		Version int
		Data    map[string]any
		Raw     []byte
	}
	if err := gob.NewDecoder(file).Decode(&artifact); err != nil {
		return nil, err
	}
	if artifact.Version != 1 {
		return nil, fmt.Errorf("subscription artifact schema version mismatch: expected 1, got %d; run localclash subscriptions refresh", artifact.Version)
	}
	return artifact.Data, nil
}

func readProxies(source map[string]any) ([]any, error) {
	raw, ok := source["proxies"]
	if !ok {
		return nil, errors.New("source subscription has no proxies")
	}
	proxies, ok := raw.([]any)
	if !ok || len(proxies) == 0 {
		return nil, errors.New("source subscription proxies is empty or invalid")
	}
	return proxies, nil
}

func proxyNames(proxies []any) ([]string, error) {
	names := make([]string, 0, len(proxies))
	seen := map[string]bool{}
	for _, raw := range proxies {
		proxy, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("source proxy entry is not a map")
		}
		name, ok := proxy["name"].(string)
		if !ok || name == "" {
			return nil, errors.New("source proxy entry has no name")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names, nil
}

func buildRuntimeConfig(source map[string]any, proxies []any, fragment *rulespkg.Fragment, fallbackTarget string) (map[string]any, error) {
	config := map[string]any{
		"mixed-port":          7890,
		"allow-lan":           false,
		"mode":                "rule",
		"log-level":           "info",
		"external-controller": "127.0.0.1:9090",
		"external-ui":         "ui/zashboard",
		"unified-delay":       true,
		"proxies":             proxies,
	}
	if hosts, ok := source["hosts"]; ok {
		config["hosts"] = hosts
	}
	if dns, ok := source["dns"]; ok {
		config["dns"] = withLocalDNSPolicy(dns)
	}

	ruleProviders := map[string]any{}
	if fragment != nil {
		if err := mergeRuleProviders(ruleProviders, fragment.RuleProviders); err != nil {
			return nil, err
		}
	}
	config["rule-providers"] = ruleProviders

	rules, err := buildOrderedRules(fragment, fallbackTarget)
	if err != nil {
		return nil, err
	}
	proxyGroups := []map[string]any{}
	if fragment != nil {
		proxyGroups = append(proxyGroups, fragment.ProxyGroups...)
	}
	sortProxyGroupsByDisplay(proxyGroups, baseManualChoiceSet(fragment))
	config["proxy-groups"] = proxyGroups

	config["rules"] = rules
	return config, nil
}

func baseManualChoiceSet(fragment *rulespkg.Fragment) map[string]bool {
	out := map[string]bool{}
	if fragment == nil {
		return out
	}
	for _, choice := range fragment.BaseManualChoices {
		out[choice] = true
	}
	return out
}

func sortProxyGroupsByDisplay(groups []map[string]any, regionGroups map[string]bool) {
	sort.SliceStable(groups, func(i, j int) bool {
		leftName, _ := groups[i]["name"].(string)
		rightName, _ := groups[j]["name"].(string)
		leftPinned := proxyGroupPinnedRank(leftName)
		rightPinned := proxyGroupPinnedRank(rightName)
		if leftPinned != rightPinned {
			return leftPinned < rightPinned
		}
		leftRegion := regionGroups[leftName]
		rightRegion := regionGroups[rightName]
		if leftRegion != rightRegion {
			return !leftRegion
		}
		leftKey := proxyGroupDisplaySortKey(leftName)
		rightKey := proxyGroupDisplaySortKey(rightName)
		if leftKey == rightKey {
			return leftName < rightName
		}
		return leftKey < rightKey
	})
}

func proxyGroupPinnedRank(name string) int {
	switch proxyGroupDisplaySortKey(name) {
	case "手动选择":
		return 0
	case "自动选择":
		return 1
	default:
		return 2
	}
}

func proxyGroupDisplaySortKey(name string) string {
	trimmed := strings.TrimSpace(name)
	trimmed = strings.TrimLeftFunc(trimmed, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	return strings.TrimSpace(trimmed)
}

func applySmartCoreProxyGroups(config map[string]any, opts runtimeprofile.SmartOptions) {
	groups, ok := config["proxy-groups"].([]map[string]any)
	if !ok {
		return
	}
	for _, group := range groups {
		groupType := strings.ToLower(strings.TrimSpace(stringValue(group["type"])))
		if groupType == "url-test" {
			group["type"] = "smart"
			delete(group, "url")
			delete(group, "interval")
			delete(group, "tolerance")
			groupType = "smart"
		}
		if groupType == "smart" {
			applySmartGroupOptions(group, opts)
		}
	}
}

func applySmartGroupOptions(group map[string]any, opts runtimeprofile.SmartOptions) {
	if opts.UseLightGBM {
		setDefaultAny(group, "uselightgbm", true)
	}
	if opts.PreferASN {
		setDefaultAny(group, "prefer-asn", true)
	}
	if opts.CollectData {
		setDefaultAny(group, "collectdata", true)
	}
	if opts.SampleRate != 0 {
		setDefaultAny(group, "sample-rate", opts.SampleRate)
	}
	if strings.TrimSpace(opts.PolicyPriority) != "" {
		setDefaultAny(group, "policy-priority", opts.PolicyPriority)
	}
}

func setDefaultAny(values map[string]any, key string, value any) {
	if _, exists := values[key]; !exists {
		values[key] = value
	}
}

func buildOrderedRules(fragment *rulespkg.Fragment, fallbackTarget string) ([]string, error) {
	baseline, err := renderRuleSpecs(localBaselineRules)
	if err != nil {
		return nil, err
	}
	fallbackTarget = strings.TrimSpace(fallbackTarget)
	if fallbackTarget == "" {
		fallbackTarget = rulespkg.TerminalDirect
	}
	rules := make([]string, 0, len(baseline)+1)
	rules = append(rules, baseline...)
	if fragment != nil {
		rules = append(rules, fragment.Rules...)
	}
	rules = append(rules, "MATCH,"+fallbackTarget)
	return rules, nil
}

func mergeRuleProviders(base map[string]any, extra map[string]map[string]any) error {
	for name, provider := range extra {
		if _, exists := base[name]; exists {
			return fmt.Errorf("rule-provider %q already exists", name)
		}
		base[name] = provider
	}
	return nil
}

func withLocalDNSPolicy(raw any) any {
	dns, ok := raw.(map[string]any)
	if !ok {
		return raw
	}
	dns = cloneMap(dns)
	dns["use-system-hosts"] = true

	policy, _ := dns["nameserver-policy"].(map[string]any)
	policy = cloneMap(policy)
	for _, domain := range []string{"+.local", "+.lan", "+.home.arpa", "localhost", "+.localhost"} {
		policy[domain] = "system"
	}
	dns["nameserver-policy"] = policy

	filter := stringSlice(dns["fake-ip-filter"])
	for _, domain := range []string{"*.local", "+.local", "*.lan", "+.lan", "*.home.arpa", "+.home.arpa", "localhost", "+.localhost"} {
		filter = appendUnique(filter, domain)
	}
	dns["fake-ip-filter"] = filter

	return dns
}

func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringSlice(raw any) []string {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s, ok := value.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func renderRuleSpecs(specs []ruleSpec) ([]string, error) {
	rules := make([]string, 0, len(specs))
	for _, rule := range specs {
		target := strings.TrimSpace(rule.Target)
		if target == "" {
			return nil, errors.New("rule target is required")
		}
		switch {
		case rule.Domain != "":
			rules = append(rules, fmt.Sprintf("DOMAIN,%s,%s", rule.Domain, target))
		case rule.DomainSuffix != "":
			rules = append(rules, fmt.Sprintf("DOMAIN-SUFFIX,%s,%s", rule.DomainSuffix, target))
		case rule.IPCIDR != "":
			line := fmt.Sprintf("IP-CIDR,%s,%s", rule.IPCIDR, target)
			if rule.NoResolve {
				line += ",no-resolve"
			}
			rules = append(rules, line)
		case rule.IPCIDR6 != "":
			line := fmt.Sprintf("IP-CIDR6,%s,%s", rule.IPCIDR6, target)
			if rule.NoResolve {
				line += ",no-resolve"
			}
			rules = append(rules, line)
		case rule.GeoIP != "":
			rules = append(rules, fmt.Sprintf("GEOIP,%s,%s", rule.GeoIP, target))
		case rule.Match:
			rules = append(rules, fmt.Sprintf("MATCH,%s", target))
		default:
			return nil, errors.New("empty local rule")
		}
	}
	return rules, nil
}
