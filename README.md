# localClash

Local Mihomo runtime management through CLI and MCP. localClash owns
subscriptions, routing intent, config rendering/testing, and the managed
Mihomo process. AI agents manage it through MCP; zashboard is the runtime
dashboard.

![localClash MCP environment inspection on an OpenWrt router](docs/assets/localclash-mcp-environment-inspect.png)

For an OpenWrt UI, installation, and router network takeover, use the sibling
[localclash-luci project](https://github.com/qoli/localclash-luci).
LuCI owns fw4/nftables, policy routing, DNS hijack, boot/hotplug reconciliation,
and transactions spanning runtime and takeover. Core reports versioned
runtime facts and never executes `fw4`, `nft`, `uci`, or `ip rule`.

## First Use

Run from this checkout with Go installed and a subscription URL or supported
proxy node URI available. Treat subscription URLs and node credentials as
secrets.

```bash
go run . core download --dry-run
go run . core download --force
go run . dashboard download --force
```

The default target downloads the host Meta core, for example
`bin/darwin-arm64/lc-mihomo-meta` on macOS arm64. For Linux router cores,
select the target explicitly:

```bash
go run . core download --target router --arch arm64 --force
```

For MCP-managed setup, start the server:

```bash
go run . mcp
```

Ask the connected agent to inspect `subscriptions_status`, save source URIs
with `subscriptions_configure`, refresh with `subscriptions_refresh`, and
configure the desired template/profile through the discovered product tools.
For a quick CLI-only first run, download a subscription, render, validate,
then start:

```bash
go run . subscription download --url "https://example.com/sub?token=..." --output subscription.gob --force
go run . config render --force
go run . doctor
go run . run
```

The generated config is `.runtime/mihomo/config.yaml`. zashboard is installed
under `.runtime/mihomo/ui/zashboard` and is available at
`http://127.0.0.1:9090/ui` after startup. Startup stops with diagnostics if
required subscription state is missing.

## MCP and Codex

The default MCP endpoint is `http://127.0.0.1:8765/mcp`; health is at
`http://127.0.0.1:8765/health`. Override the bind address with
`go run . mcp --addr 127.0.0.1:8766 --path /mcp`. Bootstrap diagnostics remain
available when local prerequisites are incomplete.

Use `tools_list` for current tool names, schemas, and safety metadata.
Normal product calls accept intent and task options; server bootstrap owns
artifact paths and credentials. `safe_read` observes, `safe_write` writes
local artifacts or validates, and `confirm_required` needs authorization for
the requested action. Existing clear authorization carries through its scope.
Saving intent, loading it, changing shared groups, and router takeover have
different effects.

Install the versioned [routing Skill](.codex/skills/localclash-mcp-route-operator/SKILL.md)
on the machine running Codex:

```bash
scripts/install-codex-skill.sh
scripts/install-codex-skill.sh --check
```

The destination is the `skills/localclash-mcp-route-operator` directory under
`CODEX_HOME` (default `$HOME/.codex`). Edit the repository copy and reinstall;
the installed copy is an agent artifact, outside router release assets.
If a server lacks a needed tool, report the version/capability mismatch.

For router management, use the router's MCP state. Local source inspection,
pack catalogs, and proxy names cannot prove what it has loaded or where its
traffic exits. The routing Skill distinguishes durable intent, loaded
semantics, active connections, and fresh traffic.

## Subscriptions

Inputs may be HTTP(S) subscription URLs or supported proxy share URIs.
Remote responses are parsed as Mihomo YAML, then as a node URI list if
necessary. Only proxy data is imported; localClash owns runtime groups,
rules, and providers. HTTP(S) inputs are reserved for remote subscriptions.
Accepted share-link schemes and parsing rules live in
[the URI parser](internal/subscriptions/proxy_uri.go).

MCP setup stores private source URIs in `localclash-subscriptions.json`,
per-source artifacts in `.runtime/subscriptions/`, and the merged output in
`subscription.gob`. Multiple sources receive source-prefixed proxy names;
source-local dialer references are rewritten consistently. Ambiguous
duplicate-name references fail. URI deduplication compares URI strings,
without treating credentials or similar node names as equivalent sources.

On fetch/read/parse failure, refresh can reuse that source's cache only when
its canonical URI hash, schema, and nonempty proxy list validate. The source
is marked `cached` with warnings; its cache timestamp is preserved. A source
with neither a valid download nor a valid cache is marked `failed` and
excluded. At least one valid source can produce a merged artifact; if all
are invalid, refresh fails and preserves the prior merged artifact.
The merged artifact is never a substitute for a missing source cache.

A successful merge records included source IDs. Rendering skips only sources
explicitly excluded by that refresh; missing declared artifacts, invalid
source IDs, and incomplete legacy state remain errors. Cancellation, bad
configuration, local write, render, and validation failures remain explicit.
Inspect warnings and source statuses before claiming every source was freshly
downloaded.

Refresh reevaluates saved selectors. Missing exact nodes or a selector that
no longer meets its requirements require replanning and leave the active
generated config unchanged. Added nodes alone do not trigger repair.
Proxy-name region searches do not verify egress geography.

### ChatGPT Exit Qualification

`⚡ 自动选择` keeps every selectable subscription proxy in source order and
relies on Mihomo's own automatic-group health checks instead of a separate
subscription prefilter. Referenced dialer helpers remain excluded from the
selectable group. If the intent declares
`capability: openai.chatgpt.oauth_statsig.v1`, refresh independently rebuilds that
service exit from every selectable proxy. Every ChatGPT-qualified node must
pass both a dummy refresh-token `POST https://auth.openai.com/oauth/token`
and `POST https://ab.chatgpt.com/v1/initialize` through the same isolated
proxy exit. After the domain measurements pass, the candidate resolves both
endpoints' A/AAAA records over HTTPS through its own proxy and tests every
returned IP as the CONNECT target, preserving the original HTTP Host, TLS SNI,
and certificate verification. At least one address family must pass both
measurements; any explicit regional rejection on any tested IP vetoes the
candidate. An unavailable IPv6 path does not override a positive IPv4 pair.
Statsig must return HTTP 200, Brotli JSON, and a non-empty
`derived_fields.country`.
HTTP 401 with `error.code=token_expired` proves the egress passed the regional
admission gate; HTTP 403 with
`error.code=unsupported_country_region_territory` rejects it immediately.
Other HTTP errors, malformed responses, TLS failures, and timeouts do not pass
that measurement and are recorded separately from regional rejection.
Inconclusive failures are retried once per measurement. Probes use 16 workers.
Snapshot v8 records DNS results and each IP path's admission evidence; older
domain-only qualifications expire and require a fresh subscription refresh.
The check is independent from the active core's cached `alive` state.

IP resolution uses Cloudflare DNS over HTTPS through the candidate proxy;
no CDN address or node-name country blacklist is embedded in the check.
Both endpoint measurements must pass within at least one common address
family. Snapshot v8 retains resolution, target family/address, attempts,
HTTP status, error code, and country per IP measurement. Inconclusive paths
cannot supply positive admission, and there is no failure hysteresis.

MCP refresh renders `config.yaml.candidate`, validates it with `mihomo -t`,
and promotes the capability snapshot and generated config only after the
test passes. It does not silently reload the running core; use the authorized
`restart_runtime` hot reload after reviewing the result.

## Routing Intent and Runtime Config

Durable patches under `patches/` compile into `localclash-intent.json`;
`localclash-packs.gob` and runtime YAML are derived artifacts. Use
`config_patch_draft` followed by `config_patch_apply` for reviewed changes.
Agents should not edit generated YAML or provider caches as intent.

The non-disableable local safety baseline renders first, followed by custom
sites and configured optional/template/user policy layers, then fallback.
The default fallback `MATCH,🌐 全球直连` starts with `DIRECT` so unmatched
traffic can coexist with game accelerators; an explicit `DIRECT` rule remains
direct. Policy targets are `DIRECT`, `REJECT`, or actual group names.
Nested policy groups form a DAG; cycles are invalid.

`config apply-template` selects `localclash-default` or `minimal`.
With `refresh_subscription=true` it performs a rollback-protected
template/refresh/capability/render/config-test transaction. `reset_patches`
replaces policy patches but preserves independent custom-site lists.

The full default template includes `📺 ViuTV` for ViuTV 99/96. Its only
geographic exit is the existing `🇭🇰 香港节点` group; without matching Hong Kong
nodes, this service exposes only `REJECT`. The dedicated rules cover `viu.tv`
and its subdomains, `api.viu.now.com`, `cdn01r.now.com`, and `fwp.nowe.com`
before broad media routing. They do not capture Viu.com or all of now.com.
The reusable Viu packs include Viu.com and omit the observed CDN/license
hosts, so this policy uses scoped endpoint rules. Region selectors match node
names; actual Hong Kong egress and successful DRM playback still need testing.

Runtime-only keys such as DNS, TUN, ports, and sniffer come from the selected
builtin runtime template, or `localclash-user.json` when present. That file
replaces the builtin fragment without deep backfill; dynamic routing keys
(`proxies`, groups, providers, rules, and `x-localclash`) are rejected.
Smart default injection is separate and preserves explicit supported values.
Use `go run . config render --runtime-profile <path> --force` to inspect an
alternate selector.

The builtin router profile disables Mihomo auto-route/auto-redirect so LuCI
can own takeover. Its local DNS policy sends single-label and private names
to the fixed template address `192.168.6.1#DIRECT`; it does not discover the
router's LAN address. Other LAN setups need a matching runtime profile.
Public DNS uses the encrypted baseline in the
[builtin router template](internal/runtimeprofile/profiles/router.default.json).
There is no `dnsqualify` input or WAN-resolver promotion.

## Custom Website Routing

For a persistent website proxy/direct decision through existing exits:

```bash
localclash custom-sites list --json
localclash custom-sites transact --input request.json --json
```

MCP exposes `custom_sites_list` and `custom_sites_transact`. One add request is
`{"version":1,"operation":"add","pattern":"example.com","route":"proxy","wait":true}`;
delete by an actual listed entry ID with
`{"version":1,"operation":"delete","id":"<entry-id>","wait":true}`.

Plain hosts compile to `DOMAIN-SUFFIX`, covering the host and subdomains.
Patterns containing `*` or `?` compile to whole-host `DOMAIN-WILDCARD` and
may cross dots. Enter host patterns without URLs, ports, paths, IPs, CIDRs,
or regex. After the safety baseline, highest matching sequence wins across
both lists. Older overlaps remain; deleting the newest may reveal an older
decision. LuCI's same-pattern warning does not block saving.

`proxy` uses the current reserved `自訂代理網站` exit; `direct` uses
`自訂直連網站` → `DIRECT`. Named-country or Smart-selection requests need a
service-owned routing patch. Lists live under `custom-sites/` and survive
default-policy sync and `reset_patches`; general workspace reset deletes them.

The transaction validates, renders, runs `mihomo -t`, promotes, and, if runtime
is active, hot reloads and reads back rules/groups. A stopped runtime stays
stopped with `pending_next_start`. Failures trigger rollback attempts;
inspect the outcome rather than assuming restoration. Loaded semantics do
not prove actual traffic. The routing Skill covers uncertain writes.

## Runtime and Diagnostics

```bash
go run . config render --json
go run . mihomo config-test --json
go run . doctor --json
go run . status --json
go run . restart
go run . stop
```

CLI `restart` defaults to process restart. MCP `restart_runtime` defaults to
hot reload and requires a passing config-test attestation for the current
config hash. A reload timeout is indeterminate: inspect task results and
changed controller semantics before retrying. Do not substitute process
restart automatically.

Runtime stdout/stderr logs are under `.runtime/mihomo/logs/`. Core stop
interfaces do not inspect platform capture; on OpenWrt use LuCI's transaction,
which removes and verifies takeover before stopping Mihomo.

### Independent Debug Log Collector

```sh
localclash logs collect \
  --config /root/localclash/.runtime/mihomo/config.yaml \
  --output-dir /root/localclash/.runtime/logs/mihomo-history
```

This observer reads controller credentials and subscribes to existing debug
logs without bootstrap, config/log-level changes, Mihomo/MCP restarts, or
takeover changes. After explicitly installing a binary with this command,
copy [the service script](scripts/localclash-log-collector.init) to
`/etc/init.d/localclash-log-collector`, chmod 0755, then enable/start that
service. Stop/disable only this observer to stop collection; the general
router deployment script performs unrelated work.

Retention is a sliding 48-hour window subject to a 32 MiB budget; volume may
evict records earlier. The collector must run to expire files. Upstream and
local queues can lose records; no gap marker does not imply complete coverage.
`received_at` is receipt time, not source event time. Controller logs omit
startup failures before it listens, so lifecycle stdout/stderr is still needed.
Restarts, clock jumps, and power loss limit continuity. Storage errors affect
only the collector.

Directories use 0700 and files 0600. Common credentials are redacted, but raw
archives still contain addresses, node names, and browsing metadata; do not
publish them. Historical capture/isolation checks do not establish continuous
48-hour coverage or ARM runtime acceptance.

## Local State and Reset

Do not commit `.runtime/`, `generated/`, `bin/`, `subscription*.gob`,
`localclash-subscriptions.json`, `localclash-intent.json`,
`localclash-packs.gob`, or private `localclash-user.json`.
Builtin templates and rule-source definitions are source inputs.

```bash
go run . reset --dry-run
go run . reset
```

General reset removes runtime/generated state, legacy `profiles/`,
subscription artifacts/config, intent/packs/runtime state, and `custom-sites/`.
It preserves downloaded binaries, builtin assets, source, and advanced
`localclash-user.json`. Stop the managed runtime first. Inspect the plan and
type the requested confirmation, or use `--yes` for an authorized
noninteractive reset.

Full reset deletes an installed workspace including downloaded assets and
the user fragment:

```bash
localclash reset --full --workspace /root/localclash
```

It requires an explicit workspace, `.localclash-workspace` marker, and its
own confirmation; source checkouts and protected paths are refused.

## Releases and Verification

Core and LuCI are separate release channels. Core releases contain Linux
binaries, base assets, checksums, and `localclash-release-manifest.json`.
LuCI consumes the manifest for online updates; a Core release does not
require a LuCI release. LuCI offline bundles pin Core separately.

- [更新日誌](docs/changelog.md) supplies X cards and Telegram announcements.
- [功能測試表](docs/istoreos-test-features.md) preserves last tested versions,
  failures, partial results, and evidence.
- [測試 Skill](.codex/skills/localclash-istoreos-test/SKILL.md) defines affected
  QEMU selection, real subscription scenes, upgrade provenance, and external
  oracles. Documentation-only changes need documentation checks; a release
  does not require rerunning the whole table.
- [發布與公告 Skill](.codex/skills/localclash-release-broadcast/SKILL.md)
  owns release checks, immutable LuCI tags, and authorized announcements.

Use code, schemas, templates, and workflows for current implementation details.
For development checks, run `go test ./...` and MCP smoke scripts
`scripts/test-mcp-http.sh` / `scripts/test-mcp-cli.sh` when affected.
CI/package checks do not replace selected QEMU functional acceptance.

<a id="支持-localclash"></a>
## 支持 localClash

### 中文

localClash 是免費項目。

如果它幫你節省了配置、調試、排錯或日常使用的時間，可以自願贊賞支持後續維護。
贊賞會用於持續維護、測試、文檔和開發。

使用 localClash 不需要捐贈或贊賞。

贊賞不構成購買、訂閱、會員、授權或付費技術支持，也不代表任何功能承諾、
回覆時效承諾、優先支持承諾或付費技術支持權益。

#### USDT

你可以通過 USDT 支持 localClash。

- 網絡：Ethereum / ERC-20
- 收款地址：`0x932eC04e3806b6BaC0E47Ba947b55bc023105Bec`
- ENS 別名：`ronniewong.eth`

金額自定。

請只向上述地址發送 Ethereum / ERC-20 網絡上的 USDT。使用其他網絡、其他
代幣或錯誤地址可能導致資產無法找回。若錢包支持 ENS，也可以輸入
`ronniewong.eth`，但發送前請確認它解析到上述地址。

請勿使用測試帳戶、重複極小額轉帳或自動化付款方式進行測試。如發現異常交易，
作者可能會移除公開支持入口。

### English

localClash is free to use.

If it saves you time on configuration, debugging, troubleshooting, or daily
usage, you may voluntarily support its maintenance. Support helps with ongoing
maintenance, testing, documentation, and development.

You are not required to donate or provide support to use localClash.

Voluntary support is not a purchase, subscription, membership, license, or paid
support service. It does not create any feature commitment, response-time
commitment, priority support commitment, or paid technical support entitlement.

#### USDT

You may support localClash with USDT.

- Network: Ethereum / ERC-20
- Address: `0x932eC04e3806b6BaC0E47Ba947b55bc023105Bec`
- ENS alias: `ronniewong.eth`

Amount is voluntary.

Please send only USDT on the Ethereum / ERC-20 network to the address above.
Using another network, token, or address may make the transfer unrecoverable. If
your wallet supports ENS, you may enter `ronniewong.eth`, but please confirm that
it resolves to the address above before sending.

Please do not use test accounts, repeated micro-transfers, or automated payment
behavior. If abnormal transactions are observed, the public support entry may be
removed.


## Community

Thanks to [LINUX DO](https://linux.do/) for providing a community for sharing
and discussing open-source projects.

## License

localClash is released under the MIT License. See [LICENSE](LICENSE).
