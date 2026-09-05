# Policy Templates

`docs/rule-model.md` is the authoritative development contract for rule
layering, customization, and target ownership. This document describes the
current disk-backed localClash policy templates.

## Boundary

Policy templates are localClash-owned default patch sources, not generated
Mihomo YAML. Template manifests live under `policy-templates/`, and
`config_configure(policy_template=...)` imports their resolved patches into
`patches/*.json`, then builds the compiled `localclash-intent.json` artifact.
The renderer then combines that compiled intent with the effective subscription
and runtime profile.

Do not model a removed upstream preset as hidden renderer behavior. Broad default
behavior must appear as explicit patch files in the selected template manifest.

## Built-In Templates

- `minimal`: loads only `policy-templates/minimal.json`. It defines the compact
  default graph for advanced manual customization and does not auto-load the
  built-in patch set.
- `localclash-default`: loads all built-in patches listed by
  `policy-templates/localclash-default.json`. Each ordered file under
  `policy-templates/localclash-default.d/` contributes one stable default patch,
  such as region exits, communication/social routing, Steam, media groups, games,
  Syncnext app-maintenance routing, Binance, Cloudflare GeoIP routing, and tail routing. Syncnext-maintained app
  domains are evaluated before the broad `GEOSITE,cn,DIRECT` tail rule, while
  `SyncnextUnbreak` remains explicitly direct.

Both templates are patch-layered product configuration. Neither depends on a
separate preset file outside `policy-templates/`.

## Default Structure

The default Dashboard-facing structure is layered as:

```text
business group -> exit group -> subscription nodes
```

`minimal` defines `⚡ 自动选择`, `🎯 手动选择`, and `DNSProxy` in the minimal
strategy layer. `DNSProxy` exits through `⚡ 自动选择`, so router DNS `#DNSProxy`
references have a concrete target even without loading the default patch set.
`⚡ 自动选择` uses the complete selectable subscription set by default.
Referenced `dialer-proxy` helpers are excluded, but nodes are not collapsed by
hostname, resolved IP, port, protocol, or credentials. Mihomo performs the
automatic group's runtime health checks directly; subscription refresh does not
apply a separate connectivity prefilter.

`localclash-default` adds regional exits plus business routing groups. Its
`🌐 全球直连` policy defaults to `DIRECT` while exposing automatic and regional
proxy exits for an explicit Dashboard override. The broad `geolocation-!cn`
pack targets `🌍 非中國網站`, making its known non-China scope explicit in the
Dashboard.
Ordinary proxy-oriented business groups default to `⚡ 自动选择` and keep
`🎯 手动选择` as the first manual override. Groups with explicit safety or product
semantics can still choose a different first exit. `🤖 ChatGPT` defaults to the
United States regional exit and places `ChatGPT-available` last as an opt-in
choice. That localClash-owned automatic exit is rebuilt during
`subscriptions_refresh` from nodes that successfully initialize ChatGPT's
Statsig control plane at `ab.chatgpt.com/v1/initialize`. Qualification requires
HTTP 200, Brotli encoding, valid JSON, and a non-empty service-observed
`derived_fields.country`. Rejections, connection resets, timeouts, malformed
responses, and bounded-size violations are recorded explicitly; one failed
observation removes a previously-qualified node without failure hysteresis. The
regional exits remain available before that opt-in choice. `🚦 QUIC` defaults to
`REJECT`; game platform/Apple/Microsoft/speed-test
defaulting to direct; `🧲 BT/PT 下载` defaulting to direct while exposing automatic,
manual, and regional proxy exits for Dashboard overrides; or Bahamut defaulting
to Taiwan. Region exits are optional so subscriptions without a given region can
still initialize. Patch files
intentionally keep emoji identifiers as YAML `\U...` escapes so OpenWrt/BusyBox
display locale quirks do not change on-disk template bytes.

`📥 大模型下载` also defaults to `🌐 全球直连`, followed by automatic and
regional exits. Its exact-domain rules cover dedicated Hugging Face Xet/LFS/CDN
hosts, observed ModelScope CDN hosts, and the Ollama model registry before the
broader AI `GEOSITE` pack. The rules intentionally exclude `huggingface.co`,
`modelscope.cn`, `modelscope.ai`, generic GitHub release hosts, and shared
cloud-object-storage suffixes: domain matching cannot distinguish a model file
URL path from unrelated traffic on those hosts. Hugging Face payload hosts can
also carry datasets, CAS and model registries can serve metadata or uploads, and
ModelScope CDN names may evolve, so the group is a conservative
artifact-delivery boundary rather than a claim that every matched byte is an LLM
weight download.

Capability groups are derived configuration, not Mihomo health-check aliases.
localClash starts an isolated temporary Mihomo and assigns one loopback mixed
listener to every capability candidate. It waits for every listener before
starting HTTP workers and checks the process plus all listeners again after the
workers complete; a startup race or listener collapse is an infrastructure
failure, not an empty capability observation. The ChatGPT capability sends the
Statsig initialize request for every selectable subscription proxy. Endpoint
checks use 16-worker bounded concurrency. A failed ChatGPT request is retried
once; either attempt may qualify the candidate, while two failures remove it.
Large subscriptions finish according to
their finite batch count and per-request timeout rather than an unrelated fixed
whole-batch deadline. The probes do not mutate or depend on the active core's
`alive` state. The resulting secret-safe snapshot
is stored below `.runtime/capabilities/`; raw proxy credentials are never written
there. Probe infrastructure errors still fail the localClash refresh impact
explicitly. A completed measurement with zero qualified nodes is different: it
publishes an explicit empty capability snapshot instead of being misclassified
as probe failure. No previous snapshot, alternate endpoint, or all-node set is
substituted for the observed result.

DNS resolution belongs to the isolated Mihomo probe path. localClash does not
pre-resolve or merge candidates by their effective endpoint, so definitions that
share a hostname, resolved address, port, or protocol are still probed
independently. Structural subscription errors such as duplicate names and missing
or cyclic `dialer-proxy` references still fail qualification. If no candidate
qualifies, the snapshot records the per-candidate failures and an explicit empty
`qualified` list.

The product CLI `subscription refresh --json` and MCP `subscriptions_refresh`
both rebuild the capabilities selected by the subscription policy. ChatGPT is
always rebuilt when it is declared by the product template. `⚡ 自动选择`
resolves directly from the complete selectable subscription set. Capability snapshots record the
ordered qualified node names as derived state so a following `config render
--json` or MCP `config_render` can resolve the same capability even after the
patch registry recompiles `localclash-intent.json`. A missing, legacy, malformed,
or unsupported capability snapshot fails explicitly and requires another
subscription refresh. An optional capability may resolve to an explicit empty
set. A required empty capability fails during config resolution.
MCP subscription refresh renders a candidate config, runs isolated `mihomo -t`,
and only then promotes the candidate snapshot and config. Applying the promoted
config to an active runtime remains the explicit confirm-required hot-reload step.
The product `config apply-template` input can set `refresh_subscription: true`
to place template import, subscription refresh, all configured capability
snapshots, resolved selection, rendered config, and recorded `mihomo -t`
attestation in one rollback-protected material transaction. This is the LuCI
one-click-update and configured first-use path; callers do not need to coordinate
those intermediate states themselves.

ChatGPT Statsig qualification uses a two-attempt failure threshold.
There is no cross-refresh failure hysteresis or stale-result retention.
ChatGPT qualification starts from every selectable subscription proxy.

When Smart Core is active, `⚡ 自动选择` applies HK `6`, JP `5`, SG `4`, TW `3`,
US `2`, and Other `1` to the complete selectable set. The
weights are soft Smart preferences, not a fallback chain. Meta Core receives the
same qualified candidate set as a normal `url-test` group.

When Smart Core is active, `ChatGPT-available` applies ordered proxy-name labels
and weights: US `5`, JP `4`, SG `3`, TW `2`, and Other `1`. This is a soft
preference layered onto Smart's learned quality, not a fallback chain or a hard
regional lock: the normal bias favors the United States, while a materially
better lower-priority node can still win. The priority belongs only to this
service-qualified group and does not alter other automatic or Smart groups.
Meta Core keeps the group as a normal `url-test` without emitting this option.

The Cloudflare default is an explicit `GEOIP,cloudflare` exception before the
terminal `MATCH,DIRECT` rule. It targets the Dashboard-visible `☁️ Cloudflare`
business group, whose first exit is `⚡ 自动选择`; it intentionally does not add
`GEOSITE,cloudflare`, because Cloudflare-owned domains can be directly reachable
even where Cloudflare's direct IP space is not.

MCP `config_status(patches=true)` exposes the active patch registry and compiled
intent. For compact Agent-facing routing discovery, use the read-only
`routing_explain` tool.

## Router And Game Accelerators

Router transparent-proxy mode defaults to blacklist semantics. The default template
may send known non-China `GEOSITE` categories to a Dashboard-visible policy group,
while the final `MATCH` fallback targets the `🌐 全球直连` policy whose first
exit is direct:

```yaml
- MATCH,🌐 全球直连
```

This is required for game accelerator compatibility. A template that renders
The default selection therefore leaves unknown traffic direct for game accelerator
compatibility. Users can explicitly switch `🌐 全球直连` to automatic or a regional
exit when they need a broader proxy strategy; explicit rules targeting `DIRECT`
remain direct.

## Binance Default Policy

`default.binance.v1` adds the manual `🪙 Binance` selector and the
`blackmatrix7 / Binance` rule-provider pack. Its exits are ordered as
`🇹🇼 台湾节点`, `🇺🇸 美国节点`, `🇯🇵 日本节点`, `⚡ 自动选择`, and
`🎯 手动选择`. This is the configured selector order, not automatic failover
or a measured availability ranking. The broad Crypto pack is not included in
the default template.

The Binance pack includes `DOMAIN-SUFFIX,binance.com` and related Binance
domains, rather than routing unrelated exchanges and DeFi services together.

The patch has stable order `0760.000000`, after Syncnext maintenance and before
the Cloudflare GeoIP patch and broad tail packs in the template registry.
The renderer emits custom rules before catalog packs, so the existing Cloudflare
GEOIP custom rule still precedes Binance in the rendered rules; patch order does
not override that cross-type ordering. Binance precedes the broad tail packs.

Updating the repository template alone does not change an existing running
configuration; the normal template import and configuration deployment workflow
applies.
