---
name: localclash-mcp-route-operator
description: Use localClash MCP to inspect routing intent, reusable rule packs, dedicated service exits, loaded Mihomo state, active connections, and bounded logs, or to make scoped live route changes without unconfirmed shared-group mutations. Trigger for localClash MCP, domain/service/app routing, rule-pack lookup, policy or proxy groups, route observability, current connection chains, and requests such as "使用 localclash MCP", "查詢這個域名", "讓這個服務走代理", or "看看實際流量怎麼走". For generic network debugging, use this skill only when localClash is explicitly relevant and treat it as an evidence source rather than assuming it is the cause.
---

# localClash MCP Route Operator

## Operating Model

Treat localClash MCP as a product API, not as raw Clash YAML or a generic rule
string editor.

Separate every task into these decisions:

1. What traffic is in scope: domain, pack, app, service, device, or endpoint?
2. Who owns its routing target: a service-owned group or a shared/default exit?
3. Which evidence layer is needed to answer the question?
4. Is this observation only, or did the user authorize a persisted and loaded change?

Do not turn a domain query into a custom rule by default. Do not turn a
service-specific request into a modification of a shared exit without explicit
informed user confirmation.

## Tool Access

Prefer registered `mcp__localclash.*` tools. If the namespace is not visible,
use tool discovery before raw HTTP, curl, file inspection, or SSH.

Do not ask for config, runtime, subscription, cache, provider-cache, binary, or
controller-secret paths. MCP server state owns those locations and secrets.

## Choose the Smallest Observation

Do not run the full environment/config/runtime sequence for every question.
Select only the evidence layer that can prove the requested claim.

### Catalog and pack evidence

Use `packs_list`, `packs_get`, `pack_rules_query`, `pack_rules_prefetch`, and
`pack_rules_read` to answer what reusable packs exist or contain a domain.

Catalog or cache evidence does not prove a pack is enabled. Stop here when the
user only asked whether a pack exists.

### Durable localClash intent

Use `config_status`, `config_patch_get`, and `routing_explain` to answer what
the patch registry and compiled intent say should happen.

`routing_explain` is intent evidence. It does not prove Mihomo loaded the
config or that current traffic used the route.

### Loaded Mihomo runtime

Use `runtime_status` only to establish process health. Use bounded
`mihomo_api_request` paths for loaded semantics:

- `/rules` and `/providers/rules` for loaded rule order and providers
- `/proxies` for group type, membership, health, and current selection
- `/configs` for loaded runtime configuration

Do not use `mihomo_api_request` as a generic HTTP client.

### Active data plane

Use `mihomo_connections_read` to inspect currently tracked connections and
report the observed source, destination, network, host or sniffed host, matched
rule, rule payload, selected proxy, and chain when available.

A connection snapshot can prove how that connection routed. Absence cannot
prove how a future connection will route.

### Fresh traffic and time-bounded behavior

Use bounded `mihomo_logs_read`, a connection stream, or deliberately fresh
traffic when current connections are missing or stale. Align the observation
window with the user's action and identify the source device before attributing
traffic to an app.

Do not identify an app endpoint from one coincident connection, a familiar
port, ASN, or timing alone. Require a user-confirmed endpoint or repeatable
before/after evidence from a fresh app action. If several candidates remain,
report candidates and uncertainty instead of writing a rule.

Use `router_takeover_status` only when the question is specifically about
router interception, DNS hijack, firewall, fwmark, or TUN ownership. Do not
expand a product-routing task into firewall or takeover changes.

## Pack-First Rule Selection

For a domain, company, app, or service:

1. Use `routing_explain` when current intent matters.
2. Query domain and service keywords with pack tools.
3. Prefetch or read an exact plausible `{source, pack}` when cache coverage is incomplete.
4. Prefer enabling, retargeting, or ordering an existing pack over copying its domains into custom rules.
5. Check whether a broader category would render before and capture the specific route.

Use selectors returned by MCP, for example:

```json
{"source": "v2fly-dlc", "pack": "apple-intelligence"}
```

Do not invent composite selectors such as `v2fly-dlc_apple-intelligence`.

Use `custom_rules_build` only for an explicit narrow override, a confirmed
endpoint, missing suitable pack coverage, an over-broad pack, or an intentional
target different from the product category. State why no reusable pack was
used.

## Exit Ownership Is a Hard Boundary

Treat policy and proxy groups as owned routing objects, not convenient names.

- A service/app/game-specific request must reuse a group owned by that service
  or create a dedicated group for it.
- Shared/default exits such as global auto, global direct, regional exits, and
  groups owned by `default.*` patches may be referenced freely. Rewriting one
  for a service is allowed only after explicit informed user confirmation.
- Words such as "auto", "smart", "five best nodes", or "fallback" describe
  the dedicated group's behavior. They do not authorize changing a shared
  group with a similar name.
- Before requesting confirmation, show the exact shared group and owning patch,
  the current behavior, the proposed behavior, affected consumers, blast
  radius, and the dedicated-group alternative.
- Treat confirmation to change a shared/default group separately from approval
  to persist the draft or reload the runtime. Do not infer one from the other.

Choose the product object from the intent:

- Use `proxy_group_build` when the service needs its own node-selection exit,
  such as a Smart group containing five selected nodes.
- Use `policy_group_build` when the service needs a business-layer choice among
  existing exits.
- Keep the service's rule and dedicated group in one service-owned patch when
  practical, for example `user.palworld-server.v1`.

When a service-specific draft modifies a shared/default group, pause before
apply and request explicit confirmation for that named mutation. If confirmation
is absent, redesign it with a dedicated group or leave the draft unapplied.
Unrelated owning patches remain out of scope unless separately authorized.
Record unchanged shared groups and any explicitly approved shared mutation as
acceptance criteria.

## Live Change Workflow

For an authorized route change:

1. Confirm the traffic identity; use fresh evidence when endpoint attribution is uncertain.
2. Read `config_status(patches=true, detail=true)` and the exact owning patches.
3. Use `routing_explain` and pack tools for current behavior and reusable coverage.
4. Decide the dedicated service target before building rules.
5. Build the smallest service-owned intent with `proxy_group_build`, `policy_group_build`, `rule_provider_build`, or justified `custom_rules_build`.
6. Draft atomic patch-registry operations with base hashes using `config_patch_draft`.
7. Audit the draft for unrelated patches, shared/default group mutations, rule ordering, and broad-category collisions.
8. For each shared/default mutation, present the exact impact and dedicated alternative, then obtain explicit confirmation naming that change. Without it, do not apply.
9. Apply the reviewed generation with `config_patch_apply` and run `mihomo_config_test` when needed.
10. Ask separately before reload, run, stop, process restart, or router takeover unless the user clearly delegated that live action.
11. Prefer approved `restart_runtime(strategy=hot_reload)` and treat a timeout as indeterminate.
12. Verify the changed rule and dedicated group through `/rules`, `/providers/rules`, or `/proxies`.
13. When the claim is about actual traffic, create a fresh connection and verify its matched rule and proxy chain.
14. Prove unapproved shared/default groups remain unchanged and verify every explicitly approved shared mutation against its reviewed impact.

Do not stop at `runtime_status`; it proves process health, not loaded route
semantics.

## Observation During General Debugging

When localClash is used to increase observability for another problem:

1. State the hypothesis being tested, such as route divergence, DNS handling,
   proxy selection, or failure before network activity.
2. Capture only the intent/runtime/data-plane evidence needed for that hypothesis.
3. Correlate localClash evidence with the owning app's logs or user action.
4. If no relevant connection is created, report that the failure may precede
   networking; do not invent a routing cause.
5. Once localClash is excluded, stop changing routes and continue diagnosis at
   the owning layer.
6. Prefer the smallest reversible remedy at that owning layer. Observation is
   not permission to make a persistent DNS, route, firewall, or takeover change.

## Removal and Maintenance

For a dedicated service route removal, verify no other patch references its
group, then remove the owning patch atomically with its base hash. After an
approved reload, prove the dedicated rule, group, and patch are absent and all
protected shared groups are unchanged.

## Safety Boundaries

- Do not edit `.runtime/mihomo/config.yaml`, `generated/`, `subscription*.gob`, or provider caches as source of truth.
- Do not perform router takeover, firewall, runtime stop, or process restart without explicit authorization.
- Do not silently substitute or modify a shared group when dedicated-group construction fails.
- Do not apply a shared/default group mutation without explicit informed confirmation naming the affected group and change.
- Do not apply a draft when endpoint identity, target ownership, or affected patch scope is unresolved.
- If hot reload times out, verify loaded state through runtime APIs before deciding the result.

## Reporting

Label every claim by evidence surface:

- catalog or pack cache
- durable patch/compiled intent
- loaded Mihomo runtime
- current active connection
- bounded logs or fresh-traffic observation

For a route change, report the rule source, target, owning patch, unchanged
shared groups, any explicitly approved shared mutation, persisted result,
loaded-runtime proof, and whether a fresh connection proved the final chain.
State uncertainty explicitly.
