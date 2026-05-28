# Rule Model

This document defines the development contract for localClash routing rules.
It keeps the renderer explicit: local safety behavior is built in, while product
defaults are visible policy-template patches.

## Product Position

localClash should not be another hand-edited Clash YAML manager. It should
compile localClash-owned policy data into a Mihomo runtime config with:

- deterministic rendering
- readable diffs
- validation before run
- local-only storage
- no cloud dependency
- no sensitive config collection

The web UI must edit localClash policy data, not `generated/mihomo.yaml`
directly.

## Layers

Rules are layered in this order:

```text
1. local safety baseline
2. user explicit overrides
3. optional rule packs
4. policy template patches
5. fallback
```

Mihomo evaluates rules from top to bottom, so higher-priority layers must be
rendered earlier.

## Layer Responsibilities

### 1. Local Safety Baseline

The local safety baseline is built into localClash and cannot be disabled.

It protects local, LAN, mDNS, loopback, link-local, private address, and local
resolver behavior. It must keep local network operation stable even when an
upstream preset, subscription, or optional pack is wrong.

Current examples include:

- `localhost`
- `.local`
- `.lan`
- `.home.arpa`
- loopback ranges
- private IPv4 ranges
- link-local ranges
- local IPv6 ranges
- system DNS policy for local names

This layer is not a place for product categories such as AI, media, games,
developer tools, ads, or company domains.

### 2. User Explicit Overrides

User explicit overrides are direct user decisions. They should have the
highest user-controlled precedence, below only the safety baseline.

Examples:

```json
{
  "overrides": {
    "direct": {
      "domains": ["nas.home.arpa", "printer.lan"]
    },
    "proxy": {
      "domains": ["example-work-service.com"]
    }
  }
}
```

Overrides are for small, concrete fixes. They should not become a hidden
category system.

### 3. Optional Rule Packs

Optional rule packs are the primary web UI customization layer. Users should
be able to enable or disable packs from the UI and choose the target behavior
when a pack supports more than one target. Targets are exact Mihomo/localClash
targets such as `DIRECT`, `REJECT`, or a configured proxy/policy group name;
there are no Go-side aliases such as `proxy` or `smart`.

Examples:

```text
[ ] AI services
[ ] Streaming media
[ ] Ads and tracking
[ ] Developer services
[ ] Games
[ ] Mainland services
```

Rule packs should be declarative files owned by localClash. They can include
domain rules, domain suffix rules, IP CIDR rules, or references to rule
providers when that becomes necessary.

First version schema:

```json
{
  "id": "ai",
  "name": "AI Services",
  "description": "Route common AI services through a selected target.",
  "version": 1,
  "default_target": "⚡ 自动选择",
  "target_options": ["⚡ 自动选择", "🎯 手动选择", "DIRECT"],
  "rules": [
    {"domain_suffix": "openai.com"},
    {"domain_suffix": "chatgpt.com"},
    {"domain_suffix": "anthropic.com"}
  ]
}
```

Local user selection should live in localClash config, for example:

```json
{
  "policy_template": "minimal",
  "enabled_rule_packs": [
    {"id": "ai", "target": "proxy"},
    {"id": "ads", "target": "reject"}
  ]
}
```

The UI should save this localClash config and trigger a render. It should not
patch the generated Mihomo runtime config.

### 4. Policy Template Patches

Policy template patches define product defaults that are broader than a single
optional pack but still must remain visible localClash-owned data. They are not
Go-side aliases and are not hidden renderer defaults.

Built-in templates:

```text
minimal = load only policy-templates/minimal.json
localclash-default = load every patch listed by policy-templates/localclash-default.json
```

Template patches may define:

- Dashboard-facing proxy groups
- business-layer policy groups
- built-in pack selections
- custom rules
- external rule-provider declarations

The web UI should present policy templates separately from optional rule packs.
Changing the selected template changes the durable `localclash.json` intent; it
does not mutate `generated/mihomo.yaml` directly.

### 5. Fallback

Fallback is the final `MATCH` rule emitted by the renderer.

Examples:

- minimal routing: unmatched traffic goes `DIRECT`
- default template routing: unmatched traffic follows explicit template rules
  before the final `DIRECT` fallback

Optional packs and overrides must be rendered before fallback.

Targets are graph references, not Go-side aliases. The only terminal runtime
actions are `DIRECT` and `REJECT`. Names such as `⚡ 自动选择`, `🎯 手动选择`,
`DNSProxy`, and regional exits must be defined by the policy template or patch
before any rule, rule-provider, pack, policy group, or DNS `#group` reference can
use them.

## Renderer Contract

The renderer should compile inputs into `generated/mihomo.yaml` in this order:

```text
subscription proxies
+ local runtime settings
+ local safety baseline
+ user explicit overrides
+ enabled rule packs
+ selected policy template patches
+ fallback
```

The renderer owns:

- proxy groups
- optional policy groups that expose business-layer choices before selecting
  proxy-group exits
- rule provider definitions
- rule order
- local DNS safety policy
- generated runtime output

The subscription is only a proxy source. It must not be treated as the owner of
runtime rules.

## Current Implementation State

Current code has:

- built-in local safety baseline in `internal/configrender/render.go`
- generated Mihomo config under `generated/`
- doctor checks for baseline injection, rule targets, provider references, and
  `mihomo -t`
- a localClash user config file model
- a `policy_template` field for durable config intent
- a `config_configure` MCP tool for base product configuration: core,
  runtime profile, and policy template
- disk-backed `minimal` and `localclash-default` policy templates under
  `policy-templates/`; `localclash-default` is a patch-set manifest whose
  ordered files under `policy-templates/localclash-default.d/` are merged during
  initialization into the same durable `localclash.json` intent model that MCP
  patches use
- default patch files for region exits, direct baselines,
  communication/social/Telegram routing (including Telegram IP CIDR ranges),
  AI/developer routing, Steam,
  media/platform routing, games, and tail fallback routing
- standalone local `rule-packs/*.json` files enabled through durable
  `enabled_rule_packs`
- renderer support for selected third-party packs
- renderer support for enabled local rule packs, emitted after inline
  `custom_rules` and before catalog/template packs
- renderer support for inline `custom_rules`
- renderer support for user-supplied external `rule_providers`
- MCP patch tools for proxy groups, custom rules, external rule-providers,
  reviewed config apply, and atomic generated config rendering

Current code still does not yet have:

- UI support for policy template and rule pack selection
- doctor checks for custom rule or external provider schema and target
  references

## MCP Routing Discovery

`config_status` exposes the factual source of truth for default routing:

- `intent.proxy_groups` lists reusable exit groups such as region selectors and
  direct exits
- `intent.policy_groups` lists business-layer Dashboard groups and their exits
- `intent.enabled_rule_packs` lists local rule packs selected from
  `rule-packs/*.json`
- `intent.packs` lists active generated/catalog rule packs and their targets
- `overlay.rules` shows rendered localClash-managed rule targets

That is enough for a careful Agent to discover that `localclash-default` is a
business -> exit -> node model created by default patches, for example
`default.steam.v1` contributing `source: v2fly-dlc` / `pack: steam` targeting
`🎮 Steam`, whose exits include direct, manual, automatic, and regional groups.

Agents should not infer active default rules from
`generated_summary.rules_sample` alone because that sample is intentionally
truncated and often dominated by the local safety baseline.

Use the read-only MCP `routing_explain` tool for compact routing discovery.
It reads durable `localclash.json` intent and returns matching packs, policy
groups, reusable exit groups, optional cached provider-rule evidence, and the
safe reviewed patch path. Example queries:

- `routing_explain(query: "Steam")`: explains the active Steam pack, the
  Dashboard-facing Steam policy group, and its exits.
- `routing_explain(query: "ChatGPT through Singapore")`: surfaces matching
  business groups and reusable Singapore exits so an Agent can build a reviewed
  policy-group patch.
- `routing_explain(query: "openai.com")`: can include cached provider-rule
  matches when provider-cache coverage exists; if cache is incomplete, the tool
  still reports durable intent and says which prefetch/read path to use.

`routing_explain` is not a mutation tool. For changes, follow its
`patch_guidance`: `config_status` -> optional `proxy_group_build` /
`policy_group_build` -> `config_patch_create` -> review -> `config_patch_apply`
-> verification.

## Development Sequence

Build this in small steps:

1. Extend MCP patch tools until agents can express common routing intent without
   editing YAML directly.
2. Add read-only MCP routing discovery tools so Agents can inspect default
   business groups without parsing the full `config_status` payload.
3. Add doctor checks for pack parsing, custom rule validity, target validity,
   and missing providers.
4. Add CLI flags for config path and dry-run diff.
5. Expose the same model through the local web UI.

Do not start by adding many pack contents. First make the mechanism correct.

## Acceptance Criteria

A correct implementation must satisfy:

- local safety baseline is always rendered first and cannot be disabled
- user overrides render before optional packs
- optional packs render before template-managed fallback behavior
- product defaults live in explicit policy-template patches, not hidden Go code
- generated config is reproducible from localClash-owned inputs
- UI changes are stored in localClash config, not in generated Mihomo YAML
- doctor can explain missing files, invalid rules, missing targets, missing
  providers, and failed `mihomo -t`
- sensitive local files remain ignored by git
