---
name: localclash-release-broadcast
description: Write and publish localClash release changelogs, X.com changelog cards, and Telegram channel update announcements. Use when Codex needs to update docs/changelog.md, summarize Core and localclash-luci releases, generate the X.com update image, prepare or send Telegram update posts to @RonnieAppsChannel, verify generated release-broadcast files stay out of Git tracking, or maintain the localClash release broadcast workflow.
---

# localClash Release and Broadcast

Work from the Core root. Inspect both repositories' `git status --short --branch`;
preserve unrelated edits and use the current checkout. Core and LuCI have
separate release channels. Updating Core or its source lock alone does not
require publishing LuCI.

## Release Facts and Acceptance

Before latest-version claims, inspect `gh release list --limit 8` in the
owning repository and `gh release view <tag>` for assets/time/URL. For LuCI,
also inspect `gh run list --workflow Release --limit 5` and `gh run view <run-id>`;
a Release page alone does not prove workflow checks passed.

For affected functionality, follow the [test Skill](../localclash-istoreos-test/SKILL.md)
and [feature table](../../../docs/istoreos-test-features.md): select by change
impact, verify reusable history, preserve actual tested versions, and report
failures/retests/risk. CI, host tests, package integrity, and archive checks do
not substitute for selected QEMU external oracles. ARM hardware is not a
mandatory release gate; x86 QEMU or an ARM build does not establish ARM runtime acceptance.

Publishing, pushing tags, and sending announcements require the user's
authorization for those actions. Carry existing authorization through its
clear scope; do not add a second approval merely for a required step.

## LuCI Release Procedure

Use sibling `../localclash-luci`. Its workflows/scripts define build details:

- `.github/workflows/ci.yml` validates PR/main candidates without publishing.
- `.github/workflows/release.yml` rebuilds and validates the exact tag before
  publication; tag is the release entrypoint.
- `scripts/build-release-assets.sh <tag>` and `scripts/verify-release-assets.sh <tag>`
  build/check the complete set. Use `SOURCE_DATE_EPOCH` from the reviewed commit.
- `scripts/generate-release-notes.py` generates user guidance from tag/Makefile;
  do not hand-copy versioned URLs into workflows.

1. Review the current version commit, `openwrt/luci-app-localclash/Makefile`
   `PKG_VERSION`/`PKG_RELEASE`, recent releases, and successful main CI.
   The tag must be `v<PKG_VERSION>-<PKG_RELEASE>` and point to the reviewed commit.
2. Complete affected QEMU acceptance, evidence reuse review, defect disposition,
   and G99 risk summary before pushing a release tag. If source/assets changed,
   reassess affected evidence rather than automatically rerunning all.
3. With publication authorization, create/push the immutable tag; wait for
   the `Release` workflow. A manual rerun for an unpublished tag must still
   check out and validate that exact reviewed tag.
4. Verify workflow success, non-draft/non-prerelease Release, remote tag commit,
   exact asset set, SHA-256 sidecars, and correspondence to the acceptance candidate.
5. Inspect both offline archives with `--info`, `--list`, `--check` and
   `--noexec` without installing. These establish archive integrity/contents,
   not router runtime compatibility.

LuCI's eight owned assets are IPK + SHA-256, APK + SHA-256, and x86_64/aarch64
iStore `.run` bundles + SHA-256. There are no dnsqualify assets.
Core releases carry their own binaries/base assets/manifest/checksums.
Ordinary LuCI packages do not embed Core; offline bundles pin it explicitly.

Never bypass failed CI with `gh release create`, `gh release upload`,
`--clobber`, or locally rebuilt substitutes. Do not move/rebind public tags
or overwrite public releases. Correct a bad public package by bumping
`PKG_RELEASE` and publishing a new tag through the workflow.

### Offline Bundle Contract

Only change `release/core-release.json` when an offline bundle needs another
Core. Pin an exact tag, official manifest URL, and manifest SHA-256; no
`latest`, mirrors, or unverified URL. `scripts/resolve-core-release.py`
validates tag, repository, Linux amd64/arm64 binaries, and base assets.
`release/makeself-release.json` similarly pins Makeself's version, official
URL, and SHA-256; validate an upgrade separately on both archive architectures.

The `.run` installer targets iStoreOS/opkg, not APK systems. It verifies root,
architecture, required commands, checksums, and base-file completeness.
It does not access the network, run `opkg update`, choose latest, or fall back
to downloads. Official policy/rule files update atomically per file; extra
user files remain. Installation does not configure subscriptions, routing
intent, runtime, or takeover. `dist/` and `.build/` stay untracked.

## Changelog and X Card

Update [docs/changelog.md](../../../docs/changelog.md) in Traditional Chinese.
Preserve the latest-version table and dated channel headings consumed by the
generators. Add only released channels for that date, with explicit release
links and concrete impact. Keep relevant verification short; avoid command transcripts.

```bash
python3 -m unittest scripts/test_release_broadcast.py
scripts/x-release-card.py --channel core
# Or --channel luci; omit only when all latest-date blocks are intended.
```

Inspect `telegram/out/localclash-x-release-card.png` before drafting a post.
Use the renderer's fixed dark technical style; only changelog content belongs
on the card, without Telegram fixed top, product introduction, or filler.
Keep generated HTML/PNG under ignored `telegram/out/`. `--html-only` permits style iteration.

Rendering/publishing reuses Arc's existing persistent CDP context at
`http://localhost:9222`. Fail explicitly if dependencies/context are missing;
do not reuse a stale image or create an isolated browser context/Arc window.
Close only the temporary tab, never the connected Arc browser.

## X Publication and Uncertain Submission

Write reviewed text to ignored `telegram/out/x-post.txt`, then validate:

```bash
scripts/x-post.py \
  --account @llqoli \
  --text-file telegram/out/x-post.txt \
  --image telegram/out/localclash-x-release-card.png
```

With explicit X publication authorization, run those inputs once with `--publish`.
The publisher verifies account, one image, actual composer text, CreateTweet
response, resulting status, and durable duplicate state. Do not bypass failure
with manual publishing or automatically retry an unknown submission.

If creation returned a status URL but verification failed, inspect that exact
status read-only. Only after account, text, resolved links and image match,
run the same inputs with `--verify-existing-status <status-url>` to record
the existing post without resubmitting. Failed verification leaves state unchanged.
`x/broadcast-state.json` is tracked verified publication state.

## Telegram

```bash
scripts/telegram-channel-update.py --dry-run --no-write
```

The generator reads `docs/changelog.md`, `telegram/top.md`, and tracked
`telegram/broadcast-state.json`. It selects only blocks newer than the
Core/LuCI cursors; no unannounced blocks must fail rather than repost.
`--dry-run` alone writes the ignored Markdown preview.

With explicit Telegram authorization, run `scripts/telegram-channel-update.py`
for `@RonnieAppsChannel` and the default X card. With an image, the complete
post must fit the 1024-character caption limit; shorten it, do not split
image/text. Use `--no-image` or another `--chat-id` only when requested.
After successful posting, preserve the tracked cursor update with broadcast changes.

Token lookup is `TELEGRAM_BOT_TOKEN`, `telegram/.token`, then the existing
Syncnext token at `/Volumes/Data/Github/SyncnextProjects/Syncnext/telegram/.token`.
Never print tokens. Never stage `telegram/changelog.md`, `telegram/.token`,
`telegram/out/`, or `telegram/sent/`. Keep both broadcast-state JSON files tracked.

Before handoff, run `git diff --check` and verify generated files are ignored:

```bash
git check-ignore -v telegram/changelog.md telegram/.token telegram/out/example.md telegram/sent/example.json
```
