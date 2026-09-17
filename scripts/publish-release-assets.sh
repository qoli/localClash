#!/usr/bin/env bash
set -euo pipefail

tag="${1:-}"
repo="${RELEASE_REPO:-${GITHUB_REPOSITORY:-}}"

if [[ -z "$tag" || -z "$repo" ]]; then
	echo "usage: RELEASE_REPO=owner/repo scripts/publish-release-assets.sh <tag>" >&2
	exit 2
fi
if [[ "$tag" != v* ]]; then
	echo "release tag must start with v: $tag" >&2
	exit 2
fi

assets=(
	dist/localclash-base-assets.tar.gz
	dist/localclash-base-assets.tar.gz.sha256
	dist/localclash-linux-amd64
	dist/localclash-linux-arm64
	dist/localclash-linux-amd64.sha256
	dist/localclash-linux-arm64.sha256
	dist/localclash-release-manifest.json
)

for asset in "${assets[@]}"; do
	if [[ ! -f "$asset" ]]; then
		echo "missing release asset: $asset" >&2
		exit 1
	fi
done

release_json="$(gh release view "$tag" --repo "$repo" --json isDraft,assets 2>/dev/null || true)"
if [[ -z "$release_json" ]]; then
	gh release create "$tag" --repo "$repo" --draft --verify-tag --title "localClash $tag"
elif [[ "$(jq -r '.isDraft' <<<"$release_json")" != "true" ]]; then
	echo "release $tag is already published; refusing to replace immutable assets" >&2
	exit 1
fi

for asset in "${assets[@]}"; do
	name="$(basename "$asset")"
	local_digest="sha256:$(sha256sum "$asset" | awk '{print $1}')"

	for attempt in 1 2 3; do
		remote_digest="$(gh release view "$tag" --repo "$repo" --json assets --jq ".assets[] | select(.name == \"$name\") | .digest" 2>/dev/null || true)"
		if [[ "$remote_digest" == "$local_digest" ]]; then
			echo "verified existing release asset: $name"
			break
		fi

		echo "uploading release asset $name (attempt $attempt/3)"
		if timeout 300 gh release upload "$tag" "$asset" --repo "$repo" --clobber; then
			remote_digest="$(gh release view "$tag" --repo "$repo" --json assets --jq ".assets[] | select(.name == \"$name\") | .digest")"
			if [[ "$remote_digest" == "$local_digest" ]]; then
				break
			fi
			echo "release asset digest mismatch after upload: $name" >&2
		else
			echo "release asset upload failed: $name attempt $attempt/3" >&2
		fi

		if [[ "$attempt" == 3 ]]; then
			exit 1
		fi
		sleep $((attempt * 5))
	done
done

for asset in "${assets[@]}"; do
	name="$(basename "$asset")"
	local_digest="sha256:$(sha256sum "$asset" | awk '{print $1}')"
	remote_digest="$(gh release view "$tag" --repo "$repo" --json assets --jq ".assets[] | select(.name == \"$name\") | .digest")"
	if [[ "$remote_digest" != "$local_digest" ]]; then
		echo "release asset verification failed: $name" >&2
		exit 1
	fi
done

gh release edit "$tag" --repo "$repo" --draft=false --latest --title "localClash $tag"
