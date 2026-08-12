# Release Broadcast Workflow Reference

## Files

- Changelog source: `docs/changelog.md`
- Telegram fixed top Markdown: `telegram/top.md`
- Telegram announcement cursor: `telegram/broadcast-state.json`
- Telegram generator/poster: `scripts/telegram-channel-update.py`
- X.com card generator: `scripts/x-release-card.py`
- Generated Telegram update body: `telegram/changelog.md`
- Default Telegram image:
  `telegram/out/localclash-x-release-card.png`
- X.com changelog image style:
  `docs/x-release-card-style.md`
- Local token file: `telegram/.token`
- Fallback Syncnext token file:
  `/Volumes/Data/Github/SyncnextProjects/Syncnext/telegram/.token`

## Git-Tracked Versus Local Files

Tracked source files:

- `docs/changelog.md`
- `telegram/top.md`
- `telegram/broadcast-state.json`
- `scripts/telegram-channel-update.py`
- `scripts/x-release-card.py`
- `docs/x-release-card-style.md`
- `.codex/skills/localclash-release-broadcast/**`

Local generated files that must stay ignored:

- `telegram/changelog.md`
- `telegram/.token`
- `telegram/out/`
- `telegram/sent/`

Verify with:

```bash
git check-ignore -v \
  telegram/changelog.md \
  telegram/.token \
  telegram/out/example.md \
  telegram/sent/example.json
```

## LuCI CI Release Evidence

The authoritative LuCI release procedure is:

```text
version bump commit -> main CI success -> matching immutable tag
-> tag-triggered Release workflow success -> GitHub Release verification
-> changelog/card/Telegram dry-run
```

Read `../localclash-luci/docs/github-release-runbook.md` before operating a
LuCI release. The CI workflows are:

- `.github/workflows/ci.yml`: pull request and `main` candidate validation;
- `.github/workflows/release.yml`: tag-only publication after rebuilding and
  validating the exact asset allow-list.

Useful evidence commands from the LuCI repository:

```bash
git status --short --branch
gh run list --workflow CI --limit 5
gh run list --workflow Release --limit 5
gh run view <run-id>
gh release view <tag> --json tagName,isDraft,isPrerelease,assets,url
git ls-remote --tags origin <tag>
```

For CI-enabled LuCI releases, verify these artifact families:

```text
luci-app-localclash_<version>-<release>_all.ipk[.sha256]
luci-app-localclash-<version>-r<release>.apk[.sha256]
dnsqualify-linux-amd64[.sha256]
dnsqualify-linux-arm64[.sha256]
dnsqualify-release-manifest.json
localclash-istore-<tag>-x86_64.run[.sha256]
localclash-istore-<tag>-aarch64.run[.sha256]
```

Download `.run` files to a temporary directory and inspect without installing:

```bash
sh localclash-istore-<tag>-x86_64.run --info
sh localclash-istore-<tag>-x86_64.run --list
sh localclash-istore-<tag>-x86_64.run --check
sh localclash-istore-<tag>-x86_64.run --noexec --noprogress
```

Repeat for aarch64. These checks prove archive integrity and content only;
real-router compatibility still requires target-router evidence.

Do not use `gh release create`, `gh release upload`, `--clobber`, or manual
local build artifacts to bypass the workflow. If the workflow fails, fix the
source-controlled contract. If a public release is already wrong, bump the
LuCI `PKG_RELEASE` and publish a new tag.

## Telegram Commands

## X.com Image Command

Generate the X.com changelog image from the latest dated changelog section:

```bash
python3 -m unittest scripts/test_release_broadcast.py
scripts/x-release-card.py
```

If the latest date contains both release channels but the current announcement
is for only one of them, select it explicitly:

```bash
scripts/x-release-card.py --channel core
scripts/x-release-card.py --channel luci
```

Do not include already-announced blocks from the other channel merely because
they share the latest changelog date.

Generated files:

```text
telegram/out/localclash-x-release-card.html
telegram/out/localclash-x-release-card.png
```

The script writes the ignored HTML working file and renders a `1600 x 2000` PNG
through the existing Arc CDP endpoint at `http://localhost:9222`. If CDP or
Playwright is unavailable, the script fails explicitly instead of silently
reusing an old image.

Rendering must reuse Arc's existing persistent browser context and create only
a temporary tab inside it. Never use `browser.new_context()` as a fallback:
Arc can map it to an independent window that cannot be closed. If the existing
context is absent, fail explicitly. Close only the temporary tab after capture;
do not call `browser.close()` on the connected Arc session.

HTML-only generation for style/debug iteration:

```bash
scripts/x-release-card.py --html-only
```

## Telegram Commands

Preview without writing:

```bash
scripts/telegram-channel-update.py --dry-run --no-write
```

The Telegram script reads `telegram/broadcast-state.json` and extracts only
release blocks newer than the tracked `core` / `luci` cursor. A dry run with no
new release blocks must fail with `No unannounced Telegram release blocks
found.` rather than reposting an older changelog section.

When the default image is used, the full announcement is the image caption. It
must fit Telegram's 1024-character caption limit. If it is too long, shorten the
announcement; the script fails explicitly and must not split the post into a
standalone image plus a separate text message.

Preview and write the ignored local Markdown:

```bash
scripts/telegram-channel-update.py --dry-run
```

Post to the default channel with the default image:

```bash
scripts/telegram-channel-update.py
```

After a successful live post, the script updates
`telegram/broadcast-state.json` to the newly announced Core/LuCI versions. Commit
that tracked state change with the broadcast update.

Post text only:

```bash
scripts/telegram-channel-update.py --no-image
```

Override the channel:

```bash
scripts/telegram-channel-update.py --chat-id @SomeChannel
```

Token lookup order:

1. `TELEGRAM_BOT_TOKEN`
2. `telegram/.token`
3. `/Volumes/Data/Github/SyncnextProjects/Syncnext/telegram/.token`

## Changelog Style

- Use Traditional Chinese for `docs/changelog.md`.
- Keep release links explicit.
- Avoid internal command transcripts in public-facing change bullets.
- Include only current, verified facts for "latest" claims.
- Put verification evidence in a short `Verification:` list when relevant.

## X.com Update Image

- Use `scripts/x-release-card.py` for every release broadcast before drafting an
  X.com post.
- Use the fixed dark technical card documented in `docs/x-release-card-style.md`.
- The X.com image is a changelog summary only; do not include the Telegram fixed
  top, product feature introduction, or right-bottom explanatory filler.
- Generate local working files under `telegram/out/` and keep them ignored.
