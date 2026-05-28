package rules

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type PackListOptions struct {
	CacheDir string
	Source   string
	Name     string
	Target   string
	Limit    int
}

type PackGetOptions struct {
	CacheDir   string
	RuntimeDir string
	Source     string
	Pack       string
}

type PackRef struct {
	Source             string
	Pack               string
	Name               string
	Type               string
	RenderStrategy     string
	RenderRuleTemplate string
}

type PackListResult struct {
	Total       int           `json:"total"`
	Returned    int           `json:"returned"`
	Packs       []PackSummary `json:"packs"`
	Truncated   bool          `json:"truncated,omitempty"`
	Guidance    []string      `json:"guidance,omitempty"`
	NextActions []string      `json:"next_actions,omitempty"`
}

type PackSummary struct {
	Source             string `json:"source"`
	Pack               string `json:"pack"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	RenderStrategy     string `json:"render_strategy"`
	RenderRuleTemplate string `json:"render_rule_template"`
	Target             string `json:"target"`
	TargetMeaning      string `json:"target_meaning,omitempty"`
	ProviderCount      int    `json:"provider_count"`
	RuleCount          int    `json:"rule_count"`
}

type PackGetResult struct {
	Pack        PackDetail `json:"pack"`
	NextActions []string   `json:"next_actions,omitempty"`
}

type PackCatalog struct {
	Packs   []PackSummary         `json:"packs"`
	Details map[string]PackDetail `json:"details"`
}

type PackDetail struct {
	Source             string            `json:"source"`
	Pack               string            `json:"pack"`
	Name               string            `json:"name"`
	Type               string            `json:"type"`
	RenderStrategy     string            `json:"render_strategy"`
	RenderRuleTemplate string            `json:"render_rule_template"`
	Backend            PackBackend       `json:"backend"`
	Target             string            `json:"target"`
	TargetMeaning      string            `json:"target_meaning,omitempty"`
	Renderable         bool              `json:"renderable"`
	Reason             string            `json:"reason,omitempty"`
	Providers          []ProviderSummary `json:"providers"`
	Rules              []string          `json:"rules"`
	ProviderCount      int               `json:"provider_count"`
	RuleCount          int               `json:"rule_count"`
}

type ProviderSummary struct {
	Name      string `json:"name"`
	Component string `json:"component"`
	Type      string `json:"type"`
	Behavior  string `json:"behavior"`
	Format    string `json:"format"`
	URL       string `json:"-"`
	Path      string `json:"path,omitempty"`
}

type catalogEntry struct {
	Cache PackCache
	Pack  Pack
}

func ListPacks(opts PackListOptions) (PackListResult, error) {
	catalog, err := LoadPackCatalog(opts.CacheDir)
	if err != nil {
		return PackListResult{}, err
	}

	nameFilter := strings.ToLower(strings.TrimSpace(opts.Name))
	var packs []PackSummary
	for _, pack := range catalog.Packs {
		if opts.Source != "" && pack.Source != opts.Source {
			continue
		}
		if opts.Target != "" && pack.Target != opts.Target {
			continue
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(pack.Name), nameFilter) && !strings.Contains(strings.ToLower(pack.Pack), nameFilter) {
			continue
		}
		packs = append(packs, pack)
	}

	total := len(packs)
	truncated := false
	if opts.Limit > 0 && len(packs) > opts.Limit {
		packs = packs[:opts.Limit]
		truncated = true
	}
	return PackListResult{Total: total, Returned: len(packs), Packs: packs, Truncated: truncated, Guidance: PackListGuidance(), NextActions: PackListNextActions()}, nil
}

func GetPack(opts PackGetOptions) (PackGetResult, error) {
	source := strings.TrimSpace(opts.Source)
	pack := strings.TrimSpace(opts.Pack)
	if source == "" {
		return PackGetResult{}, fmt.Errorf("pack source is required")
	}
	if pack == "" {
		return PackGetResult{}, fmt.Errorf("pack name is required")
	}
	catalog, err := LoadPackCatalog(opts.CacheDir)
	if err != nil {
		return PackGetResult{}, err
	}
	if detail, ok := catalog.Details[PackKey(source, pack)]; ok {
		return PackGetResult{Pack: AnnotatePackRuntime(detail, opts.RuntimeDir), NextActions: packRuleNextActions()}, nil
	}
	return PackGetResult{}, fmt.Errorf("pack %q/%q not found in pack cache", source, pack)
}

func packRuleNextActions() []string {
	return []string{
		"Use pack_rules_read with this exact source and pack to inspect provider rule contents.",
		"Use pack_rules_prefetch with candidate source/pack pairs before pack_rules_query when local provider-cache coverage is incomplete.",
	}
}

func PackListGuidance() []string {
	return []string{
		"packs_list lists available catalog packs, not currently active routing policy.",
		"The pack target field is the pack's default/recommended render target from the catalog. It is not evidence that the pack is currently configured.",
		"Use config_status to inspect active localclash.json intent and generated/mihomo.yaml overlay before claiming a pack is configured.",
	}
}

func PackListNextActions() []string {
	return []string{
		"Use packs_get or pack_rules_read on candidate source/pack pairs before choosing packs.",
		"To change routing, call config_status first, then config_patch_create with the full desired retained config plus new pack targets.",
		"Apply only the exact patch_id returned by config_patch_create, then call config_status to verify.",
	}
}

func AnnotatePackRuntime(detail PackDetail, runtimeDir string) PackDetail {
	runtimeDir = strings.TrimSpace(runtimeDir)
	if runtimeDir == "" {
		runtimeDir = ".runtime/mihomo"
	}
	for i, provider := range detail.Providers {
		provider.Path = resolveProviderRuntimePath(runtimeDir, provider.Path)
		detail.Providers[i] = provider
	}
	return detail
}

func LoadPackCatalog(cacheDir string) (PackCatalog, error) {
	index, err := LoadPackIndex(PackIndexPath(cacheDir))
	if err != nil {
		return PackCatalog{}, err
	}
	return index.Catalog, nil
}

func catalogEntriesFromCaches(caches map[string]PackCache) ([]catalogEntry, error) {
	var entries []catalogEntry
	for _, source := range sortedPackCacheSources(caches) {
		cache := caches[source]
		packs := append([]Pack(nil), cache.Packs...)
		sortPacksByDisplay(packs)
		for _, pack := range packs {
			entries = append(entries, catalogEntry{Cache: cache, Pack: pack})
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no packs found in pack cache; run rules adapt first")
	}
	return entries, nil
}

func sortPacksByDisplay(packs []Pack) {
	sort.Slice(packs, func(i, j int) bool {
		left, right := packDisplayName(packs[i]), packDisplayName(packs[j])
		if left == right {
			return packs[i].ID < packs[j].ID
		}
		return left < right
	})
}

func packSummary(entry catalogEntry) PackSummary {
	backend := packBackend(entry.Cache.Source, entry.Pack, "<target>")
	return PackSummary{
		Source:             entry.Cache.Source,
		Pack:               entry.Pack.ID,
		Name:               packDisplayName(entry.Pack),
		Type:               backend.Type,
		RenderStrategy:     backend.RenderStrategy,
		RenderRuleTemplate: backend.RenderRuleTemplate,
		Target:             entry.Pack.Target,
		TargetMeaning:      "catalog default/recommended target; not active configuration",
		ProviderCount:      len(entry.Pack.Components),
		RuleCount:          len(entry.Pack.Components),
	}
}

func packRef(entry catalogEntry) PackRef {
	backend := packBackend(entry.Cache.Source, entry.Pack, "<target>")
	return PackRef{
		Source:             entry.Cache.Source,
		Pack:               entry.Pack.ID,
		Name:               packDisplayName(entry.Pack),
		Type:               backend.Type,
		RenderStrategy:     backend.RenderStrategy,
		RenderRuleTemplate: backend.RenderRuleTemplate,
	}
}

func packDetail(entry catalogEntry) PackDetail {
	providers := make([]ProviderSummary, 0, len(entry.Pack.Components))
	rules := make([]string, 0, len(entry.Pack.Components))
	target := entry.Pack.Target
	if target == "" {
		target = "<target>"
	}
	backend := packBackend(entry.Cache.Source, entry.Pack, target)
	for _, component := range entry.Pack.Components {
		name := providerName(entry.Cache.Source, entry.Pack.ID, component.ID)
		providers = append(providers, ProviderSummary{
			Name:      name,
			Component: component.ID,
			Type:      "http",
			Behavior:  component.Behavior,
			Format:    component.Format,
			URL:       component.URL,
			Path:      component.Path,
		})
		if strings.EqualFold(component.Behavior, "v2fly-dlc") {
			rules = append(rules, fmt.Sprintf("GEOSITE,%s,%s", entry.Pack.ID, target))
			continue
		}
		rules = append(rules, fmt.Sprintf("RULE-SET,%s,%s", name, target))
	}
	renderable := entry.Pack.Renderable || packIsGeoSite(entry.Pack)
	reason := entry.Pack.Reason
	if packIsGeoSite(entry.Pack) {
		reason = "v2fly domain-list-community raw data is queryable here and renders as a Mihomo GEOSITE rule; runtime geosite.dat must contain the same tag"
	}
	return PackDetail{
		Source:             entry.Cache.Source,
		Pack:               entry.Pack.ID,
		Name:               packDisplayName(entry.Pack),
		Type:               backend.Type,
		RenderStrategy:     backend.RenderStrategy,
		RenderRuleTemplate: backend.RenderRuleTemplate,
		Backend:            backend,
		Target:             entry.Pack.Target,
		TargetMeaning:      "catalog default/recommended target; not active configuration",
		Renderable:         renderable,
		Reason:             reason,
		Providers:          providers,
		Rules:              rules,
		ProviderCount:      len(providers),
		RuleCount:          len(rules),
	}
}

func packBackend(source string, pack Pack, target string) PackBackend {
	if packIsGeoSite(pack) {
		return PackBackend{
			Type:               PackTypeGeoSite,
			QuerySource:        QuerySourceRawDLC,
			RenderStrategy:     RenderStrategyGeoSite,
			RenderRuleTemplate: fmt.Sprintf("GEOSITE,%s,%s", pack.ID, target),
			DataFile:           GeoSiteDataFileDLC,
			Note:               "This pack renders as Mihomo GEOSITE. Keep using config_patch_create with this exact source and pack; localClash will render GEOSITE instead of RULE-SET.",
		}
	}
	providerID := "<provider>"
	if len(pack.Components) > 0 {
		providerID = providerName(source, pack.ID, pack.Components[0].ID)
	}
	return PackBackend{
		Type:               PackTypeRuleProvider,
		QuerySource:        QuerySourceProviderCache,
		RenderStrategy:     RenderStrategyRuleSet,
		RenderRuleTemplate: fmt.Sprintf("RULE-SET,%s,%s", providerID, target),
	}
}

func packIsGeoSite(pack Pack) bool {
	if len(pack.Components) == 0 {
		return false
	}
	for _, component := range pack.Components {
		if !strings.EqualFold(component.Behavior, "v2fly-dlc") {
			return false
		}
	}
	return true
}

func resolveProviderRuntimePath(runtimeDir, providerPath string) string {
	providerPath = strings.TrimSpace(providerPath)
	if providerPath == "" {
		return ""
	}
	if filepath.IsAbs(providerPath) {
		return filepath.ToSlash(filepath.Clean(providerPath))
	}
	cleanRuntime := filepath.ToSlash(filepath.Clean(runtimeDir))
	cleanProvider := filepath.ToSlash(filepath.Clean(providerPath))
	if cleanProvider == cleanRuntime || strings.HasPrefix(cleanProvider, cleanRuntime+"/") {
		return cleanProvider
	}
	return filepath.ToSlash(filepath.Clean(filepath.Join(runtimeDir, providerPath)))
}

func packDisplayName(pack Pack) string {
	if strings.TrimSpace(pack.Name) != "" {
		return pack.Name
	}
	return pack.ID
}
