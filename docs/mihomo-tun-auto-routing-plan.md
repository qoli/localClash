# Mihomo TUN Auto-Routing Ownership Plan

Status: draft; source investigation completed on 2026-07-12, localClash
implementation not started.

## Current Status

localClash currently uses a split ownership model on OpenWrt:

- Mihomo creates the `mixed` TUN device, but the built-in router profile
  explicitly sets `auto-route`, `auto-detect-interface`, and `auto-redirect` to
  `false`.
- `router_takeover_apply` separately installs localClash-owned policy routing,
  TCP redirect, UDP/ICMP marking, DNS hijack, TUN forwarding, and local DNS
  bypass rules.
- `router_takeover_status`, `router_takeover_stop`, runtime lifecycle guards,
  and LuCI restore behavior all understand only this localClash-owned backend.

Mihomo and its `sing-tun` dependency already implement all three automatic
features on Linux. OpenWrt uses that Linux implementation, and `sing-tun` also
contains an explicit `fw4` adapter. That upstream capability is real, but it is
not yet a supported localClash product mode.

This plan proposes an explicit, initially opt-in OpenWrt mode in which Mihomo
becomes the sole owner of TUN routing and firewall redirect state. It does not
change the current router default until the acceptance gates in this document
have passed.

## Decision Summary

1. Add a distinct router runtime mode, provisionally named `router-auto`, for
   Mihomo-owned routing. Keep the existing `router` mode localClash-owned.
2. Treat the three settings as one atomic ownership contract. `router-auto`
   requires all three values to be explicitly `true`; the existing `router`
   mode requires all three to be explicitly `false`.
3. Never run Mihomo auto-routing and `router_takeover_apply` concurrently.
4. Limit the first supported target to modern OpenWrt with active
   `fw4`/nftables, one ordinary main-table default route, and no external PBR or
   multi-WAN owner.
5. Do not silently fall back to iptables or the localClash-managed backend.
   Missing capabilities, mixed ownership, or unverifiable cleanup are hard
   failures.
6. Treat improved performance as a hypothesis until the current backend and
   the new backend are benchmarked on identical traffic and core builds.

## Goal And Performance Hypothesis

The product goal is to support these Mihomo TUN features as one coherent mode:

| Setting | Runtime responsibility | Expected value |
| --- | --- | --- |
| `auto-route` | Installs OS routes and policy rules that direct traffic into the TUN device. | Removes localClash's separate fwmark route owner; primarily an ownership and correctness change. |
| `auto-detect-interface` | Detects the current outbound interface and binds Mihomo-created sockets to it. | Prevents outbound traffic from looping back into TUN and follows ordinary WAN changes. It is not by itself a data-plane acceleration. |
| `auto-redirect` | Redirects TCP into Mihomo's internal redirect listener while UDP and ICMP continue through TUN/policy routing. | Can reduce TCP work compared with a pure-TUN path and keeps redirect rules aligned with Mihomo's TUN lifecycle. |

The current localClash backend already redirects TCP and sends UDP/ICMP through
TUN. Therefore, enabling `auto-redirect` does not automatically prove a speed
gain over the current product. The likely benefits are unified ownership,
fewer localClash-maintained packet-capture rules, and potentially better
upstream optimization. CPU, throughput, latency, connection failure rate, and
restart behavior must be measured before release notes claim improved
performance.

## Current And Proposed Ownership

Current router flow:

```text
config_render
  -> Mihomo starts TUN with the three auto settings disabled
  -> explicit router_takeover_apply
  -> localClash owns route table 27747, fwmark 0x6c63, rule pref 1890, fw4 rules, and DNS bypass
```

Proposed `router-auto` flow:

```text
config_render
  -> exact auto-routing contract validation
  -> OpenWrt capability and ownership preflight
  -> Mihomo process start
  -> Mihomo/sing-tun owns table 2022, rules 9000..9010, inet mihomo, and fw4 include
  -> bounded post-start verification
```

The numeric route and mark values used by the two implementations are
different, but that does not make concurrent operation safe. Both paths would
own packet capture and TUN delivery. Hook priority and installation order would
decide which rules win, producing an unsupported dual-owner state.

## Exact Configuration Contract

For router runtime modes, the rendered TUN block is an ownership declaration:

| Runtime mode | `auto-route` | `auto-detect-interface` | `auto-redirect` | Owner |
| --- | ---: | ---: | ---: | --- |
| `router` | `false` | `false` | `false` | localClash through `router_takeover_*` |
| `router-auto` | `true` | `true` | `true` | Mihomo through process lifecycle |
| Any mixed, missing, or non-boolean combination | invalid | invalid | invalid | Render/start must fail |

The built-in `router-auto` template must write all three values explicitly. It
must not rely on Mihomo defaults. It should also explicitly write the expected
`iproute2-table-index` and `iproute2-rule-index` instead of relying on hidden
`sing-tun` defaults.

If `localclash-user.json` replaces the built-in runtime template while
`router-auto` is selected, localClash must validate that the user profile
contains the exact three-value contract. It must not backfill missing values or
repair a partial combination.

The generated config expresses desired ownership. Loaded Mihomo config and
live route/firewall artifacts express effective ownership. Status must compare
the two rather than assuming that a successful render or a live process proves
takeover.

## Upstream Capability And OpenWrt Boundary

The source investigation established the following behavior:

- Mihomo enables `auto-redirect` only in Linux builds. Non-Linux builds force
  it off. OpenWrt is inside the supported Linux path.
- `auto-redirect` requires `auto-route`; an invalid combination fails TUN
  listener creation.
- On Linux, `sing-tun` normally uses nftables and can otherwise select an
  iptables implementation. localClash `router-auto` will require the nftables
  path and reject iptables fallback.
- The nftables implementation creates `table inet mihomo`, redirects TCP from
  output/prerouting, handles DNS, and coordinates UDP/ICMP with TUN policy
  routing.
- On OpenWrt it detects `table inet fw4` and the `fw4` executable, writes
  `/etc/nftables.d/0-mihomo-auto-redirect.nft`, and reloads `fw4` to accept TUN
  input and forwarding.
- `auto-detect-interface` reads the Linux main routing table and selects the
  first default route it encounters. It does not understand `mwan3`, OpenWrt
  PBR policy, or arbitrary non-main routing tables.
- The default route table is `2022`; default policy-rule priorities begin at
  `9000`. Auto-redirect mark mode additionally uses fallback priority `32768`.
  Cleanup is based heavily on these numeric identifiers rather than a strong
  owner tag.
- Graceful close attempts to remove the nft table and fw4 include. A crash,
  `SIGKILL`, or power loss can leave artifacts, and several upstream cleanup
  errors are ignored.
- Mihomo hot reload closes the old TUN before creating the new one. TUN
  recreation failure may be visible only in logs while the controller request
  still reports success.
- Direct unit or integration coverage for this complete OpenWrt route,
  firewall, reload, and cleanup path is absent in the inspected upstream
  versions.

At investigation time, the local Meta source and readable Meta binary metadata
used `sing-tun v0.4.18`, while the inspected Alpha source used `v0.4.20`. The
checked-in Smart binary is UPX-packed and does not expose readable Go build
metadata through `go version -m`, so its exact embedded dependency must be made
auditable through release metadata instead of inferred from the filename. The
first localClash rollout should require both managed core variants to use a
reviewed `sing-tun` version at least as new as `v0.4.20`.

## OpenWrt Preflight Contract

Before starting Mihomo in `router-auto`, localClash must prove every required
invariant:

- the host is Linux and positively identified as OpenWrt;
- the selected managed core and embedded `sing-tun` version are approved;
- the process has the privileges required for TUN, netlink, nftables, and fw4;
- `/dev/net/tun`, `ip`, `nft`, and `fw4` are available;
- `table inet fw4` and the required base chains are active;
- `/etc/nftables.d` is writable;
- `DISABLE_NFTABLES` is not enabled;
- no unmanaged or second Mihomo process/TUN listener is active, and any
  existing managed runtime is handled through an explicit restart path;
- localClash-managed takeover chains, rules, and same-boot repair state are
  absent;
- route table `2022` and rule priorities `9000..9010` are not owned by another
  subsystem; priority `32768` and Mihomo's redirect marks must also be free if
  a later release enables auto-redirect mark mode;
- `table inet mihomo` and
  `/etc/nftables.d/0-mihomo-auto-redirect.nft` are either absent or supported by
  a matching localClash ownership attestation;
- a global `interface-name` does not override the requested automatic
  interface detection;
- `mwan3`, OpenWrt PBR, multiple competing default routes, and flow offload
  configurations that have not passed the acceptance matrix are absent;
- the router's local DNS addresses and LAN-local domains have a verified
  preservation strategy.

Any failed check must name the failed invariant and stop before process start.
Preflight must not delete conflicting rules whose ownership cannot be proven.

After process start, localClash must verify the nftables implementation is
actually active. The presence of Mihomo-created iptables chains is a forbidden
nftables-to-iptables fallback and must fail activation.

## LAN DNS Is A Blocking Parity Requirement

The current localClash takeover discovers OpenWrt LAN DNS addresses and local
domains, then installs local DNS bypass rules before broad DNS hijack. This was
added after DHCP hostnames such as `Ronnie-PC` stopped resolving when queries
to the router's dnsmasq instance were redirected into Mihomo.

The inspected `sing-tun` nftables order evaluates route-exclude addresses before
DNS hijack, but its generic local-address exclusion occurs after DNS hijack.
Turning on `auto-redirect` without an additional localClash integration can
therefore reintroduce the dnsmasq/DHCP hostname regression.

Before `router-auto` reaches a live router, localClash must provide and verify
one explicit solution. The preferred first candidate is to discover the
current OpenWrt LAN DNS addresses before Mihomo starts and place them in the
appropriate IPv4/IPv6 route-exclude fields so they return before auto DNS
hijack. The design must still define how address changes invalidate and
regenerate the config. It must not hard-code one router address or assume every
LAN uses `.lan`.

## Lifecycle And Transition Rules

Changing ownership is a network migration, not a normal boolean edit.

### LocalClash-managed to Mihomo-managed

1. Render the `router-auto` candidate and validate its exact contract.
2. Run `mihomo -t` against the candidate.
3. Complete OpenWrt preflight while the current owner is still observable.
4. Explicitly stop and verify the localClash-managed takeover.
5. Perform a process restart into the promoted `router-auto` config.
6. Verify loaded config, TUN, routes, rules, nft table, fw4 include, DNS
   preservation, and the absence of legacy ownership.
7. Write a bounded ownership attestation only after all checks pass.

The transition necessarily contains a short capture interruption. It must be a
confirmed operation and should first run from a local console or an isolated
OpenWrt test target so the control connection is not mistaken for a reliable
rollback channel.

### Mihomo-managed to LocalClash-managed

1. Stop Mihomo gracefully and verify all Mihomo-owned route/firewall artifacts
   are absent.
2. Render and test the existing `router` config.
3. Start Mihomo with the three auto settings disabled.
4. Explicitly apply and verify the localClash-managed takeover.

Backend changes must reject hot reload and require a process restart. There is
no automatic fallback or automatic restoration to the other backend. A failed
activation may stop the newly started process and verify cleanup, but it must
return the original error and require an explicit next action.

`stop_runtime` must understand that stopping Mihomo in `router-auto` also
removes the active takeover and can interrupt the router network. A graceful
`SIGTERM` is the normal cleanup path. If the user explicitly forces `SIGKILL`,
status must report `cleanup_required: true` until every persistent and runtime
artifact has been verified absent.

Selecting `router-auto` must not silently enable boot-time restoration. Existing
same-boot hotplug repair for the localClash-managed backend must not run in this
mode. An explicit boot auto-restore setting may start Mihomo after boot, at
which point Mihomo owns takeover as part of process startup.

## Status And Ownership Attestation

Router status needs to represent desired state, loaded state, and observed
artifacts independently. At minimum it should expose:

```text
desired_owner
loaded_owner
artifact_owner
effective
cleanup_required
config_hash
loaded_tun_flags
checks[]
conflicts[]
```

Observed owner states should include `none`, `localclash`, `mihomo`, `partial`,
and `conflict`. `effective` is true only when the desired and observed owner
match, every required artifact is present, and the other backend is absent.

The Mihomo ownership attestation should record the managed PID, config hash,
TUN device, route table/rule indices, nft table, fw4 include path, and artifact
fingerprints. Stale cleanup is permitted only when no managed runtime is alive
and the attestation matches the artifacts to be removed. Missing or mismatched
attestation must result in an explicit refusal rather than broad deletion.

## Implementation Areas

| Area | Required change |
| --- | --- |
| `internal/runtimeprofile` | Add the explicit `router-auto` profile/mode, validate the exact three-value contract, expose all three values in status, and stop instructing auto mode to call manual takeover. |
| `internal/configrender` | Validate ownership before writing config; expose desired owner and fixed indices in render results; fail incompatible user profiles. |
| `internal/configplan` | Include runtime-profile/ownership hashes in drafts and reject apply when the contract changed after review. Config-test success must not be presented as OpenWrt preflight success. |
| New bounded package such as `internal/tunautomation` | Own typed contract parsing, preflight, artifact inspection, attestation, and verified stale cleanup without creating import cycles. |
| `internal/routertakeover` | Dispatch status by expected owner; reject manual apply/stop semantics when Mihomo owns takeover; detect dual ownership. |
| `internal/corerun` | Run preflight before auto-mode start, bounded verification after start, graceful failure cleanup, and verified stop semantics. |
| `internal/doctor` | Report platform/core capability, contract validity, ownership conflicts, nft/fw4 prerequisites, DNS preservation readiness, and cleanup-required state. |
| `internal/mcp` and product CLI | Update configure/start/restart/stop/status contracts and safety messages; backend switches remain confirmation-required. |
| `../localclash-luci` | Expose the new mode and status without moving routing logic into LuCI; keep explicit boot auto-restore separate from same-boot repair. |

The current implementation anchors are the built-in
[router profile](../internal/runtimeprofile/profiles/router.default.json),
[runtime profile model](../internal/runtimeprofile/profile.go),
[renderer](../internal/configrender/render.go), and
[router takeover implementation](../internal/routertakeover/routertakeover.go).

## Acceptance Matrix

### Unit And Contract Tests

- Existing `router` renders all three values explicitly false and remains
  localClash-owned.
- `router-auto` renders all three values explicitly true plus explicit route
  table/rule indices.
- Missing, non-boolean, or mixed values fail rendering without producing a
  valid-looking output.
- `router_takeover_apply` refuses Mihomo ownership; start/restart refuses dual
  ownership.
- Unsupported platform, core version, fw4/nft state, route collision, stale
  artifact, or forbidden iptables fallback fails explicitly.
- A profile or ownership change after config-plan review invalidates apply.
- Process liveness without required route/firewall artifacts reports
  `effective: false`.
- Stale cleanup without a matching attestation removes nothing.
- `auto-detect-interface` appears in runtime-profile and router status output.

Go tests remain necessary but are not a functional OpenWrt acceptance gate.
The current takeover tests already document this limitation.

### Docker OpenWrt

Use the prepared OpenWrt Docker environment for destructive integration tests:

- LAN-forwarded TCP reaches Mihomo through REDIRECT with the original
  destination intact.
- LAN UDP and ICMP use TUN without routing loops.
- Router-local TCP/UDP traffic exits through the detected WAN interface.
- IPv4 and IPv6 routing, DNS, and cleanup pass independently.
- Queries to the router's dnsmasq instance preserve DHCP hostnames and local
  domains while ordinary public DNS hijack remains effective.
- `fw4 reload`, WAN ifup/ifupdate, and default-interface change leave status
  effective or produce an explicit failure state.
- Graceful stop removes routes, rules, `inet mihomo`, the fw4 include, and TUN.
- `SIGKILL` produces `cleanup_required` and a subsequent attested cleanup or
  restart repairs it.
- Ten consecutive process restarts do not duplicate or leak rules, tables, or
  files.
- Manual and Mihomo artifacts together always report a conflict.

Run the first matrix with software and hardware flow offload disabled. Flow
offload becomes supported only after a separate on/off comparison proves that
traffic is not bypassing the intended path.

### Performance Comparison

Use the same core build, config, proxy node, client, and traffic shape for both
backends. Record at least:

- Mihomo and system CPU during sustained TCP and UDP traffic;
- TCP throughput and UDP loss/jitter;
- connection setup and p50/p95 request latency;
- DNS latency and failure rate;
- memory use and tracked connection count;
- process start, restart, and takeover-ready time;
- packet/rule counters proving which capture path handled the traffic.

The low-resource UTM OpenWrt environment can expose CPU, memory, and restart
regressions. Docker is suitable for lifecycle correctness. Neither x86 target
proves behavior or performance on the ARM64 production router, so the final
Meta and Smart canaries must run on an isolated ARM64 router with explicit user
authorization.

Performance promotion requires measured improvement or meaningful operational
simplification without a material performance regression. The numeric release
threshold should be set after a stable baseline is captured, not invented in
advance.

## Rollout Sequence

1. **Source and baseline — partially complete.** Preserve this investigation
   and capture repeatable performance measurements for the current backend.
2. **Core readiness.** Move both managed core variants to an approved
   `sing-tun` baseline and record their source revisions in release metadata.
3. **Contract and observability.** Add `router-auto`, exact flag validation,
   preflight, ownership status, attestation, and hard failure tests before any
   live activation path.
4. **Lifecycle support.** Implement process-restart-only transitions, verified
   start/stop, DNS preservation, stale cleanup, and MCP/CLI safety contracts.
5. **Isolated OpenWrt canary.** Pass the Docker matrix, then UTM performance
   checks, then an explicitly authorized ARM64 router canary.
6. **Opt-in release.** Expose the mode in LuCI/MCP while retaining the current
   `router` default.
7. **Default decision.** Consider changing the default only after Meta and
   Smart results show stable lifecycle behavior and the performance claim has
   measured evidence.

## Non-Goals For The First Release

- non-Linux platforms;
- generic Linux distributions that are not positively identified as OpenWrt;
- legacy OpenWrt `fw3` or iptables takeover;
- `mwan3`, OpenWrt PBR, arbitrary custom route tables/marks, or multiple TUN
  listeners;
- route-address-set auto-redirect mark mode;
- automatic fallback between takeover backends;
- automatic deletion of artifacts without proven ownership;
- claiming a performance improvement before comparative measurements exist;
- making LuCI the owner of route/firewall implementation logic.

## Relationship To Existing Documents

- [Mihomo API Hot Reload Development Plan](mihomo-api-hot-reload-plan.md)
  defines config-test and reload semantics. This plan overrides hot reload for
  takeover-owner transitions by requiring process restart.
- [OpenWrt Test Environments](openwrt-test-environments.md) defines which
  environment may run destructive lifecycle and performance checks.
- [Router Incident Register](router-incident-register.md) records the fw4
  reload and local DNS regressions that this mode must not reintroduce.
- [OpenWrt LuCI Support](openwrt-luci.md) defines the core/LuCI ownership
  boundary. The core remains responsible for this mode's contract and state.
- [First Use](first-use.md) and [MCP](mcp.md) should change only when the new
  mode is implemented and ready for users.

## Remaining Design Questions

- What deterministic artifact should carry discovered LAN DNS exclusions into
  config render without making the renderer depend on unrecorded host state?
- Which exact managed Mihomo/`sing-tun` revisions become the minimum supported
  Meta and Smart baselines?
- What measured threshold is required before `router-auto` can be described as
  a performance improvement rather than an ownership simplification?
- Which OpenWrt flow-offload configurations can graduate from the initial
  unsupported set after canary testing?

## Source Basis

The investigation compared the local Mihomo Meta and Alpha source checkouts,
their declared `sing-tun` module versions, and these principal upstream paths:

- Mihomo `config/config.go` and `listener/sing_tun/server.go` for defaults,
  platform guards, monitor setup, and the auto-redirect requirement;
- Mihomo `listener/listener.go` and `hub/route/configs.go` for TUN recreation
  and controller error propagation;
- `sing-tun/redirect_linux.go`, `redirect_nftables.go`,
  `redirect_nftables_rules.go`, and
  `redirect_nftables_rules_openwrt.go` for Linux/OpenWrt routing and firewall
  behavior;
- `sing-tun/tun_linux.go` and `monitor_linux_default.go` for route ownership,
  cleanup, and default-interface detection.

Re-check these paths and the actual bundled core metadata before implementation
or release because upstream behavior and dependency versions can change.
