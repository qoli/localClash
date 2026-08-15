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
	Binary        string `json:"binary,omitempty"`
	BinarySHA256  string `json:"binary_sha256,omitempty"`
	BinaryError   string `json:"binary_error,omitempty"`
	WorkingDir    string `json:"working_dir,omitempty"`
	WorkspaceRoot string `json:"workspace_root,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
}

type ToolsListResult struct {
	Server ServerRuntimeInfo `json:"server"`
	Tools  []ToolSummary     `json:"tools"`
	Count  int               `json:"count"`
}

func Registry() []Tool {
	tools := []Tool{
		{Name: "config_configure", SafetyLevel: SafeWrite, Description: "Configure localClash base product state with optional core, runtime_profile, and policy_template. Policy templates are imported into patches/*.json and compiled into localclash-intent.json; reset_patches=true restores default template patches."},
		{Name: "config_status", SafetyLevel: SafeRead, Description: "Inspect localClash config status: durable patches/*.json registry, compiled localclash-intent.json, .runtime/mihomo/config.yaml build artifact, and render readiness. Default output is lightweight; pass patches=true for patch inventory, resolve=true for selected-node matches, or detail=true for generated-summary/overlay audit."},
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
		{Name: "runtime_status", SafetyLevel: SafeRead, Description: "Inspect localClash-owned Mihomo runtime processes by managed core process name without changing runtime state."},
		{Name: "mihomo_connections_read", SafetyLevel: SafeRead, Description: "Read bounded Mihomo active connection snapshots from the controller over HTTP or WebSocket without exposing the controller token. This is runtime data-plane evidence for currently tracked active connections only: it can prove a live connection's matched rule and selected proxy chain, but absence of a domain is not proof of how a future connection would route. Use routing_explain for intended routing and mihomo_logs_read or fresh traffic when runtime evidence is needed."},
		{Name: "mihomo_logs_read", SafetyLevel: SafeRead, Description: "Read a bounded batch of Mihomo controller logs over WebSocket or HTTP streaming without exposing the controller token."},
		{Name: "router_takeover_status", SafetyLevel: SafeRead, Description: "Inspect localClash-owned OpenWrt router takeover runtime state: runtime profile, Mihomo runtime, fw4/nft chains, DNS hijack, fwmark route, and TUN device."},
		{Name: "routing_explain", SafetyLevel: SafeRead, Description: "Explain localClash compiled routing intent for a service, domain, pack, policy group, or exit query. Reads localclash-intent.json, patch provenance, active packs, policy groups, proxy groups, custom rules, and cached rule matches; this is config/intent evidence, not proof that Mihomo has loaded the config or that current traffic is using it. Verify loaded runtime with mihomo_api_request (/rules, /providers/rules, /proxies) and active traffic with mihomo_connections_read."},
		{Name: "tools_list", SafetyLevel: SafeRead, Description: "List localClash MCP tools as ordinary tool output for clients that do not expose MCP registry introspection to the model."},
		{Name: "config_patch_apply", SafetyLevel: SafeWrite, Description: "Apply reviewed patch-registry operations from the current config_patch_draft or explicit operations, then compile localclash-intent.json, derive localclash-packs.gob, and regenerate .runtime/mihomo/config.yaml without starting runtime."},
		{Name: "config_patch_draft", SafetyLevel: SafeWrite, Description: "Preview patch-registry operations in one in-memory current draft slot. Supports upsert_patch, remove_patch, set_patch_status, and reorder_patch; writes no files until config_patch_apply."},
		{Name: "config_patch_get", SafetyLevel: SafeRead, Description: "Read one durable patches/*.json patch by patch_id with full overlay, sha256, provides, and registry_hash for safe modification."},
		{Name: "config_render", SafetyLevel: SafeWrite, Description: "Compile patches/*.json into localclash-intent.json when a patch registry exists, then render .runtime/mihomo/config.yaml from the compiled intent, subscription, policy template graph, and runtime profile. Does not start runtime."},
		{Name: "custom_rules_build", SafetyLevel: SafeWrite, Description: "Build and validate user custom routing rules for domains, CIDRs, or GEOIP tags before adding them to a config patch."},
		{Name: "pack_rules_prefetch", SafetyLevel: SafeWrite, Description: "Download provider rules for selected packs into local provider-cache so pack_rules_query can search them locally."},
		{Name: "pack_rules_read", SafetyLevel: SafeWrite, Description: "Read rules for one exact source/pack pair, downloading missing provider-cache entries for that pack only, and return source/pack/type/render_strategy/component metadata."},
		{Name: "policy_group_build", SafetyLevel: SafeWrite, Description: "Build and validate a business-layer policy group that routes one rule domain, app, or scenario to existing exits such as HK, JP, US, ⚡ 自动选择, or DIRECT. This does not persist state; copy the returned policy_group into config_patch_draft.operations[].overlay.policy_groups."},
		{Name: "proxy_group_build", SafetyLevel: SafeWrite, Description: "Build and validate a reusable proxy group target from subscription node selectors or exact nodes. This does not persist state; copy the returned proxy_group into config_patch_draft.operations[].overlay.proxy_groups when a patch should use it."},
		{Name: "rule_provider_build", SafetyLevel: SafeWrite, Description: "Build and validate a reusable external rule-provider intent for user-supplied Mihomo rule-provider URLs before adding it to config_patch_draft.operations[].overlay.rule_providers."},
		{Name: "mihomo_api_request", SafetyLevel: SafeWrite, Description: "Call a bounded Mihomo controller API path using the active local runtime endpoint and secret; rejects full URLs and is not a generic HTTP client. Prefer read paths such as /version, /configs, /rules, /providers/rules, and /proxies for loaded-runtime evidence. /connections/ exists, but prefer mihomo_connections_read for active connection summaries."},
		{Name: "mihomo_config_test", SafetyLevel: SafeWrite, Description: "Run explicit mihomo -t validation for the server state's generated config and record a passing config hash attestation for hot reload. MCP callers do not provide config/runtime/core paths."},
		{Name: "subscriptions_configure", SafetyLevel: SafeWrite, Description: "Write local subscription source configuration without refreshing."},
		{Name: "subscriptions_refresh", SafetyLevel: SafeWrite, Description: "Refresh configured subscriptions, rebuild service capabilities, render and test a candidate Mihomo config, then promote it without changing the running core."},
		{Name: "run_runtime", SafetyLevel: ConfirmRequired, Description: "Start the Mihomo runtime from .runtime/mihomo/config.yaml, assuming the config has already been validated by config_patch_apply, mihomo_config_test, or doctor. Requires external Agent/MCP client confirmation; starting the proxy runtime may temporarily interrupt network connectivity, and the Agent itself may be disconnected if it depends on the current network/proxy path."},
		{Name: "restart_runtime", SafetyLevel: ConfirmRequired, Description: "Reload the running Mihomo runtime. MCP defaults to hot reload, which verifies the current config hash against a prior mihomo_config_test attestation before calling Mihomo PUT /configs. Mihomo reload is synchronous and may exceed the request timeout; a timeout is indeterminate, not proof of failure. Agents should use mihomo_api_request for change-specific runtime verification. Use strategy=process_restart for an explicit stop/start process restart."},
		{Name: "router_takeover_apply", SafetyLevel: ConfirmRequired, Description: "Apply localClash-owned OpenWrt router takeover runtime rules for router profile mode. Uses localClash router redir-host-mix behavior: TCP redir-host, DNS hijack, fwmark route, and TUN forwarding. Does not persist firewall config; call only after run_runtime or restart_runtime and user confirmation."},
		{Name: "router_takeover_stop", SafetyLevel: ConfirmRequired, Description: "Remove localClash-owned OpenWrt router takeover runtime rules without stopping Mihomo. This changes firewall, DNS, and policy-routing runtime state and requires user confirmation."},
		{Name: "sed_file", SafetyLevel: SafeWrite, Description: "Apply sed-style repository-local text edits with dry-run diff output. Defaults to dry_run=true."},
		{Name: "stop_runtime", SafetyLevel: ConfirmRequired, Description: "Stop localClash-owned Mihomo runtime processes identified by managed core process names. Refuses by default when router takeover is effective because router traffic still depends on Mihomo; call router_takeover_stop first or pass force=true only after explicit user confirmation."},
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
				"core":            map[string]any{"type": "string", "enum": []string{"meta", "smart"}, "description": "Optional Mihomo core flavor to activate."},
				"runtime_profile": map[string]any{"type": "string", "enum": []string{"normal", "router"}, "description": "Optional runtime profile mode to activate."},
				"policy_template": map[string]any{"type": "string", "description": "Optional policy template id loaded from policy-templates/. Built-ins include minimal and localclash-default."},
				"reset_patches":   map[string]any{"type": "boolean", "description": "Replace patches/*.json with policy-template defaults before compiling. Use to restore default strategies."},
			},
		}
	case "config_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"patches": map[string]any{"type": "boolean", "description": "Include compact patch inventory, registry_hash, and artifact paths."},
				"detail":  map[string]any{"type": "boolean", "description": "Include generated-summary and overlay audit details."},
				"resolve": map[string]any{"type": "boolean", "description": "Resolve selected subscription nodes. Defaults to detail."},
				"limit":   map[string]any{"type": "integer", "minimum": 1, "description": "Maximum summary entries per section. Defaults to 20."},
			},
		}
	case "config_render":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"force":      map[string]any{"type": "boolean", "description": "Overwrite generated output. Defaults to true because .runtime/mihomo/config.yaml is a build artifact."},
				"background": map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for write tools that may render or test Mihomo config."},
				"wait":       map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "routing_explain":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"query"},
			"properties": map[string]any{
				"query":                map[string]any{"type": "string", "description": "Service, app, domain, exact pack name/source, policy group, or exit to explain, for example Steam, ChatGPT, openai.com, or Singapore."},
				"include_rule_matches": map[string]any{"type": "boolean", "description": "Search cached rule/provider contents for the query. Defaults to true; does not download missing provider rules."},
				"limit":                map[string]any{"type": "integer", "minimum": 1, "description": "Maximum matches per section. Defaults to 20."},
			},
		}
	case "config_patch_get":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"patch_id"},
			"properties": map[string]any{
				"patch_id": map[string]any{"type": "string", "description": "Durable patch registry id from config_status(patches=true)."},
			},
		}
	case "config_patch_draft":
		operation := patchOperationSchema()
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"operations"},
			"properties": map[string]any{
				"draft_name": map[string]any{"type": "string", "description": "Optional display name for the single in-memory current draft. Not persisted and not used as an id."},
				"operations": map[string]any{"type": "array", "minItems": 1, "items": operation, "description": "Reviewed patch-registry operations. A new config_patch_draft call replaces the current in-memory draft slot."},
				"test":       map[string]any{"type": "boolean", "description": "Resolve and render the draft for validation. Defaults to false for lightweight preview."},
				"background": map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for write tools that may render or test Mihomo config."},
				"wait":       map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "config_patch_apply":
		operation := patchOperationSchema()
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"use_current_draft":  map[string]any{"type": "boolean", "description": "Apply the current in-memory draft created by config_patch_draft. Requires generation."},
				"generation":         map[string]any{"type": "integer", "minimum": 1, "description": "Reviewed in-memory draft generation returned by config_patch_draft."},
				"operations":         map[string]any{"type": "array", "minItems": 1, "items": operation, "description": "Explicit patch-registry operations to apply instead of use_current_draft."},
				"base_hashes":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Optimistic patch-content hashes returned by config_patch_draft."},
				"base_registry_hash": map[string]any{"type": "string", "description": "Registry hash returned by config_patch_draft; required for explicit operations."},
				"test":               map[string]any{"type": "boolean", "description": "Run Mihomo config test before committing. Defaults to true; test=false skips only the external core validation."},
				"background":         map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for write tools that may render or test Mihomo config."},
				"wait":               map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
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
		smartPriorityRule := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"label":   map[string]any{"type": "string", "description": "Stable human-readable region label; rule order is first-match-wins."},
				"pattern": map[string]any{"type": "string", "description": "RE2-compatible proxy-name pattern. Semicolons are not supported by Mihomo policy-priority."},
				"factor":  map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "Positive multiplier applied to Smart's learned weight."},
			},
			"required": []string{"label", "pattern", "factor"},
		}
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
				"id":             map[string]any{"type": "string", "description": "Reusable proxy group id, for example TempLine or SteamHK."},
				"match":          matchIntent,
				"nodes":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Exact subscription proxy names for a user-specified line. Use either match or nodes, not both."},
				"mode":           map[string]any{"type": "string", "enum": []string{"manual", "auto", "smart", "direct"}, "description": "Desired proxy-group mode. manual becomes select; auto becomes url-test; smart becomes smart; direct becomes a named DIRECT-only exit."},
				"smart_priority": map[string]any{"type": "array", "items": smartPriorityRule, "description": "Optional ordered Smart-core proxy-name priorities for this group only. Requires auto or smart mode; Meta core ignores them."},
				"reason":         map[string]any{"type": "string", "description": "Short durable reason used if selector repair needs user involvement."},
				"boundary":       map[string]any{"type": "string", "description": "Boundary note, for example name_based_hint_only."},
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
				"type":       map[string]any{"type": "string", "enum": []string{"domain", "domain_suffix", "domain_regex", "ip_cidr", "ip_cidr6", "geoip"}, "description": "Mihomo rule type to generate. Use domain_regex for bounded dynamic CDN host patterns."},
				"value":      map[string]any{"type": "string", "description": "Domain, domain suffix, domain regex, CIDR, or GEOIP tag value."},
				"no_resolve": map[string]any{"type": "boolean", "description": "Append no-resolve for IP CIDR or GEOIP rules when needed."},
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
	case "run_runtime":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"foreground":        map[string]any{"type": "boolean", "description": "Foreground mode is not supported by MCP run_runtime; use CLI run for foreground execution."},
				"force_config_test": map[string]any{"type": "boolean", "description": "Bypass the Mihomo config validation cache and run a fresh mihomo -t before starting."},
				"background":        map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for MCP execution tools."},
				"wait":              map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "mihomo_api_request":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"method":     map[string]any{"type": "string", "enum": []string{"GET", "POST", "PUT", "PATCH", "DELETE"}, "description": "Mihomo API method. Defaults to GET."},
				"path":       map[string]any{"type": "string", "description": "Absolute Mihomo API path such as /version, /configs, /rules, /providers/rules, or /proxies. Full URLs are rejected. Prefer mihomo_connections_read instead of raw /connections/ for active traffic summaries."},
				"query":      map[string]any{"type": "object", "description": "Query parameters to append to the Mihomo API path."},
				"body":       map[string]any{"description": "JSON body for state-changing Mihomo API calls."},
				"timeout_ms": map[string]any{"type": "integer", "minimum": 0, "description": "Request timeout in milliseconds. Defaults to 5000."},
				"max_bytes":  map[string]any{"type": "integer", "minimum": 1, "description": "Maximum response bytes to return. Defaults to 262144."},
			},
			"required": []string{"path"},
		}
	case "mihomo_connections_read":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"mode":            map[string]any{"type": "string", "enum": []string{"snapshot", "stream"}, "description": "Connection read mode. snapshot performs one GET /connections/ request. stream reads bounded WebSocket frames from /connections/. Defaults to snapshot. Both modes observe currently active tracked connections only."},
				"interval_ms":     map[string]any{"type": "integer", "minimum": 1, "description": "WebSocket stream interval in milliseconds. Only used for mode=stream. Defaults to 1000."},
				"duration_ms":     map[string]any{"type": "integer", "minimum": 1, "description": "Maximum request or stream collection duration. Defaults to 3000."},
				"max_frames":      map[string]any{"type": "integer", "minimum": 1, "description": "Maximum WebSocket frames to return for mode=stream. Defaults to 3."},
				"max_connections": map[string]any{"type": "integer", "minimum": 1, "description": "Maximum active connections to include per snapshot. Defaults to 200."},
				"max_bytes":       map[string]any{"type": "integer", "minimum": 1, "description": "Maximum bytes to read from Mihomo. Defaults to 262144."},
				"include_raw":     map[string]any{"type": "boolean", "description": "Include raw Mihomo connection snapshots in returned frames for field-level debugging. Defaults to false because raw snapshots are large."},
			},
		}
	case "mihomo_logs_read":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"level":       map[string]any{"type": "string", "enum": []string{"debug", "info", "warning", "warn", "error", "silent"}, "description": "Mihomo log level. Defaults to info."},
				"format":      map[string]any{"type": "string", "enum": []string{"default", "structured"}, "description": "Mihomo log output format. Defaults to default."},
				"transport":   map[string]any{"type": "string", "enum": []string{"websocket", "http_stream"}, "description": "Log stream transport. Defaults to websocket."},
				"duration_ms": map[string]any{"type": "integer", "minimum": 1, "description": "Maximum collection duration. Defaults to 3000."},
				"max_lines":   map[string]any{"type": "integer", "minimum": 1, "description": "Maximum log lines to return. Defaults to 200."},
				"max_bytes":   map[string]any{"type": "integer", "minimum": 1, "description": "Maximum log bytes to return. Defaults to 131072."},
			},
		}
	case "mihomo_config_test":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"timeout_ms": map[string]any{"type": "integer", "minimum": 0, "description": "Validation timeout in milliseconds. Defaults to 90000."},
				"background": map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for write tools that may run external validation."},
				"wait":       map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "restart_runtime":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"strategy":          map[string]any{"type": "string", "enum": []string{"hot_reload", "process_restart"}, "description": "Restart strategy. Defaults to hot_reload for MCP restart_runtime. Hot reload submits Mihomo PUT /configs and does not perform change-specific semantic verification."},
				"timeout_ms":        map[string]any{"type": "integer", "minimum": 0, "description": "For hot_reload, Mihomo PUT /configs request timeout in milliseconds; timeout means the reload result is indeterminate and the Agent should verify with mihomo_api_request. For process_restart, milliseconds to wait after SIGTERM. Defaults to 5000."},
				"force":             map[string]any{"type": "boolean", "description": "Send SIGKILL if the runtime does not exit before timeout. Defaults to false."},
				"force_config_test": map[string]any{"type": "boolean", "description": "Only valid with strategy=process_restart. Hot reload requires a prior mihomo_config_test attestation instead."},
				"background":        map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for MCP execution tools."},
				"wait":              map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "runtime_profile_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}
	case "runtime_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}
	case "router_takeover_status", "router_takeover_apply", "router_takeover_stop":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"dry_run":    map[string]any{"type": "boolean", "description": "Return the shell script without applying changes. Supported by router_takeover_apply and router_takeover_stop."},
				"background": map[string]any{"type": "boolean", "description": "Run apply/stop as a background task and immediately return task_id/log_file. Defaults to true for MCP execution tools; ignored by router_takeover_status."},
				"wait":       map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false; ignored by router_takeover_status."},
			},
		}
	case "stop_runtime":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"timeout_ms": map[string]any{"type": "integer", "minimum": 0, "description": "Milliseconds to wait after SIGTERM before reporting timeout. Defaults to 5000."},
				"force":      map[string]any{"type": "boolean", "description": "Bypass the active router takeover guard and send SIGKILL if the runtime does not exit before timeout. Defaults to false."},
				"background": map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for MCP execution tools."},
				"wait":       map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
			},
		}
	case "subscriptions_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}
	case "subscription_nodes_list":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"limit": map[string]any{"type": "integer", "minimum": 0, "description": "Maximum returned proxy summaries. Defaults to 100."},
			},
		}
	case "subscription_nodes_search":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
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
				"uri":          map[string]any{"type": "string", "description": "HTTP/HTTPS subscription URI or MVP proxy URI. The source id is generated automatically."},
				"url":          map[string]any{"type": "string", "description": "Legacy alias for uri when the source is an HTTP or HTTPS subscription URL."},
				"display_name": map[string]any{"type": "string", "description": "Optional two-digit visual source label from 01 to 99. Defaults to source order when configuring."},
			},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
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
				"ids":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional source ids to refresh. Defaults to all."},
				"force":      map[string]any{"type": "boolean", "description": "Reserved compatibility flag. Existing artifacts are replaced."},
				"user_agent": map[string]any{"type": "string", "description": "Subscription request User-Agent. Defaults to a Clash/Mihomo-like user agent."},
				"background": map[string]any{"type": "boolean", "description": "Run as a background task and immediately return task_id/log_file. Defaults to true for write tools that may perform network downloads."},
				"wait":       map[string]any{"type": "boolean", "description": "Set true to wait synchronously for completion. Equivalent to background=false."},
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
				"source": map[string]any{"type": "string", "description": "Exact pack source from packs_list[].source or tool_args, for example blackmatrix7 or v2fly-dlc."},
				"pack":   map[string]any{"type": "string", "description": "Exact upstream pack name from packs_list[].pack or tool_args, for example OpenAI or geolocation-!cn."},
			},
			"required": []string{"source", "pack"},
		}
	case "pack_rules_read":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"source":    map[string]any{"type": "string", "description": "Exact pack source from packs_list[].source or tool_args, for example sukkaw, blackmatrix7, or v2fly-dlc."},
				"pack":      map[string]any{"type": "string", "description": "Exact upstream pack name from packs_list[].pack or tool_args, for example ai, OpenAI, or geolocation-!cn."},
				"component": map[string]any{"type": "string", "description": "Optional component id such as domainset, non_ip, ip, or mixed provider id."},
				"limit":     map[string]any{"type": "integer", "minimum": 0, "description": "Maximum sample rules per component. Defaults to 120. Use 0 to omit samples."},
				"refresh":   map[string]any{"type": "boolean", "description": "Force refetching provider rules instead of using provider-cache."},
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
				"packs":   map[string]any{"type": "array", "items": packSelector, "description": "Explicit source/pack pairs to prefetch."},
				"source":  map[string]any{"type": "string", "description": "Exact pack source filter, for example sukkaw or blackmatrix7."},
				"name":    map[string]any{"type": "string", "description": "Case-insensitive substring filter for pack name or exact upstream pack value."},
				"target":  map[string]any{"type": "string", "description": "Exact target filter."},
				"limit":   map[string]any{"type": "integer", "minimum": 1, "description": "Maximum packs selected by filters. Defaults to 20."},
				"refresh": map[string]any{"type": "boolean", "description": "Force refetching provider rules instead of using provider-cache."},
			},
		}
	case "pack_rules_query":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"query":  map[string]any{"type": "string", "description": "Domain or keyword to search in locally cached provider rules, for example huggingface.co."},
				"source": map[string]any{"type": "string", "description": "Optional exact pack source filter."},
				"name":   map[string]any{"type": "string", "description": "Optional case-insensitive pack name or exact upstream pack filter."},
				"target": map[string]any{"type": "string", "description": "Optional exact target filter."},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "description": "Maximum returned matches. Defaults to 20."},
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

func patchOperationSchema() map[string]any {
	overlay := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"description":          "Full replacement overlay for upsert_patch. Omitted overlay fields are empty, not retained from the existing patch.",
		"properties": map[string]any{
			"packs":              map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}},
			"transport_rules":    map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}},
			"custom_rules":       map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}},
			"enabled_rule_packs": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}},
			"rule_providers":     map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}},
			"proxy_groups":       map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}},
			"policy_groups":      map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}},
		},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"op", "patch_id"},
		"properties": map[string]any{
			"op":                map[string]any{"type": "string", "enum": []string{"upsert_patch", "remove_patch", "set_patch_status", "reorder_patch"}},
			"patch_id":          map[string]any{"type": "string", "description": "Durable patch id. Allowed characters: letters, digits, dot, underscore, and hyphen."},
			"base_patch_sha256": map[string]any{"type": "string", "description": "Optional optimistic content hash from config_patch_get when replacing an existing patch."},
			"title":             map[string]any{"type": "string", "description": "Human readable patch title for upsert_patch."},
			"source":            map[string]any{"type": "string", "description": "Patch source. Defaults to user for new patches and preserves existing source for updates."},
			"source_ref":        map[string]any{"type": "string", "description": "Optional provenance reference, normally policy-templates/... for template patches."},
			"status":            map[string]any{"type": "string", "enum": []string{"enabled", "disabled", "tombstoned"}, "description": "Patch status for upsert_patch or set_patch_status."},
			"order_id":          map[string]any{"type": "string", "description": "Fixed-width decimal order id such as 1200.500000. Never send as a JSON number."},
			"summary":           map[string]any{"type": "string", "description": "Short durable summary."},
			"overlay":           overlay,
			"before_patch_id":   map[string]any{"type": "string", "description": "For reorder_patch, allocate an order id before this patch."},
			"after_patch_id":    map[string]any{"type": "string", "description": "For reorder_patch, allocate an order id after this patch."},
		},
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
