---
name: localclash-release-broadcast
description: Write and publish localClash release changelogs, X.com changelog cards, and Telegram channel update announcements. Use when Codex needs to update docs/changelog.md, summarize Core and localclash-luci releases, generate the X.com update image, prepare or send Telegram update posts to @RonnieAppsChannel, verify generated release-broadcast files stay out of Git tracking, or maintain the localClash release broadcast workflow.
---

# localClash Release Broadcast

Use this skill inside the `localClash` workspace to produce user-facing release
notes and Telegram channel announcements.

## Workflow

1. Work from the workspace root.
2. Inspect `git status --short --branch` first. Do not stage, revert, or edit
   unrelated user changes.
3. Confirm current release facts from GitHub before writing latest-version
   claims:
   - `gh release list --limit 8` in this repo for Core.
   - `gh release list --limit 8` in `../localclash-luci` for LuCI.
   - Use `gh release view <tag>` when asset lists, publish time, or release URL
     matters.
   - For a new LuCI release, inspect the tag-triggered `Release` workflow with
     `gh run list --workflow Release --limit 5` and `gh run view <run-id>`.
     A GitHub Release page alone is not sufficient evidence that the automated
     build and installer checks passed.
   - When the user explicitly asks to publish a LuCI release, follow
     `../localclash-luci/docs/github-release-runbook.md`: review the version
     commit and successful main CI, then create and push the matching tag. Do
     not run a local `gh release create` or upload locally rebuilt substitutes.
4. Update `docs/changelog.md`:
   - Keep Core and LuCI as separate release channels.
   - Update the latest-version table.
   - Add a dated section only for channels released on that date.
   - Write user/maintainer-facing impact, not raw commit logs.
   - Include verification evidence when install, update, manifest, OpenWrt
     package, or router behavior changed.
5. Generate the X.com changelog image from the latest dated changelog section:
   ```bash
   python3 -m unittest scripts/test_release_broadcast.py
   scripts/x-release-card.py
   ```
   When the latest date contains both Core and LuCI blocks but only one channel
   is being announced, pass `--channel core` or `--channel luci`. Do not render
   already-announced blocks into the current channel's image.
   Inspect `telegram/out/localclash-x-release-card.png` before using it
   in an X.com post. The card must contain changelog content only: no Telegram
   fixed top, no product feature introduction, no right-bottom filler text.
   The renderer must reuse Arc's existing CDP browser context. It must never
   create an isolated browser context or independent Arc window; if the existing
   context is unavailable, generation must fail explicitly.
   Publish the reviewed text and card through the fail-closed X publisher:
   ```bash
   scripts/x-post.py \
     --account @llqoli \
     --text-file telegram/out/x-post.txt \
     --image telegram/out/localclash-x-release-card.png
   scripts/x-post.py \
     --account @llqoli \
     --text-file telegram/out/x-post.txt \
     --image telegram/out/localclash-x-release-card.png \
     --publish
   ```
   The first command validates local inputs and duplicate state. The second is
   a live side effect and requires explicit user authorization. Never bypass a
   script failure with manual browser publishing or an automatic retry.
   If X confirms creation with a status URL but final verification fails, inspect
   that exact status read-only. When the intended text, resolved links, account,
   and image are all confirmed, record it without resubmitting via
   `--verify-existing-status <status-url>`; otherwise leave the state untouched.
6. Generate and inspect the Telegram announcement:
   ```bash
   scripts/telegram-channel-update.py --dry-run --no-write
   ```
   The script reads `telegram/broadcast-state.json` and includes only release
   blocks newer than the tracked Telegram cursor. If there are no unannounced
   blocks, it must fail instead of reposting old changelog content.
7. For live Telegram posting:
   - If the user explicitly asks to send/post/publish the Telegram notice, run
     `scripts/telegram-channel-update.py`.
   - With the default image, the complete announcement must fit Telegram's
     1024-character photo caption limit. The script fails when it is too long;
     shorten the changelog instead of sending text separately.
   - After a successful live post, the script updates
     `telegram/broadcast-state.json`; commit that tracked state update with the
     release broadcast changes.
   - Otherwise stop after dry-run and ask for approval before posting.
   - The default channel is `@RonnieAppsChannel`.
   - The default image is the generated X.com changelog card:
     `telegram/out/localclash-x-release-card.png`.
8. Verify local generated release-broadcast files are ignored:
   ```bash
   git check-ignore -v telegram/changelog.md telegram/.token telegram/out/example.md telegram/sent/example.json telegram/out/localclash-x-release-card.html telegram/out/localclash-x-release-card.png
   ```
9. Run `git diff --check` before claiming the docs/tooling are ready.

For LuCI release announcements, verify the published asset names against the
CI allow-list. In addition to IPK/APK and dnsqualify assets, CI-enabled releases
carry architecture-specific iStore `.run` bundles and SHA-256 sidecars. Verify
both `.run` archives with `--info`, `--list`, `--check`, and `--noexec` before
describing them as available. Do not claim real-router compatibility until the
corresponding x86_64/aarch64 router evidence exists.

## Boundaries

- Never commit or stage `telegram/changelog.md`, `telegram/.token`,
  `telegram/out/`, or `telegram/sent/`.
- Do commit `telegram/broadcast-state.json`; it is the durable Telegram
  announcement cursor.
- Do commit `x/broadcast-state.json`; it records verified X content fingerprints
  and status URLs to prevent duplicate publication.
- Never print Telegram bot tokens.
- Do not treat Core and LuCI releases as the same artifact. Core releases carry
  binaries, base assets, and `localclash-release-manifest.json`; LuCI releases
  carry IPK/APK package artifacts, LuCI-owned dnsqualify assets, and checksums.
  LuCI CI releases may also carry iStore offline `.run` bundles; the Core inside
  those bundles is pinned by the LuCI repository and does not merge the two
  release channels.
- Do not overwrite public release history. If a LuCI package changed after a
  release, bump `PKG_RELEASE` and publish a new tag instead.
- Never bypass a failed LuCI workflow by creating a Release or uploading an
  asset manually. Fix the source-controlled build or validation contract and
  publish a new package release when required.

## Reference

For exact paths, commands, and output ownership rules, read
`references/workflow.md`.
