# Repository Guidelines

## Local Artifacts

Treat `.runtime/`, `generated/`, `bin/`, `subscription*.gob`, and `localclash-subscriptions.json` as local artifacts or secrets, not source.

## LuCI Project Relationship

`/Volumes/Data/Github/localclash-luci` is the sibling OpenWrt integration project. Keep ownership explicit: this repository owns subscriptions, Mihomo config rendering/testing, Mihomo runtime lifecycle, release manifests, and versioned runtime facts. The LuCI repository owns UI, package, ACL, OpenWrt takeover desired state, fw4/nft/policy-routing/DNS-hijack implementation, ownership markers, boot/hotplug reconciliation, and cross-module runtime/takeover transactions. Core must not execute `fw4`, `nft`, `uci`, or `ip rule`. Start future LuCI maintenance tasks from this core project so agents first verify the current Core facts contract and release compatibility before changing the LuCI layer.

## Mihomo Core Source Checkouts

The local source checkout for the Mihomo Meta core is `/Volumes/Data/Github/mihomo-Meta`. The local source checkout for the Mihomo Alpha Smart core is `/Volumes/Data/Github/mihomo-Alpha`. Use these sibling repositories for source inspection, upstream behavior checks, and core-specific debugging only; localClash still owns runtime selection, config rendering, lifecycle commands, and managed binary paths such as `bin/*/lc-mihomo-meta` and `bin/*/lc-mihomo-smart`.

## Build, Test, and Development Commands

- `go test ./...`: run the full Go test suite.
- `go run . mcp`: start the MCP HTTP server.
- `go run . doctor` or `go run . doctor --json`: inspect local runtime prerequisites and generated config health.
- `go run . core download --dry-run`: verify Mihomo release asset selection without writing binaries.
- `go run . config render --json`: render `.runtime/mihomo/config.yaml` through the product flow using patch-registry intent, local subscriptions, and pack inputs.
- `go run . mihomo config-test --json`: run explicit `mihomo -t` validation and record the config hash attestation used by hot reload.
- `scripts/test-mcp-http.sh`: run the local HTTP MCP doctor smoke test.
- `scripts/test-mcp-cli.sh`: run the third-party MCP client compatibility smoke test.

## Coding Style & Naming Conventions

Use standard Go formatting: tabs via `gofmt`, short package names, and table-driven tests where practical. Keep command behavior explicit and deterministic; prefer typed structs and YAML parsing over ad hoc string manipulation. New MCP tools must include registry metadata, JSON input schema, safety level, server dispatch, and tests.

## Testing Guidelines

Place tests next to implementation as `*_test.go` files. Cover both success paths and safety/error boundaries, especially for config rendering, MCP inputs, filesystem writes, and secret-bearing local data. For Go code changes, run `go test ./...` before handoff. For runtime-facing changes, use `go run . doctor --json` in the relevant initialized runtime environment. Documentation-only changes do not require Go tests or runtime diagnostics.

## Commit & Pull Request Guidelines

Recent commits use short imperative subjects such as `Add MCP config plan render tool` and `Trim MCP product tool surface`. Follow that style, keep commits focused, and avoid mixing generated/runtime artifacts with source changes. PRs should describe behavior changes, list verification commands, call out safety-level changes for MCP tools, and include screenshots only for dashboard/UI-facing work.

## Agent-Specific Instructions

Use commands available directly in the current shell. For debugging, inspect relevant logs, config state, diagnostics, or MCP responses before changing code unless the root cause is already directly evident. For browser automation, prefer the existing ARC CDP endpoint at `http://localhost:9222` after a quick availability check.

For iStoreOS test planning or release acceptance, start with
[the test Skill](.codex/skills/localclash-istoreos-test/SKILL.md).
Select tests by change impact. Keep results with the evidence for the versions
actually tested.

## Durable Policy Constraints

Keep subscriptions as proxy inputs, patches as routing intent, and generated
intent/packs/YAML as derived artifacts. The local safety baseline cannot be
disabled. Preserve explicit DIRECT semantics and the default direct fallback
needed for game-accelerator coexistence. Do not rebuild the reserved custom-site
groups through patch builders; custom-site decisions are user-owned state
independent of template patches. Template reset must preserve that layer.
Validate configs through the isolated product config-test path; do not run
Mihomo validation against the live runtime directory or copy its cache.db.

Use code, schemas, templates, and workflows for implementation facts, published
releases for release facts, and run evidence for test results. Keep README
limited to user entry points and use the Skills for test and release procedures.
Do not recreate a parallel changelog or feature table.
