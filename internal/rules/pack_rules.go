package rules

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type PackRulesReadOptions struct {
	SourcesDir    string
	CacheDir      string
	ProviderCache string
	Source        string
	Pack          string
	Component     string
	Limit         int
	Refresh       bool
}

type PackSelector struct {
	Source string `json:"source"`
	Pack   string `json:"pack"`
}

type PackRulesPrefetchOptions struct {
	SourcesDir    string
	CacheDir      string
	ProviderCache string
	Packs         []PackSelector
	Source        string
	Name          string
	Target        string
	Limit         int
	Refresh       bool
}

type PackRulesQueryOptions struct {
	SourcesDir    string
	CacheDir      string
	ProviderCache string
	Query         string
	Source        string
	Name          string
	Target        string
	Limit         int
}

type PackRulesReadResult struct {
	Pack        PackRulePackSummary `json:"pack"`
	Backend     PackBackend         `json:"backend"`
	Summary     PackRulesSummary    `json:"summary"`
	Components  []PackRuleComponent `json:"components"`
	NextActions []string            `json:"next_actions,omitempty"`
}

type PackRulesPrefetchResult struct {
	SelectedPacks []PackRulePackSummary `json:"selected_packs"`
	Components    []PackRuleComponent   `json:"components"`
	Summary       PrefetchSummary       `json:"summary"`
}

type PackRulesQueryResult struct {
	Query               string          `json:"query"`
	Backend             PackBackend     `json:"backend,omitempty"`
	Matches             []PackRuleMatch `json:"matches"`
	SearchedCachedPacks int             `json:"searched_cached_packs"`
	UncachedPacks       int             `json:"uncached_packs"`
	CacheComplete       bool            `json:"cache_complete"`
	Truncated           bool            `json:"truncated,omitempty"`
	NextActions         []string        `json:"next_actions,omitempty"`
}

type PackRulePackSummary struct {
	Source             string `json:"source"`
	Pack               string `json:"pack"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	RenderStrategy     string `json:"render_strategy"`
	RenderRuleTemplate string `json:"render_rule_template"`
	Target             string `json:"target,omitempty"`
	Renderable         bool   `json:"renderable"`
}

type PackRulesSummary struct {
	ComponentCount    int  `json:"component_count"`
	AvailableCount    int  `json:"available_count"`
	RuleCount         int  `json:"rule_count"`
	DomainCount       int  `json:"domain_count"`
	DomainSuffixCount int  `json:"domain_suffix_count"`
	KeywordCount      int  `json:"keyword_count"`
	IPCIDRCount       int  `json:"ip_cidr_count"`
	Truncated         bool `json:"truncated,omitempty"`
}

type PrefetchSummary struct {
	PackCount      int `json:"pack_count"`
	ComponentCount int `json:"component_count"`
	AvailableCount int `json:"available_count"`
	ErrorCount     int `json:"error_count"`
}

type PackRuleComponent struct {
	ID                string   `json:"id"`
	Behavior          string   `json:"behavior"`
	Format            string   `json:"format"`
	URL               string   `json:"url"`
	CachePath         string   `json:"cache_path"`
	Available         bool     `json:"available"`
	Cached            bool     `json:"cached"`
	Refreshed         bool     `json:"refreshed,omitempty"`
	RuleCount         int      `json:"rule_count"`
	DomainCount       int      `json:"domain_count,omitempty"`
	DomainSuffixCount int      `json:"domain_suffix_count,omitempty"`
	KeywordCount      int      `json:"keyword_count,omitempty"`
	IPCIDRCount       int      `json:"ip_cidr_count,omitempty"`
	RulesSample       []string `json:"rules_sample,omitempty"`
	DomainsSample     []string `json:"domains_sample,omitempty"`
	Truncated         bool     `json:"truncated,omitempty"`
	Error             string   `json:"error,omitempty"`
}

type PackRuleMatch struct {
	Source         string `json:"source"`
	Pack           string `json:"pack"`
	PackName       string `json:"pack_name"`
	Type           string `json:"type"`
	RenderStrategy string `json:"render_strategy"`
	Component      string `json:"component"`
	Rule           string `json:"rule"`
	Kind           string `json:"kind"`
	Value          string `json:"value"`
	SourceURL      string `json:"source_url"`
}

type parsedRule struct {
	Raw   string
	Kind  string
	Value string
}

func ReadPackRules(ctx context.Context, opts PackRulesReadOptions) (PackRulesReadResult, error) {
	limit := normalizedPackRulesLimit(opts.Limit, 120)
	catalog, err := ensurePackRulesCatalog(ctx, opts.SourcesDir, opts.CacheDir)
	if err != nil {
		return PackRulesReadResult{}, err
	}
	source := strings.TrimSpace(opts.Source)
	pack := strings.TrimSpace(opts.Pack)
	if source == "" {
		return PackRulesReadResult{}, fmt.Errorf("pack source is required")
	}
	if pack == "" {
		return PackRulesReadResult{}, fmt.Errorf("pack name is required")
	}
	detail, ok := catalog.Details[PackKey(source, pack)]
	if !ok {
		return PackRulesReadResult{}, fmt.Errorf("pack %q/%q not found in pack cache", source, pack)
	}
	if !packRulesQueryable(detail) {
		return PackRulesReadResult{Pack: packRuleSummary(detail), Backend: detail.Backend, NextActions: []string{"choose a renderable pack from packs_list"}}, nil
	}
	components := make([]PackRuleComponent, 0, len(detail.Providers))
	for _, provider := range detail.Providers {
		componentID := provider.Component
		if opts.Component != "" && componentID != opts.Component {
			continue
		}
		component := readProviderRules(ctx, detail, provider, componentID, opts.ProviderCache, limit, opts.Refresh)
		components = append(components, component)
	}
	if opts.Component != "" && len(components) == 0 {
		return PackRulesReadResult{}, fmt.Errorf("component %q not found in pack %q/%q", opts.Component, detail.Source, detail.Pack)
	}
	return PackRulesReadResult{
		Pack:       packRuleSummary(detail),
		Backend:    detail.Backend,
		Summary:    summarizeComponents(components),
		Components: components,
	}, nil
}

func PrefetchPackRules(ctx context.Context, opts PackRulesPrefetchOptions) (PackRulesPrefetchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	catalog, err := ensurePackRulesCatalog(ctx, opts.SourcesDir, opts.CacheDir)
	if err != nil {
		return PackRulesPrefetchResult{}, err
	}
	details, err := selectPackDetails(catalog, opts.Packs, opts.Source, opts.Name, opts.Target, limit, false)
	if err != nil {
		return PackRulesPrefetchResult{}, err
	}
	var result PackRulesPrefetchResult
	for _, detail := range details {
		result.SelectedPacks = append(result.SelectedPacks, packRuleSummary(detail))
		if !packRulesQueryable(detail) {
			continue
		}
		for _, provider := range detail.Providers {
			component := readProviderRules(ctx, detail, provider, provider.Component, opts.ProviderCache, 0, opts.Refresh)
			result.Components = append(result.Components, component)
		}
	}
	result.Summary.PackCount = len(result.SelectedPacks)
	result.Summary.ComponentCount = len(result.Components)
	for _, component := range result.Components {
		if component.Available {
			result.Summary.AvailableCount++
		}
		if component.Error != "" {
			result.Summary.ErrorCount++
		}
	}
	return result, nil
}

func QueryPackRules(ctx context.Context, opts PackRulesQueryOptions) (PackRulesQueryResult, error) {
	query := strings.ToLower(strings.TrimSpace(opts.Query))
	if query == "" {
		return PackRulesQueryResult{}, fmt.Errorf("query is required")
	}
	limit := normalizedPackRulesLimit(opts.Limit, 20)
	catalog, err := ensurePackRulesCatalog(ctx, opts.SourcesDir, opts.CacheDir)
	if err != nil {
		return PackRulesQueryResult{}, err
	}
	details, err := selectPackDetails(catalog, nil, opts.Source, opts.Name, opts.Target, 0, true)
	if err != nil {
		return PackRulesQueryResult{}, err
	}
	result := PackRulesQueryResult{Query: opts.Query, Backend: commonBackend(details)}
	for _, detail := range details {
		if !packRulesQueryable(detail) {
			continue
		}
		packCached := false
		packComplete := true
		for _, provider := range detail.Providers {
			componentID := provider.Component
			cachePath := providerCachePath(opts.ProviderCache, detail.Source, packLocalID(detail), componentID, provider.Format)
			data, err := os.ReadFile(cachePath)
			if err != nil {
				packComplete = false
				continue
			}
			packCached = true
			rules := parseProviderRules(data, provider.Behavior, provider.Format)
			for _, rule := range rules {
				if !ruleMatches(query, rule) {
					continue
				}
				if len(result.Matches) >= limit {
					result.Truncated = true
					continue
				}
				result.Matches = append(result.Matches, PackRuleMatch{
					Source:         detail.Source,
					Pack:           detail.Pack,
					PackName:       detail.Name,
					Type:           detail.Type,
					RenderStrategy: detail.RenderStrategy,
					Component:      componentID,
					Rule:           rule.Raw,
					Kind:           rule.Kind,
					Value:          rule.Value,
					SourceURL:      provider.URL,
				})
			}
		}
		if packCached {
			result.SearchedCachedPacks++
		}
		if !packComplete {
			result.UncachedPacks++
		}
	}
	result.CacheComplete = result.UncachedPacks == 0
	if !result.CacheComplete {
		result.NextActions = []string{
			"Use packs_list with a semantic keyword such as ai, openai, google, steam, netflix, game, or the service name to find candidate packs.",
			"Call pack_rules_prefetch with those candidate source/pack pairs.",
			"Call pack_rules_query again.",
		}
	}
	return result, nil
}

func ensurePackRulesCatalog(ctx context.Context, sourcesDir, cacheDir string) (PackCatalog, error) {
	catalog, err := LoadPackCatalog(cacheDir)
	if err == nil {
		return catalog, nil
	}
	if _, adaptErr := Adapt(ctx, Options{SourcesDir: sourcesDir, CacheDir: cacheDir}); adaptErr != nil {
		return PackCatalog{}, err
	}
	return LoadPackCatalog(cacheDir)
}

func selectPackDetails(catalog PackCatalog, packs []PackSelector, source, name, target string, limit int, allowAll bool) ([]PackDetail, error) {
	if !allowAll && len(packs) == 0 && source == "" && name == "" && target == "" {
		return nil, fmt.Errorf("select packs by source/pack, source, name, or target; refusing implicit all-pack prefetch")
	}
	var details []PackDetail
	if len(packs) > 0 {
		seen := map[string]bool{}
		for _, selector := range packs {
			source := strings.TrimSpace(selector.Source)
			pack := strings.TrimSpace(selector.Pack)
			if source == "" || pack == "" {
				return nil, fmt.Errorf("pack source and pack are required")
			}
			key := PackKey(source, pack)
			if seen[key] {
				continue
			}
			detail, ok := catalog.Details[key]
			if !ok {
				return nil, fmt.Errorf("pack %q/%q not found in pack cache", source, pack)
			}
			seen[key] = true
			details = append(details, detail)
		}
		return details, nil
	}
	nameFilter := strings.ToLower(strings.TrimSpace(name))
	for _, summary := range catalog.Packs {
		if source != "" && summary.Source != source {
			continue
		}
		if target != "" && summary.Target != target {
			continue
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(summary.Pack), nameFilter) && !strings.Contains(strings.ToLower(summary.Name), nameFilter) {
			continue
		}
		details = append(details, catalog.Details[PackKey(summary.Source, summary.Pack)])
		if limit > 0 && len(details) >= limit {
			break
		}
	}
	if source != "" && len(details) == 0 {
		return nil, unknownPackSourceError(catalog, source)
	}
	return details, nil
}

func unknownPackSourceError(catalog PackCatalog, source string) error {
	source = strings.TrimSpace(source)
	known := knownPackSources(catalog)
	for _, knownSource := range known {
		if strings.EqualFold(knownSource, source) {
			return fmt.Errorf("pack source %q must match exact case %q", source, knownSource)
		}
	}
	for _, knownSource := range known {
		if strings.Contains(strings.ToLower(knownSource), strings.ToLower(source)) || strings.Contains(strings.ToLower(source), strings.ToLower(knownSource)) {
			return fmt.Errorf("unknown pack source %q; source is exact, did you mean %q?", source, knownSource)
		}
	}
	return fmt.Errorf("unknown pack source %q; known sources: %s", source, strings.Join(known, ", "))
}

func knownPackSources(catalog PackCatalog) []string {
	seen := map[string]bool{}
	var sources []string
	for _, summary := range catalog.Packs {
		if summary.Source == "" || seen[summary.Source] {
			continue
		}
		seen[summary.Source] = true
		sources = append(sources, summary.Source)
	}
	sort.Strings(sources)
	return sources
}

func readProviderRules(ctx context.Context, detail PackDetail, provider ProviderSummary, componentID, cacheDir string, limit int, refresh bool) PackRuleComponent {
	cachePath := providerCachePath(cacheDir, detail.Source, packLocalID(detail), componentID, provider.Format)
	component := PackRuleComponent{
		ID:        componentID,
		Behavior:  provider.Behavior,
		Format:    provider.Format,
		URL:       provider.URL,
		CachePath: cachePath,
	}
	data, cached, err := loadOrFetchProvider(ctx, cachePath, provider.URL, refresh)
	if err != nil {
		component.Error = err.Error()
		return component
	}
	component.Cached = true
	component.Refreshed = !cached
	component.Available = true
	rules := parseProviderRules(data, provider.Behavior, provider.Format)
	component.RuleCount = len(rules)
	for _, rule := range rules {
		switch rule.Kind {
		case "domain":
			component.DomainCount++
		case "domain_suffix":
			component.DomainSuffixCount++
		case "domain_keyword":
			component.KeywordCount++
		case "ip_cidr":
			component.IPCIDRCount++
		}
	}
	if limit <= 0 {
		return component
	}
	for _, rule := range rules {
		if len(component.RulesSample) >= limit {
			component.Truncated = true
			break
		}
		component.RulesSample = append(component.RulesSample, rule.Raw)
		if isDomainLike(rule.Kind) {
			component.DomainsSample = append(component.DomainsSample, rule.Value)
		}
	}
	return component
}

func loadOrFetchProvider(ctx context.Context, cachePath, sourceURL string, refresh bool) ([]byte, bool, error) {
	if !refresh {
		data, err := os.ReadFile(cachePath)
		if err == nil {
			return data, true, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", "localclash-pack-rules")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("fetch provider rules failed: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		return nil, false, err
	}
	return data, false, nil
}

func parseProviderRules(data []byte, behavior, format string) []parsedRule {
	lines := providerRuleLines(data, format)
	rules := make([]parsedRule, 0, len(lines))
	for _, line := range lines {
		if rule, ok := parseRuleLine(line, behavior); ok {
			rules = append(rules, rule)
		}
	}
	return rules
}

func providerRuleLines(data []byte, format string) []string {
	if strings.EqualFold(format, "yaml") || bytes.Contains(data, []byte("payload:")) {
		var doc struct {
			Payload []string `yaml:"payload"`
		}
		if err := yaml.Unmarshal(data, &doc); err == nil && len(doc.Payload) > 0 {
			return doc.Payload
		}
	}
	rawLines := strings.Split(string(data), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		lines = append(lines, strings.TrimSpace(line))
	}
	return lines
}

func parseRuleLine(line, behavior string) (parsedRule, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
		return parsedRule{}, false
	}
	if strings.EqualFold(behavior, "v2fly-dlc") {
		return parseV2FlyDLCLine(line)
	}
	line = strings.TrimPrefix(line, "- ")
	parts := strings.Split(line, ",")
	if len(parts) >= 2 {
		kind := normalizeRuleKind(parts[0])
		value := strings.TrimSpace(parts[1])
		if value == "" {
			return parsedRule{}, false
		}
		return parsedRule{Raw: line, Kind: kind, Value: value}, true
	}
	value := strings.TrimPrefix(line, "+.")
	value = strings.TrimPrefix(value, ".")
	kind := "domain"
	if strings.EqualFold(behavior, "domain") {
		kind = "domain_suffix"
	}
	return parsedRule{Raw: line, Kind: kind, Value: value}, true
}

func parseV2FlyDLCLine(line string) (parsedRule, bool) {
	token := firstV2FlyDLCToken(line)
	if token == "" {
		return parsedRule{}, false
	}
	key, value, ok := strings.Cut(token, ":")
	if !ok {
		return parsedRule{Raw: token, Kind: "domain_suffix", Value: token}, true
	}
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)
	if value == "" {
		return parsedRule{}, false
	}
	switch key {
	case "domain":
		return parsedRule{Raw: token, Kind: "domain_suffix", Value: value}, true
	case "full":
		return parsedRule{Raw: token, Kind: "domain", Value: value}, true
	case "keyword":
		return parsedRule{Raw: token, Kind: "domain_keyword", Value: value}, true
	case "regexp":
		return parsedRule{Raw: token, Kind: "domain_regex", Value: value}, true
	case "include":
		return parsedRule{Raw: token, Kind: "include", Value: value}, true
	default:
		return parsedRule{Raw: token, Kind: "v2fly_" + key, Value: value}, true
	}
}

func firstV2FlyDLCToken(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
		return ""
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	token := strings.TrimSpace(fields[0])
	if token == "" || strings.HasPrefix(token, "#") {
		return ""
	}
	return token
}

func normalizeRuleKind(kind string) string {
	switch strings.ToUpper(strings.TrimSpace(kind)) {
	case "DOMAIN-SUFFIX":
		return "domain_suffix"
	case "DOMAIN":
		return "domain"
	case "DOMAIN-KEYWORD":
		return "domain_keyword"
	case "IP-CIDR", "IP-CIDR6":
		return "ip_cidr"
	default:
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(kind), "-", "_"))
	}
}

func ruleMatches(query string, rule parsedRule) bool {
	value := strings.ToLower(rule.Value)
	switch rule.Kind {
	case "domain":
		return query == value
	case "domain_suffix":
		return query == value || strings.HasSuffix(query, "."+value)
	case "domain_keyword":
		return strings.Contains(query, value)
	case "domain_regex":
		return regexpRuleMatches(query, rule.Value)
	default:
		return strings.Contains(strings.ToLower(rule.Raw), query)
	}
}

func regexpRuleMatches(query, pattern string) bool {
	matched, err := regexp.MatchString(pattern, query)
	if err != nil {
		return strings.Contains(strings.ToLower(pattern), query)
	}
	return matched
}

func summarizeComponents(components []PackRuleComponent) PackRulesSummary {
	var summary PackRulesSummary
	summary.ComponentCount = len(components)
	for _, component := range components {
		if component.Available {
			summary.AvailableCount++
		}
		summary.RuleCount += component.RuleCount
		summary.DomainCount += component.DomainCount
		summary.DomainSuffixCount += component.DomainSuffixCount
		summary.KeywordCount += component.KeywordCount
		summary.IPCIDRCount += component.IPCIDRCount
		if component.Truncated {
			summary.Truncated = true
		}
	}
	return summary
}

func packRuleSummary(detail PackDetail) PackRulePackSummary {
	return PackRulePackSummary{
		Source:             detail.Source,
		Pack:               detail.Pack,
		Name:               detail.Name,
		Type:               detail.Type,
		RenderStrategy:     detail.RenderStrategy,
		RenderRuleTemplate: detail.RenderRuleTemplate,
		Target:             detail.Target,
		Renderable:         detail.Renderable,
	}
}

func commonBackend(details []PackDetail) PackBackend {
	if len(details) == 0 {
		return PackBackend{}
	}
	backend := details[0].Backend
	for _, detail := range details[1:] {
		if detail.Backend.Type != backend.Type ||
			detail.Backend.QuerySource != backend.QuerySource ||
			detail.Backend.RenderStrategy != backend.RenderStrategy {
			return PackBackend{Type: "mixed", Note: "Multiple pack backend types were searched."}
		}
	}
	return backend
}

func packRulesQueryable(detail PackDetail) bool {
	return detail.Renderable || len(detail.Providers) > 0
}

func providerCachePath(cacheDir, source, packID, componentID, format string) string {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		cacheDir = filepath.Join(".runtime", "rules", "provider-cache")
	}
	ext := ".txt"
	if strings.EqualFold(format, "yaml") || strings.EqualFold(format, "yml") {
		ext = ".yaml"
	}
	return filepath.Join(cacheDir, safePathSegment(source), safePathSegment(packID), safePathSegment(componentID)+ext)
}

func packLocalID(detail PackDetail) string {
	return detail.Pack
}

func safePathSegment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, "..", "_")
	if value == "" {
		return "_"
	}
	return value
}

func isDomainLike(kind string) bool {
	return kind == "domain" || kind == "domain_suffix" || kind == "domain_keyword"
}

func normalizedPackRulesLimit(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
