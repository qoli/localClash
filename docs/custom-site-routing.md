# Custom Site Routing

Status: implemented and locally verified on `codex/custom-site-routing`;
OpenWrt router deployment and live-runtime acceptance remain unverified.

## Current Status

The product and persistence contracts are implemented across Core and
`../localclash-luci`. Automated tests cover storage, rendering, transactions,
reserved names, RPC transport, browser-only warnings, update preservation, and
package assembly. Router installation and live Mihomo reload/read-back still
require an explicit deployment acceptance run.

## Problem

The default LuCI experience serves users who do not want to manage routing
rules, while MCP patch tools serve advanced users who can reason about a full
policy graph. A middle group can operate LuCI and wants a small number of
explicit website overrides, but should not have to learn rule order, patch
merging, generated YAML, or AI/MCP workflows.

## Product Interface

LuCI exposes one narrow action:

```text
add website -> enter pattern -> choose proxy or direct -> save
```

The list view has two sections named `自訂代理網站` and `自訂直連網站`.
Each row can be deleted. The first version adds one rule per save; batch entry
is out of scope.

Inputs without `*` or `?` are suffix matches and compile to Mihomo `DOMAIN-SUFFIX`.
Inputs containing `*` or `?` are wildcard matches and compile to
`DOMAIN-WILDCARD`. Wildcards use Mihomo semantics: `*` matches zero or more
characters and `?` matches exactly one character. They operate on the complete
host string and may cross `.` separators. URLs, ports, paths, regular
expressions, IP addresses, and CIDRs are outside the v1 interface.

## Durable State

Custom proxy and direct rules are stored in two versioned JSON documents that
are not part of `patches/*.json` and are not compiled patch-registry artifacts.
They survive `reset_patches` and default-policy synchronization.

Each entry has an immutable id, match type, normalized value, globally ordered
sequence, and observational added-at timestamp. `sequence`, not wall-clock
time, is authoritative. Core allocates it while holding the custom-site write
lock across both documents.

Both documents may contain the same pattern. Existing entries are not moved or
deleted when a later decision is added to the other document.

Missing, malformed, unsupported-version, duplicate-id, or duplicate-sequence
state fails explicitly after the feature has been initialized. It is never
silently treated as an empty list or repaired during rendering.

## Rule And Policy Model

Core owns the reserved policy-group names:

- `自訂代理網站`
- `自訂直連網站`

MCP patch and policy-group write paths must reject these names. Pre-existing
collisions block feature initialization or rendering and require an explicit
user repair.

`自訂代理網站` is a manual selector whose first exit is `⚡ 自动选择`, followed
by `🎯 手动选择` and the regional exits that actually exist in the compiled
policy graph. Automatic and manual exits are required. Regional exits are
optional and are never invented or silently substituted.

`自訂直連網站` has exactly one exit: `DIRECT`.

The renderer merges both documents, sorts every entry by descending sequence,
and emits rules in that exact order. The single custom-layer precedence rule is:

> The last successfully added custom rule always has the highest priority.

This applies across proxy/direct targets and full/wildcard match types. Deleting
the newest entry exposes any older matching decision that remains below it.

Custom-site rules render after the non-disableable local safety baseline and
before optional packs, policy-template patches, and fallback:

```text
local safety baseline
custom site rules, newest first
optional rule packs
policy template patches
fallback
```

## Save Transaction

The LuCI button is labelled `儲存`, but one successful Core transaction means:

```text
validate input
build candidate custom-site state
compile policy groups and ordered rules
render candidate Mihomo config
run mihomo -t
atomically promote durable custom-site state and generated artifacts
reload an active runtime
read back effective state
```

If the runtime is inactive, valid state is stored and LuCI reports that it will
take effect on the next start. Any candidate, validation, test, promotion,
reload, or read-back failure preserves the previously active state and reports
the exact failed invariant. There is no fallback target or best-effort partial
apply.

## LuCI-Only Warning

LuCI JavaScript marks both rows yellow when the same normalized pattern exists
in the proxy and direct lists. This is informational only:

- it does not block save or delete;
- it does not change rule order;
- it is not persisted in either JSON document;
- it is recomputed in the browser from the returned lists;
- Core does not expose or enforce a conflict state.

Semantic intersection between different wildcard expressions is outside the
v1 warning. Runtime precedence remains fully determined by sequence.

## Default-Policy Update Contract

The existing interactive checkbox `同步最新默认策略（推荐）` retains its
meaning: checked synchronization performs a complete patch-registry reset and
imports the latest upstream defaults, replacing AI/user policy patches.

Below it LuCI renders an always-checked, disabled indicator named
`保留用戶自訂網站列表`. It is not a preference and no false request value
exists. Core proves the promise by keeping both JSON documents outside the
patch registry and reporting their before/after counts and hashes during the
update read-back.

## Ownership

Core owns JSON schema, locking, sequence allocation, normalization, reserved
names, compilation, candidate validation, artifact promotion, and runtime
read-back. LuCI is an adapter: it collects one decision, calls the Core
transaction, lists/deletes entries, and computes the yellow warning.

Neither layer edits `.runtime/mihomo/config.yaml` directly.

## Acceptance

- Plain and wildcard inputs compile to `DOMAIN-SUFFIX` and `DOMAIN-WILDCARD`.
- A later wildcard can override an earlier suffix match and vice versa.
- The same pattern may exist in both lists; deleting the newest reveals the
  older decision.
- Ordering survives process restart, render, subscription refresh, component
  update, and checked default-policy synchronization.
- `reset_patches` never modifies the custom-site documents.
- Reserved policy-group names are rejected at every MCP mutation seam.
- Candidate `mihomo -t` failure leaves durable and active state unchanged.
- LuCI warnings are yellow, non-blocking, JS-only, and absent from persisted
  state.
- LuCI reports whether a successful save is active now or pending next start.

## Relationship To Other Docs

[`rule-model.md`](rule-model.md) remains authoritative for the overall rule
layers, target graph, and generated-config ownership. This document specializes
its user-explicit-override layer. The OpenWrt adapter and RPC behavior belong in
`../localclash-luci/docs/openwrt-luci.md` and must reference this contract rather
than redefine routing semantics.

## Open Questions

No product-semantic questions remain. Physical paths, command names, and staged
transaction mechanics are implementation decisions and must preserve the
contracts above.
