# Rule Sources

Rule source files are adapter inputs, not pack catalogs.

Each file under `rule-sources/` should stay minimal:

```json
{
  "id": "sukkaw",
  "adapter": "sukkaw",
  "url": "https://github.com/SukkaW/Surge",
  "base_url": "https://ruleset.skk.moe"
}
```

The adapter owns source-specific transformation. It discovers or derives packs
and writes a runtime cache under `.runtime/rules/packs/`.

User selection belongs in a separate packs selection gob:

```json
{
  "proxy_groups": {
    "HK": {
      "nodes": ["🇭🇰香港01 | HK", "🇭🇰香港02 | HK"],
      "manual": true,
      "direct": false
    }
  },
  "policy_groups": {
    "Steam": {
      "exits": ["HK", "DIRECT"],
      "manual": true
    }
  },
  "enabled_packs": [
    {
      "source": "blackmatrix7",
      "pack": "Steam",
      "target": "Steam"
    }
  ]
}
```

`proxy_groups` materialize to Clash/Mihomo runtime proxy-groups. `nodes` must
be exact proxy names from `subscription.gob`; use `subscription_nodes_search`
to find candidate names first. Most groups do not verify egress regions with IP
lookup or hostname geolocation. Two built-in capabilities are available during
`subscriptions_refresh`. `network.connectivity.g204.v1` is optional subscription
filtering for `⚡ 自动选择` and defaults to disabled. When enabled, it excludes
referenced `dialer-proxy` helpers, probes every remaining proxy definition
independently without endpoint or protocol deduplication, and requires an exact HTTP 204 from
`https://cp.cloudflare.com/generate_204` through an isolated temporary Mihomo.
Each candidate may be observed twice after one initial failure, with no
cross-refresh failure hysteresis: either attempt may admit it, and two failures
remove it from the high-quality automatic set.
`openai.chatgpt.statsig.v1` is always rebuilt and derives its nodes by
requiring a successful Brotli-compressed Statsig initialization at
`https://ab.chatgpt.com/v1/initialize` through an isolated temporary Mihomo. Its
candidate set is the complete selectable subscription when g204 filtering is
disabled, or the current `network.connectivity.g204.v1` qualified list when it is
enabled.
HTTP 200, valid JSON, and a non-empty `derived_fields.country` are required.
Rejection, connection reset, timeout, malformed response, or a bounded-size
violation removes the candidate after two failed observations with one retry and
no failure hysteresis. A completed probe with no qualified nodes publishes an
explicit empty capability result. When the empty profile is
`network.connectivity.g204.v1`, `⚡ 自动选择` resolves through its original
all-subscription-nodes automatic structure. With g204 enabled, ChatGPT consumes
the observed same-refresh g204 set and therefore remains empty in that case.
With g204 disabled, ChatGPT instead probes the complete selectable set.
Infrastructure, missing, and
malformed snapshot errors do not use this fallback. A `capability` group
cannot also declare `nodes` or `match`. Choose either
`auto: true` or `manual: true`; enabling both is rejected because it would
create competing runtime groups for the same target.

An `auto` or `smart` proxy group may also declare ordered, group-scoped Smart
priorities:

```yaml
smart_priority:
  - {label: US, pattern: "(美国|US)", factor: 5}
  - {label: JP, pattern: "(日本|JP)", factor: 4}
  - {label: Other, pattern: ".*", factor: 1}
```

Rules are first-match-wins. `label` is durable localClash metadata; `pattern`
matches the subscription proxy name and `factor` is a positive multiplier for
Smart's learned weight. localClash validates and serializes these typed rules
instead of exposing Mihomo's delimiter string. Patterns are restricted to the
RE2-compatible subset and cannot contain semicolons. Under Smart Core, an
`auto` group becomes `type: smart` and receives its own `policy-priority`.
Under Meta Core it stays `url-test` and the Smart-only setting is not emitted.

`policy_groups` are the optional business layer for ACL4SSR-style UX. Rules and
packs can target a visible group such as `Steam`; that group then offers exits
such as `HK`, `JP`, `⚡ 自动选择`, or `DIRECT` in Dashboard. Non-built-in exits must
refer to `proxy_groups`; policy groups do not directly select subscription
nodes.

The CLI surface is intentionally small:

```bash
go run . rules adapt
go run . rules index-dump --format json
go run . rules render --selection localclash-packs.gob
```

`rules adapt` reads source JSON and writes runtime pack cache plus
`.runtime/rules/packs/index.gob`. `rules index-dump` exposes that runtime index
for inspection. `rules render` reads the cache plus the selection gob and
renders rule-provider, proxy-group, and rule fragments only. It does not modify
`.runtime/mihomo/config.yaml`.
