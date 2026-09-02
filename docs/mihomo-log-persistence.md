# Mihomo log persistence (A baseline only)

Keep `alpha-smart-5f29934a-local-active-monitor5` unchanged. The independent
`localclash logs collect` process subscribes to the existing controller
`GET /logs?level=debug` stream. It does not bootstrap localClash, change log level
in the generated config, change selection, start/stop Mihomo, or apply takeover.

```sh
localclash logs collect \
  --config /root/localclash/.runtime/mihomo/config.yaml \
  --output-dir /root/localclash/.runtime/logs/mihomo-history
```

The config is read only to obtain controller address and secret. JSONL records
contain `received_at` (UTC receipt time), `kind`, and the original log's `level`
and `message`. Common URL-query and named credential forms are redacted; logs
still contain network addresses, node names and browsing metadata. Do not publish
raw archives. Directory permissions are 0700 and files are 0600.

## Retention and bounded resource use

- Sliding 48 hours, not today/yesterday. Minute segments are expired every 30
  seconds even while the controller is unavailable. Segments at the old edge are
  removed conservatively up to two minutes early to avoid intentionally retaining
  records older than 48 hours between sweeps.
- 32 MiB archive budget, with each file charged in 4 KiB blocks; filesystem
  metadata and the lock file are additional. Each segment is at most 256 KiB.
  Oldest segments are evicted sooner if volume exceeds the budget, with a
  `capacity_evicted_segments` marker. Full 48-hour coverage is not guaranteed at
  high volume.
- Bounded 1024-record queue and 16 KiB source-frame limit. Queue overflow,
  stream loss and collector restarts are explicitly marked. No substitute data.
- Storage errors stop only the collector and are reported on stderr. procd may
  restart it. No collector error can request a Mihomo restart or takeover change.
- The collector must remain running for physical expiry. A stopped machine or
  stopped collector cannot delete files; startup prunes before recording again.
  Interpret records only within `now - 48h <= received_at <= now` when reading.
- Writes go directly to the filesystem, without an application flush buffer.
  There is no per-record fsync: abrupt power loss can lose the OS's pending
  writes. A collector restart marks prior coverage as unknown. Retention relies
  on the system wall clock; clock jumps must not be treated as continuous coverage.

## Important evidence limitations

The upstream controller uses a bounded lossy queue and has no source sequence
numbers. Local gap markers cannot detect every upstream loss: absence of gaps is
not proof of complete coverage. Receipt timestamps are not original event times.
The controller cannot expose startup errors emitted before it starts listening;
existing runtime stdout/stderr and lifecycle logs are still needed for those.
This is persistence, not a network-quality score or a selector replay dataset.
No `network_quality_report` tool or new Smart journal schema is required.

## Independent OpenWrt service

After explicitly installing a localClash binary containing this command, copy
`scripts/localclash-log-collector.init` to
`/etc/init.d/localclash-log-collector`, chmod 0755, then enable/start that service.
The service uses procd and bounded system log reporting. Installing or restarting
this observer does not require restarting Mihomo or MCP. Stop/disable only this
service to stop collection. Do not use the general router deployment script for
this observer: it performs unrelated asset and MCP work.

Acceptance must use iStoreOS first: unchanged core SHA/PID/config, debug records
persist, observer stop/start leaves runtime unchanged, disconnect/reconnect gaps,
48h expiry including controller downtime, capacity eviction, and explicit storage
failure. ARM64 production deployment needs separate user authorization.

## Scoped verification — 2026-09-03

This is observer verification, not the full release SOP or a real-router network
quality baseline. No production router was accessed for this rewrite.

- `go test ./...`, `go test -race ./internal/logarchive`, `go vet ./...` passed.
  Tests cover rolling expiry and sweep headroom, idle expiry, reopen/rotation,
  byte budget, credentials, reconnect gaps, writer lock and explicit storage errors.
- Linux amd64 and arm64 binaries built. Only amd64 was runtime-tested.
- iStoreOS QEMU ran the saved, unmodified
  `alpha-smart-5f29934a-local-active-monitor5` binary in an isolated fixture
  (controller 19092, proxy 17890, no TUN/DNS takeover).
  SHA-256: `d9b1ee182204866d53a0a6f2b3bf1e72ba04221264d0113c1644ee689ebc0613`.
- Local HTTP traffic through its Smart group produced persisted `SmartTiming`
  first-read/close events with config log level still `info`.
- Stopping/restarting the fixture core produced a disconnect gap and reconnect
  marker. Separately restarting only its collector kept core PID 20616,
  core hash and fixture config hash unchanged.
- An observer also captured debug events from the VM's existing Meta runtime.
  Managed runtime PID 19032, binary/config hashes and effective takeover remained
  unchanged across observer stop/start. Managed core/config were not replaced.
- Fixture files are under `/root/localclash/.log-persistence-test/` in the VM;
  test-only procd instances are `log-persistence-test`, `active-monitor5-test`
  and `active-monitor5-collector-test`. They were not enabled for boot.
  The installed `/usr/local/bin/localclash` was not replaced.

These checks demonstrate capture and isolation, not continuous 48-hour uptime,
external proxy quality, router startup-incident root cause, or selector improvement.
