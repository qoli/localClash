package configinspect

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"localclash/internal/configmeta"
	"localclash/internal/localconfig"

	"gopkg.in/yaml.v3"
)

type Options struct {
	ConfigPath string
	Limit      int
}

type IntentOptions struct {
	ConfigPath          string
	Subscription        string
	SubscriptionConfig  string
	SubscriptionRuntime string
	RulesCache          string
	Limit               int
	SkipResolve         bool
}

type BaseResult struct {
	Config     string      `json:"config"`
	Layer      string      `json:"layer"`
	Modifiable bool        `json:"modifiable"`
	Summary    BaseSummary `json:"summary"`
	Note       string      `json:"note"`
}

type BaseSummary struct {
	Mode               string                `json:"mode"`
	ExternalController string                `json:"external_controller,omitempty"`
	ExternalUI         string                `json:"external_ui,omitempty"`
	ProxiesCount       int                   `json:"proxies_count"`
	ProxyGroups        []ProxyGroupSummary   `json:"proxy_groups"`
	RuleProviders      []RuleProviderSummary `json:"rule_providers"`
	RulesCount         int                   `json:"rules_count"`
	RulesSample        []string              `json:"rules_sample"`
}

type ProxyGroupSummary struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	ProxiesCount int    `json:"proxies_count"`
}

type RuleProviderSummary struct {
	Name     string `json:"name"`
	Behavior string `json:"behavior"`
	Type     string `json:"type"`
}

type OverlayResult struct {
	Config          string                           `json:"config"`
	Layer           string                           `json:"layer"`
	Modifiable      bool                             `json:"modifiable"`
	MetadataPresent bool                             `json:"metadata_present"`
	OverlayPresent  bool                             `json:"overlay_present"`
	Packs           []configmeta.OverlayPack         `json:"packs"`
	ProxyGroups     []configmeta.OverlayProxyGroup   `json:"proxy_groups"`
	PolicyGroups    []configmeta.OverlayPolicyGroup  `json:"policy_groups"`
	RuleProviders   []configmeta.OverlayRuleProvider `json:"rule_providers"`
	Rules           []configmeta.OverlayRule         `json:"rules"`
	Insertion       string                           `json:"insertion,omitempty"`
}

type IntentResult struct {
	Config             string               `json:"config"`
	Layer              string               `json:"layer"`
	Modifiable         bool                 `json:"modifiable"`
	Exists             bool                 `json:"exists"`
	Valid              bool                 `json:"valid"`
	Resolved           bool                 `json:"resolved"`
	ResolveSkipped     bool                 `json:"resolve_skipped,omitempty"`
	Version            int                  `json:"version,omitempty"`
	PolicyTemplate     string               `json:"policy_template,omitempty"`
	LoadError          string               `json:"load_error,omitempty"`
	ResolveError       string               `json:"resolve_error,omitempty"`
	ProxyGroupsCount   int                  `json:"proxy_groups_count"`
	ProxyGroups        []IntentProxyGroup   `json:"proxy_groups"`
	PolicyGroupsCount  int                  `json:"policy_groups_count"`
	PolicyGroups       []IntentPolicyGroup  `json:"policy_groups"`
	CustomRulesCount   int                  `json:"custom_rules_count"`
	CustomRules        []IntentCustomRule   `json:"custom_rules"`
	RulePacksCount     int                  `json:"enabled_rule_packs_count"`
	RulePacks          []IntentRulePack     `json:"enabled_rule_packs"`
	RuleProvidersCount int                  `json:"rule_providers_count"`
	RuleProviders      []IntentRuleProvider `json:"rule_providers"`
	PacksCount         int                  `json:"packs_count"`
	Packs              []IntentPack         `json:"packs"`
	Truncated          bool                 `json:"truncated,omitempty"`
	Note               string               `json:"note"`
}

type IntentProxyGroup struct {
	ID            string             `json:"id"`
	Mode          string             `json:"mode"`
	Match         *localconfig.Match `json:"match,omitempty"`
	Nodes         []string           `json:"nodes,omitempty"`
	SelectedNodes []string           `json:"selected_nodes,omitempty"`
	NodeCount     int                `json:"node_count"`
	Reason        string             `json:"reason,omitempty"`
	Boundary      string             `json:"boundary,omitempty"`
	Status        string             `json:"status"`
}

type IntentPolicyGroup struct {
	ID        string   `json:"id"`
	Mode      string   `json:"mode"`
	Exits     []string `json:"exits"`
	ExitCount int      `json:"exit_count"`
	Reason    string   `json:"reason,omitempty"`
	Boundary  string   `json:"boundary,omitempty"`
	Status    string   `json:"status"`
}

type IntentCustomRule struct {
	ID        string                       `json:"id"`
	Target    string                       `json:"target"`
	RuleCount int                          `json:"rule_count"`
	Reason    string                       `json:"reason,omitempty"`
	Rules     []localconfig.CustomRuleLine `json:"rules,omitempty"`
	Status    string                       `json:"status"`
}

type IntentRulePack struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Target    string `json:"target"`
	RuleCount int    `json:"rule_count"`
	Reason    string `json:"reason,omitempty"`
	Status    string `json:"status"`
}

type IntentRuleProvider struct {
	ID       string `json:"id"`
	Target   string `json:"target"`
	Reason   string `json:"reason,omitempty"`
	Type     string `json:"type"`
	Behavior string `json:"behavior"`
	Format   string `json:"format"`
	Path     string `json:"path"`
	URL      string `json:"url,omitempty"`
	Interval int    `json:"interval,omitempty"`
	Status   string `json:"status"`
}

type IntentPack struct {
	Source string `json:"source"`
	Pack   string `json:"pack"`
	Type   string `json:"type,omitempty"`
	Target string `json:"target"`
	Reason string `json:"reason,omitempty"`
	Status string `json:"status"`
}

func InspectBase(opts Options) (BaseResult, error) {
	path := defaultConfigPath(opts.ConfigPath)
	limit := normalizedLimit(opts.Limit)
	config, err := readConfigMap(path)
	if err != nil {
		return BaseResult{}, err
	}
	metadata, metadataPresent, err := readMetadata(config)
	if err != nil {
		return BaseResult{}, err
	}
	overlayProviderNames := map[string]bool{}
	overlayProxyGroupNames := map[string]bool{}
	overlayRuleLines := map[string]bool{}
	if metadataPresent {
		for _, provider := range metadata.Overlay.RuleProviders {
			overlayProviderNames[provider.Name] = true
		}
		for _, group := range metadata.Overlay.ProxyGroups {
			overlayProxyGroupNames[group.ID] = true
		}
		for _, group := range metadata.Overlay.PolicyGroups {
			overlayProxyGroupNames[group.ID] = true
		}
		for _, rule := range metadata.Overlay.Rules {
			overlayRuleLines[formatOverlayRule(rule)] = true
		}
	}
	rules := filterStrings(stringSlice(config["rules"]), overlayRuleLines)
	return BaseResult{
		Config:     path,
		Layer:      "base",
		Modifiable: false,
		Summary: BaseSummary{
			Mode:               stringFromMap(config, "mode"),
			ExternalController: stringFromMap(config, "external-controller"),
			ExternalUI:         stringFromMap(config, "external-ui"),
			ProxiesCount:       len(anySlice(config["proxies"])),
			ProxyGroups:        limitProxyGroups(filterProxyGroups(summarizeProxyGroups(config["proxy-groups"]), overlayProxyGroupNames), limit),
			RuleProviders:      limitRuleProviders(filterRuleProviders(summarizeRuleProviders(config["rule-providers"]), overlayProviderNames), limit),
			RulesCount:         len(rules),
			RulesSample:        limitStrings(rules, limit),
		},
		Note: "Base config is generated by localClash and is not modified through MCP plan tools.",
	}, nil
}

func InspectOverlay(opts Options) (OverlayResult, error) {
	path := defaultConfigPath(opts.ConfigPath)
	limit := normalizedLimit(opts.Limit)
	config, err := readConfigMap(path)
	if err != nil {
		return OverlayResult{}, err
	}
	result := OverlayResult{
		Config:        path,
		Layer:         "overlay",
		Modifiable:    true,
		Packs:         []configmeta.OverlayPack{},
		ProxyGroups:   []configmeta.OverlayProxyGroup{},
		PolicyGroups:  []configmeta.OverlayPolicyGroup{},
		RuleProviders: []configmeta.OverlayRuleProvider{},
		Rules:         []configmeta.OverlayRule{},
	}
	metadata, ok, err := readMetadata(config)
	if err != nil {
		return OverlayResult{}, err
	}
	if !ok {
		return result, nil
	}
	result.MetadataPresent = true
	result.Packs = limitOverlayPacks(metadata.Overlay.Packs, limit)
	result.ProxyGroups = limitOverlayProxyGroups(metadata.Overlay.ProxyGroups, limit)
	result.PolicyGroups = limitOverlayPolicyGroups(metadata.Overlay.PolicyGroups, limit)
	result.RuleProviders = limitOverlayRuleProviders(metadata.Overlay.RuleProviders, limit)
	result.Rules = limitOverlayRules(metadata.Overlay.Rules, limit)
	result.Insertion = metadata.Overlay.Insertion
	result.OverlayPresent = len(metadata.Overlay.Packs) > 0 ||
		len(metadata.Overlay.ProxyGroups) > 0 ||
		len(metadata.Overlay.PolicyGroups) > 0 ||
		len(metadata.Overlay.RuleProviders) > 0 ||
		len(metadata.Overlay.Rules) > 0
	return result, nil
}

func InspectIntent(opts IntentOptions) (IntentResult, error) {
	path := defaultIntentConfigPath(opts.ConfigPath)
	limit := normalizedLimit(opts.Limit)
	result := IntentResult{
		Config:        path,
		Layer:         "intent",
		Modifiable:    true,
		ProxyGroups:   []IntentProxyGroup{},
		PolicyGroups:  []IntentPolicyGroup{},
		CustomRules:   []IntentCustomRule{},
		RulePacks:     []IntentRulePack{},
		RuleProviders: []IntentRuleProvider{},
		Packs:         []IntentPack{},
		Note:          "Intent is read from durable localclash.json. Use it before creating a patch to preserve existing proxy groups, policy groups, packs, enabled rule packs, custom rules, and rule providers.",
	}
	config, err := localconfig.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		result.Exists = fileExists(path)
		result.LoadError = err.Error()
		return result, nil
	}
	if config.Version == 0 {
		config.Version = localconfig.ConfigSchemaVersion
	}
	result.Exists = true
	result.Valid = true
	result.Version = config.Version
	result.PolicyTemplate = config.PolicyTemplate
	result.ProxyGroupsCount = len(config.ProxyGroups)
	result.PolicyGroupsCount = len(config.PolicyGroups)
	result.CustomRulesCount = len(config.CustomRules)
	result.RulePacksCount = len(config.EnabledRulePacks)
	result.RuleProvidersCount = len(config.RuleProviders)
	result.PacksCount = len(config.Packs)

	result.ProxyGroups = proxyGroupIntents(config.ProxyGroups, nil, limit)
	result.PolicyGroups = policyGroupIntents(config.PolicyGroups, nil, limit)
	result.CustomRules = customRuleIntents(config.CustomRules, nil, limit)
	result.RulePacks = rulePackIntents(config.EnabledRulePacks, nil, limit)
	result.RuleProviders = ruleProviderIntents(config.RuleProviders, nil, limit)
	result.Packs = packIntents(config.Packs, nil, limit)
	result.Truncated = result.ProxyGroupsCount > len(result.ProxyGroups) ||
		result.PolicyGroupsCount > len(result.PolicyGroups) ||
		result.CustomRulesCount > len(result.CustomRules) ||
		result.RulePacksCount > len(result.RulePacks) ||
		result.RuleProvidersCount > len(result.RuleProviders) ||
		result.PacksCount > len(result.Packs)

	if opts.SkipResolve {
		result.ResolveSkipped = true
		return result, nil
	}
	resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
		Config:              config,
		SubscriptionPath:    opts.Subscription,
		SubscriptionConfig:  opts.SubscriptionConfig,
		SubscriptionRuntime: opts.SubscriptionRuntime,
		RulesCache:          opts.RulesCache,
	})
	if err != nil {
		result.ResolveError = err.Error()
		return result, nil
	}
	result.Resolved = true
	result.ProxyGroups = proxyGroupIntents(resolved.Config.ProxyGroups, resolved.ProxyGroups, limit)
	result.PolicyGroups = policyGroupIntents(resolved.Config.PolicyGroups, resolved.PolicyGroups, limit)
	result.CustomRules = customRuleIntents(resolved.Config.CustomRules, resolved.CustomRules, limit)
	result.RulePacks = rulePackIntents(resolved.Config.EnabledRulePacks, resolved.RulePacks, limit)
	result.RuleProviders = ruleProviderIntents(resolved.Config.RuleProviders, resolved.RuleProviders, limit)
	result.Packs = packIntents(resolved.Config.Packs, resolved.Packs, limit)
	return result, nil
}

func readMetadata(config map[string]any) (configmeta.Metadata, bool, error) {
	raw, ok := config[configmeta.Key]
	if !ok {
		return configmeta.Metadata{}, false, nil
	}
	var metadata configmeta.Metadata
	data, err := yaml.Marshal(raw)
	if err != nil {
		return configmeta.Metadata{}, false, err
	}
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return configmeta.Metadata{}, false, err
	}
	return metadata, true, nil
}

func readConfigMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config %q does not exist; run config_render first", path)
		}
		return nil, err
	}
	var config map[string]any
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return config, nil
}

func defaultConfigPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return "generated/mihomo.yaml"
	}
	return path
}

func defaultIntentConfigPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return "localclash.json"
	}
	return path
}

func normalizedLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	return limit
}

func proxyGroupIntents(groups map[string]localconfig.ProxyGroup, resolved []localconfig.ProxyGroupResult, limit int) []IntentProxyGroup {
	resolvedByID := map[string]localconfig.ProxyGroupResult{}
	for _, group := range resolved {
		resolvedByID[group.ID] = group
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]IntentProxyGroup, 0, len(ids))
	for _, id := range ids {
		group := groups[id]
		selected := append([]string{}, group.SelectedNodes...)
		nodeCount := len(selected)
		status := "configured"
		if resolvedGroup, ok := resolvedByID[id]; ok {
			selected = append([]string{}, resolvedGroup.SelectedNodes...)
			nodeCount = resolvedGroup.NodeCount
			status = "resolved"
		} else if nodeCount == 0 {
			nodeCount = len(group.Nodes)
		}
		out = append(out, IntentProxyGroup{
			ID:            id,
			Mode:          strings.ToLower(strings.TrimSpace(group.Mode)),
			Match:         group.Match,
			Nodes:         append([]string{}, group.Nodes...),
			SelectedNodes: selected,
			NodeCount:     nodeCount,
			Reason:        group.Reason,
			Boundary:      group.Boundary,
			Status:        status,
		})
	}
	return out
}

func policyGroupIntents(groups map[string]localconfig.PolicyGroup, resolved []localconfig.PolicyGroupResult, limit int) []IntentPolicyGroup {
	resolvedByID := map[string]localconfig.PolicyGroupResult{}
	for _, group := range resolved {
		resolvedByID[group.ID] = group
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]IntentPolicyGroup, 0, len(ids))
	for _, id := range ids {
		group := groups[id]
		exits := append([]string{}, group.Exits...)
		exitCount := len(exits)
		status := "configured"
		if resolvedGroup, ok := resolvedByID[id]; ok {
			exits = append([]string{}, resolvedGroup.Exits...)
			exitCount = resolvedGroup.ExitCount
			status = "resolved"
		}
		out = append(out, IntentPolicyGroup{
			ID:        id,
			Mode:      strings.ToLower(strings.TrimSpace(group.Mode)),
			Exits:     exits,
			ExitCount: exitCount,
			Reason:    group.Reason,
			Boundary:  group.Boundary,
			Status:    status,
		})
	}
	return out
}

func customRuleIntents(rules []localconfig.CustomRule, resolved []localconfig.CustomRuleResult, limit int) []IntentCustomRule {
	resolvedByID := map[string]localconfig.CustomRuleResult{}
	for _, rule := range resolved {
		resolvedByID[rule.ID] = rule
	}
	if len(rules) > limit {
		rules = rules[:limit]
	}
	out := make([]IntentCustomRule, 0, len(rules))
	for _, rule := range rules {
		status := "configured"
		if _, ok := resolvedByID[rule.ID]; ok {
			status = "resolved"
		}
		out = append(out, IntentCustomRule{
			ID:        rule.ID,
			Target:    rule.Target,
			RuleCount: len(rule.Rules),
			Reason:    rule.Reason,
			Rules:     append([]localconfig.CustomRuleLine{}, rule.Rules...),
			Status:    status,
		})
	}
	return out
}

func rulePackIntents(packs []localconfig.RulePackSelection, resolved []localconfig.RulePackResult, limit int) []IntentRulePack {
	resolvedByID := map[string]localconfig.RulePackResult{}
	for _, pack := range resolved {
		resolvedByID[pack.ID] = pack
	}
	if len(packs) > limit {
		packs = packs[:limit]
	}
	out := make([]IntentRulePack, 0, len(packs))
	for _, pack := range packs {
		status := "configured"
		name := ""
		ruleCount := 0
		target := pack.Target
		reason := pack.Reason
		if resolvedPack, ok := resolvedByID[pack.ID]; ok {
			status = "resolved"
			name = resolvedPack.Name
			ruleCount = resolvedPack.RuleCount
			target = resolvedPack.Target
			reason = resolvedPack.Reason
		}
		out = append(out, IntentRulePack{
			ID:        pack.ID,
			Name:      name,
			Target:    target,
			RuleCount: ruleCount,
			Reason:    reason,
			Status:    status,
		})
	}
	return out
}

func ruleProviderIntents(providers []localconfig.ExternalRuleProvider, resolved []localconfig.RuleProviderResult, limit int) []IntentRuleProvider {
	resolvedByID := map[string]localconfig.RuleProviderResult{}
	for _, provider := range resolved {
		resolvedByID[provider.ID] = provider
	}
	if len(providers) > limit {
		providers = providers[:limit]
	}
	out := make([]IntentRuleProvider, 0, len(providers))
	for _, provider := range providers {
		status := "configured"
		if resolvedProvider, ok := resolvedByID[provider.ID]; ok {
			status = "resolved"
			provider.Type = resolvedProvider.Type
			provider.Behavior = resolvedProvider.Behavior
			provider.Format = resolvedProvider.Format
			provider.Path = resolvedProvider.Path
			provider.URL = resolvedProvider.URL
			provider.Interval = resolvedProvider.Interval
		}
		out = append(out, IntentRuleProvider{
			ID:       provider.ID,
			Target:   provider.Target,
			Reason:   provider.Reason,
			Type:     provider.Type,
			Behavior: provider.Behavior,
			Format:   provider.Format,
			Path:     provider.Path,
			URL:      provider.URL,
			Interval: provider.Interval,
			Status:   status,
		})
	}
	return out
}

func packIntents(packs []localconfig.Pack, resolved []localconfig.PackResult, limit int) []IntentPack {
	resolvedByKey := map[string]localconfig.PackResult{}
	for _, pack := range resolved {
		resolvedByKey[packKey(pack.Source, pack.Pack)] = pack
	}
	if len(packs) > limit {
		packs = packs[:limit]
	}
	out := make([]IntentPack, 0, len(packs))
	for _, pack := range packs {
		status := "configured"
		if _, ok := resolvedByKey[packKey(pack.Source, pack.Pack)]; ok {
			status = "resolved"
		}
		out = append(out, IntentPack{Source: pack.Source, Pack: pack.Pack, Type: pack.Type, Target: pack.Target, Reason: pack.Reason, Status: status})
	}
	return out
}

func packKey(source, pack string) string {
	return strings.TrimSpace(source) + "/" + strings.TrimSpace(pack)
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func summarizeProxyGroups(value any) []ProxyGroupSummary {
	var out []ProxyGroupSummary
	for _, raw := range anySlice(value) {
		group, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, ProxyGroupSummary{
			Name:         stringFromMap(group, "name"),
			Type:         stringFromMap(group, "type"),
			ProxiesCount: len(anySlice(group["proxies"])),
		})
	}
	return out
}

func summarizeRuleProviders(value any) []RuleProviderSummary {
	providers, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]RuleProviderSummary, 0, len(names))
	for _, name := range names {
		provider, _ := providers[name].(map[string]any)
		out = append(out, RuleProviderSummary{
			Name:     name,
			Behavior: stringFromMap(provider, "behavior"),
			Type:     stringFromMap(provider, "type"),
		})
	}
	return out
}

func filterProxyGroups(values []ProxyGroupSummary, excluded map[string]bool) []ProxyGroupSummary {
	if len(excluded) == 0 {
		return values
	}
	out := make([]ProxyGroupSummary, 0, len(values))
	for _, value := range values {
		if !excluded[value.Name] {
			out = append(out, value)
		}
	}
	return out
}

func filterRuleProviders(values []RuleProviderSummary, excluded map[string]bool) []RuleProviderSummary {
	if len(excluded) == 0 {
		return values
	}
	out := make([]RuleProviderSummary, 0, len(values))
	for _, value := range values {
		if !excluded[value.Name] {
			out = append(out, value)
		}
	}
	return out
}

func filterStrings(values []string, excluded map[string]bool) []string {
	if len(excluded) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !excluded[value] {
			out = append(out, value)
		}
	}
	return out
}

func formatOverlayRule(rule configmeta.OverlayRule) string {
	if rule.Type == "" || rule.Target == "" {
		return ""
	}
	if rule.Provider == "" && rule.Value != "" {
		return rule.Type + "," + rule.Value + "," + rule.Target
	}
	if rule.Provider == "" {
		return ""
	}
	return rule.Type + "," + rule.Provider + "," + rule.Target
}

func anySlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return nil
}

func stringSlice(value any) []string {
	var out []string
	for _, item := range anySlice(value) {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	if text, ok := values[key].(string); ok {
		return text
	}
	return ""
}

func limitProxyGroups(values []ProxyGroupSummary, limit int) []ProxyGroupSummary {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]ProxyGroupSummary{}, values...)
}

func limitRuleProviders(values []RuleProviderSummary, limit int) []RuleProviderSummary {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]RuleProviderSummary{}, values...)
}

func limitStrings(values []string, limit int) []string {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]string{}, values...)
}

func limitOverlayPacks(values []configmeta.OverlayPack, limit int) []configmeta.OverlayPack {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]configmeta.OverlayPack{}, values...)
}

func limitOverlayProxyGroups(values []configmeta.OverlayProxyGroup, limit int) []configmeta.OverlayProxyGroup {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]configmeta.OverlayProxyGroup{}, values...)
}

func limitOverlayPolicyGroups(values []configmeta.OverlayPolicyGroup, limit int) []configmeta.OverlayPolicyGroup {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]configmeta.OverlayPolicyGroup{}, values...)
}

func limitOverlayRuleProviders(values []configmeta.OverlayRuleProvider, limit int) []configmeta.OverlayRuleProvider {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]configmeta.OverlayRuleProvider{}, values...)
}

func limitOverlayRules(values []configmeta.OverlayRule, limit int) []configmeta.OverlayRule {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]configmeta.OverlayRule{}, values...)
}
