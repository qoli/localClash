#!/usr/bin/env sh
set -eu

usage() {
	cat >&2 <<'EOF'
usage:
  scripts/trigger-github-release.sh <tag> [--watch]
  scripts/trigger-github-release.sh <tag> --create-tag [--watch]

Examples:
  scripts/trigger-github-release.sh v0.105
  scripts/trigger-github-release.sh v0.106 --create-tag --watch

Modes:
  default       Trigger the existing GitHub Actions Release workflow with
                workflow_dispatch. The tag must already exist on origin.
  --create-tag Create an annotated tag at HEAD and push it. The tag push
                triggers the Release workflow automatically.

Options:
  --allow-dirty Allow --create-tag when the worktree has local changes.
  --dispatch    Force workflow_dispatch mode.
  --dry-run     Check inputs and print the action without triggering anything.
  --repo NAME   GitHub repo, for example qoli/localClash.
  --workflow W  Workflow name or file. Defaults to Release.
  --ref REF     Ref that owns the workflow_dispatch file. Defaults to main.
  --watch       Watch the triggered run and return its exit status.
EOF
}

die() {
	echo "error: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

tag=""
mode="dispatch"
allow_dirty=0
dry_run=0
watch=0
repo="${GITHUB_REPOSITORY:-}"
workflow="${LOCALCLASH_RELEASE_WORKFLOW:-Release}"
ref="${LOCALCLASH_RELEASE_REF:-main}"

while [ "$#" -gt 0 ]; do
	case "$1" in
		--allow-dirty)
			allow_dirty=1
			shift
			;;
		--create-tag)
			mode="create-tag"
			shift
			;;
		--dispatch)
			mode="dispatch"
			shift
			;;
		--dry-run)
			dry_run=1
			shift
			;;
		--repo)
			[ "$#" -ge 2 ] || die "--repo requires a value"
			repo="$2"
			shift 2
			;;
		--repo=*)
			repo="${1#--repo=}"
			shift
			;;
		--workflow)
			[ "$#" -ge 2 ] || die "--workflow requires a value"
			workflow="$2"
			shift 2
			;;
		--workflow=*)
			workflow="${1#--workflow=}"
			shift
			;;
		--ref)
			[ "$#" -ge 2 ] || die "--ref requires a value"
			ref="$2"
			shift 2
			;;
		--ref=*)
			ref="${1#--ref=}"
			shift
			;;
		--watch)
			watch=1
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		-*)
			die "unknown option: $1"
			;;
		*)
			if [ -n "$tag" ]; then
				die "unexpected argument: $1"
			fi
			tag="$1"
			shift
			;;
	esac
done

[ -n "$tag" ] || {
	usage
	exit 2
}

case "$tag" in
	v*) ;;
	*) die "release tag must start with v, got $tag" ;;
esac

if [ "$mode" = "create-tag" ] && ! printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+$'; then
	die "new Core release tags must use v<major>.<sequence>, for example v0.105"
fi

need git

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

if [ -z "$repo" ]; then
	if command -v gh >/dev/null 2>&1; then
		repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null || true)"
	fi
fi
[ -n "$repo" ] || repo="qoli/localClash"

git fetch --tags origin >/dev/null

local_tag_exists() {
	git rev-parse -q --verify "refs/tags/$tag" >/dev/null
}

remote_tag_exists() {
	git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null 2>&1
}

trigger_event="workflow_dispatch"
trigger_branch="$ref"

case "$mode" in
	create-tag)
		if remote_tag_exists; then
			die "origin already has $tag; rerun with workflow_dispatch: scripts/trigger-github-release.sh $tag --watch"
		fi
		if [ "$allow_dirty" -ne 1 ] && [ -n "$(git status --porcelain)" ]; then
			die "worktree has local changes; commit/stash them or pass --allow-dirty"
		fi
		head="$(git rev-parse HEAD)"
		if local_tag_exists; then
			tag_head="$(git rev-list -n 1 "$tag")"
			[ "$tag_head" = "$head" ] || die "local tag $tag points at $tag_head, not HEAD $head"
		elif [ "$dry_run" -eq 1 ]; then
			echo "would create annotated tag $tag at $head"
		else
			git tag -a "$tag" -m "localClash $tag"
		fi
		if [ "$dry_run" -eq 1 ]; then
			echo "would push tag $tag to origin"
		else
			git push origin "$tag"
		fi
		trigger_event="push"
		trigger_branch="$tag"
		if [ "$dry_run" -eq 1 ]; then
			echo "dry run: tag push would trigger $workflow on $repo"
		else
			echo "pushed tag $tag; Release workflow will start from tag push"
		fi
		;;
	dispatch)
		if ! remote_tag_exists; then
			die "origin does not have $tag; create it first with: scripts/trigger-github-release.sh $tag --create-tag"
		fi
		if [ "$dry_run" -eq 1 ]; then
			echo "dry run: would dispatch $workflow for $tag on $repo using ref $ref"
		else
			need gh
			gh workflow run "$workflow" --repo "$repo" --ref "$ref" -f "tag=$tag"
			echo "dispatched $workflow for $tag on $repo"
		fi
		;;
	*)
		die "unknown mode: $mode"
		;;
esac

if [ "$watch" -eq 1 ]; then
	if [ "$dry_run" -eq 1 ]; then
		echo "dry run: would watch the triggered $workflow run"
		exit 0
	fi
	need gh
	sleep 3
	run_id="$(gh run list \
		--repo "$repo" \
		--workflow "$workflow" \
		--limit 20 \
		--json databaseId,event,headBranch \
		--jq ".[] | select(.event == \"$trigger_event\" and .headBranch == \"$trigger_branch\") | .databaseId" \
		| head -n 1)"
	if [ -z "$run_id" ]; then
		run_id="$(gh run list --repo "$repo" --workflow "$workflow" --limit 1 --json databaseId --jq '.[0].databaseId')"
	fi
	[ -n "$run_id" ] || die "could not find a Release workflow run to watch"
	gh run watch "$run_id" --repo "$repo" --exit-status
fi
