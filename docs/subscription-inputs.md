# Subscription Inputs

localClash subscription input is a list of whitelisted URIs. Each URI must be
one of:

- `http://` or `https://` remote subscription URI
- MVP proxy URI listed below

Remote subscription responses are parsed as full Mihomo-compatible YAML first.
If the response is not a usable Mihomo subscription YAML document, localClash
then parses it as one proxy URI per line. Inline MVP proxy URI inputs are parsed
directly as one proxy URI per line. In all cases, accepted input is normalized
to a top-level `proxies` list, then stored under `.runtime/subscriptions/` and
merged into the effective `subscription.gob`.

URI deduplication is string-level only. localClash does not decode or compare
proxy server fields, passwords, UUIDs, or node names when removing duplicate URI
inputs.

## Subscription refresh cache recovery

When a remote subscription fetch, response read, or parse fails (including HTTP
errors such as 522, connection failures, and invalid payloads), localClash reuses that
source's existing `.runtime/subscriptions/<source-id>.gob` only if its ID matches
the canonical subscription URI hash and the artifact has a supported schema
and a valid, nonempty proxy list. The source artifact and its modification time
are preserved; other sources still refresh normally and the merged subscription
is rebuilt from the resulting source documents.

The refresh result marks the source `status: "cached"`, includes an explicit
warning in `warnings`, and emits a `refresh_source` warning event identifying
the failure and the cache artifact. Policy-template transactions carry this warning
through to their product result and may commit only after the remaining render
and validation steps succeed. A completed refresh does not mean every source
was freshly downloaded; callers must inspect source statuses and warnings.

When both refresh and source-cache recovery fail, that source is marked
`status: "failed"`, reported in `warnings`, and excluded from the new merged
subscription. The refresh succeeds when at least one configured source yields a
valid freshly downloaded, cached, or existing artifact. It fails explicitly
when every configured source is invalid, and does not overwrite the prior
merged artifact in that case. No merged artifact is used as a substitute for a
missing source cache.

Each successfully written merged artifact also records the exact source IDs
included by that refresh. Downstream config rendering uses this declaration to
skip only sources that the latest successful refresh explicitly excluded. An
artifact declared active but missing on disk, an unknown or duplicate active
source ID, and a legacy merged artifact without this declaration remain strict:
rendering requires the configured source artifacts and fails rather than
guessing which sources should be active.

Cancellation, invalid source configuration, local write failures, and
subsequent render or validation failures remain explicit operation failures.
Inline proxy URI inputs are one configured source: if that source fails to
parse, another valid source can still satisfy the refresh.

## Proxy URI Import Scope

Use the Mihomo Meta source checkout at `/Volumes/Data/Github/mihomo-Meta` as
the reference for share-link parsing behavior. The relevant upstream parser is
`common/convert/converter.go`, with shared VMess/VLESS handling in
`common/convert/v.go`.

MVP-supported proxy URI schemes:

- `ss://`
- `ssr://`
- `vmess://`
- `vless://`
- `trojan://`
- `hysteria://`
- `hysteria2://`
- `hy2://`
- `tuic://`
- `anytls://`
- `socks://`
- `socks5://`
- `socks5h://`

Exclusions and notes:

- `http://` and `https://` are valid Mihomo proxy-node URI schemes only when
  they appear in a clearly delimited proxy-node list. localClash does not treat
  them as proxy URI inputs in the MVP because they are reserved for remote
  subscription URIs.
- `snell://` is not in Mihomo Meta's proxy URI parser. `snell` is a supported
  Mihomo YAML proxy `type`, but localClash should not treat `snell://` as an
  MVP proxy URI input unless a separate parser is intentionally added.
- `mierus://` is supported by Mihomo Meta's proxy URI parser, but it is outside
  the accepted MVP scope for localClash unless product scope changes.
