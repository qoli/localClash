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

The web UI must edit localClash policy data, not `.runtime/mihomo/config.yaml`
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
- the router's configured LAN domain, for example `.lan` when OpenWrt dnsmasq
  uses `local=/lan/` and `domain=lan`
- `.home.arpa`
- DHCP hostnames learned by the router, such as `Ronnie-PC`
- loopback ranges
- private IPv4 ranges
- link-local ranges
- local IPv6 ranges
- system DNS policy for local names

In router transparent-proxy mode, DNS hijack is part of the local safety surface.
It must preserve OpenWrt dnsmasq behavior for router-local names and DHCP lease
hostnames. The LAN DNS address and LAN domain are environment facts: discover
them from the active router configuration instead of hard-coding a private IP or
domain suffix. A DNS hijack rule that captures client queries to the router's LAN
DNS service and sends them to Mihomo without a local dnsmasq forwarding policy
breaks this contract, because Mihomo does not know the DHCP lease table by
itself.

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

The LuCI-facing custom-site slice specializes this layer with two isolated,
versioned JSON documents for proxy and direct website decisions. Core compiles
them newest-first into reserved `自訂代理網站` and `自訂直連網站` policy groups;
they are not patch-registry files and survive a complete default-policy patch
reset. Its persistence, wildcard, ordering, update, and transaction contract is
defined in [Custom Site Routing](custom-site-routing.md).

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
domain rules, domain suffix rules, IP CIDR rules, GEOIP rules, or references to rule
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
Changing the selected template changes the durable `localclash-intent.json` intent; it
does not mutate `.runtime/mihomo/config.yaml` directly.

### 5. Fallback

Fallback is the final `MATCH` rule emitted by the renderer.

Examples:

- minimal routing: unmatched traffic goes `DIRECT`
- default template routing: unmatched traffic follows explicit template rules
  before the `🌐 全球直连` policy, whose default exit is `DIRECT`

Optional packs and overrides must be rendered before fallback.

Router transparent-proxy mode must default to blacklist-oriented behavior for game
accelerator compatibility. Known domains, CIDRs, GEOIP, GEOSITE, transport rules,
and user overrides may route selected traffic to policy groups, while unknown
traffic follows a Dashboard-visible global policy that defaults to the physical
network. The default template therefore ends with:

```yaml
- MATCH,🌐 全球直连
```

`🌐 全球直连` must keep `DIRECT` as its first/default exit. An explicit Dashboard
selection may switch it to automatic or a regional proxy exit when the user wants
a broader proxy strategy. Explicit rules targeting `DIRECT` remain direct.

Targets are graph references, not Go-side aliases. The only terminal runtime
actions are `DIRECT` and `REJECT`. Names such as `⚡ 自动选择`, `🎯 手动选择`,
`DNSProxy`, and regional exits must be defined by the policy template or patch
before any rule, rule-provider, pack, policy group, or DNS `#group` reference can
use them.

## Renderer Contract

The renderer should compile inputs into `.runtime/mihomo/config.yaml` in this order:

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
  other policy groups, reusable proxy-group exits, or terminal actions;
  policy-group references form an acyclic selector graph and cycles fail
  validation
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
  initialization into the same durable `localclash-intent.json` intent model that MCP
  patches use
- default patch files for region exits, direct baselines,
  QUIC UDP/443 control, communication/social/Telegram routing (including Telegram GEOIP coverage),
  AI/developer routing, Steam,
  media/platform routing, games, and tail fallback routing
- standalone local `rule-packs/*.json` files enabled through durable
  `enabled_rule_packs`
- renderer support for selected third-party packs
- renderer support for enabled local rule packs, emitted after inline
  `custom_rules` and before catalog/template packs
- renderer and resolver support for high-priority `transport_rules`
- renderer support for inline `custom_rules`
- renderer support for user-supplied external `rule_providers`
- resolver checks for transport-rule, custom-rule, external-provider, pack, and
  local rule-pack targets before rendering
- MCP patch tools for proxy groups, policy groups, transport rules, custom
  rules, external rule-providers, reviewed config apply, and atomic generated
  config rendering
- Core and LuCI support for the custom-site routing contract in
  `docs/custom-site-routing.md`, including separate durable JSON documents,
  newest-first priority, reserved policy groups, candidate config validation,
  and active-runtime reload/read-back

Current code still does not yet have:

- UI support for policy template and rule pack selection
- a dedicated doctor pre-render audit for durable patch schema. Current doctor
  checks generated rule targets and rule-provider references after rendering,
  while config resolve/render paths reject invalid durable target references.

## MCP Routing Discovery

`config_status(patches=true)` exposes the patch registry and compiled intent
used for default routing:

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
It reads compiled `localclash-intent.json` intent, generated patch provenance,
and returns matching packs, policy groups, reusable exit groups, optional cached
provider-rule evidence, and the safe reviewed patch-registry path. Example
queries:

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
`policy_group_build` -> `config_patch_draft` -> review ->
`config_patch_apply` -> verification.

## Development Sequence

Build this in small steps:

1. Keep MCP patch tools as the primary write path for common routing intent;
   agents should not edit generated YAML directly.
2. Keep read-only MCP routing discovery (`routing_explain`, `packs_list`,
   `pack_rules_*`) aligned with the patch-registry model so Agents can inspect
   default business groups without parsing full generated YAML.
3. Improve doctor/status coverage for durable patch registry problems that are
   currently caught during resolve/render.
4. Expose the same model through LuCI or another UI only after the patch
   registry remains the source of truth.

Do not start by adding many pack contents. First make the mechanism correct.

## Acceptance Criteria

A correct implementation must satisfy:

- local safety baseline is always rendered first and cannot be disabled
- user overrides render before optional packs
- optional packs render before template-managed fallback behavior
- product defaults live in explicit policy-template patches, not hidden Go code
- generated config is reproducible from localClash-owned inputs
- UI changes are stored in localClash config, not in generated Mihomo YAML
- resolve/render fails fast on invalid durable rule targets, missing providers,
  unsupported custom rule types, and failed `mihomo -t`; doctor explains
  missing files and generated rule/provider target problems
- sensitive local files remain ignored by git
