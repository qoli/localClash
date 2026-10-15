# Routing Packs, Service Exits, and Patches

## Reuse Pack Coverage

Use `routing_explain` when current intent matters. Search domain and service
keywords with `pack_rules_query`; use `packs_list` / `packs_get` for catalog
details. When cache coverage is incomplete, `pack_rules_prefetch` or
`pack_rules_read` can fetch an exact plausible source/pack.

Prefer enabling, retargeting, or ordering an existing pack over copying its
domains into custom rules. Check whether a broader category renders earlier
and captures the specific route. Use selectors returned by MCP, for example:

```json
{"source":"v2fly-dlc","pack":"apple-intelligence"}
```

Do not invent composite selectors such as `v2fly-dlc_apple-intelligence`.
Use `custom_rules_build` for a patch-owned narrow override, confirmed endpoint,
missing suitable pack coverage, over-broad pack, or intentional target outside
the product category; explain why a reusable pack was unsuitable.

## Choose an Owned Exit

Use `proxy_group_build` when a service needs its own node-selection exit, such
as a Smart group with five selected nodes. Use `policy_group_build` for a
business-layer choice among existing exits. Keep the rule and dedicated group
in one service-owned patch when practical, such as `user.palworld-server.v1`.

Words such as "auto", "smart", "five best nodes", or "fallback" describe the
requested service group's behavior; they do not authorize rewriting a shared
group with a similar name.

If a proposed change affects a shared/default exit, identify the exact group
and owning patch, current/proposed behavior, affected consumers and blast
radius, and the dedicated-group alternative. Obtain approval for that named
mutation before applying it. Without that authority, use a dedicated target
or leave the draft unapplied; unrelated owning patches remain out of scope.

## Save and Load the Requested Change

Read `config_status(patches=true, detail=true)` and the exact owning patches
with `config_patch_get` when modifying patch state. Preserve their base hashes
for concurrency-safe operations. Build the smallest service-owned intent with
`proxy_group_build`, `policy_group_build`, `rule_provider_build`, or justified
`custom_rules_build`.

Use `config_patch_draft` for atomic registry operations; inspect the draft for
unrelated patches, shared/default group mutations, rule order, and broad-pack
collisions. Do not apply while endpoint identity, target ownership, or affected
patch scope remains unresolved.

`config_patch_apply` persists the reviewed generation and renders config
without starting runtime. For a requested live apply, use `mihomo_config_test`
when needed to establish a passing attestation for the current config hash,
then approved `restart_runtime(strategy=hot_reload)`. Existing clear live-apply
authorization covers this work; storage-only approval does not authorize
reload or runtime start. Process restart and router takeover are separate
actions, not substitutes for hot reload.

Verify the changed rule/provider/group with the relevant `/rules`,
`/providers/rules`, or `/proxies` response. Verify protected shared groups
remain unchanged and any approved shared mutation matches its reviewed scope.
A fresh connection and its matched rule/proxy chain are needed only when
claiming the actual traffic outcome.

## Remove a Dedicated Service Route

Verify that no other patch references the service-owned group, then remove
the owning patch atomically with its base hash. When live removal was
authorized, hot reload and establish that the dedicated rule, group, and patch
are absent while protected shared groups remain unchanged.
