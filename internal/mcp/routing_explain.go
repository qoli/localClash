package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"localclash/internal/localconfig"
	"localclash/internal/rules"
)

type routingExplainInput struct {
	Query               string `json:"query"`
	Config              string `json:"config"`
	Subscription        string `json:"subscription"`
	SubscriptionConfig  string `json:"subscription_config"`
	SubscriptionRuntime string `json:"subscription_runtime"`
	RulesCache          string `json:"rules_cache"`
	RuleSources         string `json:"rule_sources"`
	ProviderCache       string `json:"provider_cache"`
	IncludeRuleMatches  *bool  `json:"include_rule_matches"`
	Limit               int    `json:"limit"`
}

type routingExplainResult struct {
	Query            string                    `json:"query"`
	Config           string                    `json:"config"`
	ConfigExists     bool                      `json:"config_exists"`
	PolicyTemplate   string                    `json:"policy_template,omitempty"`
	RouteModel       string                    `json:"route_model"`
	Resolved         bool                      `json:"resolved"`
	ResolveError     string                    `json:"resolve_error,omitempty"`
	CatalogAvailable bool                      `json:"catalog_available"`
	CatalogError     string                    `json:"catalog_error,omitempty"`
	Matches          []routingExplainMatch     `json:"matches"`
	ActiveRoutes     []routingExplainRoute     `json:"active_routes"`
	ReusableExits    []routingExplainExit      `json:"reusable_exits"`
	RuleMatches      []routingExplainRuleMatch `json:"rule_matches,omitempty"`
	Warnings         []string                  `json:"warnings,omitempty"`
	NextActions      []string                  `json:"next_actions"`
	PatchGuidance    routingPatchGuidance      `json:"patch_guidance"`
}

type routingExplainMatch struct {
	Kind   string  `json:"kind"`
	ID     string  `json:"id"`
	Target string  `json:"target,omitempty"`
	Reason string  `json:"reason,omitempty"`
	Score  int     `json:"score"`
	Route  *string `json:"route,omitempty"`
}

type routingExplainRoute struct {
	SourceKind  string                `json:"source_kind"`
	ID          string                `json:"id"`
	Target      string                `json:"target"`
	TargetKind  string                `json:"target_kind"`
	Reason      string                `json:"reason,omitempty"`
	Pack        *routingExplainPack   `json:"pack,omitempty"`
	PolicyGroup *routingExplainPolicy `json:"policy_group,omitempty"`
	ProxyGroup  *routingExplainProxy  `json:"proxy_group,omitempty"`
	Exits       []routingExplainExit  `json:"exits,omitempty"`
}

type routingExplainPack struct {
	Source         string `json:"source,omitempty"`
	Pack           string `json:"pack"`
	Name           string `json:"name,omitempty"`
	Type           string `json:"type,omitempty"`
	RenderStrategy string `json:"render_strategy,omitempty"`
}

type routingExplainPolicy struct {
	ID       string   `json:"id"`
	Mode     string   `json:"mode"`
	Exits    []string `json:"exits"`
	Reason   string   `json:"reason,omitempty"`
	Boundary string   `json:"boundary,omitempty"`
}

type routingExplainProxy struct {
	ID            string             `json:"id"`
	Mode          string             `json:"mode"`
	Match         *localconfig.Match `json:"match,omitempty"`
	Nodes         []string           `json:"nodes,omitempty"`
	SelectedNodes []string           `json:"selected_nodes,omitempty"`
	NodeCount     int                `json:"node_count"`
	Optional      bool               `json:"optional,omitempty"`
	Reason        string             `json:"reason,omitempty"`
	Boundary      string             `json:"boundary,omitempty"`
}

type routingExplainExit struct {
	ID            string             `json:"id"`
	Kind          string             `json:"kind"`
	Mode          string             `json:"mode,omitempty"`
	Match         *localconfig.Match `json:"match,omitempty"`
	NodeCount     int                `json:"node_count,omitempty"`
	SelectedNodes []string           `json:"selected_nodes,omitempty"`
	Optional      bool               `json:"optional,omitempty"`
	Reason        string             `json:"reason,omitempty"`
	Boundary      string             `json:"boundary,omitempty"`
}

type routingExplainRuleMatch struct {
	Source         string `json:"source"`
	Pack           string `json:"pack"`
	PackName       string `json:"pack_name,omitempty"`
	Active         bool   `json:"active"`
	ActiveTarget   string `json:"active_target,omitempty"`
	ActiveRoute    string `json:"active_route,omitempty"`
	Rule           string `json:"rule"`
	Kind           string `json:"kind"`
	Value          string `json:"value,omitempty"`
	RenderStrategy string `json:"render_strategy,omitempty"`
}

type routingPatchGuidance struct {
	Summary string   `json:"summary"`
	Steps   []string `json:"steps"`
	Notes   []string `json:"notes,omitempty"`
}

func (s *Server) callRoutingExplain(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in routingExplainInput
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if strings.TrimSpace(in.Query) == "" {
		return toolResult{}, fmt.Errorf("query is required")
	}
	s.applyRoutingExplainDefaults(&in)
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	result := routingExplainResult{
		Query:      in.Query,
		Config:     in.Config,
		RouteModel: "localclash.json durable intent; default template model is business group -> exit group -> subscription nodes",
	}
	config, err := localconfig.Load(in.Config)
	if err != nil {
		if os.IsNotExist(err) {
			result.NextActions = []string{"call config_configure with policy_template=localclash-default or minimal before asking routing_explain", "call config_status to inspect current localClash config state"}
			result.PatchGuidance = defaultRoutingPatchGuidance("")
			return jsonToolResult(result)
		}
		return toolResult{}, err
	}
	result.ConfigExists = true
	result.PolicyTemplate = config.PolicyTemplate

	resolved, resolveErr := localconfig.Resolve(localconfig.ResolveOptions{
		Config:              config,
		SubscriptionPath:    in.Subscription,
		SubscriptionConfig:  in.SubscriptionConfig,
		SubscriptionRuntime: in.SubscriptionRuntime,
		RulesCache:          in.RulesCache,
		OnStage:             localConfigTaskLogger(ctx, "routing_explain.resolve_config"),
	})
	if resolveErr == nil {
		result.Resolved = true
		config = resolved.Config
	} else {
		result.ResolveError = resolveErr.Error()
		result.Warnings = append(result.Warnings, "selector/node resolution is unavailable; explanation is based on durable localclash.json intent only")
	}

	catalog, catalogErr := rules.LoadPackCatalog(in.RulesCache)
	if catalogErr == nil {
		result.CatalogAvailable = true
	} else {
		result.CatalogError = catalogErr.Error()
		result.Warnings = append(result.Warnings, "pack catalog is unavailable; pack names and render backends may be incomplete")
	}

	index := buildRoutingIndex(config, resolved, result.Resolved, catalog)
	result.ReusableExits = limitRoutingExits(queryMatchingExits(in.Query, index), limit)
	result.Matches = limitRoutingMatches(queryRoutingMatches(in.Query, index), limit)
	result.ActiveRoutes = limitRoutingRoutes(routesFromMatches(result.Matches, index), limit)

	includeRuleMatches := true
	if in.IncludeRuleMatches != nil {
		includeRuleMatches = *in.IncludeRuleMatches
	}
	if includeRuleMatches {
		ruleMatches, warnings := queryRoutingRuleMatches(ctx, in, index, limit)
		result.RuleMatches = ruleMatches
		result.Warnings = append(result.Warnings, warnings...)
	}

	if len(result.ActiveRoutes) == 0 && len(result.RuleMatches) > 0 {
		result.ActiveRoutes = limitRoutingRoutes(routesFromRuleMatches(result.RuleMatches, index), limit)
	}
	result.PatchGuidance = defaultRoutingPatchGuidance(firstRouteTarget(result.ActiveRoutes))
	result.NextActions = routingExplainNextActions(result)
	return jsonToolResult(result)
}

func (s *Server) applyRoutingExplainDefaults(in *routingExplainInput) {
	cfg := configToolInput{
		Config:              in.Config,
		Subscription:        in.Subscription,
		SubscriptionConfig:  in.SubscriptionConfig,
		SubscriptionRuntime: in.SubscriptionRuntime,
		RulesCache:          in.RulesCache,
	}
	s.applyConfigToolDefaults(&cfg)
	in.Config = cfg.Config
	in.Subscription = cfg.Subscription
	in.SubscriptionConfig = cfg.SubscriptionConfig
	in.SubscriptionRuntime = cfg.SubscriptionRuntime
	in.RulesCache = cfg.RulesCache
	if strings.TrimSpace(in.RuleSources) == "" {
		if s.state != nil && s.state.Paths.RuleSourcesDir != "" {
			in.RuleSources = s.state.Paths.RuleSourcesDir
		} else {
			in.RuleSources = "rule-sources"
		}
	}
	if strings.TrimSpace(in.ProviderCache) == "" {
		if s.state != nil && s.state.Paths.RuntimeRoot != "" {
			in.ProviderCache = filepath.Join(s.state.Paths.RuntimeRoot, "rules", "provider-cache")
		} else {
			in.ProviderCache = filepath.Join(".runtime", "rules", "provider-cache")
		}
	}
}

type routingIndex struct {
	Config        localconfig.Config
	Packs         map[string]localconfig.Pack
	PackDetails   map[string]rules.PackDetail
	PolicyGroups  map[string]localconfig.PolicyGroup
	ProxyGroups   map[string]localconfig.ProxyGroup
	ResolvedProxy map[string]localconfig.ProxyGroupResult
}

func buildRoutingIndex(config localconfig.Config, resolved localconfig.Resolved, hasResolved bool, catalog rules.PackCatalog) routingIndex {
	index := routingIndex{
		Config:        config,
		Packs:         map[string]localconfig.Pack{},
		PackDetails:   map[string]rules.PackDetail{},
		PolicyGroups:  map[string]localconfig.PolicyGroup{},
		ProxyGroups:   map[string]localconfig.ProxyGroup{},
		ResolvedProxy: map[string]localconfig.ProxyGroupResult{},
	}
	for _, pack := range config.Packs {
		index.Packs[rules.PackKey(pack.Source, pack.Pack)] = pack
	}
	for id, group := range config.PolicyGroups {
		index.PolicyGroups[id] = group
	}
	for id, group := range config.ProxyGroups {
		index.ProxyGroups[id] = group
	}
	for id, detail := range catalog.Details {
		index.PackDetails[id] = detail
	}
	if hasResolved {
		for _, group := range resolved.ProxyGroups {
			index.ResolvedProxy[group.ID] = group
		}
	}
	return index
}

func queryRoutingMatches(query string, index routingIndex) []routingExplainMatch {
	var matches []routingExplainMatch
	for id, group := range index.PolicyGroups {
		if score := matchScore(query, id, group.Reason, group.Boundary, strings.Join(group.Exits, " ")); score > 0 {
			route := "policy_group:" + id
			matches = append(matches, routingExplainMatch{Kind: "policy_group", ID: id, Reason: group.Reason, Score: score, Route: &route})
		}
	}
	for id, group := range index.ProxyGroups {
		if score := matchScore(query, id, group.Reason, group.Boundary, group.Mode, matchText(group.Match)); score > 0 {
			route := "proxy_group:" + id
			matches = append(matches, routingExplainMatch{Kind: "proxy_group", ID: id, Reason: group.Reason, Score: score, Route: &route})
		}
	}
	for _, pack := range index.Config.Packs {
		key := rules.PackKey(pack.Source, pack.Pack)
		detail := index.PackDetails[key]
		if score := matchScore(query, pack.Source, pack.Pack, pack.Target, pack.Reason, detail.Name, detail.Source); score > 0 {
			route := "pack:" + key + " -> " + pack.Target
			matches = append(matches, routingExplainMatch{Kind: "pack", ID: key, Target: pack.Target, Reason: pack.Reason, Score: score, Route: &route})
		}
	}
	for _, pack := range index.Config.EnabledRulePacks {
		if score := matchScore(query, pack.ID, pack.Target, pack.Reason); score > 0 {
			route := "local_rule_pack:" + pack.ID + " -> " + pack.Target
			matches = append(matches, routingExplainMatch{Kind: "local_rule_pack", ID: pack.ID, Target: pack.Target, Reason: pack.Reason, Score: score, Route: &route})
		}
	}
	for _, custom := range index.Config.CustomRules {
		if score := matchScore(query, custom.ID, custom.Target, custom.Reason, customRulesText(custom.Rules)); score > 0 {
			route := "custom_rule:" + custom.ID + " -> " + custom.Target
			matches = append(matches, routingExplainMatch{Kind: "custom_rule", ID: custom.ID, Target: custom.Target, Reason: custom.Reason, Score: score, Route: &route})
		}
	}
	for _, provider := range index.Config.RuleProviders {
		if score := matchScore(query, provider.ID, provider.Target, provider.Reason, provider.URL, provider.Path); score > 0 {
			route := "rule_provider:" + provider.ID + " -> " + provider.Target
			matches = append(matches, routingExplainMatch{Kind: "rule_provider", ID: provider.ID, Target: provider.Target, Reason: provider.Reason, Score: score, Route: &route})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			if matches[i].Kind == matches[j].Kind {
				return matches[i].ID < matches[j].ID
			}
			return matches[i].Kind < matches[j].Kind
		}
		return matches[i].Score > matches[j].Score
	})
	return matches
}

func queryMatchingExits(query string, index routingIndex) []routingExplainExit {
	var exits []routingExplainExit
	for id, group := range index.ProxyGroups {
		if matchScore(query, id, group.Reason, group.Boundary, group.Mode, matchText(group.Match)) > 0 {
			exits = append(exits, exitForTarget(id, index))
		}
	}
	sort.Slice(exits, func(i, j int) bool { return exits[i].ID < exits[j].ID })
	return exits
}

func routesFromMatches(matches []routingExplainMatch, index routingIndex) []routingExplainRoute {
	seen := map[string]bool{}
	var routes []routingExplainRoute
	for _, match := range matches {
		switch match.Kind {
		case "pack", "local_rule_pack":
			route := routeForSource("pack", match.ID, match.Target, match.Reason, index)
			if match.Kind == "local_rule_pack" {
				route = routeForSource("local_rule_pack", match.ID, match.Target, match.Reason, index)
			}
			if addRoute(route, seen) {
				routes = append(routes, route)
			}
		case "custom_rule", "rule_provider":
			route := routeForSource(match.Kind, match.ID, match.Target, match.Reason, index)
			if addRoute(route, seen) {
				routes = append(routes, route)
			}
		case "policy_group":
			route := routeForSource("policy_group", match.ID, match.ID, match.Reason, index)
			if addRoute(route, seen) {
				routes = append(routes, route)
			}
		case "proxy_group":
			route := routeForSource("proxy_group", match.ID, match.ID, match.Reason, index)
			if addRoute(route, seen) {
				routes = append(routes, route)
			}
		}
	}
	return routes
}

func routesFromRuleMatches(matches []routingExplainRuleMatch, index routingIndex) []routingExplainRoute {
	seen := map[string]bool{}
	var routes []routingExplainRoute
	for _, match := range matches {
		if !match.Active {
			continue
		}
		key := rules.PackKey(match.Source, match.Pack)
		pack := index.Packs[key]
		route := routeForSource("pack", key, pack.Target, pack.Reason, index)
		if addRoute(route, seen) {
			routes = append(routes, route)
		}
	}
	return routes
}

func addRoute(route routingExplainRoute, seen map[string]bool) bool {
	key := route.SourceKind + "\x00" + route.ID + "\x00" + route.Target
	if seen[key] {
		return false
	}
	seen[key] = true
	return true
}

func routeForSource(sourceKind, id, target, reason string, index routingIndex) routingExplainRoute {
	route := routingExplainRoute{
		SourceKind: sourceKind,
		ID:         id,
		Target:     target,
		TargetKind: targetKind(target, index),
		Reason:     reason,
	}
	if sourceKind == "pack" {
		detail := index.PackDetails[id]
		pack := routingExplainPack{Source: detail.Source, Pack: detail.Pack, Name: detail.Name, Type: detail.Type, RenderStrategy: detail.RenderStrategy}
		route.Pack = &pack
	}
	if sourceKind == "local_rule_pack" {
		route.Pack = &routingExplainPack{Pack: id, Type: "local_rule_pack"}
	}
	if group, ok := index.PolicyGroups[target]; ok {
		policy := routingExplainPolicy{ID: target, Mode: group.Mode, Exits: append([]string{}, group.Exits...), Reason: group.Reason, Boundary: group.Boundary}
		route.PolicyGroup = &policy
		for _, exit := range group.Exits {
			route.Exits = append(route.Exits, exitForTarget(exit, index))
		}
		return route
	}
	if _, ok := index.ProxyGroups[target]; ok || isBuiltInRoutingTarget(target) {
		exit := exitForTarget(target, index)
		route.Exits = []routingExplainExit{exit}
		if exit.Kind == "proxy_group" {
			proxy := routingExplainProxy{ID: exit.ID, Mode: exit.Mode, Match: exit.Match, NodeCount: exit.NodeCount, SelectedNodes: exit.SelectedNodes, Optional: exit.Optional, Reason: exit.Reason, Boundary: exit.Boundary}
			if group, ok := index.ProxyGroups[target]; ok {
				proxy.Nodes = append([]string{}, group.Nodes...)
			}
			route.ProxyGroup = &proxy
		}
	}
	return route
}

func exitForTarget(target string, index routingIndex) routingExplainExit {
	if group, ok := index.ProxyGroups[target]; ok {
		exit := routingExplainExit{ID: target, Kind: "proxy_group", Mode: group.Mode, Match: group.Match, Optional: group.Optional, Reason: group.Reason, Boundary: group.Boundary}
		if resolved, ok := index.ResolvedProxy[target]; ok {
			exit.NodeCount = resolved.NodeCount
			exit.SelectedNodes = append([]string{}, resolved.SelectedNodes...)
		} else {
			exit.NodeCount = len(group.SelectedNodes)
			exit.SelectedNodes = append([]string{}, group.SelectedNodes...)
		}
		return exit
	}
	if canonical := canonicalRoutingTarget(target); canonical != "" {
		return routingExplainExit{ID: canonical, Kind: "builtin", Mode: strings.ToLower(canonical)}
	}
	return routingExplainExit{ID: strings.TrimSpace(target), Kind: "unknown"}
}

func targetKind(target string, index routingIndex) string {
	if _, ok := index.PolicyGroups[target]; ok {
		return "policy_group"
	}
	if _, ok := index.ProxyGroups[target]; ok {
		return "proxy_group"
	}
	if isBuiltInRoutingTarget(target) {
		return "builtin"
	}
	return "unknown"
}

func queryRoutingRuleMatches(ctx context.Context, in routingExplainInput, index routingIndex, limit int) ([]routingExplainRuleMatch, []string) {
	if len(index.Config.Packs) == 0 {
		return nil, nil
	}
	result, err := rules.QueryPackRules(ctx, rules.PackRulesQueryOptions{
		SourcesDir:    in.RuleSources,
		CacheDir:      in.RulesCache,
		ProviderCache: in.ProviderCache,
		Query:         in.Query,
		Limit:         limit,
	})
	if err != nil {
		return nil, []string{"cached rule match lookup skipped: " + err.Error()}
	}
	activePacks := map[string]localconfig.Pack{}
	for _, pack := range index.Config.Packs {
		activePacks[rules.PackKey(pack.Source, pack.Pack)] = pack
	}
	var out []routingExplainRuleMatch
	for _, match := range result.Matches {
		active := false
		activeTarget := ""
		activeRoute := ""
		key := rules.PackKey(match.Source, match.Pack)
		if pack, ok := activePacks[key]; ok {
			active = true
			activeTarget = pack.Target
			activeRoute = "pack:" + key + " -> " + pack.Target
		}
		out = append(out, routingExplainRuleMatch{
			Source:         match.Source,
			Pack:           match.Pack,
			PackName:       match.PackName,
			Active:         active,
			ActiveTarget:   activeTarget,
			ActiveRoute:    activeRoute,
			Rule:           match.Rule,
			Kind:           match.Kind,
			Value:          match.Value,
			RenderStrategy: match.RenderStrategy,
		})
	}
	warnings := []string{}
	if !result.CacheComplete {
		warnings = append(warnings, "provider-cache is incomplete; routing_explain returned durable intent and any cached rule matches, but domain-level evidence may be incomplete")
	}
	return out, warnings
}

func defaultRoutingPatchGuidance(target string) routingPatchGuidance {
	summary := "Use config_patch_create to make a reviewed routing change; routing_explain is read-only."
	if target != "" {
		summary = "To change this route, create a reviewed patch for target " + target + "."
	}
	return routingPatchGuidance{
		Summary: summary,
		Steps: []string{
			"Call config_status to capture current durable localclash.json intent.",
			"For a new or changed exit, call proxy_group_build or reuse an existing proxy group returned by routing_explain.reusable_exits.",
			"For ACL4SSR-style business routing, call policy_group_build with the desired exits, then config_patch_create with overlay.policy_groups and matching packs/enabled_rule_packs/custom_rules/rule_providers.",
			"Review the returned candidate localclash.json and mihomo.yaml, then call config_patch_apply with the exact patch_id.",
			"Call config_status or routing_explain again to verify the durable intent; restart_runtime only after user confirmation if the running Mihomo process should load the change.",
		},
		Notes: []string{
			"Do not edit generated/mihomo.yaml directly; it is a build artifact.",
			"Do not infer active routing from generated_summary.rules_sample alone because it is truncated.",
		},
	}
}

func routingExplainNextActions(result routingExplainResult) []string {
	if !result.ConfigExists {
		return []string{"call config_configure with policy_template=localclash-default", "call config_status"}
	}
	if len(result.ActiveRoutes) == 0 && len(result.RuleMatches) == 0 {
		return []string{"call packs_list with a semantic keyword from the query", "call pack_rules_prefetch for candidate packs if domain-level evidence is needed", "call config_patch_create if this is a new route request"}
	}
	actions := []string{"use active_routes to identify the current target and exits", "use patch_guidance.steps for a safe reviewed change path"}
	if result.ResolveError != "" {
		actions = append(actions, "call subscriptions_status and subscriptions_refresh if node-level selector evidence is needed")
	}
	return actions
}

func firstRouteTarget(routes []routingExplainRoute) string {
	if len(routes) == 0 {
		return ""
	}
	return routes[0].Target
}

func matchScore(query string, fields ...string) int {
	normalizedQuery := normalizeRoutingText(query)
	if normalizedQuery == "" {
		return 0
	}
	score := 0
	for _, field := range fields {
		text := normalizeRoutingText(field)
		if text == "" {
			continue
		}
		switch {
		case text == normalizedQuery:
			score += 100
		case strings.Contains(text, normalizedQuery):
			score += 40
		case containsAllRoutingTokens(text, normalizedQuery):
			score += 20
		}
	}
	return score
}

func normalizeRoutingText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", " ", "-", " ", ".", " ", "/", " ", ":", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func containsAllRoutingTokens(text, query string) bool {
	for _, token := range strings.Fields(query) {
		if !strings.Contains(text, token) {
			return false
		}
	}
	return true
}

func matchText(match *localconfig.Match) string {
	if match == nil {
		return ""
	}
	return strings.Join(append([]string{match.Type, match.Pattern}, match.SourceIDs...), " ")
}

func customRulesText(lines []localconfig.CustomRuleLine) string {
	var parts []string
	for _, line := range lines {
		parts = append(parts, line.Type, line.Value)
	}
	return strings.Join(parts, " ")
}

func limitRoutingMatches(items []routingExplainMatch, limit int) []routingExplainMatch {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func limitRoutingRoutes(items []routingExplainRoute, limit int) []routingExplainRoute {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func limitRoutingExits(items []routingExplainExit, limit int) []routingExplainExit {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func isBuiltInRoutingTarget(target string) bool {
	return canonicalRoutingTarget(target) != ""
}

func canonicalRoutingTarget(target string) string {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "direct", "reject", "proxy", "manual", "auto":
		return strings.ToUpper(strings.TrimSpace(target))
	default:
		return ""
	}
}
