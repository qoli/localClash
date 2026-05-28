package localconfig

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"localclash/internal/rules"
)

const ConfigSchemaVersion = 3

type Config struct {
	Version          int                    `json:"version" yaml:"version"`
	PolicyTemplate   string                 `json:"policy_template,omitempty" yaml:"policy_template,omitempty"`
	FallbackTarget   string                 `json:"fallback_target,omitempty" yaml:"fallback_target,omitempty"`
	ProxyGroups      map[string]ProxyGroup  `json:"proxy_groups" yaml:"proxy_groups,omitempty"`
	PolicyGroups     map[string]PolicyGroup `json:"policy_groups,omitempty" yaml:"policy_groups,omitempty"`
	CustomRules      []CustomRule           `json:"custom_rules,omitempty" yaml:"custom_rules,omitempty"`
	EnabledRulePacks []RulePackSelection    `json:"enabled_rule_packs,omitempty" yaml:"enabled_rule_packs,omitempty"`
	RuleProviders    []ExternalRuleProvider `json:"rule_providers,omitempty" yaml:"rule_providers,omitempty"`
	Packs            []Pack                 `json:"packs" yaml:"packs,omitempty"`
}

type ProxyGroup struct {
	Mode          string   `json:"mode" yaml:"mode"`
	Match         *Match   `json:"match,omitempty" yaml:"match,omitempty"`
	Nodes         []string `json:"nodes,omitempty" yaml:"nodes,omitempty"`
	SelectedNodes []string `json:"selected_nodes,omitempty" yaml:"selected_nodes,omitempty"`
	Optional      bool     `json:"optional,omitempty" yaml:"optional,omitempty"`
	Reason        string   `json:"reason,omitempty" yaml:"reason,omitempty"`
	Boundary      string   `json:"boundary,omitempty" yaml:"boundary,omitempty"`
}

type PolicyGroup struct {
	Mode     string   `json:"mode" yaml:"mode"`
	Exits    []string `json:"exits" yaml:"exits"`
	Reason   string   `json:"reason,omitempty" yaml:"reason,omitempty"`
	Boundary string   `json:"boundary,omitempty" yaml:"boundary,omitempty"`
}

type Match struct {
	Type          string   `json:"type" yaml:"type"`
	Pattern       string   `json:"pattern" yaml:"pattern"`
	SourceIDs     []string `json:"source_ids,omitempty" yaml:"source_ids,omitempty"`
	Min           int      `json:"min,omitempty" yaml:"min,omitempty"`
	Max           int      `json:"max,omitempty" yaml:"max,omitempty"`
	CaseSensitive bool     `json:"case_sensitive,omitempty" yaml:"case_sensitive,omitempty"`
}

type Pack struct {
	Source string `json:"source" yaml:"source"`
	Pack   string `json:"pack" yaml:"pack"`
	Type   string `json:"type,omitempty" yaml:"type,omitempty"`
	Target string `json:"target" yaml:"target"`
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

func (pack *Pack) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID     *json.RawMessage `json:"id"`
		Source string           `json:"source"`
		Pack   string           `json:"pack"`
		Type   string           `json:"type"`
		Target string           `json:"target"`
		Reason string           `json:"reason"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.ID != nil {
		return fmt.Errorf("packs[].id is no longer supported; use packs[].source and packs[].pack")
	}
	*pack = Pack{Source: raw.Source, Pack: raw.Pack, Type: raw.Type, Target: raw.Target, Reason: raw.Reason}
	return nil
}

type CustomRule struct {
	ID     string           `json:"id" yaml:"id"`
	Target string           `json:"target" yaml:"target"`
	Reason string           `json:"reason,omitempty" yaml:"reason,omitempty"`
	Rules  []CustomRuleLine `json:"rules" yaml:"rules"`
}

type CustomRuleLine struct {
	Type      string `json:"type" yaml:"type"`
	Value     string `json:"value" yaml:"value"`
	NoResolve bool   `json:"no_resolve,omitempty" yaml:"no_resolve,omitempty"`
}

type RulePackSelection struct {
	ID     string `json:"id" yaml:"id"`
	Target string `json:"target,omitempty" yaml:"target,omitempty"`
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type LocalRulePack struct {
	ID            string              `json:"id" yaml:"id"`
	Name          string              `json:"name,omitempty" yaml:"name,omitempty"`
	Description   string              `json:"description,omitempty" yaml:"description,omitempty"`
	Version       int                 `json:"version" yaml:"version"`
	DefaultTarget string              `json:"default_target,omitempty" yaml:"default_target,omitempty"`
	TargetOptions []string            `json:"target_options,omitempty" yaml:"target_options,omitempty"`
	Rules         []LocalRulePackRule `json:"rules" yaml:"rules"`
}

type LocalRulePackRule struct {
	Type         string `json:"type,omitempty" yaml:"type,omitempty"`
	Value        string `json:"value,omitempty" yaml:"value,omitempty"`
	Domain       string `json:"domain,omitempty" yaml:"domain,omitempty"`
	DomainSuffix string `json:"domain_suffix,omitempty" yaml:"domain_suffix,omitempty"`
	IPCIDR       string `json:"ip_cidr,omitempty" yaml:"ip_cidr,omitempty"`
	IPCIDR6      string `json:"ip_cidr6,omitempty" yaml:"ip_cidr6,omitempty"`
	NoResolve    bool   `json:"no_resolve,omitempty" yaml:"no_resolve,omitempty"`
}

type ExternalRuleProvider struct {
	ID       string `json:"id" yaml:"id"`
	Target   string `json:"target" yaml:"target"`
	Reason   string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Type     string `json:"type,omitempty" yaml:"type,omitempty"`
	Behavior string `json:"behavior,omitempty" yaml:"behavior,omitempty"`
	Format   string `json:"format,omitempty" yaml:"format,omitempty"`
	Path     string `json:"path,omitempty" yaml:"path,omitempty"`
	URL      string `json:"url,omitempty" yaml:"url,omitempty"`
	Interval int    `json:"interval,omitempty" yaml:"interval,omitempty"`
}

type ResolveOptions struct {
	Config              Config
	SubscriptionPath    string
	SubscriptionConfig  string
	SubscriptionRuntime string
	SubscriptionNodes   []SubscriptionNode `json:"-"`
	RulesCache          string
	LocalRulePacksDir   string
	OnStage             func(StageEvent) `json:"-"`
}

type StageEvent struct {
	Stage      string         `json:"stage"`
	Event      string         `json:"event"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Error      string         `json:"error,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type SubscriptionSourceArtifact struct {
	SourceID string
	Proxies  []map[string]any
}

type SubscriptionNodeBuildStats struct {
	ArtifactCount    int `json:"artifact_count"`
	ProxyIterations  int `json:"proxy_iterations"`
	EmptyNameSkipped int `json:"empty_name_skipped"`
	NodeAppends      int `json:"node_appends"`
	UniqueNameChecks int `json:"unique_name_checks"`
}

type proxyGroupResolveStats struct {
	RegexCompiles            int
	MatchNodeScans           int
	RegexMatchAttempts       int
	SourceScopeChecks        int
	ExactAvailableIndexItems int
	ExactNodeRefs            int
	SelectedNodeAppends      int
}

type Resolved struct {
	Config        Config               `json:"config"`
	Selection     rules.Selection      `json:"selection"`
	ProxyGroups   []ProxyGroupResult   `json:"proxy_groups"`
	PolicyGroups  []PolicyGroupResult  `json:"policy_groups"`
	CustomRules   []CustomRuleResult   `json:"custom_rules"`
	RulePacks     []RulePackResult     `json:"enabled_rule_packs"`
	RuleProviders []RuleProviderResult `json:"rule_providers"`
	Packs         []PackResult         `json:"packs"`
	Warnings      []string             `json:"warnings"`
}

type ProxyGroupResult struct {
	ID            string   `json:"id"`
	Mode          string   `json:"mode"`
	Match         *Match   `json:"match,omitempty"`
	SelectedNodes []string `json:"selected_nodes"`
	NodeCount     int      `json:"node_count"`
	Optional      bool     `json:"optional,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Boundary      string   `json:"boundary,omitempty"`
}

type PolicyGroupResult struct {
	ID        string   `json:"id"`
	Mode      string   `json:"mode"`
	Exits     []string `json:"exits"`
	ExitCount int      `json:"exit_count"`
	Reason    string   `json:"reason,omitempty"`
	Boundary  string   `json:"boundary,omitempty"`
}

type PackResult struct {
	Source string `json:"source"`
	Pack   string `json:"pack"`
	Type   string `json:"type"`
	Target string `json:"target"`
	Reason string `json:"reason,omitempty"`
}

type CustomRuleResult struct {
	ID        string           `json:"id"`
	Target    string           `json:"target"`
	RuleCount int              `json:"rule_count"`
	Reason    string           `json:"reason,omitempty"`
	Rules     []CustomRuleLine `json:"rules,omitempty"`
}

type RulePackResult struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Target    string `json:"target"`
	RuleCount int    `json:"rule_count"`
	Reason    string `json:"reason,omitempty"`
}

type RuleProviderResult struct {
	ID       string `json:"id"`
	Target   string `json:"target"`
	Reason   string `json:"reason,omitempty"`
	Type     string `json:"type"`
	Behavior string `json:"behavior"`
	Format   string `json:"format"`
	Path     string `json:"path"`
	URL      string `json:"url,omitempty"`
	Interval int    `json:"interval,omitempty"`
}

type SubscriptionNode struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	SourceID string `json:"source_id,omitempty"`
}

type MissingNodesError struct {
	GroupID string
	Nodes   []string
}

func (err *MissingNodesError) Error() string {
	if len(err.Nodes) == 1 {
		return fmt.Sprintf("proxy group %q references unknown subscription node %q", err.GroupID, err.Nodes[0])
	}
	return fmt.Sprintf("proxy group %q references %d unknown subscription nodes", err.GroupID, len(err.Nodes))
}

type subscriptionSources struct {
	Sources []struct {
		ID string `json:"id"`
	} `json:"sources"`
}

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register([]map[string]any{})
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func Write(path string, config Config) error {
	if config.Version == 0 {
		config.Version = ConfigSchemaVersion
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func WriteSelection(path string, selection rules.Selection) error {
	return rules.WriteSelection(path, selection)
}

func Resolve(opts ResolveOptions) (Resolved, error) {
	opts = normalizeResolveOptions(opts)
	stage := localConfigStageEmitter(opts.OnStage)
	if opts.Config.Version == 0 {
		opts.Config.Version = ConfigSchemaVersion
	}
	finish := stage("load_subscription_nodes", map[string]any{
		"subscription":         opts.SubscriptionPath,
		"subscription_config":  opts.SubscriptionConfig,
		"subscription_runtime": opts.SubscriptionRuntime,
	})
	var nodes []SubscriptionNode
	var err error
	if len(opts.SubscriptionNodes) > 0 {
		nodes = append([]SubscriptionNode(nil), opts.SubscriptionNodes...)
	} else {
		nodes, err = LoadSubscriptionNodes(SubscriptionNodeOptions{
			SubscriptionPath:    opts.SubscriptionPath,
			SubscriptionConfig:  opts.SubscriptionConfig,
			SubscriptionRuntime: opts.SubscriptionRuntime,
			OnStage:             nestedLocalConfigStage(opts.OnStage, "load_subscription_nodes"),
		})
		if err != nil {
			finish(err, nil)
			return Resolved{}, err
		}
	}
	finish(nil, map[string]any{"node_count": len(nodes), "source": resolveSubscriptionNodeSource(opts.SubscriptionNodes)})
	selection := rules.Selection{
		Version:      1,
		ProxyGroups:  map[string]rules.ProxyGroup{},
		PolicyGroups: map[string]rules.PolicyGroup{},
	}
	resolvedConfig := opts.Config
	if resolvedConfig.ProxyGroups == nil {
		resolvedConfig.ProxyGroups = map[string]ProxyGroup{}
	}
	if resolvedConfig.PolicyGroups == nil {
		resolvedConfig.PolicyGroups = map[string]PolicyGroup{}
	}
	groupIDs := make([]string, 0, len(resolvedConfig.ProxyGroups))
	for id := range resolvedConfig.ProxyGroups {
		groupIDs = append(groupIDs, id)
	}
	sort.Strings(groupIDs)
	var groupResults []ProxyGroupResult
	var groupStats proxyGroupResolveStats
	finish = stage("resolve_proxy_groups", map[string]any{"proxy_group_count": len(groupIDs), "node_count": len(nodes)})
	for _, id := range groupIDs {
		group := resolvedConfig.ProxyGroups[id]
		mode := strings.ToLower(strings.TrimSpace(group.Mode))
		var selected []string
		var err error
		finishGroup := stage("resolve_proxy_group", map[string]any{
			"group_id":     id,
			"mode":         mode,
			"has_match":    group.Match != nil,
			"node_refs":    len(group.Nodes),
			"optional":     group.Optional,
			"total_nodes":  len(nodes),
			"source_scope": matchSourceScope(group.Match),
		})
		if mode == "direct" {
			if group.Match != nil || len(group.Nodes) > 0 {
				err := fmt.Errorf("proxy group %q direct mode cannot use match or nodes", id)
				finishGroup(err, nil)
				finish(err, nil)
				return Resolved{}, err
			}
		} else {
			var currentStats proxyGroupResolveStats
			selected, err = resolveProxyGroupMeasured(id, group, nodes, &currentStats)
			if err != nil {
				finishGroup(err, currentStats.fields())
				finish(err, groupStats.fields())
				return Resolved{}, err
			}
			groupStats.add(currentStats)
			finishGroup(nil, mergeFields(map[string]any{"selected_nodes": len(selected)}, currentStats.fields()))
		}
		if mode == "direct" {
			finishGroup(nil, map[string]any{"selected_nodes": len(selected)})
		}
		group.Mode = mode
		group.SelectedNodes = selected
		resolvedConfig.ProxyGroups[id] = group
		ruleGroup := rules.ProxyGroup{Nodes: selected, Optional: group.Optional}
		switch mode {
		case "manual":
			ruleGroup.Manual = true
		case "auto":
			ruleGroup.Auto = true
		case "smart":
			ruleGroup.Smart = true
		case "direct":
			ruleGroup.Direct = true
		default:
			err := fmt.Errorf("proxy group %q mode must be manual, auto, smart, or direct", id)
			finish(err, nil)
			return Resolved{}, err
		}
		selection.ProxyGroups[id] = ruleGroup
		groupResults = append(groupResults, ProxyGroupResult{
			ID:            id,
			Mode:          mode,
			Match:         group.Match,
			SelectedNodes: append([]string{}, selected...),
			NodeCount:     len(selected),
			Optional:      group.Optional,
			Reason:        group.Reason,
			Boundary:      group.Boundary,
		})
	}
	finish(nil, mergeFields(map[string]any{"resolved_proxy_groups": len(groupResults)}, groupStats.fields()))
	policyIDs := make([]string, 0, len(resolvedConfig.PolicyGroups))
	for id := range resolvedConfig.PolicyGroups {
		if _, exists := resolvedConfig.ProxyGroups[id]; exists {
			err := fmt.Errorf("policy group %q conflicts with a proxy group id", id)
			stage("resolve_policy_groups", nil)(err, nil)
			return Resolved{}, err
		}
		policyIDs = append(policyIDs, id)
	}
	sort.Strings(policyIDs)
	var policyResults []PolicyGroupResult
	finish = stage("resolve_policy_groups", map[string]any{"policy_group_count": len(policyIDs), "proxy_group_count": len(selection.ProxyGroups)})
	for _, id := range policyIDs {
		group := resolvedConfig.PolicyGroups[id]
		mode := strings.ToLower(strings.TrimSpace(group.Mode))
		ruleGroup := rules.PolicyGroup{Exits: normalizePolicyGroupExits(group.Exits)}
		finishGroup := stage("resolve_policy_group", map[string]any{"group_id": id, "mode": mode, "exit_count": len(ruleGroup.Exits)})
		switch mode {
		case "manual":
			ruleGroup.Manual = true
		case "auto":
			ruleGroup.Auto = true
		case "smart":
			ruleGroup.Smart = true
		default:
			err := fmt.Errorf("policy group %q mode must be manual, auto, or smart", id)
			finishGroup(err, nil)
			finish(err, nil)
			return Resolved{}, err
		}
		if err := validatePolicyGroupExits(id, ruleGroup.Exits, selection.ProxyGroups); err != nil {
			finishGroup(err, nil)
			finish(err, nil)
			return Resolved{}, err
		}
		finishGroup(nil, map[string]any{"exits": len(ruleGroup.Exits)})
		group.Mode = mode
		group.Exits = append([]string{}, ruleGroup.Exits...)
		resolvedConfig.PolicyGroups[id] = group
		selection.PolicyGroups[id] = ruleGroup
		policyResults = append(policyResults, PolicyGroupResult{
			ID:        id,
			Mode:      mode,
			Exits:     append([]string{}, ruleGroup.Exits...),
			ExitCount: len(ruleGroup.Exits),
			Reason:    group.Reason,
			Boundary:  group.Boundary,
		})
	}
	finish(nil, map[string]any{"resolved_policy_groups": len(policyResults)})
	fallbackTarget := strings.TrimSpace(resolvedConfig.FallbackTarget)
	if fallbackTarget == "" {
		fallbackTarget = rules.TerminalDirect
	}
	if !isKnownTarget(fallbackTarget, selection.ProxyGroups, selection.PolicyGroups) {
		err := fmt.Errorf("fallback_target %q requires a matching proxy group or policy group", fallbackTarget)
		stage("resolve_fallback_target", map[string]any{"fallback_target": fallbackTarget})(err, nil)
		return Resolved{}, err
	}
	resolvedConfig.FallbackTarget = fallbackTarget
	selection.FallbackTarget = fallbackTarget
	resolvedPacks := make([]Pack, 0, len(resolvedConfig.Packs))
	packResults := make([]PackResult, 0, len(resolvedConfig.Packs))
	finish = stage("resolve_packs", map[string]any{"pack_count": len(resolvedConfig.Packs), "rules_cache": opts.RulesCache})
	var packIndex *rules.PackIndex
	if len(resolvedConfig.Packs) > 0 {
		finishIndex := stage("load_pack_index", map[string]any{"index": rules.PackIndexPath(opts.RulesCache)})
		packIndex, err = rules.LoadPackIndex(rules.PackIndexPath(opts.RulesCache))
		if err != nil {
			finishIndex(err, nil)
			finish(err, nil)
			return Resolved{}, err
		}
		finishIndex(nil, map[string]any{"pack_count": len(packIndex.Catalog.Packs), "ref_count": len(packIndex.Refs)})
	}
	for _, pack := range resolvedConfig.Packs {
		source := strings.TrimSpace(pack.Source)
		packName := strings.TrimSpace(pack.Pack)
		label := rules.PackKey(source, packName)
		finishPack := stage("resolve_pack", map[string]any{"source": source, "pack": packName, "target": pack.Target, "declared_type": pack.Type})
		ref, err := packIndex.ResolvePackRef(source, packName)
		if err != nil {
			finishPack(err, nil)
			finish(err, nil)
			return Resolved{}, err
		}
		if err := assertPackType(label, pack.Type, ref.Type); err != nil {
			finishPack(err, map[string]any{"resolved_type": ref.Type})
			finish(err, nil)
			return Resolved{}, err
		}
		target := strings.TrimSpace(pack.Target)
		if target == "" {
			err := fmt.Errorf("pack %q target is required", label)
			finishPack(err, map[string]any{"resolved_type": ref.Type})
			finish(err, nil)
			return Resolved{}, err
		}
		if !isKnownTarget(target, selection.ProxyGroups, selection.PolicyGroups) {
			err := fmt.Errorf("pack target %q requires a matching proxy group or policy group", target)
			finishPack(err, map[string]any{"resolved_type": ref.Type})
			finish(err, nil)
			return Resolved{}, err
		}
		selection.EnabledPack = append(selection.EnabledPack, rules.SelectedPack{Source: ref.Source, Pack: ref.Pack, Target: target})
		resolvedPacks = append(resolvedPacks, Pack{Source: ref.Source, Pack: ref.Pack, Type: ref.Type, Target: target, Reason: pack.Reason})
		packResults = append(packResults, PackResult{Source: ref.Source, Pack: ref.Pack, Type: ref.Type, Target: target, Reason: pack.Reason})
		finishPack(nil, map[string]any{"resolved_type": ref.Type, "source": ref.Source, "pack": ref.Pack})
	}
	finish(nil, map[string]any{"resolved_packs": len(packResults)})
	resolvedConfig.Packs = resolvedPacks
	finish = stage("resolve_custom_rules", map[string]any{"custom_rule_count": len(resolvedConfig.CustomRules)})
	resolvedCustomRules, customRuleResults, err := resolveCustomRules(resolvedConfig.CustomRules, selection.ProxyGroups, selection.PolicyGroups)
	if err != nil {
		finish(err, nil)
		return Resolved{}, err
	}
	finish(nil, map[string]any{"resolved_custom_rules": len(customRuleResults)})
	resolvedConfig.CustomRules = resolvedCustomRules
	selection.CustomRules = customRulesForSelection(resolvedCustomRules)
	finish = stage("resolve_enabled_rule_packs", map[string]any{"enabled_rule_pack_count": len(resolvedConfig.EnabledRulePacks), "rule_packs_dir": opts.LocalRulePacksDir})
	resolvedRulePacks, rulePackResults, rulePackRules, err := resolveEnabledRulePacks(resolvedConfig.EnabledRulePacks, opts.LocalRulePacksDir, selection.ProxyGroups, selection.PolicyGroups)
	if err != nil {
		finish(err, nil)
		return Resolved{}, err
	}
	finish(nil, map[string]any{"resolved_rule_packs": len(rulePackResults)})
	resolvedConfig.EnabledRulePacks = resolvedRulePacks
	selection.LocalRulePacks = localRulePacksForSelection(rulePackResults)
	selection.CustomRules = append(selection.CustomRules, rulePackRules...)
	finish = stage("resolve_rule_providers", map[string]any{"rule_provider_count": len(resolvedConfig.RuleProviders)})
	resolvedRuleProviders, ruleProviderResults, err := resolveRuleProviders(resolvedConfig.RuleProviders, selection.ProxyGroups, selection.PolicyGroups)
	if err != nil {
		finish(err, nil)
		return Resolved{}, err
	}
	finish(nil, map[string]any{"resolved_rule_providers": len(ruleProviderResults)})
	resolvedConfig.RuleProviders = resolvedRuleProviders
	selection.RuleProviders = ruleProvidersForSelection(resolvedRuleProviders)
	return Resolved{Config: resolvedConfig, Selection: selection, ProxyGroups: groupResults, PolicyGroups: policyResults, CustomRules: customRuleResults, RulePacks: rulePackResults, RuleProviders: ruleProviderResults, Packs: packResults}, nil
}

func assertPackType(id, declared, actual string) error {
	declared = strings.TrimSpace(declared)
	if declared == "" {
		return nil
	}
	if actual == "" {
		return fmt.Errorf("pack %q has no catalog type; remove type or refresh pack catalog", id)
	}
	if declared != actual {
		return fmt.Errorf("pack %q is type %q, but request declared %q", id, actual, declared)
	}
	return nil
}

type localConfigStageFunc func(string, map[string]any) func(error, map[string]any)

func localConfigStageEmitter(callback func(StageEvent)) localConfigStageFunc {
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

func nestedLocalConfigStage(callback func(StageEvent), parent string) func(StageEvent) {
	if callback == nil {
		return nil
	}
	return func(event StageEvent) {
		stage := event.Stage
		if parent != "" {
			stage = parent + "." + stage
		}
		callback(StageEvent{
			Stage:      stage,
			Event:      event.Event,
			DurationMS: event.DurationMS,
			Error:      event.Error,
			Fields:     event.Fields,
		})
	}
}

func matchSourceScope(match *Match) int {
	if match == nil {
		return 0
	}
	count := 0
	for _, sourceID := range match.SourceIDs {
		if strings.TrimSpace(sourceID) != "" {
			count++
		}
	}
	return count
}

type SubscriptionNodeOptions struct {
	SubscriptionPath    string
	SubscriptionConfig  string
	SubscriptionRuntime string
	OnStage             func(StageEvent) `json:"-"`
}

func LoadSubscriptionNodes(opts SubscriptionNodeOptions) ([]SubscriptionNode, error) {
	opts = normalizeSubscriptionNodeOptions(opts)
	stage := localConfigStageEmitter(opts.OnStage)
	if !fileExists(opts.SubscriptionConfig) {
		finish := stage("load_source_subscription_nodes", map[string]any{
			"subscription_config":  opts.SubscriptionConfig,
			"subscription_runtime": opts.SubscriptionRuntime,
			"used":                 false,
			"fallback_reason":      "subscription sources are not configured",
		})
		finish(nil, nil)
		finish = stage("load_merged_subscription_nodes", map[string]any{"subscription": opts.SubscriptionPath})
		nodes, err := loadMergedSubscriptionNodes(opts.SubscriptionPath, stage)
		if err != nil {
			finish(err, nil)
			return nil, err
		}
		finish(nil, map[string]any{"node_count": len(nodes)})
		return nodes, nil
	}
	finish := stage("load_source_subscription_nodes", map[string]any{
		"subscription_config":  opts.SubscriptionConfig,
		"subscription_runtime": opts.SubscriptionRuntime,
	})
	sourceNodes, err := loadSourceSubscriptionNodes(opts)
	if err == nil && len(sourceNodes) > 0 {
		finish(nil, map[string]any{"node_count": len(sourceNodes), "used": true})
		return sourceNodes, nil
	}
	if err != nil {
		finish(err, map[string]any{"node_count": len(sourceNodes), "used": false})
		return nil, err
	}
	err = fmt.Errorf("subscription source artifacts produced no nodes; run subscriptions_refresh")
	finish(err, map[string]any{"node_count": len(sourceNodes), "used": false})
	return nil, err
}

func BuildSubscriptionNodesFromArtifacts(artifacts []SubscriptionSourceArtifact) []SubscriptionNode {
	nodes, _ := BuildSubscriptionNodesFromArtifactsMeasured(artifacts)
	return nodes
}

func BuildSubscriptionNodesFromArtifactsMeasured(artifacts []SubscriptionSourceArtifact) ([]SubscriptionNode, SubscriptionNodeBuildStats) {
	prefixSource := len(artifacts) > 1
	usedNames := map[string]bool{}
	var nodes []SubscriptionNode
	stats := SubscriptionNodeBuildStats{ArtifactCount: len(artifacts)}
	for _, artifact := range artifacts {
		for _, proxy := range artifact.Proxies {
			stats.ProxyIterations++
			name := stringValue(proxy["name"])
			if name == "" {
				stats.EmptyNameSkipped++
				continue
			}
			if prefixSource {
				name = "[" + artifact.SourceID + "] " + name
			}
			var checks int
			name, checks = uniqueNameMeasured(name, usedNames)
			stats.UniqueNameChecks += checks
			usedNames[name] = true
			nodes = append(nodes, SubscriptionNode{Name: name, Type: stringValue(proxy["type"]), SourceID: artifact.SourceID})
			stats.NodeAppends++
		}
	}
	return nodes, stats
}

func loadMergedSubscriptionNodes(path string, stage localConfigStageFunc) ([]SubscriptionNode, error) {
	doc, err := readGobMapObserved(path, stage, "merged_subscription", nil)
	if err != nil {
		return nil, err
	}
	var nodes []SubscriptionNode
	for _, proxy := range anyMapSlice(doc["proxies"]) {
		name := stringValue(proxy["name"])
		if name == "" {
			return nil, fmt.Errorf("subscription %q contains a proxy without name", path)
		}
		nodes = append(nodes, SubscriptionNode{Name: name, Type: stringValue(proxy["type"])})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("subscription %q has no proxies", path)
	}
	return nodes, nil
}

func loadSourceSubscriptionNodes(opts SubscriptionNodeOptions) ([]SubscriptionNode, error) {
	stage := localConfigStageEmitter(opts.OnStage)
	finish := stage("read_subscription_config", map[string]any{"path": opts.SubscriptionConfig})
	data, err := os.ReadFile(opts.SubscriptionConfig)
	if err != nil {
		finish(err, nil)
		return nil, err
	}
	finish(nil, map[string]any{"bytes": len(data)})
	var sources subscriptionSources
	finish = stage("parse_subscription_config", map[string]any{"path": opts.SubscriptionConfig, "bytes": len(data)})
	if err := json.Unmarshal(data, &sources); err != nil {
		finish(err, nil)
		return nil, err
	}
	finish(nil, map[string]any{"source_count": len(sources.Sources)})
	sourceDocs := map[string][]map[string]any{}
	for _, source := range sources.Sources {
		if source.ID == "" {
			continue
		}
		path := filepath.Join(opts.SubscriptionRuntime, source.ID+".gob")
		finish := stage("read_subscription_source_artifact", map[string]any{"source_id": source.ID, "path": path})
		doc, err := readGobMapObserved(path, stage, "subscription_source_artifact", map[string]any{"source_id": source.ID})
		if err != nil {
			finish(err, nil)
			return nil, err
		}
		proxies := anyMapSlice(doc["proxies"])
		finish(nil, map[string]any{"proxy_count": len(proxies)})
		sourceDocs[source.ID] = proxies
	}
	prefixSource := len(sourceDocs) > 1
	finish = stage("build_subscription_nodes", map[string]any{"source_count": len(sourceDocs), "prefix_source": prefixSource})
	artifacts := make([]SubscriptionSourceArtifact, 0, len(sources.Sources))
	for _, source := range sources.Sources {
		if proxies, ok := sourceDocs[source.ID]; ok {
			artifacts = append(artifacts, SubscriptionSourceArtifact{SourceID: source.ID, Proxies: proxies})
		}
	}
	nodes, buildStats := BuildSubscriptionNodesFromArtifactsMeasured(artifacts)
	finish(nil, mergeFields(map[string]any{"node_count": len(nodes)}, buildStats.fields()))
	return nodes, nil
}

func readGobMapObserved(path string, stage localConfigStageFunc, prefix string, fields map[string]any) (map[string]any, error) {
	readFields := map[string]any{"path": path}
	for key, value := range fields {
		readFields[key] = value
	}
	finish := stage(prefix+".open_file", readFields)
	file, err := os.Open(path)
	if err != nil {
		finish(err, nil)
		return nil, err
	}
	defer file.Close()
	finish(nil, nil)

	parseFields := map[string]any{"path": path}
	for key, value := range fields {
		parseFields[key] = value
	}
	finish = stage(prefix+".decode_gob", parseFields)
	var artifact struct {
		Version int
		Data    map[string]any
		Raw     []byte
	}
	if err := gob.NewDecoder(file).Decode(&artifact); err != nil {
		finish(err, nil)
		return nil, err
	}
	if artifact.Version != 1 {
		err := fmt.Errorf("subscription artifact schema version mismatch: expected 1, got %d; run localclash subscriptions refresh", artifact.Version)
		finish(err, nil)
		return nil, err
	}
	doc := artifact.Data
	finish(nil, map[string]any{"top_level_keys": len(doc)})
	return doc, nil
}

func resolveProxyGroup(id string, group ProxyGroup, nodes []SubscriptionNode) ([]string, error) {
	return resolveProxyGroupMeasured(id, group, nodes, nil)
}

func resolveProxyGroupMeasured(id string, group ProxyGroup, nodes []SubscriptionNode, stats *proxyGroupResolveStats) ([]string, error) {
	if group.Match != nil {
		if len(group.Nodes) > 0 {
			return nil, fmt.Errorf("proxy group %q must use either match or nodes, not both", id)
		}
		return resolveMatchMeasured(id, *group.Match, nodes, group.Optional, stats)
	}
	return resolveExactNodesMeasured(id, group.Nodes, nodes, stats)
}

func resolveSubscriptionNodeSource(nodes []SubscriptionNode) string {
	if len(nodes) > 0 {
		return "provided"
	}
	return "disk"
}

func (stats SubscriptionNodeBuildStats) Fields() map[string]any {
	return map[string]any{
		"artifact_count":     stats.ArtifactCount,
		"proxy_iterations":   stats.ProxyIterations,
		"empty_name_skipped": stats.EmptyNameSkipped,
		"node_appends":       stats.NodeAppends,
		"unique_name_checks": stats.UniqueNameChecks,
	}
}

func (stats SubscriptionNodeBuildStats) fields() map[string]any {
	return stats.Fields()
}

func (stats *proxyGroupResolveStats) add(other proxyGroupResolveStats) {
	stats.RegexCompiles += other.RegexCompiles
	stats.MatchNodeScans += other.MatchNodeScans
	stats.RegexMatchAttempts += other.RegexMatchAttempts
	stats.SourceScopeChecks += other.SourceScopeChecks
	stats.ExactAvailableIndexItems += other.ExactAvailableIndexItems
	stats.ExactNodeRefs += other.ExactNodeRefs
	stats.SelectedNodeAppends += other.SelectedNodeAppends
}

func (stats proxyGroupResolveStats) fields() map[string]any {
	return map[string]any{
		"regex_compiles":              stats.RegexCompiles,
		"match_node_scans":            stats.MatchNodeScans,
		"regex_match_attempts":        stats.RegexMatchAttempts,
		"source_scope_checks":         stats.SourceScopeChecks,
		"exact_available_index_items": stats.ExactAvailableIndexItems,
		"exact_node_refs":             stats.ExactNodeRefs,
		"selected_node_appends":       stats.SelectedNodeAppends,
	}
}

func mergeFields(base map[string]any, extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return base
	}
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range extra {
		base[key] = value
	}
	return base
}

func resolveMatch(id string, match Match, nodes []SubscriptionNode, optional bool) ([]string, error) {
	return resolveMatchMeasured(id, match, nodes, optional, nil)
}

func resolveMatchMeasured(id string, match Match, nodes []SubscriptionNode, optional bool, stats *proxyGroupResolveStats) ([]string, error) {
	if strings.TrimSpace(match.Type) == "" {
		match.Type = "name_regex"
	}
	if match.Type != "name_regex" {
		return nil, fmt.Errorf("proxy group %q match type %q is unsupported", id, match.Type)
	}
	pattern := strings.TrimSpace(match.Pattern)
	if pattern == "" {
		return nil, fmt.Errorf("proxy group %q match.pattern is required", id)
	}
	if !match.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	if stats != nil {
		stats.RegexCompiles++
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("proxy group %q match.pattern is invalid: %w", id, err)
	}
	sourceIDs := map[string]bool{}
	for _, sourceID := range match.SourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID != "" {
			sourceIDs[sourceID] = true
		}
	}
	var selected []string
	seen := map[string]bool{}
	for _, node := range nodes {
		if stats != nil {
			stats.MatchNodeScans++
		}
		if len(sourceIDs) > 0 && !sourceIDs[node.SourceID] {
			if stats != nil {
				stats.SourceScopeChecks++
			}
			continue
		}
		if stats != nil {
			if len(sourceIDs) > 0 {
				stats.SourceScopeChecks++
			}
			stats.RegexMatchAttempts++
		}
		if !re.MatchString(node.Name) || seen[node.Name] {
			continue
		}
		seen[node.Name] = true
		selected = append(selected, node.Name)
		if stats != nil {
			stats.SelectedNodeAppends++
		}
		if match.Max > 0 && len(selected) >= match.Max {
			break
		}
	}
	min := match.Min
	if min < 0 {
		return nil, fmt.Errorf("proxy group %q match.min must be >= 0", id)
	}
	if min == 0 && !optional {
		min = 1
	}
	if len(selected) < min {
		return nil, fmt.Errorf("proxy group %q match selected %d nodes, below min %d", id, len(selected), min)
	}
	return selected, nil
}

func resolveExactNodes(id string, rawNodes []string, nodes []SubscriptionNode) ([]string, error) {
	return resolveExactNodesMeasured(id, rawNodes, nodes, nil)
}

func resolveExactNodesMeasured(id string, rawNodes []string, nodes []SubscriptionNode, stats *proxyGroupResolveStats) ([]string, error) {
	available := map[string]bool{}
	for _, node := range nodes {
		available[node.Name] = true
		if stats != nil {
			stats.ExactAvailableIndexItems++
		}
	}
	seen := map[string]bool{}
	var selected []string
	var missing []string
	for _, raw := range rawNodes {
		if stats != nil {
			stats.ExactNodeRefs++
		}
		node := strings.TrimSpace(raw)
		if node == "" {
			return nil, fmt.Errorf("proxy group %q has an empty node name", id)
		}
		if !available[node] {
			missing = append(missing, node)
			continue
		}
		if seen[node] {
			continue
		}
		seen[node] = true
		selected = append(selected, node)
		if stats != nil {
			stats.SelectedNodeAppends++
		}
	}
	if len(missing) > 0 {
		return nil, &MissingNodesError{GroupID: id, Nodes: missing}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("proxy group %q has no nodes or match selector", id)
	}
	return selected, nil
}

func normalizePolicyGroupExits(rawExits []string) []string {
	exits := make([]string, 0, len(rawExits))
	seen := map[string]bool{}
	for _, raw := range rawExits {
		exit := strings.TrimSpace(raw)
		if seen[exit] {
			continue
		}
		seen[exit] = true
		exits = append(exits, exit)
	}
	return exits
}

func validatePolicyGroupExits(id string, exits []string, proxyGroups map[string]rules.ProxyGroup) error {
	if len(exits) == 0 {
		return fmt.Errorf("policy group %q exits is required", id)
	}
	for _, exit := range exits {
		if strings.TrimSpace(exit) == "" {
			return fmt.Errorf("policy group %q contains an empty exit", id)
		}
		if rules.IsTerminalAction(exit) {
			continue
		}
		if _, ok := proxyGroups[exit]; !ok {
			return fmt.Errorf("policy group %q exit %q requires a terminal action or matching proxy group", id, exit)
		}
	}
	return nil
}

func resolveCustomRules(customRules []CustomRule, proxyGroups map[string]rules.ProxyGroup, policyGroups map[string]rules.PolicyGroup) ([]CustomRule, []CustomRuleResult, error) {
	resolved := make([]CustomRule, 0, len(customRules))
	results := make([]CustomRuleResult, 0, len(customRules))
	ids := map[string]bool{}
	for _, custom := range customRules {
		id := strings.TrimSpace(custom.ID)
		if id == "" {
			return nil, nil, fmt.Errorf("custom rule id is required")
		}
		if ids[id] {
			return nil, nil, fmt.Errorf("custom rule %q is defined more than once", id)
		}
		ids[id] = true
		target := strings.TrimSpace(custom.Target)
		if target == "" {
			return nil, nil, fmt.Errorf("custom rule %q target is required", id)
		}
		if !isKnownTarget(target, proxyGroups, policyGroups) {
			return nil, nil, fmt.Errorf("custom rule target %q requires a matching proxy group or policy group", target)
		}
		if len(custom.Rules) == 0 {
			return nil, nil, fmt.Errorf("custom rule %q rules is required", id)
		}
		lines := make([]CustomRuleLine, 0, len(custom.Rules))
		for _, rule := range custom.Rules {
			line, err := normalizeCustomRuleLine(id, rule)
			if err != nil {
				return nil, nil, err
			}
			lines = append(lines, line)
		}
		custom.ID = id
		custom.Target = target
		custom.Rules = lines
		resolved = append(resolved, custom)
		results = append(results, CustomRuleResult{
			ID:        id,
			Target:    target,
			RuleCount: len(lines),
			Reason:    custom.Reason,
			Rules:     append([]CustomRuleLine{}, lines...),
		})
	}
	return resolved, results, nil
}

func normalizeCustomRuleLine(id string, rule CustomRuleLine) (CustomRuleLine, error) {
	rule.Type = strings.ToLower(strings.TrimSpace(rule.Type))
	rule.Value = strings.TrimSpace(rule.Value)
	if rule.Value == "" {
		return CustomRuleLine{}, fmt.Errorf("custom rule %q contains an empty value", id)
	}
	switch rule.Type {
	case "domain", "domain_suffix", "ip_cidr", "ip_cidr6":
	default:
		return CustomRuleLine{}, fmt.Errorf("custom rule %q type %q is unsupported", id, rule.Type)
	}
	return rule, nil
}

func resolveEnabledRulePacks(selections []RulePackSelection, dir string, proxyGroups map[string]rules.ProxyGroup, policyGroups map[string]rules.PolicyGroup) ([]RulePackSelection, []RulePackResult, []rules.CustomRule, error) {
	if len(selections) == 0 {
		return nil, nil, nil, nil
	}
	packs, err := LoadLocalRulePacks(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	resolved := make([]RulePackSelection, 0, len(selections))
	results := make([]RulePackResult, 0, len(selections))
	renderRules := make([]rules.CustomRule, 0, len(selections))
	ids := map[string]bool{}
	for _, selection := range selections {
		id := strings.TrimSpace(selection.ID)
		if id == "" {
			return nil, nil, nil, fmt.Errorf("enabled rule pack id is required")
		}
		if ids[id] {
			return nil, nil, nil, fmt.Errorf("enabled rule pack %q is defined more than once", id)
		}
		ids[id] = true
		pack, ok := packs[id]
		if !ok {
			return nil, nil, nil, fmt.Errorf("local rule pack %q not found in %s", id, dir)
		}
		target := strings.TrimSpace(selection.Target)
		if target == "" {
			target = strings.TrimSpace(pack.DefaultTarget)
		}
		if target == "" {
			return nil, nil, nil, fmt.Errorf("enabled rule pack %q target is required", id)
		}
		if !localRulePackAllowsTarget(pack, target) {
			return nil, nil, nil, fmt.Errorf("enabled rule pack %q target %q is not listed in target_options", id, target)
		}
		if !isKnownTarget(target, proxyGroups, policyGroups) {
			return nil, nil, nil, fmt.Errorf("enabled rule pack target %q requires a terminal action, proxy group, or policy group", target)
		}
		lines, err := localRulePackLines(pack)
		if err != nil {
			return nil, nil, nil, err
		}
		reason := strings.TrimSpace(selection.Reason)
		if reason == "" {
			reason = strings.TrimSpace(pack.Description)
		}
		resolved = append(resolved, RulePackSelection{ID: id, Target: target, Reason: reason})
		results = append(results, RulePackResult{ID: id, Name: pack.Name, Target: target, RuleCount: len(lines), Reason: reason})
		renderRules = append(renderRules, rules.CustomRule{
			ID:     "rule_pack:" + id,
			Target: target,
			Reason: reason,
			Rules:  lines,
		})
	}
	return resolved, results, renderRules, nil
}

func LoadLocalRulePacks(dir string) (map[string]LocalRulePack, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = DefaultLocalRulePacksDir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	packs := map[string]LocalRulePack{}
	for _, entry := range entries {
		if shouldSkipLocalRulePackFile(entry.Name(), entry.IsDir()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var pack LocalRulePack
		if err := json.Unmarshal(data, &pack); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		pack, err = normalizeLocalRulePack(path, pack)
		if err != nil {
			return nil, err
		}
		if _, exists := packs[pack.ID]; exists {
			return nil, fmt.Errorf("local rule pack %q is defined more than once", pack.ID)
		}
		packs[pack.ID] = pack
	}
	if len(packs) == 0 {
		return nil, fmt.Errorf("no local rule packs found in %s", dir)
	}
	return packs, nil
}

const DefaultLocalRulePacksDir = "rule-packs"

func shouldSkipLocalRulePackFile(name string, isDir bool) bool {
	return isDir || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "._")
}

func normalizeLocalRulePack(path string, pack LocalRulePack) (LocalRulePack, error) {
	pack.ID = strings.TrimSpace(pack.ID)
	if pack.ID == "" {
		return LocalRulePack{}, fmt.Errorf("%s: id is required", path)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(pack.ID) {
		return LocalRulePack{}, fmt.Errorf("%s: local rule pack id %q contains unsupported characters", path, pack.ID)
	}
	if pack.Version == 0 {
		pack.Version = 1
	}
	if pack.Version != 1 {
		return LocalRulePack{}, fmt.Errorf("%s: local rule pack %q version must be 1", path, pack.ID)
	}
	pack.Name = strings.TrimSpace(pack.Name)
	pack.Description = strings.TrimSpace(pack.Description)
	pack.DefaultTarget = strings.TrimSpace(pack.DefaultTarget)
	pack.TargetOptions = normalizeTargetOptions(pack.TargetOptions)
	if len(pack.Rules) == 0 {
		return LocalRulePack{}, fmt.Errorf("%s: local rule pack %q rules is required", path, pack.ID)
	}
	if _, err := localRulePackLines(pack); err != nil {
		return LocalRulePack{}, fmt.Errorf("%s: %w", path, err)
	}
	return pack, nil
}

func normalizeTargetOptions(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

func localRulePackAllowsTarget(pack LocalRulePack, target string) bool {
	if len(pack.TargetOptions) == 0 {
		return true
	}
	for _, option := range pack.TargetOptions {
		if option == target {
			return true
		}
	}
	return false
}

func localRulePackLines(pack LocalRulePack) ([]rules.CustomRuleLine, error) {
	lines := make([]rules.CustomRuleLine, 0, len(pack.Rules))
	for i, rule := range pack.Rules {
		line, err := normalizeLocalRulePackRule(pack.ID, i, rule)
		if err != nil {
			return nil, err
		}
		lines = append(lines, rules.CustomRuleLine{
			Type:      line.Type,
			Value:     line.Value,
			NoResolve: line.NoResolve,
		})
	}
	return lines, nil
}

func normalizeLocalRulePackRule(packID string, index int, rule LocalRulePackRule) (CustomRuleLine, error) {
	if strings.TrimSpace(rule.Type) != "" || strings.TrimSpace(rule.Value) != "" {
		return normalizeCustomRuleLine(fmt.Sprintf("rule pack %s rule %d", packID, index+1), CustomRuleLine{
			Type:      rule.Type,
			Value:     rule.Value,
			NoResolve: rule.NoResolve,
		})
	}
	candidates := []CustomRuleLine{
		{Type: "domain", Value: rule.Domain, NoResolve: rule.NoResolve},
		{Type: "domain_suffix", Value: rule.DomainSuffix, NoResolve: rule.NoResolve},
		{Type: "ip_cidr", Value: rule.IPCIDR, NoResolve: rule.NoResolve},
		{Type: "ip_cidr6", Value: rule.IPCIDR6, NoResolve: rule.NoResolve},
	}
	var selected []CustomRuleLine
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Value) != "" {
			selected = append(selected, candidate)
		}
	}
	if len(selected) != 1 {
		return CustomRuleLine{}, fmt.Errorf("local rule pack %q rule %d must specify exactly one of domain, domain_suffix, ip_cidr, ip_cidr6, or type/value", packID, index+1)
	}
	return normalizeCustomRuleLine(fmt.Sprintf("rule pack %s rule %d", packID, index+1), selected[0])
}

func resolveRuleProviders(providers []ExternalRuleProvider, proxyGroups map[string]rules.ProxyGroup, policyGroups map[string]rules.PolicyGroup) ([]ExternalRuleProvider, []RuleProviderResult, error) {
	resolved := make([]ExternalRuleProvider, 0, len(providers))
	results := make([]RuleProviderResult, 0, len(providers))
	ids := map[string]bool{}
	for _, provider := range providers {
		normalized, err := NormalizeRuleProvider(provider)
		if err != nil {
			return nil, nil, err
		}
		if ids[normalized.ID] {
			return nil, nil, fmt.Errorf("rule provider %q is defined more than once", normalized.ID)
		}
		ids[normalized.ID] = true
		if !isKnownTarget(normalized.Target, proxyGroups, policyGroups) {
			return nil, nil, fmt.Errorf("rule provider target %q requires a matching proxy group or policy group", normalized.Target)
		}
		resolved = append(resolved, normalized)
		results = append(results, ruleProviderResult(normalized))
	}
	return resolved, results, nil
}

func NormalizeRuleProvider(provider ExternalRuleProvider) (ExternalRuleProvider, error) {
	provider.ID = strings.TrimSpace(provider.ID)
	if provider.ID == "" {
		return ExternalRuleProvider{}, fmt.Errorf("rule provider id is required")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(provider.ID) {
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q contains unsupported characters", provider.ID)
	}
	provider.Target = strings.TrimSpace(provider.Target)
	if provider.Target == "" {
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q target is required", provider.ID)
	}
	provider.Type = strings.ToLower(strings.TrimSpace(provider.Type))
	if provider.Type == "" {
		provider.Type = "http"
	}
	provider.Behavior = strings.ToLower(strings.TrimSpace(provider.Behavior))
	if provider.Behavior == "" {
		provider.Behavior = "classical"
	}
	provider.Format = strings.ToLower(strings.TrimSpace(provider.Format))
	if provider.Format == "" {
		provider.Format = "yaml"
	}
	provider.Path = strings.TrimSpace(provider.Path)
	if provider.Path == "" {
		provider.Path = "./rule_provider/" + provider.ID + ".yaml"
	}
	provider.URL = strings.TrimSpace(provider.URL)
	switch provider.Type {
	case "http":
		if provider.URL == "" {
			return ExternalRuleProvider{}, fmt.Errorf("rule provider %q url is required for http type", provider.ID)
		}
		parsed, err := url.Parse(provider.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return ExternalRuleProvider{}, fmt.Errorf("rule provider %q url is invalid", provider.ID)
		}
		if provider.Interval == 0 {
			provider.Interval = 86400
		}
	case "file":
		if provider.URL != "" {
			return ExternalRuleProvider{}, fmt.Errorf("rule provider %q url is only supported for http type", provider.ID)
		}
	default:
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q type %q is unsupported", provider.ID, provider.Type)
	}
	switch provider.Behavior {
	case "classical", "domain", "ipcidr":
	default:
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q behavior %q is unsupported", provider.ID, provider.Behavior)
	}
	switch provider.Format {
	case "yaml", "text", "mrs":
	default:
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q format %q is unsupported", provider.ID, provider.Format)
	}
	if provider.Interval < 0 {
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q interval must be non-negative", provider.ID)
	}
	return provider, nil
}

func ruleProviderResult(provider ExternalRuleProvider) RuleProviderResult {
	return RuleProviderResult{
		ID:       provider.ID,
		Target:   provider.Target,
		Reason:   provider.Reason,
		Type:     provider.Type,
		Behavior: provider.Behavior,
		Format:   provider.Format,
		Path:     provider.Path,
		URL:      provider.URL,
		Interval: provider.Interval,
	}
}

func customRulesForSelection(customRules []CustomRule) []rules.CustomRule {
	out := make([]rules.CustomRule, 0, len(customRules))
	for _, custom := range customRules {
		lines := make([]rules.CustomRuleLine, 0, len(custom.Rules))
		for _, line := range custom.Rules {
			lines = append(lines, rules.CustomRuleLine{
				Type:      line.Type,
				Value:     line.Value,
				NoResolve: line.NoResolve,
			})
		}
		out = append(out, rules.CustomRule{
			ID:     custom.ID,
			Target: custom.Target,
			Reason: custom.Reason,
			Rules:  lines,
		})
	}
	return out
}

func localRulePacksForSelection(packs []RulePackResult) []rules.SelectedLocalRulePack {
	out := make([]rules.SelectedLocalRulePack, 0, len(packs))
	for _, pack := range packs {
		out = append(out, rules.SelectedLocalRulePack{
			ID:        pack.ID,
			Name:      pack.Name,
			Target:    pack.Target,
			Reason:    pack.Reason,
			RuleCount: pack.RuleCount,
		})
	}
	return out
}

func ruleProvidersForSelection(providers []ExternalRuleProvider) []rules.ExternalRuleProvider {
	out := make([]rules.ExternalRuleProvider, 0, len(providers))
	for _, provider := range providers {
		out = append(out, rules.ExternalRuleProvider{
			ID:       provider.ID,
			Target:   provider.Target,
			Reason:   provider.Reason,
			Type:     provider.Type,
			Behavior: provider.Behavior,
			Format:   provider.Format,
			Path:     provider.Path,
			URL:      provider.URL,
			Interval: provider.Interval,
		})
	}
	return out
}

func normalizeResolveOptions(opts ResolveOptions) ResolveOptions {
	if strings.TrimSpace(opts.SubscriptionPath) == "" {
		opts.SubscriptionPath = "subscription.gob"
	}
	if strings.TrimSpace(opts.SubscriptionConfig) == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.json"
	}
	if strings.TrimSpace(opts.SubscriptionRuntime) == "" {
		opts.SubscriptionRuntime = filepath.Join(".runtime", "subscriptions")
	}
	if strings.TrimSpace(opts.RulesCache) == "" {
		opts.RulesCache = filepath.Join(".runtime", "rules", "packs")
	}
	if strings.TrimSpace(opts.LocalRulePacksDir) == "" {
		opts.LocalRulePacksDir = DefaultLocalRulePacksDir
	}
	return opts
}

func normalizeSubscriptionNodeOptions(opts SubscriptionNodeOptions) SubscriptionNodeOptions {
	if strings.TrimSpace(opts.SubscriptionPath) == "" {
		opts.SubscriptionPath = "subscription.gob"
	}
	if strings.TrimSpace(opts.SubscriptionConfig) == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.json"
	}
	if strings.TrimSpace(opts.SubscriptionRuntime) == "" {
		opts.SubscriptionRuntime = filepath.Join(".runtime", "subscriptions")
	}
	return opts
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func anyMapSlice(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func uniqueName(name string, used map[string]bool) string {
	name, _ = uniqueNameMeasured(name, used)
	return name
}

func uniqueNameMeasured(name string, used map[string]bool) (string, int) {
	checks := 1
	if !used[name] {
		return name, checks
	}
	base := name
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", base, i)
		checks++
		if !used[candidate] {
			return candidate, checks
		}
	}
}

func isKnownTarget(target string, proxyGroups map[string]rules.ProxyGroup, policyGroups map[string]rules.PolicyGroup) bool {
	if rules.IsTerminalAction(target) {
		return true
	}
	trimmed := strings.TrimSpace(target)
	if _, ok := proxyGroups[trimmed]; ok {
		return true
	}
	if _, ok := policyGroups[trimmed]; ok {
		return true
	}
	return false
}
