# Router Incident Register

This document records router-facing usability and performance incidents that
must be investigated with evidence. Do not treat post-removal or wrong-window
samples as proof for an incident.

## 2026-09-01 Smart Probe Listener Readiness Produced False G204 Zero

Status: fixed in source; repeated ARM64 WAN-level acceptance passed; pending release.

Observed symptom:

- Three real sources downloaded and merged successfully to 456 proxies, then the
  g204 stage failed after 46.224 seconds with
  `automatic connectivity qualification collapsed: 20 previously-qualified endpoints failed`.
- The old collapse path discarded the candidate snapshot, so the task retained
  neither individual endpoint errors nor the observed empty result and reported
  the entire subscription setup as `command_failed`.
- On the same physical ARM64 router and same Smart core, isolated retries shortly
  afterward qualified nonzero g204 sets. The zero therefore required separating
  candidate observations from the isolated Smart process and its listeners.
- The disposable iStoreOS VM's upstream is the 6.1 host's transparent proxy, not
  a bare WAN path. Its naturally qualified counts cannot represent the physical
  router's WAN ingress quality; VM evidence is limited to deterministic control
  flow, generated structure, and Smart config validation.
- With four fixed subscription bodies and 530 merged proxies, a failing ARM run
  completed g204 in about 10 seconds with zero qualified nodes. Its snapshot held
  152 loopback `connection refused`, 125 `connection reset by peer`, 100
  `unexpected EOF`, and 14 `EOF` observations. Memory, swap, OOM kills and file
  descriptor limits did not explain that correlated local failure.
- The isolated Smart startup gate waited only for the first candidate listener.
  Workers then contacted all 389 or 390 listeners while the slower ARM core was
  still creating the later listeners. The faster x86_64 environment hid this
  startup race.

Root-cause resolution:

- Isolated probe startup now waits for every candidate listener before launching
  HTTP workers. A 30-second readiness failure reports the exact ready/expected
  listener count instead of publishing node failures.
- The probe session retains the tail of Smart output, reports a post-readiness
  process exit, and verifies all listeners again after workers finish. A process
  or listener infrastructure failure therefore fails the refresh and cannot
  enter the empty-result fallback path.

Empty-measurement handling:

- Completed g204 and ChatGPT measurements now publish explicit empty snapshots.
  Structural candidate errors and probe infrastructure errors remain failures.
- ChatGPT receives only the same-refresh g204 list; an empty list produces an
  empty ChatGPT snapshot without probing all subscription nodes.
- CLI subscription refresh can commit subscription artifacts plus the two empty
  capability facts. When g204 is empty, `⚡ 自动选择` uses its original
  all-subscription-nodes automatic structure.
- MCP atomically promotes the empty snapshot set and renders the fallback
  automatic group. ChatGPT stays empty; no stale or alternate qualification
  result is presented as g204-qualified.

Acceptance evidence:

- A dedicated WireGuard interface connects iStoreOS QEMU to the physical router.
  Its default route uses the tunnel while an explicit endpoint route remains on
  QEMU NAT. The router forwards that interface only to WAN and places it in the
  localClash takeover bypass set. Before the run, the VM public IPv4 digest was
  read back equal to the router WAN digest; WireGuard handshake and transfer
  counters increased during the probes.
- The same fixed four subscription bodies produced 530 proxies in both
  environments. The WireGuard/x86_64 control qualified 234 g204 and 86 ChatGPT
  endpoints. This control proves the VM used the intended WAN entry; it is not
  ARM behavior proof.
- The readiness-fixed ARM64 candidate ran three consecutive refreshes through
  the physical router WAN. G204 qualified 86, 69 and 67 endpoints; ChatGPT
  qualified 19, 17 and 20. All three runs exited 0, each g204 pass took about
  112 seconds, and none reported an isolated-process or listener failure.
- The router's production subscription store, generated config and active Core
  binary were not replaced for this acceptance. Fixed inputs and candidate
  binaries ran only from isolated temporary directories.

## 2026-09-01 Large Subscription Capability Refresh Hit Fixed Step Timeout

Status: fixed in source; pending router deployment and acceptance.

Observed symptom:

- Four subscriptions produced 533 merged proxies. The g204 capability excluded
  18 referenced helpers, deduplicated 119 endpoints, and probed 394 candidates in
  99.777 seconds, qualifying 233.
- ChatGPT qualification then restarted from all 533 proxies. The LuCI helper
  terminated the complete subscription-refresh step at its fixed 240-second
  wall-clock limit before the second capability completed.
- The Core product CLI also imposed a fixed five-minute parent deadline even
  though the finite work scales with candidate count and 16-worker batches.

Resolution:

- ChatGPT qualification consumes only the current g204-qualified unique endpoint
  names while retaining the complete proxy definitions needed by dialer chains.
  `ChatGPT-available` is therefore a strict subset of the same-refresh g204 result.
- ChatGPT allows one retry after an initial failed Statsig observation. Either
  attempt may qualify the candidate; two failures remove it without cross-refresh
  failure hysteresis.
- Product CLI and MCP subscription refresh no longer add a node-count-independent
  parent deadline. Individual subscription downloads and capability HTTP requests
  remain bounded, and caller cancellation still propagates through the operation.
- LuCI waits for Core-owned subscription and policy-material transactions to
  complete while retaining heartbeat output and explicit task cancellation. Its
  fixed step timeout remains in force for other helper-owned operations.

Required acceptance:

- Deploy matching Core and LuCI builds to the router, repeat the same four-source
  refresh, and prove ChatGPT starts with exactly the 233 g204-qualified candidates
  from that run rather than all 533 merged proxies.
- Read back a terminal successful task result, promoted capability snapshots,
  generated-config validation, loaded proxy-group membership, runtime health, and
  effective router takeover.

## 2026-09-01 One-Click Policy Migration Used Stale Capability State

Status: fixed in source; full iStoreOS acceptance passed; pending release.

Observed symptom:

- A v0.1.70 to v0.1.71 one-click update refreshed subscriptions against the old
  intent, then installed the new default policy containing
  `network.connectivity.g204.v1`.
- Rendering failed because `auto-available.json` had not been requested by the
  old intent. The failed run nevertheless left the new intent installed.
- A retry discovered g204, but one subscription candidate had an NXDOMAIN first
  hop. Candidate construction treated that single DNS failure as a fatal error
  for the complete capability refresh.

Resolution:

- Core now owns one rollback-protected template material transaction covering
  template patches, compiled intent, subscription artifacts, capability
  snapshots, selection, generated config, runtime profile, validation cache,
  and attestation.
- LuCI one-click update and configured default bootstrap request that transaction
  instead of sequencing subscription refresh and template import independently.
- A DNS-unresolvable g204 first hop is stored as an explicit candidate-level
  unavailable observation. Structural graph errors and an all-unavailable result
  remain hard failures; no fallback candidate set is generated.

Acceptance evidence:

- A clean disposable iStoreOS VM started from the public v0.1.0-62 LuCI package
  and v0.1.70 Core. Default-policy synchronization stayed enabled; no saved
  subscription, capability, or generated-config artifact from the earlier
  incident replay was reused as the acceptance baseline.
- The actual one-click RPC downloaded and verified the LuCI package, installed
  it, re-executed the replaced helper, updated Core, installed the public
  dnsqualify v0.1.0-63 asset, updated both Mihomo flavors and Dashboard, refreshed
  both real subscriptions, synchronized the complete default policy, rebuilt all
  configured capabilities, ran `mihomo -t`, hot-reloaded the runtime, and restored
  takeover. It completed with exit code 0.
- The two real sources supplied 20 and 22 nodes. The g204 profile received 42
  candidates, probed 20 resolved and deduplicated first hops, qualified all 20,
  and recorded 2 DNS-unresolvable candidates as unavailable. The ChatGPT profile
  probed all 42 candidates. The material transaction reported `committed: true`.
- A separate subscription-refresh RPC repeated both real downloads and all
  capability probes. The resulting 42-proxy / 63-rule config passed an isolated
  `mihomo -t`; the subsequent LuCI runtime restart preserved
  `runtime_running: true`, `takeover_effective: true`, router profile, controller
  health, and the promoted config SHA256
  `05c6a55f7cf30043440cb5264609e7dc3f0d1fd855bc309c9dbaead7acbdba01`.
- A post-success fault injection removed the real candidate-probe Mihomo binary
  before repeating the full template, subscription, capability, render, and
  validation transaction. The command failed with `prior state was restored`;
  the aggregate digest across all 11 protected material paths was
  `1be0b0da73efd59ae1ca2c0a896d27c124186279574ea4504b862f1d5b1b83e5`
  both before and after the failure. The active runtime, controller, ownership
  marker, and takeover remained effective.
- LuCI was then exercised through the existing Arc session. The test stopped
  takeover while leaving Mihomo running, observed the Overview state
  `runtime=true` / `takeover ineffective`, and confirmed that the conditional
  `应用接管` action appeared in the network-takeover table. Clicking that
  Overview action showed its in-progress state and restored the final UI and
  backend observations to runtime running and takeover effective. LuCI resources
  and UBus requests returned HTTP 200 with no browser console errors.

## 2026-09-01 Runtime Watchdog Does Not Reconcile Router Takeover

Status: known issue; recovery ownership is unresolved.

Observed symptom:

- When a supervised Mihomo process exits, the Core runtime watchdog can restart
  it and verify the replacement process and controller health successfully.
- The same recovery does not verify or restore LuCI-owned router takeover.
  The resulting state can therefore be `runtime_running: true` while
  `takeover_effective: false`.
- From the user's perspective, Mihomo appears to have recovered but router
  traffic remains outside the expected localClash takeover path.

Evidence captured in the iStoreOS test environment on 2026-09-01:

- A fault-injection run terminated the supervised Mihomo PID `29711` while a
  one-click update was in progress.
- Core watchdog events recorded `runtime_exit_observed`,
  `runtime_restart_attempt`, and `runtime_restart_recovered`; the recovered PID
  was `2916`.
- The same-boot takeover repair ticket still contained `applied`, but Core
  watchdog recovery did not consume that intent or inspect
  `takeover_effective`.
- The one-click update failure finalizer added in LuCI commit `633c1e1`
  independently detected the ineffective takeover, applied it, and verified
  the final healthy state. This proves that the update-scoped mitigation works;
  it does not provide general watchdog recovery.

Current responsibility boundary:

- Core owns Mihomo runtime supervision, managed-process identity, validated
  runtime inputs, controller health, bounded restart attempts, and watchdog
  events.
- LuCI owns fw4/nft rules, policy routing, DNS hijack, takeover intent and
  repair markers, and `status`/`apply`/`stop`/`reconcile` operations.
- Core must not execute LuCI takeover commands merely because it restarted a
  Mihomo process.
- No component currently owns reconciliation between the Core event
  `runtime_restart_recovered` and the LuCI observation
  `takeover_effective: false`.

Unresolved design questions:

- Should LuCI react to a versioned Core recovery event, periodically reconcile
  desired and observed takeover state, or use an OpenWrt/procd lifecycle hook?
- Which component owns the transaction when runtime recovery overlaps an
  intentional start, stop, restart, update, cancellation, or package re-exec?
- How should reconciliation prove same-boot takeover intent without turning a
  temporary repair ticket into persistent boot policy?
- What lock, generation, or transaction identity prevents a recovery worker
  from re-applying takeover during an intentional transition?
- Where should bounded retry, terminal failure, and user-visible
  `attention_required` state live?

Safety boundary while unresolved:

- Do not make the Core watchdog directly invoke fw4, nft, UCI, policy-routing,
  or LuCI helper commands.
- Do not add an uncoordinated polling loop that applies takeover whenever
  `effective` is false; that can race intentional transitions and explicit
  user stop operations.
- Keep the update-scoped failure finalizer as a bounded mitigation, not as
  evidence that general runtime supervision now restores router takeover.

Required evidence before choosing an owner:

- A state-transition trace correlating update/task transaction identity, Core
  watchdog events, runtime PID, takeover repair intent, and observed takeover
  state.
- Reproductions for an unexpected runtime exit while idle, during one-click
  update, during explicit restart, and after task cancellation.
- Proof that explicit takeover stop remains stopped and cannot be undone by a
  delayed recovery worker.
- Verification that the selected design remains safe when the Core or LuCI
  package is upgraded independently and when recovery events are missed or
  duplicated.

## 2026-06-04 Router Reset Left Incomplete localClash Home

Observed symptom:

- After reinstalling localClash on a router that had been reset, startup failed
  during router takeover with `router_takeover_apply_failed`.
- The returned details included `profile_mode: "normal"`,
  `runtime_running: false`, and the next action
  `call config_configure with runtime_profile=router before
  router_takeover_apply`.
- This was confusing because the LuCI initialization path is expected to apply a
  router runtime profile before starting runtime takeover.

Current explanation:

- The router reset did not leave a clean first-run state. It left an incomplete
  user home/work directory under the Linux-style localClash state layout.
- localClash core path resolution is intentionally filesystem-based: the LuCI
  helper passes `LOCALCLASH_WORKDIR` to the core, and the core reads runtime
  profile, generated config, subscription state, and managed core paths from
  that work directory.
- When `localclash-runtime.json` is absent or the work directory is incomplete,
  `runtimeprofile.Load` creates the default runtime profile. The default mode is
  `normal`, so `router_takeover_apply` correctly refuses to apply router
  takeover.
- Therefore `profile_mode: "normal"` did not mean LuCI generated a normal
  router configuration. It meant the core observed an incomplete or mismatched
  working state at takeover time.

User recovery path:

- Guide the user through the LuCI `Advanced` -> full reset flow, then repeat the
  normal initialization flow from the overview page.
- Do not try to repair this class of state by manually calling
  `router_takeover_apply`; takeover depends on a coherent router runtime profile,
  rendered config, runtime process, and state directory.

Required evidence for the next similar report:

- `ubus call localclash status`
- `/root/localclash/localclash-runtime.json` presence and contents summary
- `LOCALCLASH_WORKDIR=/root/localclash /usr/local/bin/localclash config status
  --json`
- `LOCALCLASH_WORKDIR=/root/localclash /usr/local/bin/localclash doctor --json`
- recent `/tmp/localclash-helper.log` lines around initialization, reset, runtime
  start, and takeover apply

Product follow-up:

- LuCI should surface an explicit incomplete-workdir diagnosis when the core is
  installed but the runtime profile is `normal` or default-created while other
  expected router initialization artifacts are missing.
- The user-facing next action should be `Advanced -> Full Reset`, then normal
  initialization, instead of exposing only MCP-oriented recovery text such as
  `call config_configure with runtime_profile=router`.

## 2026-05-31 LuCI Reboot Restore Gap

Observed symptom:

- After rebooting the router, LuCI does not restore the localClash router
  network takeover path.
- The localClash-managed Mihomo runtime is also not restored automatically, so
  router traffic is not captured by the expected localClash runtime after boot.

Evidence boundary:

- This is currently a user-reported reboot recovery bug, not yet backed by a
  full boot-window log capture.
- Do not infer whether the missing restore is caused by the LuCI UI, ubus/rpcd
  helper, OpenWrt procd service configuration, localClash core startup, runtime
  config rendering, or router takeover apply until boot-time evidence is
  collected.

Router evidence captured on 2026-05-31:

- The router was a FriendlyElec NanoPi R5C running OpenWrt 24.10.0-rc5 with
  `luci-app-localclash` version `0.1.0-17`.
- `/etc/init.d/localclash` was absent. The installed boot service was
  `/etc/init.d/localclash-mcp`, with `/etc/rc.d/S95localclash-mcp` and
  `/etc/rc.d/K10localclash-mcp` links present.
- The generated procd script only starts `/usr/local/bin/localclash mcp serve
  --addr 0.0.0.0:8765 --path /mcp`; it does not call runtime start, runtime
  restart, or takeover apply during boot.
- Current live state was healthy at inspection time: `localclash runtime status
  --json` reported `running: true` for `lc-mihomo-smart`, and `localclash
  takeover status --json` reported `effective: true`.
- Current process timings showed the MCP server started at 2026-05-30 19:06:49
  CST and `lc-mihomo-smart` started at 2026-05-31 00:36:04 CST. This evidence
  does not prove boot-time restoration; it shows the current runtime was started
  later than the MCP service.
- `/tmp/localclash-helper.log` showed the one-time default initialization path
  starting runtime and applying takeover on 2026-05-30 16:18, and no durable
  boot-window restore log was present in `logread`.
- The LuCI rpcd helper exposes `runtime_start_takeover`, and that helper runs
  runtime start followed by takeover apply. The missing link is that the boot
  service does not invoke that helper after reboot.

Current explanation:

- localClash documents router takeover rules as runtime state: a reboot clears
  them.
- The missing product behavior is therefore not that nft/runtime takeover rules
  survive reboot; it is that the LuCI/OpenWrt integration should restore the
  configured runtime and re-apply takeover after boot when the user has enabled
  that mode.

Product requirement:

- A router reboot restore path must bring the configured localClash router mode
  back to the intended operational state after the user explicitly enables boot
  auto-restore.
- Restore should ensure the localClash service is running, the selected runtime
  profile and generated config are available, Mihomo is started, and
  localClash-owned router takeover is applied only after the runtime is ready.
- The restore path must be idempotent: repeated LuCI/service startup checks
  should not duplicate nft rules, spawn multiple Mihomo processes, or rewrite
  unrelated user state.
- Failure must be visible from LuCI and logs with enough detail to distinguish
  missing core binary, missing config, failed runtime start, failed takeover
  apply, and service supervision failures.

Current implementation note:

- The sibling LuCI package `0.1.0-19` adds explicit boot auto-restore helper
  methods: `boot_restore_status`, `boot_restore_enable`,
  `boot_restore_disable`, and `boot_restore_run`.
- A normal takeover apply should not create persistent reboot restore policy.
  Boot restore is controlled by the explicit LuCI/helper setting.

Required evidence for the next reproduction:

- OpenWrt boot timestamp and LuCI/localClash package versions.
- `logread` lines for localClash procd service startup, LuCI/rpcd helper calls,
  runtime start attempts, and takeover apply attempts.
- `service localclash status`, relevant procd init settings, and whether the
  service is enabled at boot.
- `ps` output for localClash and Mihomo after boot.
- localClash `runtime_status` and `router_takeover_status` after boot.
- Presence and contents summary for `localclash-runtime.json`,
  `.runtime/mihomo/config.yaml`, runtime PID files, and localClash MCP/service logs.
- nft/firewall state showing whether localClash-owned takeover chains or rules
  are absent, duplicated, or partially applied.

## 2026-05-31 WAN Firewall Reload Takeover Drift

Observed symptom:

- During the same boot session, localClash router takeover could become
  ineffective after WAN instability, while the Mihomo runtime itself continued
  running.
- This is similar to the reboot restore gap because both expose a missing
  restore path, but it is a separate failure mode: OpenWrt network churn reloads
  firewall state and can remove runtime-only nft hooks without rebooting.

Evidence captured on 2026-05-31:

- The WAN event was rooted below PPPoE: kernel logs showed repeated
  `igc ... eth0: NIC Link is Down` / `NIC Link is Up 1000 Mbps Full Duplex`
  transitions, and PPPoE PADO/PADS failures followed those carrier changes.
- OpenWrt then emitted firewall reload events for `ifup` / `ifupdate` of
  `wan` and `wan_6`. That explains how runtime-only localClash nft hooks can be
  lost even when Mihomo itself remains alive.
- A tactical LuCI package fix was deployed as `luci-app-localclash 0.1.0-18`.
  It added an iface hotplug restore hook and a `takeover_restore` helper path,
  but conflated same-boot repair with reboot restore intent.
- The follow-up LuCI package `0.1.0-19` implements the clean split: same-boot
  repair uses `/tmp` repair evidence, while boot auto-restore uses explicit
  persistent enable/disable helper methods.

Current explanation:

- localClash takeover rules remain runtime-only OpenWrt nft/firewall state.
- OpenWrt `fw4` reloads can remove those runtime-only hooks while leaving the
  Mihomo process, ports, and TUN device alive.
- The user-visible result is "network takeover is not effective", but the
  underlying state differs from a runtime crash or reboot: this is same-boot
  takeover drift after firewall reload.

Clean design requirement:

- Split same-boot repair from explicit boot auto-restore.
- Preserve the safety boundary that runtime takeover rules are not persistent
  firewall configuration.
- Split recovery into two explicit modes:
  - Same-boot repair: after `fw4` reload, WAN ifup/ifupdate, or similar OpenWrt
    network churn, restore takeover only when evidence under `/tmp` proves
    localClash takeover was applied during the current boot. This preserves the
    expectation that reboot clears operational takeover.
  - Boot auto-restore: restart Mihomo and re-apply takeover after router reboot
    only when the user has explicitly enabled a persistent LuCI setting for
    boot-time takeover restore.
- A plain `takeover_apply` must not silently create a durable boot auto-restore
  intent. If product UX needs boot restore, the UI and helper should name that
  setting directly and provide a matching disable path.
- `takeover_stop` and any explicit "disable takeover" action must clear the
  same-boot repair state. It must not silently disable the separate boot
  auto-restore setting; that policy needs its own visible enable/disable
  control.
- Logs and status output should distinguish `same_boot_repair`,
  `boot_auto_restore`, `manual_apply`, and `manual_stop`, so future incidents do
  not confuse a repair loop with an intentional boot policy.

Required verification for the final fix:

- Same-boot firewall reload or WAN ifupdate restores takeover only when current
  boot evidence shows localClash takeover had already been applied.
- Reboot alone clears operational takeover unless an explicit boot auto-restore
  setting is enabled.
- Enabling boot auto-restore is visible in LuCI/status output and disabling it
  prevents takeover from being restored after reboot.
- `takeover_stop` clears the same-boot repair state without changing the
  explicit boot auto-restore setting.

## 2026-05-29 DHCP Hostname DNS Hijack Regression

Observed symptom:

- LAN hostnames learned by OpenWrt DHCP, for example `Ronnie-PC`, could not be
  pinged by name while localClash router takeover was active.
- The same host was reachable by private IP, so the failure was not ICMP or
  basic LAN routing.

Evidence:

- The router DHCP lease table contained `Ronnie-PC` with a `192.168.6.x`
  address.
- `ping 192.168.6.x` from a LAN client succeeded.
- `nslookup Ronnie-PC 192.168.6.1` from the router returned the DHCP address,
  proving dnsmasq could answer when queried through the router LAN address.
- `dig @192.168.6.1 Ronnie-PC A` from a LAN client returned `NXDOMAIN`.
- In this incident, `192.168.6.1` was the user's current router LAN address. It
  is evidence from this network, not a product default or a portable assumption.
- The nft `localClash DNS hijack` counter increased during that LAN-client DNS
  query, proving the query was redirected to Mihomo DNS before dnsmasq could
  answer it.
- The active Mihomo DNS config listened on `0.0.0.0:7874` and used public DoH
  upstreams plus `geosite:gfw` policy, but did not have DHCP lease awareness or
  a local dnsmasq policy for DHCP hostnames.

Current explanation:

- Router takeover installs a broad prerouting DNS hijack:
  `meta l4proto { tcp, udp } th dport 53 redirect to :7874`.
- That rule captures client DNS queries even when the destination is the router
  LAN DNS service at `192.168.6.1:53`.
- Mihomo receives `Ronnie-PC` / `.lan` lookups but cannot answer from dnsmasq's
  DHCP lease table, so the client sees `NXDOMAIN`.

Product requirement:

- Router takeover must preserve OpenWrt local resolver behavior for DHCP
  hostnames, `.lan`, `.local`, `.home.arpa`, reverse private zones, and other
  LAN-local names.
- Router takeover must discover the active LAN DNS address and LAN domain from
  current OpenWrt state instead of hard-coding `192.168.6.1` or assuming `.lan`
  globally. Relevant evidence includes `network.lan`, interface addresses,
  dnsmasq `local`, dnsmasq `domain`, resolver search domains, DHCP lease files,
  and any configured local host records.
- DNS hijack must not turn local hostname lookups into public-DNS lookups.
- A future fix should either bypass router-destined DNS traffic before the
  hijack rule or configure Mihomo DNS to forward local zones to the router's
  local resolver. The implementation must be validated from a LAN client, not
  only from the router shell.

Required verification for the fix:

- From a LAN client, `dig @<discovered-lan-dns> Ronnie-PC A` returns the DHCP
  address.
- From a LAN client, `ping Ronnie-PC` resolves and reaches the same private IP.
- The discovered LAN DNS address and local domain/search suffix are reported in
  the verification output.
- The verification records whether the query bypassed Mihomo DNS or was
  forwarded through Mihomo to dnsmasq.
- Public DNS hijack for ordinary client traffic still works after the local
  hostname path is restored.

## 2026-05-25 CPU and Runtime Incidents

### localClash CPU Occupancy

Observed symptom:

- On the real router, localClash was reported to sometimes hold CPU near 100%.
- The router became difficult to operate, and localClash had to be removed to
  restore normal network usage.

Evidence boundary:

- A CPU sample taken after localClash had already been removed or stopped is not
  valid evidence for this incident.
- Docker OpenWrt did not reproduce the same severe CPU behavior, so the issue is
  likely tied to real-router workload, hardware, process supervision, traffic,
  filesystem, DNS, or request behavior.

Open questions:

- Which process name and PID owned the CPU during the incident?
- Was the hot path MCP HTTP request handling, config rendering, subscription
  refresh, runtime control, DNS interaction, file IO, or a retry loop?
- Did LuCI ubus requests wait on localClash long enough to stack pressure on a
  slow router?

Required evidence for the next reproduction:

- timestamped `top` or `ps` samples with PID, command, `%CPU`, `%MEM`, RSS, and
  full command line
- localClash MCP request summaries with tool name, duration, result, and error
- runtime state transition logs around start, stop, restart, takeover apply, and
  takeover stop
- process supervision logs showing restarts, exits, or respawn loops

### Mihomo CPU and Warning Volume

Observed symptom:

- Mihomo CPU was reported to reach about 14% on the real router.
- The router also showed a large volume of Mihomo warning logs during the
  localClash-managed network period.

Evidence boundary:

- The warning batch was not fully captured before the network environment was
  switched back to OpenClash.
- A previous partial local sample saw warning classes around direct match
  reports, Telegram IP timeouts, and `8.8.8.8:853` DNS connection failures, but
  that sample must be treated as partial evidence, not a complete diagnosis.

Open questions:

- Are warnings caused by rule mismatch, unreachable upstream DNS, direct routing
  of blocked destinations, retry amplification, or dashboard/API polling?
- Does warning rate correlate with Mihomo CPU, localClash CPU, or DNS latency?
- Are the warnings materially affecting forwarding latency, or only producing
  logging overhead?

Required evidence for the next reproduction:

- warning rate by class over time
- Mihomo CPU samples in the same timestamp window
- active generated `mihomo.yaml` rule count and provider count
- DNS upstream errors and rule match samples for Telegram and other affected
  services

Collection entrypoint:

- `scripts/collect-mihomo-warnings.py` streams
  `http://192.168.6.1:9090/logs?level=info` by default and writes full log,
  warning subset, snapshot, summary, event, and error JSONL artifacts under
  `.runtime/diagnostics/`.
- Use `--level warning` only when the collection target is warning volume alone.
  Use the default `info` level when warning context and runtime state-transition
  lines are needed in the same window.
- The script is read-only against the Mihomo controller. Add
  `--ssh-host root@192.168.6.1` only when process samples are needed in the
  same time window.

### Smart Config-Test Isolation

Observed symptom:

- On the real router, Smart core config validation could report
  `[Smart] DB Cache file load failed` while the active transparent-proxy runtime
  was already serving traffic.
- The active Smart process used a relative runtime directory:
  `-d .runtime/mihomo` from `/root/localclash`, and held
  `.runtime/mihomo/cache.db` open.
- `runtime_status` could report the live process as not using the configured
  runtime directory when comparing configured absolute paths with the relative
  command-line `-d` value.

Safety boundary:

- Do not run `mihomo -t` directly against the live `.runtime/mihomo` directory
  while the router network depends on localClash.
- Do not restart, stop, or start the runtime merely to validate a candidate
  config during this incident class.
- Config validation should use an isolated temporary runtime directory populated
  only with validation artifacts such as `Model.bin`, geodata/mmdb files, and
  rule-provider data. Live `cache.db`, PID files, logs, and UI assets are not
  validation inputs and must not be copied.

Follow-up:

- Fix runtime status path matching separately by resolving process cwd plus
  relative `-d` arguments before deciding whether a live process belongs to the
  configured runtime directory.

### OpenClash Baseline

Observed baseline:

- The user reported OpenClash-managed Clash usually runs around 6% to 10% CPU,
  with occasional spikes around 56%.

Evidence boundary:

- A single sample outside the incident window is not enough to compare
  localClash with OpenClash.

Required baseline:

- collect 5 to 10 minutes of process samples under the same traffic pattern
- record process command, CPU, memory, and warning/error rate
- compare with localClash using the same router, subscription, traffic, and DNS
  state

### Telegram Regression

Observed symptom:

- An older localClash core-only or minimal configuration path could cover
  Telegram automatically.
- The newer ACL4SSR-like default configuration failed for Telegram in real use.

Current explanation:

- The new default relied on GEOSITE category routing for Telegram. Telegram
  clients can connect directly to Telegram IP ranges without exposing a domain
  or SNI that `GEOSITE,telegram` can match.
- The default template now adds a `GEOIP,telegram` custom rule targeting the
  communication policy group. Isolated `mihomo -t` validation on v1.19.25
  loaded the Telegram GeoIP rule with 12 records.

Required verification:

- render the default patch set
- confirm generated Mihomo rules include `GEOIP,telegram` before fallback
- run Telegram traffic and confirm it matches the expected communication group

### Logging Gap

Observed gap:

- localClash incidents could not be answered from durable logs on the real
  router.
- `/Volumes/Data/Github/localClash/.runtime/logs/claude-code-localclash-observe.log`
  is a Claude Code client debug log. It records Claude/MCP client setup and
  transport behavior, not localClash server-side runtime decisions.

Existing logging:

- The MCP HTTP server already writes concise `mcp_http` request summaries to
  stderr, including method, path, tool, redacted arguments, HTTP status,
  response status, and duration.
- `scripts/deploy-router.sh` installs a procd service that redirects MCP
  stdout/stderr to `.runtime/logs/localclash-mcp.log`.

Integration gap:

- The LuCI-installed service path does not currently persist MCP stdout/stderr
  to a bounded router log, so the existing MCP request logs can be lost.
- Runtime operations, config rendering, takeover state transitions, and Mihomo
  warning summaries need durable router-side observability during development.

Product requirement:

- Development and diagnosis builds should make router-side evidence easy to
  collect.
- Release defaults must stay cheap for thin clients: no unbounded hot-path logs,
  no verbose logs by default, and no expensive polling.

### Duplicate Log-File Direction

Decision:

- Do not add a second generic MCP `--log-file` mechanism to the core CLI just to
  solve this incident.
- The better fix is to make deployment paths consistently preserve existing
  stderr request logs and add targeted, bounded observability for runtime and
  config operations.

Reason:

- MCP service logging already exists at the server stderr layer.
- The observed gap is deployment integration and missing runtime diagnostics,
  not a lack of another CLI flag.
