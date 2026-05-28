package mcp

import (
	"encoding/json"
	"io"
	"sort"
)

type SafetyLevel string

const (
	SafeRead        SafetyLevel = "safe_read"
	SafeWrite       SafetyLevel = "safe_write"
	ConfirmRequired SafetyLevel = "confirm_required"
	HighRisk        SafetyLevel = "high_risk"
)

type Tool struct {
	Name        string      `json:"name"`
	SafetyLevel SafetyLevel `json:"safety_level"`
	Description string      `json:"description"`
}

type ListedTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	SafetyLevel SafetyLevel    `json:"safety_level"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type ToolSummary struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	SafetyLevel SafetyLevel `json:"safety_level"`
}

type ServerRuntimeInfo struct {
	Binary       string `json:"binary,omitempty"`
	BinarySHA256 string `json:"binary_sha256,omitempty"`
	BinaryError  string `json:"binary_error,omitempty"`
	WorkingDir   string `json:"working_dir,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`
}

type ToolsListResult struct {
	Server ServerRuntimeInfo `json:"server"`
	Tools  []ToolSummary     `json:"tools"`
	Count  int               `json:"count"`
}

func Registry() []Tool {
	tools := []Tool{
		{Name: "config_configure", SafetyLevel: SafeWrite, Description: "Configure localClash base product state with optional core, runtime_profile, and policy_template. This writes localclash-runtime.json and/or localclash.json, but does not configure subscriptions, render generated config, start runtime, or apply router takeover."},
		{Name: "config_status", SafetyLevel: SafeRead, Description: "Inspect localClash config status: source-of-truth localclash.json, generated/mihomo.yaml build artifact, render readiness, and pending patches. Default output is lightweight; pass resolve=true for selected-node matches or detail=true for generated-summary/overlay audit."},
		{Name: "doctor", SafetyLevel: SafeRead, Description: "Run read-only localClash diagnostics."},
		{Name: "environment_inspect", SafetyLevel: SafeRead, Description: "Inspect host, network capability evidence, and localClash state without exposing credentials."},
		{Name: "nl_file", SafetyLevel: SafeRead, Description: "Read a repository-local text file with nl-style stable line numbers for follow-up sed_file edits."},
		{Name: "pack_rules_query", SafetyLevel: SafeRead, Description: "Search locally cached pack rules for a domain or keyword and return source/pack/type/render_strategy/component metadata. Does not download provider rules; call pack_rules_prefetch first when cache coverage is incomplete."},
		{Name: "packs_get", SafetyLevel: SafeRead, Description: "Read details for one exact source/pack cache entry, including type/backend/render_strategy. The target field is a catalog default/recommended target, not proof the pack is active; use config_status for active routing."},
		{Name: "packs_list", SafetyLevel: SafeRead, Description: "List and filter available rule pack catalog entries with type and render_strategy. This is discovery only: target is catalog default/recommended target, not current active configuration."},
		{Name: "subscription_nodes_list", SafetyLevel: SafeRead, Description: "List safe subscription proxy name/type summaries without exposing connection credentials."},
		{Name: "subscription_nodes_search", SafetyLevel: SafeRead, Description: "Search subscription proxy names and return safe name/type summaries; does not verify network egress location."},
		{Name: "runtime_profile_status", SafetyLevel: SafeRead, Description: "Inspect the active Mihomo runtime profile and its safe summary without exposing proxy credentials."},
		{Name: "subscriptions_status", SafetyLevel: SafeRead, Description: "Inspect configured subscription sources and local effective subscription state."},
		{Name: "runtime_status", SafetyLevel: SafeRead, Description: "Inspect Mihomo runtime status from the local PID file and matching orphan runtime processes without changing runtime state."},
		{Name: "router_takeover_status", SafetyLevel: SafeRead, Description: "Inspect localClash-owned OpenWrt router takeover runtime state: runtime profile, Mihomo runtime, fw4/nft chains, DNS hijack, fwmark route, and TUN device."},
		{Name: "routing_explain", SafetyLevel: SafeRead, Description: "Explain active durable routing intent for a service, domain, pack, policy group, or exit query. Reads localclash.json, active packs, policy groups, proxy groups, custom rules, and cached rule matches; does not modify config or start runtime."},
		{Name: "tools_list", SafetyLevel: SafeRead, Description: "List localClash MCP tools as ordinary tool output for clients that do not expose MCP registry introspection to the model."},
		{Name: "config_patch_apply", SafetyLevel: SafeWrite, Description: "Apply a reviewed config patch by writing localclash.json, deriving localclash-packs.gob, and regenerating generated/mihomo.yaml without starting the runtime. Use the exact patch_id returned by config_patch_create."},
		{Name: "config_patch_create", SafetyLevel: SafeWrite, Description: "Create a reviewable config patch and candidate Mihomo config from proxy groups, packs, and custom rules. It does not modify active localclash.json or generated/mihomo.yaml and does not run Mihomo config test by default; config_patch_apply is the validation boundary."},
		{Name: "config_render", SafetyLevel: SafeWrite, Description: "Render generated/mihomo.yaml from the current durable localclash.json source of truth, subscription, policy template graph, and runtime profile. Does not read patches and does not start runtime."},
		{Name: "custom_rules_build", SafetyLevel: SafeWrite, Description: "Build and validate user custom routing rules for domains or CIDRs before adding them to a config patch."},
		{Name: "pack_rules_prefetch", SafetyLevel: SafeWrite, Description: "Download provider rules for selected packs into local provider-cache so pack_rules_query can search them locally."},
		{Name: "pack_rules_read", SafetyLevel: SafeWrite, Description: "Read rules for one exact source/pack pair, downloading missing provider-cache entries for that pack only, and return source/pack/type/render_strategy/component metadata."},
		{Name: "policy_group_build", SafetyLevel: SafeWrite, Description: "Build and validate a business-layer policy group that routes one rule domain, app, or scenario to existing exits such as HK, JP, US, ⚡ 自动选择, or DIRECT. This does not persist state; copy the returned policy_group into config_patch_create.overlay.policy_groups."},
		{Name: "proxy_group_build", SafetyLevel: SafeWrite, Description: "Build and validate a reusable proxy group target from subscription node selectors or exact nodes. This does not persist state; copy the returned proxy_group into config_patch_create.overlay.proxy_groups when a patch should use it."},
		{Name: "rule_provider_build", SafetyLevel: SafeWrite, Description: "Build and validate a reusable external rule-provider intent for user-supplied Mihomo rule-provider URLs before adding it to config_patch_create.overlay.rule_providers."},
		{Name: "subscriptions_configure", SafetyLevel: SafeWrite, Description: "Write local subscription source configuration without refreshing."},
		{Name: "subscriptions_refresh", SafetyLevel: SafeWrite, Description: "Refresh configured subscription sources into local artifacts and effective subscription.gob."},
		{Name: "run_runtime", SafetyLevel: ConfirmRequired, Description: "Start the Mihomo runtime from generated config, assuming generated/mihomo.yaml has already been validated by config_patch_apply or doctor. Requires external Agent/MCP client confirmation; starting the proxy runtime may temporarily interrupt network connectivity, and the Agent itself may be disconnected if it depends on the current network/proxy path."},
		{Name: "restart_runtime", SafetyLevel: ConfirmRequired, Description: "Stop the current Mihomo runtime if needed, then start it again in one confirmed call, assuming generated/mihomo.yaml has already been validated by config_patch_apply or doctor. Use this instead of stop_runtime then run_runtime when the Agent may depend on the proxy path."},
		{Name: "router_takeover_apply", SafetyLevel: ConfirmRequired, Description: "Apply localClash-owned OpenWrt router takeover runtime rules for router profile mode. Uses localClash router redir-host-mix behavior: TCP redir-host, DNS hijack, fwmark route, and TUN forwarding. Does not persist firewall config; call only after run_runtime or restart_runtime and user confirmation."},
		{Name: "router_takeover_stop", SafetyLevel: ConfirmRequired, Description: "Remove localClash-owned OpenWrt router takeover runtime rules without stopping Mihomo. This changes firewall, DNS, and policy-routing runtime state and requires user confirmation."},
		{Name: "sed_file", SafetyLevel: SafeWrite, Description: "Apply sed-style repository-local text edits with dry-run diff output. Defaults to dry_run=true."},
		{Name: "stop_runtime", SafetyLevel: ConfirmRequired, Description: "Stop the Mihomo runtime recorded by the local PID file, plus matching orphan runtime processes. Refuses by default when router takeover is effective because router traffic still depends on Mihomo; call router_takeover_stop first or pass force=true only after explicit user confirmation."},
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

func WriteRegistry(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(Registry())
}

func ListedTools() []ListedTool {
	tools := Registry()
	out := make([]ListedTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, ListedTool{
			Name:        tool.Name,
			Description: tool.Description,
			SafetyLevel: tool.SafetyLevel,
			InputSchema: inputSchemaForTool(tool.Name),
			Annotations: map[string]any{
				"safety_level": tool.SafetyLevel,
			},
		})
	}
	return out
}

func ToolSummaries(server ServerRuntimeInfo) ToolsListResult {
	tools := Registry()
	summaries := make([]ToolSummary, 0, len(tools))
	for _, tool := range tools {
		summaries = append(summaries, ToolSummary{
			Name:        tool.Name,
			Description: tool.Description,
			SafetyLevel: tool.SafetyLevel,
		})
	}
	return ToolsListResult{
		Server: server,
		Tools:  summaries,
		Count:  len(summaries),
	}
}

func inputSchemaForTool(name string) map[string]any {
	switch name {
	case "tools_list":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}
	case "environment_inspect":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}
	case "config_configure":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"core":                   map[string]any{"type": "string", "enum": []string{"meta", "smart"}, "description": "Optional Mihomo core flavor to activate."},
				"runtime_profile":        map[string]any{"type": "string", "enum": []string{"normal", "router"}, "description": "Optional runtime profile mode to activate."},
				"policy_template":        map[string]any{"type": "string", "description": "Optional policy template id loaded from policy-templates/. Built-ins include minimal and localclash-default."},
				"policy_templates_dir":   map[string]any{"type": "string", "description": "Directory containing policy template YAML files. Defaults to policy-templates."},
				"config":                 map[string]any{"type": "string", "description": "Durable localClash source-of-truth config path. Defaults to localclash.json."},
				"runtime_profile_config": map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.json."},
				"rules_cache":            map[string]any{"type": "string", "description": "Pack cache directory used to validate policy_template pack references. Defaults to .runtime/rules/packs."},
				"subscription":           map[string]any{"type": "string", "description": "Subscription gob path for readiness reporting. Defaults to subscription.gob."},
				"subscription_config":    map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.json."},
				"subscription_runtime":   map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
			},
		}
	case "config_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":               map[string]any{"type": "string", "description": "Durable localClash source-of-truth config path. Defaults to localclash.json."},
				"subscription":         map[string]any{"type": "string", "description": "Subscription gob path. Defaults to subscription.gob."},
				"subscription_config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.json."},
				"subscription_runtime": map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
				"rules_cache":          map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"runtime_profile":      map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.json."},
				"selection":            map[string]any{"type": "string", "description": "Derived packs selection path. Defaults to localclash-packs.gob."},
				"output":               map[string]any{"type": "string", "description": "Generated Mihomo config path. Defaults to generated/mihomo.yaml."},
				"patches_dir":          map[string]any{"type": "string", "description": "Review patch artifact root. Defaults to .runtime/patches."},
				"limit":                map[string]any{"type": "integer", "minimum": 1, "description": "Maximum summary entries per section. Defaults to 20."},
			},
		}
	case "config_render":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":               map[string]any{"type": "string", "description": "Durable localClash source-of-truth config path. Defaults to localclash.json."},
				"subscription":         map[string]any{"type": "string", "description": "Subscription gob path. Defaults to subscription.gob."},
				"subscription_config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.json."},
				"subscription_runtime": map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
				"rules_cache":          map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"runtime_profile":      map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.json."},
				"selection":            map[string]any{"type": "string", "description": "Derived packs selection path. Defaults to localclash-packs.gob."},
				"output":               map[string]any{"type": "string", "description": "Generated Mihomo config path. Defaults to generated/mihomo.yaml."},
				"force":                map[string]any{"type": "boolean", "description": "Overwrite generated output. Defaults to true because generated/mihomo.yaml is a build artifact."},
				"background":           map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for write tools that may render or test Mihomo config."},
				"wait":                 map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "routing_explain":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"query"},
			"properties": map[string]any{
				"query":                map[string]any{"type": "string", "description": "Service, app, domain, exact pack name/source, policy group, or exit to explain, for example Steam, ChatGPT, openai.com, or Singapore."},
				"config":               map[string]any{"type": "string", "description": "Durable localClash source-of-truth config path. Defaults to localclash.json."},
				"subscription":         map[string]any{"type": "string", "description": "Subscription gob path used only for optional selector resolution. Defaults to subscription.gob."},
				"subscription_config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.json."},
				"subscription_runtime": map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
				"rules_cache":          map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"rule_sources":         map[string]any{"type": "string", "description": "Rule sources directory used if pack catalog must be adapted. Defaults to rule-sources."},
				"provider_cache":       map[string]any{"type": "string", "description": "Local provider-cache directory for optional domain/rule matching. Defaults to .runtime/rules/provider-cache."},
				"include_rule_matches": map[string]any{"type": "boolean", "description": "Search cached rule/provider contents for the query. Defaults to true; does not download missing provider rules."},
				"limit":                map[string]any{"type": "integer", "minimum": 1, "description": "Maximum matches per section. Defaults to 20."},
			},
		}
	case "config_patch_apply":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"patch_id":             map[string]any{"type": "string", "description": "Patch directory id returned by config_patch_create."},
				"patches_dir":          map[string]any{"type": "string", "description": "Patch artifact root. Defaults to .runtime/patches."},
				"summary_path":         map[string]any{"type": "string", "description": "Optional explicit summary.json path. Use patch_id for normal flows."},
				"config":               map[string]any{"type": "string", "description": "Persistent localClash config path. Defaults to localclash.json."},
				"subscription":         map[string]any{"type": "string", "description": "Subscription gob path. Defaults to subscription.gob."},
				"subscription_config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.json."},
				"subscription_runtime": map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
				"rules_cache":          map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"runtime_profile":      map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.json."},
				"selection":            map[string]any{"type": "string", "description": "Persistent packs selection path. Defaults to localclash-packs.gob."},
				"output":               map[string]any{"type": "string", "description": "Generated Mihomo config path. Defaults to generated/mihomo.yaml."},
				"backup_dir":           map[string]any{"type": "string", "description": "Backup root for overwritten local artifacts. Defaults to .runtime/backups/config-patch-apply."},
				"test":                 map[string]any{"type": "boolean", "description": "Run Mihomo config test before applying. Defaults to true. Setting false bypasses Mihomo validation and should only be used after explicit user confirmation."},
				"core":                 map[string]any{"type": "string", "description": "Mihomo core path for config test. Defaults to the active runtime profile core path."},
				"runtime_dir":          map[string]any{"type": "string", "description": "Mihomo runtime artifact source for isolated config test. Defaults to .runtime/mihomo; live cache.db is not copied."},
				"background":           map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for write tools that may render or test Mihomo config."},
				"wait":                 map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "nl_file":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"path":        map[string]any{"type": "string", "description": "Repository-local text file path."},
				"start_line":  map[string]any{"type": "integer", "minimum": 1, "description": "First 1-based line to return. Defaults to 1."},
				"limit_lines": map[string]any{"type": "integer", "minimum": 1, "description": "Maximum number of lines to return. Defaults to 120."},
				"max_bytes":   map[string]any{"type": "integer", "minimum": 1, "description": "Maximum returned content bytes. Defaults to 65536."},
			},
			"required": []string{"path"},
		}
	case "sed_file":
		edit := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"op":         map[string]any{"type": "string", "enum": []string{"replace", "insert_before", "insert_after", "delete_range", "append"}},
				"old":        map[string]any{"type": "string", "description": "Exact text to replace for replace edits."},
				"new":        map[string]any{"type": "string", "description": "Replacement text for replace edits."},
				"count":      map[string]any{"type": "integer", "minimum": 1, "description": "Number of exact replacements. Defaults to 1."},
				"line":       map[string]any{"type": "integer", "minimum": 1, "description": "Target line for insert_before or insert_after."},
				"start_line": map[string]any{"type": "integer", "minimum": 1, "description": "First line for delete_range."},
				"end_line":   map[string]any{"type": "integer", "minimum": 1, "description": "Last line for delete_range."},
				"text":       map[string]any{"type": "string", "description": "Text for insert_before, insert_after, or append."},
			},
			"required": []string{"op"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"path":            map[string]any{"type": "string", "description": "Repository-local text file path."},
				"dry_run":         map[string]any{"type": "boolean", "description": "Return the diff without writing. Defaults to true."},
				"expected_sha256": map[string]any{"type": "string", "description": "Optional file sha256 from nl_file; rejects edits if the file changed."},
				"edits":           map[string]any{"type": "array", "items": edit, "description": "Ordered sed-style edits to apply."},
			},
			"required": []string{"path", "edits"},
		}
	case "proxy_group_build":
		matchIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"type":           map[string]any{"type": "string", "enum": []string{"name_regex"}, "description": "Selector type. name_regex matches subscription proxy names only."},
				"pattern":        map[string]any{"type": "string", "description": "Regular expression matched against subscription proxy names."},
				"source_ids":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional subscription source ids to constrain matches."},
				"min":            map[string]any{"type": "integer", "minimum": 0, "description": "Minimum required matches. Defaults to 1."},
				"max":            map[string]any{"type": "integer", "minimum": 0, "description": "Maximum matches to materialize. 0 means unlimited."},
				"case_sensitive": map[string]any{"type": "boolean", "description": "Whether pattern matching is case-sensitive. Defaults to false."},
			},
			"required": []string{"type", "pattern"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":                   map[string]any{"type": "string", "description": "Reusable proxy group id, for example TempLine or SteamHK."},
				"match":                matchIntent,
				"nodes":                map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Exact subscription proxy names for a user-specified line. Use either match or nodes, not both."},
				"mode":                 map[string]any{"type": "string", "enum": []string{"manual", "auto", "smart", "direct"}, "description": "Desired proxy-group mode. manual becomes select; auto becomes url-test; smart becomes smart; direct becomes a named DIRECT-only exit."},
				"reason":               map[string]any{"type": "string", "description": "Short durable reason used if selector repair needs user involvement."},
				"boundary":             map[string]any{"type": "string", "description": "Boundary note, for example name_based_hint_only."},
				"subscription":         map[string]any{"type": "string", "description": "Subscription gob path. Defaults to subscription.gob."},
				"subscription_config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.json."},
				"subscription_runtime": map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
			},
			"required": []string{"id", "mode"},
		}
	case "policy_group_build":
		return policyGroupInputSchema("Policy group id referenced by packs[].target, custom_rules[].target, or rule_providers[].target, for example Steam.")
	case "custom_rules_build":
		ruleIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"type":       map[string]any{"type": "string", "enum": []string{"domain", "domain_suffix", "ip_cidr", "ip_cidr6"}, "description": "Mihomo rule type to generate."},
				"value":      map[string]any{"type": "string", "description": "Domain, domain suffix, or CIDR value."},
				"no_resolve": map[string]any{"type": "boolean", "description": "Append no-resolve for IP CIDR rules when needed."},
			},
			"required": []string{"type", "value"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":     map[string]any{"type": "string", "description": "Stable custom rule id, for example huggingface_temp."},
				"target": map[string]any{"type": "string", "description": "Terminal target DIRECT/REJECT, a proxy group id built by proxy_group_build, or a policy group id built by policy_group_build."},
				"reason": map[string]any{"type": "string", "description": "Short durable reason for this user rule."},
				"rules":  map[string]any{"type": "array", "items": ruleIntent, "description": "Rules that share the same target."},
			},
			"required": []string{"id", "target", "rules"},
		}
	case "rule_provider_build":
		return ruleProviderInputSchema("External rule-provider id, for example US-Proxy.")
	case "config_patch_create":
		matchIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"type":           map[string]any{"type": "string", "enum": []string{"name_regex"}, "description": "Selector type. name_regex matches subscription proxy names only."},
				"pattern":        map[string]any{"type": "string", "description": "Regular expression matched against subscription proxy names."},
				"source_ids":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional subscription source ids to constrain matches."},
				"min":            map[string]any{"type": "integer", "minimum": 0, "description": "Minimum required matches. Defaults to 1."},
				"max":            map[string]any{"type": "integer", "minimum": 0, "description": "Maximum matches to materialize. 0 means unlimited."},
				"case_sensitive": map[string]any{"type": "boolean", "description": "Whether pattern matching is case-sensitive. Defaults to false."},
			},
			"required": []string{"type", "pattern"},
		}
		packIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"source": map[string]any{"type": "string", "description": "Exact pack source from packs_list[].source or tool_args, for example blackmatrix7, sukkaw, or v2fly-dlc."},
				"pack":   map[string]any{"type": "string", "description": "Exact upstream pack name from packs_list[].pack or tool_args, for example OpenAI, ai_non_ip, google, or geolocation-!cn."},
				"type":   map[string]any{"type": "string", "enum": []string{"rule_provider", "geosite"}, "description": "Optional assertion for the catalog pack type. It validates the path but does not choose render behavior."},
				"target": map[string]any{"type": "string", "description": "Desired rule target, for example DIRECT, REJECT, ⚡ 自动选择, or AI."},
				"reason": map[string]any{"type": "string", "description": "Short durable reason for this pack routing choice."},
			},
			"required": []string{"source", "pack", "target"},
		}
		ruleIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"type":       map[string]any{"type": "string", "enum": []string{"domain", "domain_suffix", "ip_cidr", "ip_cidr6"}, "description": "Mihomo rule type to generate."},
				"value":      map[string]any{"type": "string", "description": "Domain, domain suffix, or CIDR value."},
				"no_resolve": map[string]any{"type": "boolean", "description": "Append no-resolve for IP CIDR rules when needed."},
			},
			"required": []string{"type", "value"},
		}
		customRuleIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":     map[string]any{"type": "string", "description": "Stable custom rule id, for example huggingface_temp."},
				"target": map[string]any{"type": "string", "description": "Terminal target DIRECT/REJECT, a proxy group id, or a policy group id."},
				"reason": map[string]any{"type": "string", "description": "Short durable reason for this user rule."},
				"rules":  map[string]any{"type": "array", "items": ruleIntent, "description": "Rules that share the same target."},
			},
			"required": []string{"id", "target", "rules"},
		}
		localRulePackIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":     map[string]any{"type": "string", "description": "Local rule pack id from rule-packs/*.json."},
				"target": map[string]any{"type": "string", "description": "Terminal target DIRECT/REJECT, a proxy group id, or a policy group id. Defaults to the pack default_target when omitted."},
				"reason": map[string]any{"type": "string", "description": "Short durable reason for enabling this local rule pack."},
			},
			"required": []string{"id"},
		}
		ruleProviderIntent := ruleProviderInputSchema("External rule-provider id, for example US-Proxy.")
		proxyGroupIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "Proxy group id referenced by packs[].target, for example SteamHK."},
				"match":    matchIntent,
				"nodes":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Exact subscription proxy names for a user-specified line. Use either match or nodes, not both."},
				"mode":     map[string]any{"type": "string", "enum": []string{"manual", "auto", "smart", "direct"}, "description": "Desired proxy-group mode. manual becomes select; auto becomes url-test; smart becomes smart; direct becomes a named DIRECT-only exit."},
				"reason":   map[string]any{"type": "string", "description": "Short durable reason used if selector repair needs Agent involvement."},
				"boundary": map[string]any{"type": "string", "description": "Boundary note, for example name_based_hint_only."},
			},
			"required": []string{"id", "mode"},
		}
		policyGroupIntent := policyGroupInputSchema("Policy group id referenced by packs[].target, custom_rules[].target, or rule_providers[].target, for example Steam.")
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"patch_name":           map[string]any{"type": "string", "description": "Human-readable patch slug prefix."},
				"subscription":         map[string]any{"type": "string", "description": "Subscription gob path. Defaults to subscription.gob."},
				"rules_cache":          map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"runtime_profile":      map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.json."},
				"patches_dir":          map[string]any{"type": "string", "description": "Patch artifact root. Defaults to .runtime/patches."},
				"config":               map[string]any{"type": "string", "description": "Candidate localClash config filename in the patch. Defaults to localclash.json."},
				"subscription_config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.json."},
				"subscription_runtime": map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
				"test":                 map[string]any{"type": "boolean", "description": "Run Mihomo config test for this review artifact. Defaults to false; config_patch_apply is the normal validation boundary."},
				"core":                 map[string]any{"type": "string", "description": "Mihomo core path for config test. Defaults to the active runtime profile core path."},
				"runtime_dir":          map[string]any{"type": "string", "description": "Mihomo runtime artifact source for isolated config test. Defaults to .runtime/mihomo; live cache.db is not copied."},
				"background":           map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for write tools that may render or test Mihomo config."},
				"wait":                 map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
				"overlay": map[string]any{
					"type":                 "object",
					"description":          "Desired localClash overlay. If a target references a proxy group or policy group that is not already in durable localclash.json, include it in overlay.proxy_groups or overlay.policy_groups in this same call. policy_groups are business-layer entries whose exits point to proxy_groups or terminal targets.",
					"additionalProperties": false,
					"properties": map[string]any{
						"packs":              map[string]any{"type": "array", "items": packIntent},
						"custom_rules":       map[string]any{"type": "array", "items": customRuleIntent},
						"enabled_rule_packs": map[string]any{"type": "array", "items": localRulePackIntent, "description": "Standalone local rule packs backed by rule-packs/*.json and rendered after custom rules but before catalog/template packs."},
						"rule_providers":     map[string]any{"type": "array", "items": ruleProviderIntent, "description": "User-supplied external Mihomo rule-providers rendered as rule-providers plus RULE-SET rules."},
						"proxy_groups":       map[string]any{"type": "array", "items": proxyGroupIntent},
						"policy_groups":      map[string]any{"type": "array", "items": policyGroupIntent},
					},
				},
			},
			"required": []string{"overlay"},
		}
	case "run_runtime":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":            map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"runtime_dir":       map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"core":              map[string]any{"type": "string", "description": "Mihomo core binary path. Defaults to the active runtime profile core path."},
				"foreground":        map[string]any{"type": "boolean", "description": "Foreground mode is not supported by MCP run_runtime; use CLI run for foreground execution."},
				"log_file":          map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
				"force_config_test": map[string]any{"type": "boolean", "description": "Bypass the Mihomo config validation cache and run a fresh mihomo -t before starting."},
				"background":        map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for MCP execution tools."},
				"wait":              map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "restart_runtime":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":            map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"runtime_dir":       map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"core":              map[string]any{"type": "string", "description": "Mihomo core binary path. Defaults to the active runtime profile core path."},
				"log_file":          map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
				"timeout_ms":        map[string]any{"type": "integer", "minimum": 0, "description": "Milliseconds to wait after SIGTERM before reporting timeout. Defaults to 5000."},
				"force":             map[string]any{"type": "boolean", "description": "Send SIGKILL if the runtime does not exit before timeout. Defaults to false."},
				"force_config_test": map[string]any{"type": "boolean", "description": "Bypass the Mihomo config validation cache and run a fresh mihomo -t before restarting."},
				"background":        map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for MCP execution tools."},
				"wait":              map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "runtime_profile_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config": map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.json."},
			},
		}
	case "runtime_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":      map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"runtime_dir": map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"core":        map[string]any{"type": "string", "description": "Mihomo core binary path. Defaults to the active runtime profile core path."},
				"log_file":    map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
			},
		}
	case "router_takeover_status", "router_takeover_apply", "router_takeover_stop":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"runtime_profile": map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.json."},
				"config":          map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"runtime_dir":     map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"log_file":        map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
				"state_dir":       map[string]any{"type": "string", "description": "localClash router takeover runtime state directory. Defaults to /tmp/localclash/router-takeover so reboot clears it."},
				"dns_port":        map[string]any{"type": "integer", "minimum": 1, "description": "Mihomo DNS listen port. Defaults to the router profile DNS listen port or 7874."},
				"redir_port":      map[string]any{"type": "integer", "minimum": 1, "description": "Mihomo redir-port. Defaults to the router profile redir-port or 7892."},
				"tun_device":      map[string]any{"type": "string", "description": "Mihomo TUN device name. Defaults to the router profile TUN device or utun."},
				"dry_run":         map[string]any{"type": "boolean", "description": "Return the shell script without applying changes. Supported by router_takeover_apply and router_takeover_stop."},
				"background":      map[string]any{"type": "boolean", "description": "Run apply/stop as a background task and immediately return task_id/log_file. Defaults to true for MCP execution tools; ignored by router_takeover_status."},
				"wait":            map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false; ignored by router_takeover_status."},
			},
		}
	case "stop_runtime":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"runtime_profile": map[string]any{"type": "string", "description": "Runtime profile YAML path used to detect router takeover. Defaults to localclash-runtime.json."},
				"config":          map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"core":            map[string]any{"type": "string", "description": "Mihomo core binary path used to detect and stop orphan runtime processes. Defaults to the active runtime profile core path."},
				"runtime_dir":     map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"log_file":        map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
				"state_dir":       map[string]any{"type": "string", "description": "localClash router takeover runtime state directory used for takeover detection. Defaults to /tmp/localclash/router-takeover."},
				"dns_port":        map[string]any{"type": "integer", "minimum": 1, "description": "Mihomo DNS listen port used for takeover detection. Defaults to router profile DNS listen port or 7874."},
				"redir_port":      map[string]any{"type": "integer", "minimum": 1, "description": "Mihomo redir-port used for takeover detection. Defaults to router profile redir-port or 7892."},
				"tun_device":      map[string]any{"type": "string", "description": "Mihomo TUN device used for takeover detection. Defaults to router profile TUN device or utun."},
				"timeout_ms":      map[string]any{"type": "integer", "minimum": 0, "description": "Milliseconds to wait after SIGTERM before reporting timeout. Defaults to 5000."},
				"force":           map[string]any{"type": "boolean", "description": "Bypass the active router takeover guard and send SIGKILL if the runtime does not exit before timeout. Defaults to false."},
				"background":      map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for MCP execution tools."},
				"wait":            map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "subscriptions_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":      map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.json."},
				"merged":      map[string]any{"type": "string", "description": "Effective merged subscription path. Defaults to subscription.gob."},
				"runtime_dir": map[string]any{"type": "string", "description": "Runtime source artifact directory. Defaults to .runtime/subscriptions."},
			},
		}
	case "subscription_nodes_list":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"subscription": map[string]any{"type": "string", "description": "Subscription gob path. Defaults to subscription.gob."},
				"limit":        map[string]any{"type": "integer", "minimum": 0, "description": "Maximum returned proxy summaries. Defaults to 100."},
			},
		}
	case "subscription_nodes_search":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"subscription":   map[string]any{"type": "string", "description": "Subscription gob path. Defaults to subscription.gob."},
				"query":          map[string]any{"type": "string", "description": "Literal substring to match against subscription proxy names."},
				"patterns":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Regular expressions to match against subscription proxy names."},
				"case_sensitive": map[string]any{"type": "boolean", "description": "Use case-sensitive query and pattern matching. Defaults to false."},
				"limit":          map[string]any{"type": "integer", "minimum": 0, "description": "Maximum returned proxy summaries. Defaults to 50."},
			},
		}
	case "subscriptions_configure":
		source := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "HTTP or HTTPS Clash/Mihomo subscription URL. The source id is generated automatically from this URL."},
			},
			"required": []string{"url"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.json."},
				"sources": map[string]any{"type": "array", "items": source, "description": "Complete subscription source list to write."},
				"replace": map[string]any{"type": "boolean", "description": "Replace existing sources. Defaults to true; false is not supported in the first version."},
			},
			"required": []string{"sources"},
		}
	case "subscriptions_refresh":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":            map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.json."},
				"ids":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional source ids to refresh. Defaults to all."},
				"runtime_dir":       map[string]any{"type": "string", "description": "Runtime source artifact directory. Defaults to .runtime/subscriptions."},
				"merged":            map[string]any{"type": "string", "description": "Effective merged subscription path. Defaults to subscription.gob."},
				"force":             map[string]any{"type": "boolean", "description": "Reserved compatibility flag. Existing artifacts are replaced."},
				"user_agent":        map[string]any{"type": "string", "description": "Subscription request User-Agent. Defaults to a Clash/Mihomo-like user agent."},
				"localclash_config": map[string]any{"type": "string", "description": "Durable localClash selector config path. Defaults to localclash.json."},
				"selection":         map[string]any{"type": "string", "description": "Derived pack selection path. Defaults to localclash-packs.gob."},
				"rules_cache":       map[string]any{"type": "string", "description": "Rule pack cache directory used when auto-rendering after selector refresh."},
				"runtime_profile":   map[string]any{"type": "string", "description": "Runtime profile YAML path used when auto-rendering after selector refresh. Defaults to localclash-runtime.json."},
				"output":            map[string]any{"type": "string", "description": "Generated Mihomo config path to update when selector refresh succeeds. Defaults to generated/mihomo.yaml."},
				"background":        map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for write tools that may perform network downloads."},
				"wait":              map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "packs_list":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"source": map[string]any{"type": "string", "description": "Exact pack source, for example sukkaw, blackmatrix7, or v2fly-dlc."},
				"name":   map[string]any{"type": "string", "description": "Case-insensitive substring filter for pack name or exact upstream pack value."},
				"target": map[string]any{"type": "string", "description": "Exact catalog default/recommended target filter, for example DIRECT, REJECT, or AI. This is not an active-config filter."},
				"limit":  map[string]any{"type": "integer", "minimum": 1},
			},
		}
	case "packs_get":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"source":      map[string]any{"type": "string", "description": "Exact pack source from packs_list[].source or tool_args, for example blackmatrix7 or v2fly-dlc."},
				"pack":        map[string]any{"type": "string", "description": "Exact upstream pack name from packs_list[].pack or tool_args, for example OpenAI or geolocation-!cn."},
				"cache":       map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"runtime_dir": map[string]any{"type": "string", "description": "Mihomo runtime data directory used to resolve provider paths. Defaults to .runtime/mihomo."},
			},
			"required": []string{"source", "pack"},
		}
	case "pack_rules_read":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"source":         map[string]any{"type": "string", "description": "Exact pack source from packs_list[].source or tool_args, for example sukkaw, blackmatrix7, or v2fly-dlc."},
				"pack":           map[string]any{"type": "string", "description": "Exact upstream pack name from packs_list[].pack or tool_args, for example ai, OpenAI, or geolocation-!cn."},
				"component":      map[string]any{"type": "string", "description": "Optional component id such as domainset, non_ip, ip, or mixed provider id."},
				"limit":          map[string]any{"type": "integer", "minimum": 0, "description": "Maximum sample rules per component. Defaults to 120. Use 0 to omit samples."},
				"refresh":        map[string]any{"type": "boolean", "description": "Force refetching provider rules instead of using provider-cache."},
				"cache":          map[string]any{"type": "string", "description": "Pack catalog cache directory. Defaults to .runtime/rules/packs."},
				"sources":        map[string]any{"type": "string", "description": "Rule sources directory used if pack catalog must be ensured. Defaults to rule-sources."},
				"provider_cache": map[string]any{"type": "string", "description": "Provider rules cache directory. Defaults to .runtime/rules/provider-cache."},
			},
			"required": []string{"source", "pack"},
		}
	case "pack_rules_prefetch":
		packSelector := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"source": map[string]any{"type": "string", "description": "Exact pack source from packs_list[].source or tool_args."},
				"pack":   map[string]any{"type": "string", "description": "Exact upstream pack name from packs_list[].pack or tool_args."},
			},
			"required": []string{"source", "pack"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"packs":          map[string]any{"type": "array", "items": packSelector, "description": "Explicit source/pack pairs to prefetch."},
				"source":         map[string]any{"type": "string", "description": "Exact pack source filter, for example sukkaw or blackmatrix7."},
				"name":           map[string]any{"type": "string", "description": "Case-insensitive substring filter for pack name or exact upstream pack value."},
				"target":         map[string]any{"type": "string", "description": "Exact target filter."},
				"limit":          map[string]any{"type": "integer", "minimum": 1, "description": "Maximum packs selected by filters. Defaults to 20."},
				"refresh":        map[string]any{"type": "boolean", "description": "Force refetching provider rules instead of using provider-cache."},
				"cache":          map[string]any{"type": "string", "description": "Pack catalog cache directory. Defaults to .runtime/rules/packs."},
				"sources":        map[string]any{"type": "string", "description": "Rule sources directory used if pack catalog must be ensured. Defaults to rule-sources."},
				"provider_cache": map[string]any{"type": "string", "description": "Provider rules cache directory. Defaults to .runtime/rules/provider-cache."},
			},
		}
	case "pack_rules_query":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"query":          map[string]any{"type": "string", "description": "Domain or keyword to search in locally cached provider rules, for example huggingface.co."},
				"source":         map[string]any{"type": "string", "description": "Optional exact pack source filter."},
				"name":           map[string]any{"type": "string", "description": "Optional case-insensitive pack name or exact upstream pack filter."},
				"target":         map[string]any{"type": "string", "description": "Optional exact target filter."},
				"limit":          map[string]any{"type": "integer", "minimum": 1, "description": "Maximum returned matches. Defaults to 20."},
				"cache":          map[string]any{"type": "string", "description": "Pack catalog cache directory. Defaults to .runtime/rules/packs."},
				"sources":        map[string]any{"type": "string", "description": "Rule sources directory used if pack catalog must be ensured. Defaults to rule-sources."},
				"provider_cache": map[string]any{"type": "string", "description": "Provider rules cache directory. Defaults to .runtime/rules/provider-cache."},
			},
			"required": []string{"query"},
		}
	default:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		}
	}
}

func ruleProviderInputSchema(idDescription string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"id":       map[string]any{"type": "string", "description": idDescription},
			"target":   map[string]any{"type": "string", "description": "Rule target such as DIRECT, REJECT, ⚡ 自动选择, a proxy group id, or a policy group id."},
			"reason":   map[string]any{"type": "string", "description": "Short durable reason for this external provider."},
			"type":     map[string]any{"type": "string", "enum": []string{"http", "file"}, "description": "Mihomo rule-provider type. Defaults to http."},
			"behavior": map[string]any{"type": "string", "enum": []string{"classical", "domain", "ipcidr"}, "description": "Mihomo rule-provider behavior. Defaults to classical."},
			"format":   map[string]any{"type": "string", "enum": []string{"yaml", "text", "mrs"}, "description": "Mihomo rule-provider format. Defaults to yaml."},
			"path":     map[string]any{"type": "string", "description": "Local rule-provider cache path. Defaults to ./rule_provider/<id>.yaml."},
			"url":      map[string]any{"type": "string", "description": "Remote provider URL. Required for http providers."},
			"interval": map[string]any{"type": "integer", "minimum": 0, "description": "Refresh interval in seconds. Defaults to 86400 for http providers."},
		},
		"required": []string{"id", "target"},
	}
}

func policyGroupInputSchema(idDescription string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"id":       map[string]any{"type": "string", "description": idDescription},
			"mode":     map[string]any{"type": "string", "enum": []string{"manual", "auto", "smart"}, "description": "Desired policy-group mode. manual becomes a Dashboard select group over exits; auto becomes url-test over exits; smart becomes a smart group over exits."},
			"exits":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Exit targets for this business layer, for example HK, JP, US, ⚡ 自动选择, or DIRECT. Non-terminal exits must be provided by overlay.proxy_groups or existing durable proxy_groups."},
			"reason":   map[string]any{"type": "string", "description": "Short durable reason for this business routing group."},
			"boundary": map[string]any{"type": "string", "description": "Boundary note, for example business_layer_selects_exit_groups."},
		},
		"required": []string{"id", "mode", "exits"},
	}
}
