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
`subscriptions_refresh` from nodes that return the expected ChatGPT IP
fingerprint (`HTTP 403`, `type: dc`, and `Request is not allowed`) from both the
iOS and Android root endpoints. Explicit ISP or region rejection removes a node
immediately; connection resets, timeouts, and unexpected responses are retried
and use failure hysteresis for previously-qualified nodes. The regional exits
remain available before that opt-in choice. `🚦 QUIC` defaults to `REJECT`; game platform/Apple/Microsoft/speed-test
defaulting to direct; `🧲 BT/PT 下载` defaulting to direct while exposing automatic,
manual, and regional proxy exits for Dashboard overrides; or Bahamut defaulting
to Taiwan. Region exits are optional so subscriptions without a given region can
still initialize. Patch files
intentionally keep emoji identifiers as YAML `\U...` escapes so OpenWrt/BusyBox
display locale quirks do not change on-disk template bytes.

Capability groups are derived configuration, not Mihomo health-check aliases.
localClash starts an isolated temporary Mihomo, assigns one loopback mixed
listener to each distinct proxy definition, and checks the two service endpoints
through each listener. Endpoint checks use bounded concurrency so subscriptions
with hundreds of nodes can finish within the refresh window. It does not mutate
or depend on the active core's `alive` state. The resulting secret-safe snapshot
is stored below `.runtime/capabilities/`; raw proxy credentials are never written
there. Probe infrastructure errors fail the localClash refresh impact explicitly. If an
existing non-empty qualified set suddenly collapses to zero, the snapshot and
generated config are not replaced, so a transient carrier outage cannot silently
rewrite the policy graph.

Qualification uses asymmetric hysteresis. A new proxy enters the group only
after a successful observation. A previously qualified proxy remains eligible
through two consecutive failed refresh observations and is removed on the third;
the runtime automatic group can still avoid it while its transport is down. A
successful observation resets the failure counter. This keeps an instantaneous
carrier interruption from being mistaken for a durable ChatGPT capability change.

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
