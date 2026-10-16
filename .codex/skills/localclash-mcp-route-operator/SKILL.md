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
  [Custom sites](#custom-sites) for matching, priority, and transaction results. This path needs neither pack lookup nor a user patch.
- For app/company-wide pack coverage, a named node/country, or a selection
  strategy such as Smart or fallback, use packs and a service-owned exit. Read
  [Routing and patches](#routing-and-patches) for selection, ownership, apply, and removal.

Read the section for the chosen path; observation-only questions usually
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


## Custom Sites

If either tool is absent, report a capability/version mismatch; do not replace
it with a patch or direct JSON/YAML edits. Read both lists first to understand
overlaps and obtain actual stable IDs. Counts/hashes describe durable state.

`custom_sites_transact` accepts one version-1 operation:

```json
{"version":1,"operation":"add","pattern":"example.com","route":"proxy","wait":true}
```

Use `route:"direct"` for 自訂直連網站. Delete only an actual listed ID:

```json
{"version":1,"operation":"delete","id":"<actual list entry id>","wait":true}
```

Do not include `id` on add or `pattern`/`route` on delete.

- Plain hosts compile to `DOMAIN-SUFFIX` (host and subdomains), even though
  stored `match` is `full`. `*`/`?` compile to whole-host `DOMAIN-WILDCARD`;
  wildcards may cross dots. No URLs, ports, paths, IPs, CIDRs, or regex.
- Lists under `custom-sites/` are independent of patches. Default-policy sync
  and `reset_patches` preserve them; general reset deletes them.
- The safety baseline stays first. Custom sites follow newest-first across
  both lists before optional/default policy and fallback. Highest matching
  `sequence` wins; adding retains older overlaps, so deletion may reveal an
  older route. Same-pattern yellow UI warnings do not block saving.
- Proxy uses the reserved group's current exit; direct uses `DIRECT`. Do not
  recreate/mutate reserved groups with builders. Specific nodes or selection
  strategies belong to a service-owned patch.

The transaction validates, renders, runs `mihomo -t`, promotes, and, when
runtime is active, hot reloads and semantically reads back rules/groups.
Authorization must cover that live apply. A stopped runtime stays stopped.
Do not add a config test/reload after successful completion.

With `wait:true` inspect `ok` and `apply`: `validated`/`promoted`/
`config_sha256` prove validation/persistence; `runtime_running`/`reloaded`/
`read_back`/`effective` distinguish loaded state. `pending_next_start` means
saved until next start. `effective` does not prove traffic.
On failure inspect `code`, `error`, lists, and `rolled_back`; rollback may be incomplete.

The default async acknowledgement returns `task_id`, `log_file` and
`status_file`. Use `nl_file` until done/error; artifacts retain summaries,
not the whole transaction result. Reconcile lists plus `/rules`/`/proxies`
before retrying an uncertain write. Repeating an add allocates another ID.
For removal, verify absence and explain any older matching route now revealed.

## Routing and Patches

Use `routing_explain` for current intent, `pack_rules_query` for coverage,
and `packs_list`/`packs_get` for catalog details. Prefetch/read an exact
plausible source/pack only when cache coverage is insufficient. Prefer a
reusable pack over copying domains; check earlier broad-category collisions.
Use returned selectors, e.g. `{"source":"v2fly-dlc","pack":"apple-intelligence"}`,
not invented composite IDs. Justify narrow `custom_rules_build` overrides by
missing/overbroad coverage, a confirmed endpoint, or an intentional exception.

`proxy_group_build` creates a service-owned node-selection exit;
`policy_group_build` chooses among existing exits. Keep a service rule and
dedicated group in one patch when practical. "Smart", "auto" or "fallback"
describes that service's behavior, not permission to rewrite a shared group.

For a shared mutation, identify the exact group, owning patch, current/proposed
behavior, consumers/blast radius, and dedicated-group alternative. Apply only
with that specific authority. Otherwise use a dedicated exit or leave the
draft unapplied.

Read `config_status(patches=true, detail=true)` and exact owning patches via
`config_patch_get`; preserve base hashes. Build the smallest intent, then
`config_patch_draft` atomic operations. Review unrelated patches, protected
groups, order, and broad-pack collisions. Unresolved endpoint/ownership/scope
means leave the draft unapplied.

`config_patch_apply` persists and renders without starting runtime. For
authorized live apply, use `mihomo_config_test` when needed for the current
hash attestation, then `restart_runtime(strategy=hot_reload)`. Save-only
authority does not cover reload/start. Timeout is indeterminate; inspect
semantics rather than falling back to process restart.

Read back relevant `/rules`, `/providers/rules` and `/proxies`, including
protected groups. Traffic claims require a fresh connection's matched rule
and proxy chain. For removal, check other patch references, remove the owning
patch atomically by base hash and, if live removal was authorized, verify
loaded absence without changing protected groups.
