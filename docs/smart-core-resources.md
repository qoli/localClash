# Smart Core Resources

localClash supports `type: smart` proxy groups through mihomo's Smart core, which
uses a LightGBM model (`Model.bin`) to predict the best proxy per connection.
This document catalogs all Smart-specific runtime artifacts managed by the mihomo
runtime under localClash control.

## Runtime Artifacts

| File | Purpose |
|---|---|
| `$HOME/.config/mihomo/Model.bin` | LightGBM serialized ensemble model |
| `$HOME/.config/mihomo/smart_weight_data.csv` | Training data collection CSV |
| `$HOME/.config/mihomo/cache.db` | bbolt KV store (`smart_stats` bucket) |

localClash does not directly write these files; they are managed by the mihomo
runtime. The home directory defaults to `$HOME/.config/mihomo`, overridable by
the `-d` CLI flag or `CLASH_HOME_DIR` environment variable on the OpenWrt host.

## Model.bin

### Path resolution

`constant/path.go` scans the home directory for any file matching `Model.bin`
(case-insensitive). If none is found, it returns `$HOME/.config/mihomo/Model.bin`.

### Download

The model is fetched lazily when a Smart proxy group first initializes:

```
NewSmart() → InitSmart() → lightgbm.GetModel() → downloadModel()
```

`GetModel()` uses `sync.Once`: if `Model.bin` already exists, it loads it; if
missing or corrupted, it downloads a fresh copy.

Two download code paths exist:

| Path | Trigger | Validation |
|---|---|---|
| `downloadModel()` | Startup, lazy init | None — 90 s timeout, writes raw body |
| `UpdateLgbmModel()` | Auto-update / API | SHA-256 hash diff, ETag, temp-file validate then replace |

### Download URL

Priority chain:

```
YAML lgbm-url  →  lightgbm.SetLgbmUrl()  →  downloadModel()
                                                 ↓ (if empty)
                                          GetModelDownloadURL()
                                          → GitHub Releases
```

localClash should render a mainland-reachable mirror URL instead of the GitHub
release direct URL. Current default:

`https://gh-proxy.com/https://github.com/vernesong/mihomo/releases/download/LightGBM-Model/Model-middle.bin`

The upstream direct URL remains:
`https://github.com/vernesong/mihomo/releases/download/LightGBM-Model/Model.bin`

### Auto-update

Controlled by globals `lgbm-auto-update` and `lgbm-update-interval` (default
`false` / `72 h`). The updater in `component/updater/update_lgbm.go`:

1. Computes SHA-256 of the local file
2. Sends `If-None-Match` (if ETag enabled)
3. Downloads to a temp file, validates with `leaves.LGEnsembleFromFile()`
4. On success, atomically replaces the active model and calls `lightgbm.ReloadModel()`

A manual trigger is available at `POST /upgrade/lgbm`.

## smart_weight_data.csv

### Path

`$HOME/.config/mihomo/smart_weight_data.csv`

### Enablement

Per-group Smart options:

```yaml
proxy-groups:
  - type: smart
    uselightgbm: true
    collectdata: true    # enables CSV collection
    sample-rate: 0.5     # fraction of connections sampled (default 1.0)
```

### Size cap

`profile.smart-collector-size` (MB, default 100). When the file exceeds this
limit, the collector stops writing. No rotation is performed. If the file is
deleted externally, the collector detects it within 5 s and recreates it.

### Schema (38 columns)

28 feature columns and 10 metadata columns. The features mirror the 28-element
vector consumed by `PredictWeight()`.

**Features (columns 0-27)**

| # | Column | Description |
|---|---|---|
| 0 | `success` | Successful connections |
| 1 | `failure` | Failed connections |
| 2 | `connect_time` | Connection setup time (ms) |
| 3 | `latency` | Latency (ms) |
| 4 | `upload_mb` | Session upload (MB) |
| 5 | `history_upload_mb` | Cumulative upload (MB) |
| 6 | `maxuploadrate_kb` | Session peak upload rate (KB/s) |
| 7 | `history_maxuploadrate_kb` | Historical peak upload rate (KB/s) |
| 8 | `download_mb` | Session download (MB) |
| 9 | `history_download_mb` | Cumulative download (MB) |
| 10 | `maxdownloadrate_kb` | Session peak download rate (KB/s) |
| 11 | `history_maxdownloadrate_kb` | Historical peak download rate (KB/s) |
| 12 | `duration_minutes` | Session duration (min) |
| 13 | `history_duration_minutes` | Historical avg duration (min) |
| 14 | `last_used_seconds` | Seconds since last use |
| 15 | `is_udp` | UDP flag (0/1) |
| 16 | `is_tcp` | TCP flag (0/1) |
| 17 | `asn_feature` | ASN category code (0-54+) |
| 18 | `country_feature` | GeoIP country code (0-31+) |
| 19 | `address_feature` | Domain type code (0-111) |
| 20 | `port_feature` | Port service category (0-36) |
| 21 | `traffic_ratio` | Upload/download ratio (-1 to 1) |
| 22 | `traffic_density` | Traffic density MB/min (log1p) |
| 23 | `connection_type_feature` | Connection type (0-7) |
| 24 | `asn_hash` | ASN FNV-1a hash (500 buckets) |
| 25 | `host_hash` | Hostname FNV-1a hash (1000 buckets) |
| 26 | `ip_hash` | IP FNV-1a hash (10000 buckets) |
| 27 | `geoip_hash` | GeoIP FNV-1a hash (200 buckets) |

**Metadata (columns 28-37)**

| # | Column | Description |
|---|---|---|
| 28 | `group_name` | Smart group name |
| 29 | `node_name` | Selected proxy node name |
| 30 | `asn_raw` | Raw ASN string |
| 31 | `host_raw` | Target hostname |
| 32 | `ip_raw` | Target IP |
| 33 | `port_raw` | Target port |
| 34 | `geoip_raw` | GeoIP country code |
| 35 | `weight` | Computed weight |
| 36 | `weight_source` | `lgbm` or `traditional` |
| 37 | `timestamp` | RFC3339 timestamp |

The collector flushes every 100 records. On startup it checks the column count;
if the schema has changed (new columns added upstream), it backs up the old file
to `smart_weight_data.csv.bak.<timestamp>` and starts a new one.

## cache.db — smart_stats Bucket

All Smart runtime state is persisted in the `smart_stats` bucket of the bbolt
database at `$HOME/.config/mihomo/cache.db`.

### Key space

Format: `smart/{KeyType}/{config}/{group}/{target}/.../{node}`

| KeyType | Content |
|---|---|
| `prefetch` | DNS resolution target mappings |
| `node` | Per-node metadata |
| `stats` | `StatsRecord` JSON (success, failure, latency, throughput, duration) |
| `ranking` | `NodeRank` JSON (ordered node list per group) |
| `failures` | Host failure entries |

Weight-type subkeys: `tcp`, `udp`, `tcp_asn`, `udp_asn`.

### Key constants

| Constant | Value | Meaning |
|---|---|---|
| `RecordExpiredTime` | 7 days | Stats record TTL |
| `HostFailureNodeTTL` | 24 hours | Blocked-node TTL |
| `DefaultMinSampleCount` | 2 | Minimum samples for ranking |
| `MaxTargetsLimit` | 5000 | Max tracked targets |
| `AllowedWeight` | 0.4 | Weight floor for selection |

Batch writes use `db.Batch()` with a `BatchSaveThreshold` that adapts to system
memory (50-300 range). The write queue flushes every 5 minutes.

## Feature Engineering

The model consumes 28 features (`MaxFeatureSize = 28`) preprocessed as follows:

| Transformation | Applied to | Count |
|---|---|---|
| `log1p` (ln(x+1)) | Connect time, latency, upload/download MB/rate, duration, last-used, traffic density | 14 |
| `(up - down) / (up + down + 1)` | Traffic ratio | 1 |
| bool → float | is_udp, is_tcp | 2 |
| Category lookup | ASN (80+ known), GeoIP (31 countries), domain type, port type, connection type | 5 |
| FNV-1a hash mod N | ASN, host, IP, GeoIP | 4 |
| Untransformed | — | 2 |

### Category features

- **ASN**: 80+ known providers mapped to codes 1-49 (Google=1, Cloudflare=6, etc.), unknown numeric ranges mapped to 50-54
- **GeoIP**: 31 countries mapped to codes (CN=1, HK=2, US=7, JP=4, SG=5, etc.)
- **Domain type**: IP=1, Streaming=2, Game=3, Communication=4, API=5, DNS=6, TLD-based (10-15), domain depth (30-31)
- **Port**: DNS=36, API=35, Game=30, Communication=31, system (20), registered (21), dynamic (22)
- **Connection type**: Web=1, Streaming=2, Interactive=3, Database=4, FileTransfer=5, API=6, DNS=7

### Fallback weight

When the LightGBM model is unavailable or sample count is below threshold, the
system falls back to `CalculateWeight()` in `component/smart/weight.go`. This
classifies connections into four scenes — Web (0), Interactive (1), Streaming
(2), Transfer (3) — and applies hand-tuned per-scene parameters (success rate
weight, connect-time penalty, latency penalty, traffic factor, duration factor,
quality bonus).

## Transform System

Model.bin carries a custom `[transforms]` section appended after the standard
LightGBM binary. This is a mihomo extension, not part of the LightGBM format.

`LoadTransformsFromModel()` reads the last 16 KB of the file looking for
`[transforms]` ... `[/transforms]` markers.

Two transform types are supported:

| Transform | Formula | Applied to |
|---|---|---|
| `StandardScaler` | `(x - mean) / scale` | 14 log-transformed continuous features |
| `RobustScaler` | `(x - center) / scale` | success, failure counts |

Eleven features bypass transforms: booleans, category codes, and hashes.

## Memory Caches

Six LRU caches sized at `MaxTargets / 4` with TTLs of 300-600 s:

| Cache | Key | Value |
|---|---|---|
| `targetCache` | Domain | Target |
| `unwrapCache` | Domain | UnwrapMap |
| `recordCache` | Key | AtomicStatsRecord |
| `dbResultCache` | Prefix | KV map |
| `blockedNodesCache` | Group | Blocked node set |
| `hostStatusCache` | Host | HostStatus |

`AdjustCacheParameters()` runs every 5 minutes and rescales `MaxTargets`
(500-5000) and `BatchSaveThreshold` (50-300) based on available system memory,
read from `/proc/meminfo` (Linux) or `wmic` (Windows).

## Periodic Tasks

Timed goroutines running within each Smart group and globally:

| Task | Interval | Purpose |
|---|---|---|
| Orphaned group cleanup | 10 min | Purge caches for removed groups |
| Cache parameter adjustment | 5 min | Memory-adaptive cache resizing |
| Queue flush | 5 min | Batch-write queue to bbolt |
| Orphaned node cleanup | 120 min | Purge caches for removed proxy nodes |
| Target prefetch | 15 min | Resolve DNS targets |
| Node stability check | 10 min | Detect node state transitions |
| Node ranking | 5 min | Recompute weight ordering |
| Blocked node recovery | 10 min | Probe blocked nodes for recovery |
| Host status check | 30 min | Verify host-level failure state |
| Old record cleanup | 120 min | Evict stats older than 7 days |

## Configuration

### localClash-global

```yaml
lgbm-url: "https://gh-proxy.com/https://github.com/vernesong/mihomo/releases/download/LightGBM-Model/Model-middle.bin"
lgbm-auto-update: true         # enable periodic update (default false)
lgbm-update-interval: 48       # update check interval hours (default 72)

profile:
  smart-collector-size: 200    # CSV size cap MB (default 100)
```

### Per-group Smart options

```yaml
proxy-groups:
  - type: smart
    uselightgbm: true
    collectdata: true
    sample-rate: 0.5
    prefer-asn: true
    policy-priority: ".*netflix.*:1.5"
```

localClash intent should express proxy-name priorities with the typed
`proxy_groups.<id>.smart_priority` rules documented in `docs/rule-sources.md`.
The renderer owns conversion to Mihomo's opaque `policy-priority` string and
applies it only to the named group. Ordered rules are first-match-wins, and a
global runtime-profile `policy-priority` remains only a default for Smart groups
that do not have group-scoped intent.

### Known issue: `localclash-user.json` Smart injection

When `core: smart` is active, localClash injects Smart runtime defaults after the
user-authored `localclash-user.json` fragment is selected as the runtime base.
There is currently no user-facing switch to disable this injection as a whole.

This means `localclash-user.json` is not a reliable place to author Smart group
boolean switches such as `uselightgbm`, `prefer-asn`, or `collectdata`:
`proxy-groups` is a localClash-owned dynamic key and is rejected from
`localclash-user.json`, while renderer-owned Smart group defaults are applied to
the generated dynamic proxy groups.

Top-level Smart runtime keys such as `lgbm-auto-update`, `lgbm-update-interval`,
and `lgbm-url` can still be supplied in `localclash-user.json`; current default
injection uses missing-key defaults and does not overwrite values already
present in the final rendered config. The missing product feature is an explicit
switch for whether localClash should inject Smart defaults at all.

## Relevant Mihomo Source Files

| File | Role |
|---|---|
| `constant/path.go` | `SmartModel()` path resolution |
| `component/smart/lightgbm/lightgbm.go` | Model load, download, prediction, features |
| `component/smart/lightgbm/transform.go` | Transform parsing and application |
| `component/smart/lightgbm/collector.go` | CSV data collector |
| `component/smart/common.go` | Store API, queue, key formatting |
| `component/smart/stats.go` | Stats records, failure tracking, ranking |
| `component/smart/memory.go` | LRU caches, memory adaptation |
| `component/smart/cachefile.go` | bbolt persistence |
| `component/smart/weight.go` | Fallback weight calculation |
| `component/updater/update_lgbm.go` | Model auto-update |
| `adapter/outboundgroup/smart.go` | Smart proxy group lifecycle |
| `config/config.go` | Config struct and defaults |
