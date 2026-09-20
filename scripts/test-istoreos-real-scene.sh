#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/localclash-real-scene-test.XXXXXX")"
cleanup() {
	rm -rf "$tmp_dir"
}
trap cleanup EXIT

mkdir -p "$tmp_dir/bin"
trace="$tmp_dir/ssh.trace"
secret='https://subscription.example.invalid/private-token'

cat > "$tmp_dir/bin/ssh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$MOCK_SSH_TRACE"
case "$*" in
	*"test -f '/router/localclash-subscriptions.json'"*) exit 0 ;;
	*"test -x '/usr/local/bin/localclash'"*) exit 0 ;;
	*"subscription get --json"*)
		printf '{"version":1,"uris":["%s"]}\n' "$MOCK_SUBSCRIPTION_SECRET"
		;;
	*"subscription set --input"*)
		body="$(cat)"
		printf '%s' "$body" | grep -Fq "$MOCK_SUBSCRIPTION_SECRET"
		printf 'real subscription scene ready: configured=true merged=true sources=1 proxies=2\n'
		;;
	*"call takeover_status"*)
		printf 'one-click scene ready: subscription=real runtime=running takeover=effective dns_path=mihomo\n'
		;;
	*) exit 64 ;;
esac
EOF
chmod +x "$tmp_dir/bin/ssh"

export MOCK_SSH_TRACE="$trace"
export MOCK_SUBSCRIPTION_SECRET="$secret"
export LOCALCLASH_ROUTER_SSH='router-test'
export LOCALCLASH_ROUTER_WORKDIR='/router'
export LOCALCLASH_ISTOREOS_SSH='guest-test'
export LOCALCLASH_ISTOREOS_SSH_PORT='12223'
export LOCALCLASH_ISTOREOS_KNOWN_HOSTS="$tmp_dir/known_hosts"

output="$(PATH="$tmp_dir/bin:$PATH" "$repo_root/scripts/istoreos-real-scene.sh" subscription-sync)"
printf '%s\n' "$output" | grep -Fq 'real subscription scene ready:'
if printf '%s\n' "$output" | grep -Fq "$secret" || grep -Fq "$secret" "$trace"; then
	printf 'secret leaked through output or argv trace\n' >&2
	exit 1
fi
if grep 'router-test' "$trace" | grep -Eq 'UserKnownHostsFile|StrictHostKeyChecking'; then
	printf 'router SSH unexpectedly used disposable-VM trust options\n' >&2
	exit 1
fi
grep 'guest-test' "$trace" | grep -Fq "UserKnownHostsFile=$tmp_dir/known_hosts"
grep 'guest-test' "$trace" | grep -Fq 'StrictHostKeyChecking=accept-new'
grep 'guest-test' "$trace" | grep -Fq 'subscription set --input "$input" --json >/dev/null 2>&1'
grep 'guest-test' "$trace" | grep -Fq 'subscription refresh --json >/dev/null 2>&1'
grep 'guest-test' "$trace" | grep -Fq 'subscription status --json > "$status" 2>/dev/null'

output="$(PATH="$tmp_dir/bin:$PATH" "$repo_root/scripts/istoreos-real-scene.sh" assert-update-ready)"
printf '%s\n' "$output" | grep -Fq 'runtime=running takeover=effective dns_path=mihomo'

cat > "$tmp_dir/bin/ssh" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod +x "$tmp_dir/bin/ssh"
if PATH="$tmp_dir/bin:$PATH" "$repo_root/scripts/istoreos-real-scene.sh" subscription-sync >"$tmp_dir/fail.out" 2>"$tmp_dir/fail.err"; then
	printf 'missing router source unexpectedly succeeded\n' >&2
	exit 1
fi
grep -Fq 'router subscription source or export tooling is unavailable' "$tmp_dir/fail.err"

cat > "$tmp_dir/bin/ssh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
	*"test -f '/router/localclash-subscriptions.json'"*) exit 0 ;;
	*"test -x '/usr/local/bin/localclash'"*) exit 0 ;;
	*"subscription get --json"*) exit 1 ;;
	*) exit 64 ;;
esac
EOF
chmod +x "$tmp_dir/bin/ssh"
if PATH="$tmp_dir/bin:$PATH" "$repo_root/scripts/istoreos-real-scene.sh" subscription-sync >"$tmp_dir/export-fail.out" 2>"$tmp_dir/export-fail.err"; then
	printf 'lossy router export unexpectedly succeeded\n' >&2
	exit 1
fi
grep -Fq 'failed to export every router subscription through the formal product contract' "$tmp_dir/export-fail.err"

cat > "$tmp_dir/bin/ssh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
	*"test -f '/router/localclash-subscriptions.json'"*) exit 0 ;;
	*"test -x '/usr/local/bin/localclash'"*) exit 0 ;;
	*"subscription get --json"*)
		printf '{"version":1,"uris":["%s"]}\n' "$MOCK_SUBSCRIPTION_SECRET"
		;;
	*"subscription set --input"*) cat >/dev/null; exit 1 ;;
	*"call takeover_status"*) exit 1 ;;
	*) exit 64 ;;
esac
EOF
chmod +x "$tmp_dir/bin/ssh"
if PATH="$tmp_dir/bin:$PATH" "$repo_root/scripts/istoreos-real-scene.sh" subscription-sync >"$tmp_dir/import-fail.out" 2>"$tmp_dir/import-fail.err"; then
	printf 'failed guest import unexpectedly succeeded\n' >&2
	exit 1
fi
grep -Fq 'failed to configure and refresh the real subscription in iStoreOS' "$tmp_dir/import-fail.err"
if grep -Fq 'scene ready' "$tmp_dir/import-fail.out"; then
	printf 'failed guest import emitted a ready result\n' >&2
	exit 1
fi

if PATH="$tmp_dir/bin:$PATH" "$repo_root/scripts/istoreos-real-scene.sh" assert-update-ready >"$tmp_dir/ready-fail.out" 2>"$tmp_dir/ready-fail.err"; then
	printf 'invalid update scene unexpectedly succeeded\n' >&2
	exit 1
fi
grep -Fq 'fixture or stopped-runtime substitution is forbidden' "$tmp_dir/ready-fail.err"

printf 'iStoreOS real-scene contract tests passed\n'
