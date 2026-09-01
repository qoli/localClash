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
  Syncnext app-maintenance routing, Cloudflare GeoIP routing, and tail routing. Syncnext-maintained app
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
`⚡ 自动选择` is rebuilt by `subscriptions_refresh`: referenced `dialer-proxy`
helpers are excluded from the selectable set, remaining nodes are deduplicated
by their resolved effective first-hop IP set and port with the first occurrence
retained, and every representative must return an exact HTTP 204 from
`https://cp.cloudflare.com/generate_204` through an isolated Mihomo.

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
listener to every capability candidate. The automatic-connectivity capability
sends a strict generate-204 request; the ChatGPT capability sends the Statsig
initialize request only for the current g204-qualified unique endpoints. Endpoint
checks use 16-worker bounded concurrency, and each capability makes exactly one
request per candidate with no retry. Large subscriptions finish according to
their finite batch count and per-request timeout rather than an unrelated fixed
whole-batch deadline. The probes do not mutate or depend on the active core's
`alive` state. The resulting secret-safe snapshot
is stored below `.runtime/capabilities/`; raw proxy credentials are never written
there. Probe infrastructure errors fail the localClash refresh impact explicitly. If an
existing non-empty qualified set suddenly collapses to zero, the snapshot and
generated config are not replaced, so a transient carrier outage cannot silently
rewrite the policy graph.

DNS resolution is part of automatic-connectivity candidate eligibility rather
than probe infrastructure. A hostname that returns no address is recorded as an
explicit unavailable candidate with zero HTTP attempts and its resolver error;
other candidates continue through the strict g204 probe. Structural subscription
errors such as duplicate names, missing or cyclic `dialer-proxy` references,
missing servers, and invalid ports still fail the entire qualification. If no
candidate qualifies, no snapshot is published.

The product CLI `subscription refresh --json` and MCP `subscriptions_refresh`
both rebuild every configured capability snapshot. The snapshots record the
ordered qualified node names as derived state so a following `config render
--json` or MCP `config_render` can resolve the same capability even after the
patch registry recompiles `localclash-intent.json`. A missing, legacy, malformed,
or unsupported capability snapshot fails explicitly and requires another
subscription refresh. An optional capability may resolve to an explicit empty
set; required capability groups still fail when no node qualifies.
MCP subscription refresh renders a candidate config, runs isolated `mihomo -t`,
and only then promotes the candidate snapshot and config. Applying the promoted
config to an active runtime remains the explicit confirm-required hot-reload step.
The product `config apply-template` input can set `refresh_subscription: true`
to place template import, subscription refresh, all configured capability
snapshots, resolved selection, rendered config, and recorded `mihomo -t`
attestation in one rollback-protected material transaction. This is the LuCI
one-click-update and configured first-use path; callers do not need to coordinate
those intermediate states themselves.

The automatic-connectivity qualification is intentionally strict: one successful
HTTP 204 observation admits a candidate, while one failed observation removes it.
It has no per-candidate retry or failure hysteresis because `⚡ 自动选择` is a
high-quality input set for Mihomo Smart, not a list of marginal paths that need
retries to work. ChatGPT Statsig qualification is equally strict about observed
availability: one failed observation removes a candidate without retry or failure
hysteresis. Consequently, `ChatGPT-available` is always a subset of the current
g204-qualified set.

When Smart Core is active, `⚡ 自动选择` applies HK `6`, JP `5`, SG `4`, TW `3`,
US `2`, and Other `1` after g204 qualification and endpoint deduplication. The
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
