# Release Manifest

localClash publishes router-installable core binaries from GitHub Releases. The
LuCI helper consumes `localclash-release-manifest.json`, selects the current
router architecture, verifies `sha256`, and atomically installs the binary to:

```text
/usr/local/bin/localclash
```

Release assets:

```text
localclash-linux-amd64
localclash-linux-arm64
localclash-linux-amd64.sha256
localclash-linux-arm64.sha256
localclash-base-assets.tar.gz
localclash-base-assets.tar.gz.sha256
localclash-release-manifest.json
```

`localclash-base-assets.tar.gz` contains the disk assets the CLI expects to
find in its working directory:

```text
policy-templates/
rule-sources/
.runtime/mihomo/Country.mmdb
.runtime/mihomo/geoip.dat
.runtime/mihomo/geosite.dat
.runtime/mihomo/ASN.mmdb
```

Manifest shape:

```json
{
  "schema_version": 1,
  "name": "localclash",
  "version": "v0.1.0",
  "created_at": "2026-05-21T00:00:00Z",
  "assets": [
    {
      "os": "linux",
      "arch": "arm64",
      "filename": "localclash-linux-arm64",
      "url": "https://github.com/qoli/localClash/releases/download/v0.1.0/localclash-linux-arm64",
      "sha256": "...",
      "size": 12345678,
      "install_path": "/usr/local/bin/localclash"
    }
  ],
  "base_assets": {
    "filename": "localclash-base-assets.tar.gz",
    "url": "https://github.com/qoli/localClash/releases/download/v0.1.0/localclash-base-assets.tar.gz",
    "sha256": "...",
    "size": 123456,
    "install_path": "/root/localclash",
    "contents": [
      "policy-templates/",
      "rule-sources/",
      ".runtime/mihomo/Country.mmdb",
      ".runtime/mihomo/geoip.dat",
      ".runtime/mihomo/geosite.dat",
      ".runtime/mihomo/ASN.mmdb"
    ]
  }
}
```

Before pushing a release tag or dispatching a release, follow the
[iStoreOS feature-based test SOP](istoreos-release-test-sop.md). Maintain each
feature's last actual tested version, result, and evidence in the
[feature table](istoreos-test-features.md). For the candidate Core/LuCI pair,
execute affected tests and review the applicability of historical evidence for
unchanged features. A release does not require every feature to be retested on
the newest version. Missing evidence is resolved for the affected feature,
not by automatically restarting the full suite.

G99 summarizes exercised interactions, bugs, fixes and retests, and remaining
release risks. An incomplete history table or an unrelated environment failure
is not an automatic release blocker. The release owner assesses those risks;
the current workflow does not run QEMU or validate the feature table. When a selected test needs unpublished candidate assets,
verify that distribution works through the real product entry point; publishing
first does not substitute for that evidence.

Agent execution follows the [test skill](../.codex/skills/localclash-istoreos-test/SKILL.md).
The primary agent selects affected tests and reviews evidence; Luna High executes
them. Public release operations remain subject to the user's authorization.

The release workflow is `.github/workflows/release.yml`. It runs the Go test
suite first, then builds linux `amd64` and `arm64` binaries with:

```bash
scripts/build-release-assets.sh v0.1.0
```

The workflow runs on tag pushes matching `v*` and can also be triggered
manually with a tag input.

To trigger it from the command line, use:

```bash
scripts/trigger-github-release.sh v0.1.17
```

That path uses `workflow_dispatch` and expects the tag to already exist on
GitHub. To create a new annotated tag at `HEAD`, push it, and let the tag push
start the Release workflow:

```bash
scripts/trigger-github-release.sh v0.1.18 --create-tag --watch
```

Use `--dry-run` to check what the script would do without pushing a tag or
dispatching a workflow.
