package rules

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type Source struct {
	ID         string `json:"id"`
	Adapter    string `json:"adapter"`
	URL        string `json:"url"`
	BaseURL    string `json:"base_url,omitempty"`
	RawBaseURL string `json:"raw_base_url,omitempty"`
}

type PackCache struct {
	Version    int    `json:"version"`
	Source     string `json:"source"`
	Adapter    string `json:"adapter"`
	Renderable bool   `json:"renderable"`
	Packs      []Pack `json:"packs"`
}

type Pack struct {
	ID         string      `json:"id"`
	Name       string      `json:"name,omitempty"`
	Target     string      `json:"target,omitempty"`
	Renderable bool        `json:"renderable"`
	Reason     string      `json:"reason,omitempty"`
	Components []Component `json:"components,omitempty"`
}

type Component struct {
	ID         string `json:"id"`
	Behavior   string `json:"behavior"`
	Format     string `json:"format"`
	OrderClass string `json:"order_class"`
	URL        string `json:"url"`
	Path       string `json:"path"`
}

const (
	PackTypeRuleProvider     = "rule_provider"
	PackTypeGeoSite          = "geosite"
	RenderStrategyRuleSet    = "RULE-SET"
	RenderStrategyGeoSite    = "GEOSITE"
	QuerySourceProviderCache = "provider_cache"
	QuerySourceRawDLC        = "raw_dlc_provider_cache"
	GeoSiteDataFileDLC       = "dlc.dat"
	TerminalDirect           = "DIRECT"
	TerminalReject           = "REJECT"
)

func IsTerminalAction(target string) bool {
	switch strings.TrimSpace(target) {
	case TerminalDirect, TerminalReject:
		return true
	default:
		return false
	}
}

type PackBackend struct {
	Type               string `json:"type"`
	QuerySource        string `json:"query_source"`
	RenderStrategy     string `json:"render_strategy"`
	RenderRuleTemplate string `json:"-"`
	DataFile           string `json:"data_file,omitempty"`
	Note               string `json:"note,omitempty"`
}

type Selection struct {
	Version         int                     `json:"version"`
	ProxyGroups     map[string]ProxyGroup   `json:"proxy_groups,omitempty"`
	PolicyGroups    map[string]PolicyGroup  `json:"policy_groups,omitempty"`
	CustomRules     []CustomRule            `json:"custom_rules,omitempty"`
	LocalRulePacks  []SelectedLocalRulePack `json:"local_rule_packs,omitempty"`
	RuleProviders   []ExternalRuleProvider  `json:"rule_providers,omitempty"`
	EnabledPack     []SelectedPack          `json:"enabled_packs"`
	RequiredTargets []string                `json:"required_targets,omitempty"`
	FallbackTarget  string                  `json:"fallback_target,omitempty"`
}

type ProxyGroup struct {
	Nodes    []string `json:"nodes"`
	Auto     bool     `json:"auto"`
	Manual   bool     `json:"manual"`
	Smart    bool     `json:"smart"`
	Direct   bool     `json:"direct"`
	Optional bool     `json:"optional,omitempty"`
}

type PolicyGroup struct {
	Exits  []string `json:"exits"`
	Auto   bool     `json:"auto"`
	Manual bool     `json:"manual"`
	Smart  bool     `json:"smart"`
}

type SelectedPack struct {
	Source string `json:"source"`
	Pack   string `json:"pack"`
	Target string `json:"target"`
}

type SelectedLocalRulePack struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Target    string `json:"target"`
	Reason    string `json:"reason,omitempty"`
	RuleCount int    `json:"rule_count"`
}

type CustomRule struct {
	ID     string           `yaml:"id" json:"id"`
	Target string           `yaml:"target" json:"target"`
	Reason string           `yaml:"reason,omitempty" json:"reason,omitempty"`
	Rules  []CustomRuleLine `yaml:"rules" json:"rules"`
}

type CustomRuleLine struct {
	Type      string `yaml:"type" json:"type"`
	Value     string `yaml:"value" json:"value"`
	NoResolve bool   `yaml:"no_resolve,omitempty" json:"no_resolve,omitempty"`
}

type ExternalRuleProvider struct {
	ID       string `yaml:"id" json:"id"`
	Target   string `yaml:"target" json:"target"`
	Reason   string `yaml:"reason,omitempty" json:"reason,omitempty"`
	Type     string `yaml:"type" json:"type"`
	Behavior string `yaml:"behavior" json:"behavior"`
	Format   string `yaml:"format" json:"format"`
	Path     string `yaml:"path" json:"path"`
	URL      string `yaml:"url,omitempty" json:"url,omitempty"`
	Interval int    `yaml:"interval,omitempty" json:"interval,omitempty"`
}

type Fragment struct {
	RuleProviders     map[string]map[string]any `yaml:"rule-providers"`
	ProxyGroups       []map[string]any          `yaml:"proxy-groups,omitempty"`
	Rules             []string                  `yaml:"rules"`
	BaseManualChoices []string                  `yaml:"-"`
}

type RenderSelectionStats struct {
	ProxyNames            int
	ProxyGroups           int
	PolicyGroups          int
	EnabledPacks          int
	LocalRulePacks        int
	CustomRules           int
	RuleProviders         int
	RenderedRules         int
	RenderedRuleProviders int
}

func (stats RenderSelectionStats) Fields() map[string]any {
	return map[string]any{
		"proxy_names":             stats.ProxyNames,
		"proxy_groups":            stats.ProxyGroups,
		"policy_groups":           stats.PolicyGroups,
		"enabled_packs":           stats.EnabledPacks,
		"local_rule_packs":        stats.LocalRulePacks,
		"custom_rules":            stats.CustomRules,
		"rule_providers":          stats.RuleProviders,
		"rendered_rules":          stats.RenderedRules,
		"rendered_rule_providers": stats.RenderedRuleProviders,
	}
}

type Options struct {
	SourcesDir    string
	CacheDir      string
	SelectionPath string
	Subscription  string
	OutputPath    string
}

func NormalizeOptions(opts Options) Options {
	if strings.TrimSpace(opts.SourcesDir) == "" {
		opts.SourcesDir = "rule-sources"
	}
	if strings.TrimSpace(opts.CacheDir) == "" {
		opts.CacheDir = ".runtime/rules/packs"
	}
	if strings.TrimSpace(opts.SelectionPath) == "" {
		opts.SelectionPath = "localclash-packs.gob"
	}
	if strings.TrimSpace(opts.Subscription) == "" {
		opts.Subscription = "subscription.gob"
	}
	return opts
}

func Adapt(ctx context.Context, opts Options) ([]PackCache, error) {
	opts = NormalizeOptions(opts)
	sources, err := LoadSources(opts.SourcesDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.CacheDir, 0o755); err != nil {
		return nil, err
	}

	caches := make([]PackCache, 0, len(sources))
	for _, source := range sources {
		cache, err := adaptSource(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("adapt %s: %w", source.ID, err)
		}
		caches = append(caches, cache)
	}
	if err := WritePackIndex(PackIndexPath(opts.CacheDir), packCachesBySource(caches)); err != nil {
		return nil, err
	}
	if err := removeLegacyPackCacheYAML(opts.CacheDir); err != nil {
		return nil, err
	}
	return caches, nil
}

func Render(opts Options) (Fragment, error) {
	opts = NormalizeOptions(opts)
	selection, err := LoadSelection(opts.SelectionPath)
	if err != nil {
		return Fragment{}, err
	}
	proxyNames, err := LoadSubscriptionProxyNames(opts.Subscription)
	if err != nil {
		return Fragment{}, err
	}
	return RenderSelection(selection, opts.CacheDir, proxyNames)
}

func RenderSelection(selection Selection, cacheDir string, proxyNames []string) (Fragment, error) {
	fragment, _, err := RenderSelectionWithStats(selection, cacheDir, proxyNames)
	return fragment, err
}

func RenderSelectionWithStats(selection Selection, cacheDir string, proxyNames []string) (Fragment, RenderSelectionStats, error) {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		cacheDir = ".runtime/rules/packs"
	}
	index, err := LoadPackIndex(PackIndexPath(cacheDir))
	if err != nil {
		return Fragment{}, RenderSelectionStats{}, err
	}
	fragment, err := RenderFragment(selection, index.Caches, proxyNames)
	if err != nil {
		return Fragment{}, RenderSelectionStats{}, err
	}
	return fragment, RenderSelectionStats{
		ProxyNames:            len(proxyNames),
		ProxyGroups:           len(selection.ProxyGroups),
		PolicyGroups:          len(selection.PolicyGroups),
		EnabledPacks:          len(selection.EnabledPack),
		LocalRulePacks:        len(selection.LocalRulePacks),
		CustomRules:           len(selection.CustomRules),
		RuleProviders:         len(selection.RuleProviders),
		RenderedRules:         len(fragment.Rules),
		RenderedRuleProviders: len(fragment.RuleProviders),
	}, nil
}

func LoadSources(dir string) ([]Source, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var sources []Source
	for _, entry := range entries {
		if shouldSkipJSONFile(entry.Name(), entry.IsDir()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var source Source
		if err := json.Unmarshal(data, &source); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := validateSource(source); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
	return sources, nil
}

func validateSource(source Source) error {
	if source.ID == "" {
		return errors.New("id is required")
	}
	if source.Adapter == "" {
		return errors.New("adapter is required")
	}
	if source.URL == "" {
		return errors.New("url is required")
	}
	switch source.Adapter {
	case "sukkaw":
		if source.BaseURL == "" {
			return errors.New("base_url is required for sukkaw")
		}
	case "blackmatrix7", "v2fly-dlc", "syncnext":
		if source.RawBaseURL == "" {
			return fmt.Errorf("raw_base_url is required for %s", source.Adapter)
		}
	default:
		return fmt.Errorf("unknown adapter %q", source.Adapter)
	}
	return nil
}

func shouldSkipJSONFile(name string, isDir bool) bool {
	if isDir || !strings.HasSuffix(name, ".json") {
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "._")
}

func LoadSelection(path string) (Selection, error) {
	file, err := os.Open(path)
	if err != nil {
		return Selection{}, err
	}
	defer file.Close()
	var selection Selection
	if err := gob.NewDecoder(file).Decode(&selection); err != nil {
		return Selection{}, err
	}
	return selection, nil
}

func WriteSelection(path string, selection Selection) error {
	if selection.Version == 0 {
		selection.Version = 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	encodeErr := gob.NewEncoder(file).Encode(selection)
	closeErr := file.Close()
	if encodeErr != nil {
		_ = os.Remove(tmp)
		return encodeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, path)
}

func LoadSubscriptionProxyNames(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var artifact struct {
		Version int
		Data    map[string]any
		Raw     []byte
	}
	gob.Register(map[string]any{})
	gob.Register([]any{})
	if err := gob.NewDecoder(file).Decode(&artifact); err != nil {
		return nil, err
	}
	if artifact.Version != 1 {
		return nil, fmt.Errorf("subscription artifact schema version mismatch: expected 1, got %d; run localclash subscriptions refresh", artifact.Version)
	}
	source := artifact.Data
	raw, ok := source["proxies"].([]any)
	if !ok {
		return nil, fmt.Errorf("subscription %q has no proxies", path)
	}
	names := make([]string, 0, len(raw))
	for _, item := range raw {
		proxy, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("subscription %q contains an invalid proxy entry", path)
		}
		name, ok := proxy["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("subscription %q contains a proxy without name", path)
		}
		names = append(names, name)
	}
	return names, nil
}

func WriteFragment(path string, fragment Fragment) error {
	data, err := yaml.Marshal(fragment)
	if err != nil {
		return err
	}
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func RenderFragment(selection Selection, caches map[string]PackCache, proxyNames ...[]string) (Fragment, error) {
	fragment := Fragment{
		RuleProviders: map[string]map[string]any{},
	}
	targets, err := prepareTargets(selection, firstProxyNames(proxyNames))
	if err != nil {
		return Fragment{}, err
	}
	usedProxyGroups := map[string]bool{}
	usedPolicyGroups := map[string]bool{}
	for _, custom := range selection.CustomRules {
		target, kind, err := renderTarget(custom.Target, targets)
		if err != nil {
			return Fragment{}, err
		}
		markUsedTarget(target, kind, usedProxyGroups, usedPolicyGroups)
		lines, err := renderCustomRuleLines(custom, target)
		if err != nil {
			return Fragment{}, err
		}
		fragment.Rules = append(fragment.Rules, lines...)
	}
	for _, provider := range selection.RuleProviders {
		target, kind, err := renderTarget(provider.Target, targets)
		if err != nil {
			return Fragment{}, err
		}
		markUsedTarget(target, kind, usedProxyGroups, usedPolicyGroups)
		rendered, err := renderExternalRuleProvider(provider)
		if err != nil {
			return Fragment{}, err
		}
		fragment.RuleProviders[provider.ID] = rendered
		fragment.Rules = append(fragment.Rules, fmt.Sprintf("RULE-SET,%s,%s", provider.ID, target))
	}
	for _, enabled := range selection.EnabledPack {
		cache, ok := caches[enabled.Source]
		if !ok {
			return Fragment{}, fmt.Errorf("source %q has no pack cache; run rules adapt first", enabled.Source)
		}
		pack, ok := findPackForSelector(cache.Packs, enabled.Pack)
		if !ok {
			return Fragment{}, fmt.Errorf("pack %q not found in source %q", enabled.Pack, enabled.Source)
		}
		if !pack.Renderable && !packIsGeoSite(pack) {
			return Fragment{}, fmt.Errorf("pack %q from source %q is not renderable: %s", enabled.Pack, enabled.Source, pack.Reason)
		}
		target, kind, err := renderTarget(enabled.Target, targets)
		if err != nil {
			return Fragment{}, err
		}
		markUsedTarget(target, kind, usedProxyGroups, usedPolicyGroups)
		for _, component := range pack.Components {
			if strings.EqualFold(component.Behavior, "v2fly-dlc") {
				fragment.Rules = append(fragment.Rules, fmt.Sprintf("GEOSITE,%s,%s", enabled.Pack, target))
				continue
			}
			providerName := providerName(enabled.Source, pack.ID, component.ID)
			fragment.RuleProviders[providerName] = map[string]any{
				"type":     "http",
				"behavior": component.Behavior,
				"format":   component.Format,
				"url":      component.URL,
				"path":     component.Path,
			}
			fragment.Rules = append(fragment.Rules, fmt.Sprintf("RULE-SET,%s,%s", providerName, target))
		}
	}
	for _, required := range selection.RequiredTargets {
		target, kind, err := renderTarget(required, targets)
		if err != nil {
			return Fragment{}, err
		}
		markUsedTarget(target, kind, usedProxyGroups, usedPolicyGroups)
	}
	policyGroups, referencedProxyGroups, err := materializePolicyGroups(usedPolicyGroups, targets)
	if err != nil {
		return Fragment{}, err
	}
	for name := range referencedProxyGroups {
		usedProxyGroups[name] = true
	}
	proxyGroups, baseManualChoices, err := materializeProxyGroups(usedProxyGroups, targets)
	if err != nil {
		return Fragment{}, err
	}
	fragment.ProxyGroups = append(policyGroups, proxyGroups...)
	fragment.BaseManualChoices = baseManualChoices
	return fragment, nil
}

func renderExternalRuleProvider(provider ExternalRuleProvider) (map[string]any, error) {
	id := strings.TrimSpace(provider.ID)
	if id == "" {
		return nil, fmt.Errorf("rule provider id is required")
	}
	out := map[string]any{
		"type":     strings.TrimSpace(provider.Type),
		"behavior": strings.TrimSpace(provider.Behavior),
		"format":   strings.TrimSpace(provider.Format),
		"path":     strings.TrimSpace(provider.Path),
	}
	if out["type"] == "" || out["behavior"] == "" || out["format"] == "" || out["path"] == "" {
		return nil, fmt.Errorf("rule provider %q requires type, behavior, format, and path", id)
	}
	if strings.TrimSpace(provider.URL) != "" {
		out["url"] = strings.TrimSpace(provider.URL)
	}
	if provider.Interval > 0 {
		out["interval"] = provider.Interval
	}
	return out, nil
}

func renderCustomRuleLines(custom CustomRule, target string) ([]string, error) {
	id := strings.TrimSpace(custom.ID)
	if id == "" {
		return nil, fmt.Errorf("custom rule id is required")
	}
	if strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("custom rule %q target is required", id)
	}
	if len(custom.Rules) == 0 {
		return nil, fmt.Errorf("custom rule %q rules is required", id)
	}
	lines := make([]string, 0, len(custom.Rules))
	for _, rule := range custom.Rules {
		line, err := renderCustomRuleLine(id, rule, target)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func renderCustomRuleLine(id string, rule CustomRuleLine, target string) (string, error) {
	value := strings.TrimSpace(rule.Value)
	if value == "" {
		return "", fmt.Errorf("custom rule %q contains an empty value", id)
	}
	var kind string
	switch strings.ToLower(strings.TrimSpace(rule.Type)) {
	case "domain":
		kind = "DOMAIN"
	case "domain_suffix":
		kind = "DOMAIN-SUFFIX"
	case "ip_cidr":
		kind = "IP-CIDR"
	case "ip_cidr6":
		kind = "IP-CIDR6"
	default:
		return "", fmt.Errorf("custom rule %q type %q is unsupported", id, rule.Type)
	}
	line := fmt.Sprintf("%s,%s,%s", kind, value, target)
	if rule.NoResolve {
		line += ",no-resolve"
	}
	return line, nil
}

func firstProxyNames(values [][]string) []string {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

func findPack(packs []Pack, id string) (Pack, bool) {
	for _, pack := range packs {
		if pack.ID == id {
			return pack, true
		}
	}
	return Pack{}, false
}

func findPackForSelector(packs []Pack, selector string) (Pack, bool) {
	if pack, ok := findPack(packs, selector); ok {
		return pack, true
	}
	base, ok := splitGeoSiteSelector(selector)
	if !ok {
		return Pack{}, false
	}
	pack, ok := findPack(packs, base)
	if !ok || !packIsGeoSite(pack) {
		return Pack{}, false
	}
	return pack, true
}

type preparedTargets struct {
	proxyGroups  map[string]ProxyGroup
	policyGroups map[string]PolicyGroup
}

func prepareTargets(selection Selection, proxyNames []string) (preparedTargets, error) {
	available := map[string]bool{}
	for _, name := range proxyNames {
		available[name] = true
	}
	for groupName, group := range selection.ProxyGroups {
		if len(group.Nodes) == 0 && !group.Direct && !group.Optional {
			return preparedTargets{}, fmt.Errorf("proxy group %q has no nodes", groupName)
		}
		enabledModes := 0
		for _, enabled := range []bool{group.Auto, group.Manual, group.Smart} {
			if enabled {
				enabledModes++
			}
		}
		if enabledModes > 1 {
			return preparedTargets{}, fmt.Errorf("proxy group %q can enable only one of auto, manual, or smart", groupName)
		}
		if enabledModes == 0 && !group.Direct {
			return preparedTargets{}, fmt.Errorf("proxy group %q has no enabled choices", groupName)
		}
		if group.Direct && (group.Auto || group.Smart) {
			return preparedTargets{}, fmt.Errorf("proxy group %q cannot combine direct with auto or smart mode", groupName)
		}
		seen := map[string]bool{}
		for _, node := range group.Nodes {
			node = strings.TrimSpace(node)
			if node == "" {
				return preparedTargets{}, fmt.Errorf("proxy group %q has an empty node name", groupName)
			}
			if seen[node] {
				continue
			}
			seen[node] = true
			if !available[node] {
				return preparedTargets{}, fmt.Errorf("proxy group %q references unknown subscription node %q", groupName, node)
			}
		}
	}
	for groupName, group := range selection.PolicyGroups {
		if _, exists := selection.ProxyGroups[groupName]; exists {
			return preparedTargets{}, fmt.Errorf("policy group %q conflicts with a proxy group id", groupName)
		}
		if len(group.Exits) == 0 {
			return preparedTargets{}, fmt.Errorf("policy group %q has no exits", groupName)
		}
		enabledModes := 0
		for _, enabled := range []bool{group.Auto, group.Manual, group.Smart} {
			if enabled {
				enabledModes++
			}
		}
		if enabledModes > 1 {
			return preparedTargets{}, fmt.Errorf("policy group %q can enable only one of auto, manual, or smart", groupName)
		}
		if enabledModes == 0 {
			return preparedTargets{}, fmt.Errorf("policy group %q has no enabled choices", groupName)
		}
		seen := map[string]bool{}
		for _, rawExit := range group.Exits {
			exit := strings.TrimSpace(rawExit)
			if exit == "" {
				return preparedTargets{}, fmt.Errorf("policy group %q has an empty exit", groupName)
			}
			if seen[exit] {
				continue
			}
			seen[exit] = true
			if IsTerminalAction(exit) {
				continue
			}
			if _, ok := selection.ProxyGroups[exit]; !ok {
				return preparedTargets{}, fmt.Errorf("policy group %q exit %q requires a terminal action or matching proxy group", groupName, exit)
			}
		}
	}
	return preparedTargets{proxyGroups: selection.ProxyGroups, policyGroups: selection.PolicyGroups}, nil
}

type targetKind int

const (
	targetKindTerminal targetKind = iota
	targetKindProxyGroup
	targetKindPolicyGroup
)

func renderTarget(target string, targets preparedTargets) (string, targetKind, error) {
	trimmed := strings.TrimSpace(target)
	if IsTerminalAction(trimmed) {
		return trimmed, targetKindTerminal, nil
	}
	if _, ok := targets.proxyGroups[trimmed]; ok {
		return trimmed, targetKindProxyGroup, nil
	}
	if _, ok := targets.policyGroups[trimmed]; ok {
		return trimmed, targetKindPolicyGroup, nil
	}
	return "", targetKindTerminal, fmt.Errorf("unknown pack target %q", target)
}

func markUsedTarget(target string, kind targetKind, usedProxyGroups map[string]bool, usedPolicyGroups map[string]bool) {
	switch kind {
	case targetKindProxyGroup:
		usedProxyGroups[target] = true
	case targetKindPolicyGroup:
		usedPolicyGroups[target] = true
	}
}

func materializeProxyGroups(used map[string]bool, targets preparedTargets) ([]map[string]any, []string, error) {
	standardNames := make([]string, 0, len(used))
	regionNames := make([]string, 0, len(used))
	for name := range used {
		group := targets.proxyGroups[name]
		if group.Optional && !group.Direct {
			regionNames = append(regionNames, name)
			continue
		}
		standardNames = append(standardNames, name)
	}
	sortNamesByDisplay(standardNames)
	sortNamesByDisplay(regionNames)
	names := append(standardNames, regionNames...)
	var groups []map[string]any
	var baseManualChoices []string
	for _, name := range names {
		group := targets.proxyGroups[name]
		candidates := candidateProxies(group)
		if len(candidates) == 0 && group.Optional && !group.Direct {
			continue
		}
		if len(candidates) == 0 && !group.Direct {
			return nil, nil, fmt.Errorf("proxy group %q has no candidate proxies", name)
		}
		if group.Optional && !group.Direct {
			baseManualChoices = append(baseManualChoices, name)
		}
		if group.Auto {
			groups = append(groups, map[string]any{
				"name":     name,
				"type":     "url-test",
				"proxies":  candidates,
				"url":      "http://www.gstatic.com/generate_204",
				"interval": 300,
			})
			continue
		}
		if group.Smart {
			groups = append(groups, map[string]any{
				"name":        name,
				"type":        "smart",
				"proxies":     candidates,
				"uselightgbm": true,
				"prefer-asn":  true,
			})
			continue
		}
		choices := append([]string{}, candidates...)
		if group.Direct {
			choices = append(choices, "DIRECT")
		}
		groups = append(groups, map[string]any{
			"name":    name,
			"type":    "select",
			"proxies": choices,
		})
	}
	return groups, baseManualChoices, nil
}

func materializePolicyGroups(used map[string]bool, targets preparedTargets) ([]map[string]any, map[string]bool, error) {
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sortNamesByDisplay(names)
	referencedProxyGroups := map[string]bool{}
	var groups []map[string]any
	for _, name := range names {
		group := targets.policyGroups[name]
		candidates, proxies, err := candidatePolicyExits(group, targets)
		if err != nil {
			return nil, nil, fmt.Errorf("policy group %q: %w", name, err)
		}
		if len(candidates) == 0 {
			return nil, nil, fmt.Errorf("policy group %q has no candidate exits", name)
		}
		for proxy := range proxies {
			referencedProxyGroups[proxy] = true
		}
		if group.Auto {
			groups = append(groups, map[string]any{
				"name":     name,
				"type":     "url-test",
				"proxies":  candidates,
				"url":      "http://www.gstatic.com/generate_204",
				"interval": 300,
			})
			continue
		}
		if group.Smart {
			groups = append(groups, map[string]any{
				"name":        name,
				"type":        "smart",
				"proxies":     candidates,
				"uselightgbm": true,
				"prefer-asn":  true,
			})
			continue
		}
		groups = append(groups, map[string]any{
			"name":    name,
			"type":    "select",
			"proxies": candidates,
		})
	}
	return groups, referencedProxyGroups, nil
}

func sortNamesByDisplay(names []string) {
	sort.Slice(names, func(i, j int) bool {
		left := displaySortKey(names[i])
		right := displaySortKey(names[j])
		if left == right {
			return names[i] < names[j]
		}
		return left < right
	})
}

func displaySortKey(name string) string {
	trimmed := strings.TrimSpace(name)
	trimmed = strings.TrimLeftFunc(trimmed, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	return strings.TrimSpace(trimmed)
}

func candidateProxies(group ProxyGroup) []string {
	var candidates []string
	seen := map[string]bool{}
	for _, proxy := range group.Nodes {
		proxy = strings.TrimSpace(proxy)
		if proxy == "" || seen[proxy] {
			continue
		}
		seen[proxy] = true
		candidates = append(candidates, proxy)
	}
	return candidates
}

func candidatePolicyExits(group PolicyGroup, targets preparedTargets) ([]string, map[string]bool, error) {
	var candidates []string
	referencedProxyGroups := map[string]bool{}
	seen := map[string]bool{}
	for _, rawExit := range group.Exits {
		exit, kind, err := renderTarget(rawExit, targets)
		if err != nil {
			return nil, nil, err
		}
		if exit == "" || seen[exit] {
			continue
		}
		if kind == targetKindProxyGroup {
			proxyGroup := targets.proxyGroups[exit]
			if proxyGroup.Optional && !proxyGroup.Direct && len(candidateProxies(proxyGroup)) == 0 {
				continue
			}
		}
		seen[exit] = true
		candidates = append(candidates, exit)
		if kind == targetKindProxyGroup {
			referencedProxyGroups[exit] = true
		}
	}
	return candidates, referencedProxyGroups, nil
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func providerName(source, pack, component string) string {
	raw := strings.TrimSpace(source) + "_" + strings.TrimSpace(pack)
	if component != "" && component != pack {
		raw += "_" + strings.TrimSpace(component)
	}
	return strings.Trim(raw, "_")
}

func githubAPIContentsURL(htmlURL string) (string, error) {
	parsed, err := url.Parse(htmlURL)
	if err != nil {
		return "", err
	}
	if parsed.Host != "github.com" {
		return "", fmt.Errorf("expected github.com url, got %q", htmlURL)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "tree" {
		return "", fmt.Errorf("expected GitHub tree url, got %q", htmlURL)
	}
	owner, repo, ref := parts[0], parts[1], parts[3]
	repoPath := strings.Join(parts[4:], "/")
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s", owner, repo, repoPath, ref), nil
}

func githubTreeAPIURL(htmlURL string) (string, string, error) {
	parsed, err := url.Parse(htmlURL)
	if err != nil {
		return "", "", err
	}
	if parsed.Host != "github.com" {
		return "", "", fmt.Errorf("expected github.com url, got %q", htmlURL)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "tree" {
		return "", "", fmt.Errorf("expected GitHub tree url, got %q", htmlURL)
	}
	owner, repo, ref := parts[0], parts[1], parts[3]
	repoPath := strings.Join(parts[4:], "/")
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, ref), repoPath, nil
}

func trimConf(name string) (string, bool) {
	if !strings.HasSuffix(name, ".conf") {
		return "", false
	}
	return strings.TrimSuffix(name, ".conf"), true
}
