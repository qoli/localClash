---
name: localclash-mcp-route-operator
description: Inspect localClash routing intent and live traffic, or apply authorized MCP routing changes, including durable 自訂代理網站 / 自訂直連網站 overrides. Use when localClash is relevant to website, service, or exit routing.
---

# localClash MCP Route Operator

## Choose the Product Path

Treat localClash MCP as a product API. Prefer registered `mcp__localclash.*`
tools; discover them when the namespace is not visible before using raw HTTP,
files, or SSH. Server state owns config/runtime paths and secrets.

- For observation, choose the evidence surface below. A domain query does not
  authorize a custom rule or any persistent change.
- For an explicit simple persistent website proxy/direct override through the
  existing exits, use `custom_sites_list` / `custom_sites_transact`. Read
  [Custom sites](references/custom-sites.md) for their API, matching, priority,
  and transaction results. This path needs neither pack lookup nor a user patch.
- For app/company-wide pack coverage, a named node/country, or a selection
  strategy such as Smart or fallback, use packs and a service-owned exit. Read
  [Routing and patches](references/routing-patches.md) for selection, ownership,
  patch apply, and removal.

Read the reference for the chosen path; observation-only questions usually
need neither. Do not run a full environment/config/runtime sequence by default.

## Match the Evidence to the Claim

- **Catalog/cache:** `packs_list`, `packs_get`, and `pack_rules_query` show
  reusable coverage, not whether a pack is enabled.
- **Durable intent:** `custom_sites_list` shows the independent custom-site
  layer. `config_status`, `config_patch_get`, and `routing_explain` show patch
  and compiled intent. None proves loaded runtime or active traffic.
- **Loaded semantics:** use `mihomo_api_request` read paths `/rules`,
  `/providers/rules`, `/proxies`, or `/configs` for the relevant rule order,
  providers, groups/current selection, or runtime configuration.
  `runtime_status` proves process health only.
- **Active traffic:** `mihomo_connections_read` can prove a tracked
  connection's source, destination/host, matched rule, selected proxy, and
  chain. Absence cannot prove how a future connection will route.
- **Fresh behavior:** use bounded `mihomo_logs_read`, a connection stream,
  or fresh traffic when snapshots are stale. Align the window with the user's
  action and identify the source device.

Do not attribute an app endpoint from timing, a familiar port, ASN, or one
coincident connection alone. Use a user-confirmed endpoint or repeatable
before/after evidence; report candidates and uncertainty when attribution
remains unresolved.

For another app's network problem, state the route hypothesis and correlate
only the needed localClash evidence with that app's logs/action. No connection
may mean failure before networking. Once routing is excluded, continue at the
owning layer; observation is not authority to change DNS, routes, or firewall.

## Authority and Ownership

Carry clear existing authorization through the requested save, live apply,
and read-back. Ask only when authority is missing or the next action materially
expands scope, such as an unrequested live operation or a shared/default group
mutation. A `confirm_required` tool needs user authorization, not invented
confirmation JSON fields.

Simple custom-site overrides use the reserved 自訂代理網站 group's current
selected exit or 自訂直連網站 → `DIRECT`. They do not authorize changing
those groups' selection/membership or recreating them with patch builders.
Other service-specific routes reuse a service-owned group or create a
dedicated one.

Shared/default exits may be referenced freely. Changing one requires informed
approval for that named group and change; approval to save/reload alone does
not cover it. Do not silently substitute a shared group if dedicated-group
construction fails.

Use the product source of truth; do not edit generated runtime YAML,
`generated/`, `subscription*.gob`, or provider caches as routing intent.
Runtime start, stop, process restart, firewall work, and router takeover need
authorization for that action. OpenWrt interception/takeover belongs to the
LuCI manager; Core's `runtime_facts` describes runtime network facts, not
firewall or DNS-hijack ownership. Core must not run `fw4`, `nft`, `uci`,
or `ip rule`.

## Completion and Uncertain Writes

After an uncertain write or hot-reload timeout, inspect returned task
artifacts with `nl_file`, durable state, and relevant loaded semantics before
retrying. A timeout is indeterminate. Do not repeat a successful config test,
apply, or reload when the requested evidence already establishes completion.

Report the rule source and target, owning patch or custom-site entry
ID/sequence, persistence result, loaded proof or pending next start, rollback
evidence when relevant, unchanged protected groups, and any approved shared
mutation. Claim actual traffic routing only when connection evidence proves
it; state remaining uncertainty.
