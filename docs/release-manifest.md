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

Before pushing a release tag or dispatching a release, complete the
[iStoreOS QEMU release test SOP](istoreos-release-test-sop.md) for the exact
Core/LuCI candidate pair and review its evidence record. This is a manual
functional gate: the current workflow does not run QEMU or verify that report.
If candidate distribution is unavailable, acceptance is blocked; publishing
first and testing the public release afterward does not satisfy this gate.

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
