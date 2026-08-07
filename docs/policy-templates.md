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
semantics can still choose a different first exit, such as `🤖 ChatGPT` defaulting
to United States, then Japan, then Singapore while excluding the Hong Kong region
exit; `🚦 QUIC` defaulting to `REJECT`; game platform/Apple/Microsoft/speed-test
defaulting to direct; `🧲 BT/PT 下载` defaulting to direct while exposing automatic,
manual, and regional proxy exits for Dashboard overrides; or Bahamut defaulting
to Taiwan. Region exits are optional so subscriptions without a given region can
still initialize. Patch files
intentionally keep emoji identifiers as YAML `\U...` escapes so OpenWrt/BusyBox
display locale quirks do not change on-disk template bytes.

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
