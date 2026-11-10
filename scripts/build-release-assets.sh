#!/usr/bin/env sh
set -eu

version="${1:-}"
if [ -z "$version" ]; then
	echo "usage: scripts/build-release-assets.sh <version-or-tag>" >&2
	exit 2
fi

repo="${RELEASE_REPO:-qoli/localClash}"
dist="${DIST_DIR:-dist}"
mkdir -p "$dist"

fetch_asset() {
	output="$1"
	shift
	for url in "$@"; do
		if command -v curl >/dev/null 2>&1; then
			if curl -fsSL --connect-timeout 10 --max-time 45 --retry 2 --retry-delay 2 --retry-all-errors "$url" -o "$output"; then
				return 0
			fi
		elif command -v wget >/dev/null 2>&1; then
			if wget -q -T 45 -O "$output" "$url"; then
				return 0
			fi
		else
			echo "curl or wget is required to download release runtime assets" >&2
			exit 1
		fi
	done
	echo "failed to download release runtime asset: $output" >&2
	exit 1
}

github_release_mirrors() {
	url="$1"
	printf '%s\n' "https://v1.ax/$url"
	printf '%s\n' "https://ghp.xptvhelper.link/$url"
	printf '%s\n' "$url"
}

raw_github_mirrors() {
	url="$1"
	rest="${url#https://raw.githubusercontent.com/}"
	owner="${rest%%/*}"
	rest="${rest#*/}"
	repo="${rest%%/*}"
	rest="${rest#*/}"
	branch="${rest%%/*}"
	path="${rest#*/}"
	printf '%s\n' "https://v1.ax/$url"
	printf '%s\n' "https://ghp.xptvhelper.link/$url"
	if [ -n "$owner" ] && [ -n "$repo" ] && [ -n "$branch" ] && [ -n "$path" ]; then
		printf 'https://fastly.jsdelivr.net/gh/%s/%s@%s/%s\n' "$owner" "$repo" "$branch" "$path"
	fi
	printf '%s\n' "$url"
}

build_asset() {
	goarch="$1"
	output="$dist/localclash-linux-$goarch"
	GOOS=linux GOARCH="$goarch" CGO_ENABLED=0 go build \
		-trimpath \
		-ldflags "-s -w -X main.version=$version" \
		-o "$output" .
	chmod 755 "$output"
	(cd "$dist" && sha256sum "localclash-linux-$goarch" > "localclash-linux-$goarch.sha256")
}

build_asset amd64
build_asset arm64

base_assets="localclash-base-assets.tar.gz"
rm -f "$dist/$base_assets" "$dist/$base_assets.sha256"
geox_tmp="$(mktemp -d "${TMPDIR:-/tmp}/localclash-geox.XXXXXX")"
cleanup() {
	rm -rf "$geox_tmp"
}
trap cleanup EXIT
mkdir -p "$geox_tmp/.runtime/mihomo"
for goarch in amd64 arm64; do
	worker_dir="$geox_tmp/bin/linux-$goarch"
	mkdir -p "$worker_dir"
	GOOS=linux GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" \
		-o "$worker_dir/localclash-pools" ./cmd/localclash-pools
done
fetch_asset "$geox_tmp/.runtime/mihomo/Country.mmdb" \
	$(raw_github_mirrors "https://raw.githubusercontent.com/alecthw/mmdb_china_ip_list/release/Country.mmdb") \
	"https://testingcf.jsdelivr.net/gh/alecthw/mmdb_china_ip_list@release/Country.mmdb"
fetch_asset "$geox_tmp/.runtime/mihomo/geoip.dat" \
	$(github_release_mirrors "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat") \
	"https://testingcf.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geoip.dat"
fetch_asset "$geox_tmp/.runtime/mihomo/geosite.dat" \
	$(github_release_mirrors "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat") \
	"https://testingcf.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geosite.dat"
fetch_asset "$geox_tmp/.runtime/mihomo/ASN.mmdb" \
	$(github_release_mirrors "https://github.com/xishang0128/geoip/releases/latest/download/GeoLite2-ASN.mmdb") \
	"https://testingcf.jsdelivr.net/gh/xishang0128/geoip@release/GeoLite2-ASN.mmdb"
COPYFILE_DISABLE=1 tar -czf "$dist/$base_assets" policy-templates rule-sources -C "$geox_tmp" .runtime bin
(cd "$dist" && sha256sum "$base_assets" > "$base_assets.sha256")

created_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
amd64_size="$(wc -c < "$dist/localclash-linux-amd64" | tr -d ' ')"
arm64_size="$(wc -c < "$dist/localclash-linux-arm64" | tr -d ' ')"
base_assets_size="$(wc -c < "$dist/$base_assets" | tr -d ' ')"
amd64_sha="$(cut -d ' ' -f 1 "$dist/localclash-linux-amd64.sha256")"
arm64_sha="$(cut -d ' ' -f 1 "$dist/localclash-linux-arm64.sha256")"
base_assets_sha="$(cut -d ' ' -f 1 "$dist/$base_assets.sha256")"
base_url="https://github.com/$repo/releases/download/$version"

cat > "$dist/localclash-release-manifest.json" <<EOF
{
  "schema_version": 1,
  "name": "localclash",
  "version": "$version",
  "created_at": "$created_at",
  "assets": [
    {
      "os": "linux",
      "arch": "amd64",
      "filename": "localclash-linux-amd64",
      "url": "$base_url/localclash-linux-amd64",
      "sha256": "$amd64_sha",
      "size": $amd64_size,
      "install_path": "/usr/local/bin/localclash"
    },
    {
      "os": "linux",
      "arch": "arm64",
      "filename": "localclash-linux-arm64",
      "url": "$base_url/localclash-linux-arm64",
      "sha256": "$arm64_sha",
      "size": $arm64_size,
      "install_path": "/usr/local/bin/localclash"
    }
  ],
  "base_assets": {
    "filename": "$base_assets",
    "url": "$base_url/$base_assets",
    "sha256": "$base_assets_sha",
    "size": $base_assets_size,
    "install_path": "/root/localclash",
    "contents": [
      "policy-templates/",
      "rule-sources/",
      "bin/linux-amd64/localclash-pools",
      "bin/linux-arm64/localclash-pools",
      ".runtime/mihomo/Country.mmdb",
      ".runtime/mihomo/geoip.dat",
      ".runtime/mihomo/geosite.dat",
      ".runtime/mihomo/ASN.mmdb"
    ]
  }
}
EOF
