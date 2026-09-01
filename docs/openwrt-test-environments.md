# OpenWrt Test Environments

This document records the reusable OpenWrt test environments used for
localClash core and LuCI integration testing. The Docker target was first
recovered from thread `019e4a76-be53-7f00-ba09-889d087535b2`; this runbook now
tracks the current repo contracts as of 2026-06-01, not the private thread
transcript.

Do not commit local router passwords, subscriptions, generated configs, or
runtime artifacts. The credentials used in the original local test thread are
intentionally not recorded here.

## Safety Boundaries

- The real router is not a default test target. If the current network depends
  on localClash, do not restart runtime, stop runtime, or apply/stop network
  takeover on the real router unless the user explicitly reopens that boundary.
- Docker OpenWrt is the destructive integration target. It can run runtime
  start/restart, takeover apply/stop, failure cleanup, and LuCI long-task UI
  checks.
- UTM OpenWrt is the low-resource performance target. It is useful for timing,
  memory pressure, process sampling, and OOM behavior.
- Both recovered test environments are `x86_64`. They are not equivalent to the
  ARM64 real router for Smart-core cache issues. Do not use them as proof that
  `bin/linux-arm64/lc-mihomo-smart` works or fails on the real router.

## Docker OpenWrt

Use this environment for LuCI first-use flows, release-manifest bootstrap,
MCP-over-HTTP smoke tests, runtime/takeover interaction, same-boot takeover
repair, boot auto-restore helper behavior, and rollback behavior.

Current verified container:

```text
container: localclash-openwrt-test
current image: localclash/openwrt-test-prepared:24.10.2
original base seen in the thread: openwrt/rootfs:x86-64-24.10.2
OpenWrt: 24.10.2 x86_64
```

Host endpoints:

```text
LuCI overview: http://127.0.0.1:18088/cgi-bin/luci/admin/services/localclash/overview
MCP health:    http://127.0.0.1:18765/health
MCP JSON-RPC:  http://127.0.0.1:18765/mcp
Dashboard API: http://127.0.0.1:19090
Proxy ports:   17890-17895 -> 7890-7895
```

Current port mapping verified with `docker inspect`:

```text
80/tcp   -> 18088
8765/tcp -> 18765
9090/tcp -> 19090
7890-7895/tcp -> 17890-17895
```

Router-side paths and services:

```text
localClash core: /usr/local/bin/localclash
workdir:         /root/localclash
restore script:  /root/start-localclash-test.sh
LuCI app:        luci-app-localclash
package manager: opkg
```

The sibling LuCI repo currently builds `luci-app-localclash 0.1.0-19`. That
package includes the split restore model:

- `takeover_restore`: same-boot repair driven by `/tmp` repair evidence.
- `boot_restore_status`, `boot_restore_enable`, `boot_restore_disable`, and
  `boot_restore_run`: explicit persistent boot auto-restore control.
- An iface hotplug hook that calls `takeover_restore` after OpenWrt network
  churn, without treating a normal takeover apply as reboot restore intent.

The Docker firewall was intentionally relaxed for browser and MCP testing:

```text
firewall defaults / lan / wan: ACCEPT
allowed test ports: 22, 80, 443, 8765, 9090
fw4 base chains: dstnat, mangle_prerouting
```

The previous takeover validation succeeded when these conditions were present:

```text
ubus call localclash takeover_apply: success
takeover_status.effective: true
policy rule: fwmark 0x6c63 lookup 27747
route table: default dev utun
LuCI overview: network takeover effective
```

Useful read-only checks:

```bash
docker inspect localclash-openwrt-test --format '{{json .Config.Image}} {{json .State.Status}} {{json .HostConfig.PortBindings}}'
docker exec localclash-openwrt-test cat /etc/openwrt_release
docker exec localclash-openwrt-test opkg list-installed 'luci-app-localclash*'
docker exec localclash-openwrt-test ubus call localclash status
```

Restore the prepared container after a restart:

```bash
docker exec localclash-openwrt-test /root/start-localclash-test.sh
```

For a true first-use LuCI test, start from the clean baseline recovered in the
thread:

```text
localClash core missing
/root/localclash cleared
subscription not configured
luci-app-localclash installed
LuCI and ubus reachable
```

The important rule is to let LuCI perform bootstrap. Do not manually copy
`/usr/local/bin/localclash` into the container when validating first-use UX.
The browser path should click one-key initialization and let the helper run
`bootstrap_core`, release manifest download, checksum verification, and install.
LuCI bootstrap should refresh the localClash core first, then install base
assets, refresh subscriptions, render config, start runtime, and apply takeover
when the selected action requires it.

Docker limitations:

- The Docker target is `x86_64`. The current repo has ARM64 Smart-core assets
  for router work, so Docker cannot prove ARM64 Smart behavior.
- Earlier x86 Smart tests in the thread were rejected as non-authoritative
  because the x86 Smart binary source was not the current repo release path.
- Docker can reproduce product flow and takeover mechanics, but it cannot stand
  in for the real router's CPU, storage, DNS, and live traffic conditions.

## UTM OpenWrt Perf

Use this VM for constrained-router timing and resource-pressure tests. It is
slower and more memory constrained than Docker by design.

Current verified VM:

```text
name:   localClash OpenWrt Perf
uuid:   8629F4BA-0DD4-4D02-801F-234521E740E6
status: stopped as of 2026-05-28
```

Recovered VM configuration:

```text
UTM backend: QEMU x86_64
acceleration: TCG, not Hypervisor
CPU / RAM: 1 vCPU / 512 MB
boot mode: BIOS/Q35 after UEFI startup stalled
disk: VirtIO with an added data partition
network: vmnet-shared
OpenWrt: 24.10.2 x86_64
package manager: opkg
```

Runtime endpoints from the thread:

```text
SSH:      root@192.168.64.11
LuCI:     http://192.168.64.11/cgi-bin/luci/
MCP:      http://192.168.64.11:8765/mcp
workdir:  /root/localclash
data:     /root/localclash mounted on a separate partition, about 3.5 GB usable
```

Local evidence artifacts from the performance run are kept outside source
control:

```text
.runtime/utm-openwrt-perf/
  summary.json
  restart-runtime.tsv
  restart-runtime-2.tsv
  restart-runtime-3.tsv
  restart-runtime-3-response.json
  dmesg-after-restart3.txt
  mihomo*.log
```

Read-only host checks:

```bash
utmctl list
ls -la .runtime/utm-openwrt-perf
sed -n '1,220p' .runtime/utm-openwrt-perf/summary.json
```

The thread's deployed state after the first performance pass was historical
evidence, not the current package baseline:

```text
localClash core: /usr/local/bin/localclash
LuCI package: luci-app-localclash 0.1.0-9
MCP: http://192.168.64.11:8765/mcp
runtime during the pass: mihomo-meta
```

Calibration results from that run:

```text
restart_runtime #1: success, 39.51s
restart_runtime #2: success, 83.45s
restart_runtime #3: failed before stop/start, 15.09s
Mihomo initial rule loading: 56.720s, 38.447s, 59.969s
```

The third restart failed because the 512 MB VM had no swap and `mihomo -t`
started while the old `mihomo-meta` runtime was still alive. The local evidence
shows the OOM killer killed the preflight `mihomo-meta` process:

```text
old runtime: mihomo-meta PID 4939
preflight config test: mihomo-meta PID 5165
OOM victim: mihomo-meta PID 5165
```

UTM limitations:

- This VM is `x86_64` and the observed performance pass used `mihomo-meta`.
- It is useful for low-memory restart behavior and LuCI/MCP latency, not ARM64
  Smart-core validation.
- `utmctl start --hide` previously hit macOS automation issues. Do not treat a
  failed hidden start as an OpenWrt failure; verify through UTM UI, `utmctl
  list`, ping, and SSH.

## Target Selection

Use Docker OpenWrt for:

- LuCI first-use flow from missing core and empty state
- release manifest and mirror download behavior
- browser-visible long-task feedback
- same-boot takeover repair and explicit boot auto-restore helper behavior
- runtime start/restart interaction in an isolated router
- network takeover apply/stop and cleanup
- MCP JSON-RPC smoke tests through `127.0.0.1:18765/mcp`

Use UTM OpenWrt Perf for:

- restart_runtime wall-clock timing
- low-memory config-test pressure
- process CPU and memory sampling
- proving whether `restart_runtime` can survive old-runtime plus preflight
  memory overlap
- collecting evidence into `.runtime/utm-openwrt-perf/`

Do not use either x86 target for:

- proving real-router ARM64 Smart-core DB cache behavior
- validating a live router while the current network depends on it
- assuming real-router DNS, filesystem, traffic, or CPU behavior

## iStoreOS Firmware QEMU

Use this target when the acceptance boundary specifically requires an actual
iStoreOS firmware rather than the generic OpenWrt Docker rootfs. It boots the
official pinned x86_64 combined image under QEMU, with an immutable raw base and
a disposable qcow2 overlay.

The default pinned baseline is:

```text
iStoreOS: 24.10.8, build 2026073111
image: istoreos-24.10.8-2026073111-x86-64-squashfs-combined.img.gz
SHA-256: 2ce609e2625f9ba67723ec29b0b509baa300c6b74f528596490d950909e09a9c
runtime directory: .runtime/istoreos-qemu/
```

Prepare, boot, and wait for LuCI:

```bash
scripts/istoreos-test-env.sh prepare
scripts/istoreos-test-env.sh start
scripts/istoreos-test-env.sh wait
```

When network setup needs inspection, attach to the firmware console with
`scripts/istoreos-test-env.sh console`; `Ctrl-C` detaches from the console
without stopping the VM.

The clean firmware reports that no root password is defined. Use the serial
console for initial inspection; deliberately configure root authentication in
the disposable overlay before relying on the SSH forward.

Host-only endpoints:

```text
LuCI:       http://127.0.0.1:18089/
SSH:        ssh -p 12223 root@127.0.0.1 (after configuring root auth)
MCP:        http://127.0.0.1:18766/mcp
Controller: http://127.0.0.1:19091/
VNC:        vnc://127.0.0.1:5902
```

The first virtual NIC is the iStoreOS WAN with outbound QEMU user-mode NAT. The
second is the iStoreOS LAN at `192.168.101.1`, as observed from the pinned
firmware's loaded UCI state. No bridge, route, firewall, or DNS setting is added
to the macOS host. On Apple Silicon the x86_64 guest uses QEMU TCG emulation, so
boot and package operations are slower than the Docker target.

Stop the guest while keeping test state, or reset only the writable overlay:

```bash
scripts/istoreos-test-env.sh stop
scripts/istoreos-test-env.sh reset
```

Use this environment for iStore `.run` installation, iStoreOS-specific package
dependencies, LuCI/procd behavior, firmware reboot, and takeover restoration.
It is still x86_64 and must not be used as proof of ARM64 Smart-core behavior.

### WireGuard WAN-equivalent entry

QEMU user-mode NAT normally exits through the 6.1 host, whose transparent proxy
changes the network seen by capability probes. Do not use that natural upstream
as evidence for the physical router's bare-WAN quality.

For WAN-level integration testing, the current persistent test overlay has a
dedicated WireGuard link:

```text
router interface: wg_istore_wan, 10.66.67.1/30, UDP 51821
iStore interface: wg_istore_wan, 10.66.67.2/30
iStore default:   dev wg_istore_wan, metric 5
endpoint route:   router endpoint through eth0/QEMU NAT
router forwarding: wg_istore_wan -> wan
```

The router network section carries `localclash_bypass=1`. The matching takeover
helper builds an nftables interface set and returns traffic from that interface
before localClash TCP redirect, UDP/TUN marking and DNS hijack. This prevents the
test tunnel from re-entering the transparent proxy it is intended to bypass.
WireGuard private keys remain device-local and must not be copied into this
repository or diagnostic output.

Before accepting a WAN-level run, verify all of the following:

- the endpoint route still uses the original QEMU WAN rather than the tunnel
- the VM default route uses `wg_istore_wan`
- a recent handshake exists and transfer counters grow during the test
- the router takeover bypass set contains `wg_istore_wan`
- the VM public IPv4 digest equals the router WAN public IPv4 digest

This topology proves an equivalent WAN entry for x86_64 product-flow tests. It
still does not prove ARM64 Smart behavior. For that boundary, use the same fixed
subscription inputs with an isolated ARM64 candidate on the physical router and
do not replace the router's production subscription store or active Core.

For live-router Smart validation, use an isolated temporary runtime directory
and copy only validation inputs such as `Model.bin`, geodata/mmdb files, and
rule providers. Do not point `mihomo -t` at the live `.runtime/mihomo` directory
while the runtime is serving the current network.
