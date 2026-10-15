# Custom Sites: 自訂代理網站 / 自訂直連網站

## API

If either custom-site tool is absent from discovered server capabilities,
report a capability/version mismatch; do not substitute a patch or direct
JSON/YAML edits.

`custom_sites_list` is `safe_read`. Before adding or deleting, read both lists
to understand existing overlaps and obtain actual stable IDs:

```json
{}
```

The result's `custom_sites.proxy[]` and `custom_sites.direct[]` entries include
stable `id`, normalized `pattern`, stored `match` (`full` or `wildcard`),
globally ordered `sequence`, and `added_at`. Snapshot counts and hashes describe
durable state, not loaded rules or active traffic.

`custom_sites_transact` is `confirm_required` and accepts one version-1 add or
delete per call. Add a website to 自訂代理網站 with:

```json
{"version":1,"operation":"add","pattern":"example.com","route":"proxy","wait":true}
```

Use `"route":"direct"` for 自訂直連網站. To delete exactly one decision,
replace the placeholder with an actual ID returned by `custom_sites_list`:

```json
{"version":1,"operation":"delete","id":"<actual list entry id>","wait":true}
```

Do not invent IDs, include `id` on add, or include `pattern`/`route` on delete.
Config paths and secrets remain server-owned.

## Matching and Priority

- A plain host such as `example.com` compiles to `DOMAIN-SUFFIX`, matching the
  host and its subdomains even though its stored `match` is `full`.
- Patterns containing `*` or `?` compile to `DOMAIN-WILDCARD`: `*` matches
  zero or more characters and `?` one character. Matching covers the whole
  host and wildcards may cross dots. Inputs are host patterns, without URLs,
  ports, paths, IPs, CIDRs, or regex; the API does not extract hosts from URLs.
- This durable layer is outside the patch registry. It survives
  `reset_patches` and default-policy synchronization, coexisting with default
  and user patches instead of replacing them.
- Rule order is the non-disableable local safety baseline, custom sites
  newest-first across both lists, optional/default policy rules, then fallback.
  The highest matching `sequence` wins within custom sites. Adding preserves
  older overlaps; deleting the newest can reveal an older route.
- The same normalized pattern may exist in both lists. LuCI's yellow conflict
  warning is frontend-only and does not block a transaction.

`route:"proxy"` uses the reserved 自訂代理網站 group's current selected exit;
it does not choose an arbitrary node or country. `route:"direct"` uses
自訂直連網站 → `DIRECT`. Specific exit/selection requests belong to the
service-owned routing path described in [Routing and patches](routing-patches.md).

For removal, delete the actual entry ID and verify that it is absent. Inspect
older matching decisions to explain what can become effective next; retain
the reserved groups and policy patches.

## Transaction Completion

The transaction validates input, renders a candidate, runs `mihomo -t`,
promotes durable state/config, and, when runtime is active, hot reloads and
semantically reads back its rules/groups. Promotion or runtime failure triggers
rollback attempts. A stopped runtime stays stopped.

Authorization must cover this built-in live apply when runtime is running.
Existing clear authorization for the requested full routing change carries
through the transaction. Do not add a separate config test or reload after
success; start a stopped runtime only if that action was also requested.

Use `wait:true` for the complete result envelope. Interpret `ok` with `apply`:

- `validated`, `promoted`, and `config_sha256` describe config validation and
  persistence.
- `runtime_running`, `reloaded`, `read_back`, and `effective` distinguish
  saved state from loaded semantics. `effective` is not proof of a traffic
  connection.
- `pending_next_start=true` means saved state awaits the next runtime start.
- On `isError` or `ok=false`, inspect `code`, `error`, `custom_sites`, and
  apply flags including `rolled_back`. A failure may precede promotion or
  leave an incomplete rollback; do not assume prior state was restored.

Without `wait:true`, the default is an async acknowledgement. Track the
returned `task_id`; read `log_file` and `status_file` with `nl_file` until
`done` or `error`. These files retain status, stages, and summaries, not the
complete transaction result. Reconcile the durable lists and, for loaded-state
claims, `/rules` and `/proxies`. Repeating an indeterminate add allocates
another ID/sequence; an extra reload does not resolve what the first call did.
Use connection evidence when the requested outcome concerns actual traffic.
