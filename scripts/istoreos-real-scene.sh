#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: scripts/istoreos-real-scene.sh <command>

commands:
  subscription-sync   Read the real subscription configuration from the router,
                      apply it through the VM product CLI, and refresh it
  assert-update-ready Require a configured subscription, running Mihomo, and
                      effective router takeover before one-click update testing

The subscription configuration is streamed directly from router SSH to guest
SSH. It is never written to a host file or printed by this script.

Environment:
  LOCALCLASH_ROUTER_SSH             default root@192.168.6.1
  LOCALCLASH_ROUTER_WORKDIR         default /root/localclash
  LOCALCLASH_ROUTER_CORE            default /usr/local/bin/localclash
  LOCALCLASH_ISTOREOS_SSH           default root@127.0.0.1
  LOCALCLASH_ISTOREOS_SSH_PORT      default 12223
  LOCALCLASH_ISTOREOS_WORKDIR       default /root/localclash
  LOCALCLASH_ISTOREOS_CORE          default /usr/local/bin/localclash
  LOCALCLASH_ISTOREOS_HELPER        default /usr/libexec/rpcd/localclash
  LOCALCLASH_ISTOREOS_KNOWN_HOSTS   default .runtime/istoreos-qemu/known_hosts
  LOCALCLASH_SSH_CONNECT_TIMEOUT    default 8
  LOCALCLASH_SSH_LOG_LEVEL          default ERROR
EOF
}

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

require_command() {
	command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

reject_unsafe_value() {
	case "$2" in
		*"'"* | *$'\n'* | *$'\r'*) die "$1 must not contain quotes or newlines" ;;
	esac
}

router_ssh="${LOCALCLASH_ROUTER_SSH:-root@192.168.6.1}"
router_workdir="${LOCALCLASH_ROUTER_WORKDIR:-/root/localclash}"
router_core="${LOCALCLASH_ROUTER_CORE:-/usr/local/bin/localclash}"
guest_ssh="${LOCALCLASH_ISTOREOS_SSH:-root@127.0.0.1}"
guest_ssh_port="${LOCALCLASH_ISTOREOS_SSH_PORT:-12223}"
guest_workdir="${LOCALCLASH_ISTOREOS_WORKDIR:-/root/localclash}"
guest_core="${LOCALCLASH_ISTOREOS_CORE:-/usr/local/bin/localclash}"
guest_helper="${LOCALCLASH_ISTOREOS_HELPER:-/usr/libexec/rpcd/localclash}"
guest_known_hosts="${LOCALCLASH_ISTOREOS_KNOWN_HOSTS:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.runtime/istoreos-qemu/known_hosts}"
ssh_connect_timeout="${LOCALCLASH_SSH_CONNECT_TIMEOUT:-8}"
ssh_log_level="${LOCALCLASH_SSH_LOG_LEVEL:-ERROR}"
router_subscription="${router_workdir}/localclash-subscriptions.json"

for pair in \
	"router_workdir:${router_workdir}" \
	"router_core:${router_core}" \
	"guest_workdir:${guest_workdir}" \
	"guest_core:${guest_core}" \
	"guest_helper:${guest_helper}" \
	"guest_known_hosts:${guest_known_hosts}" \
	"router_subscription:${router_subscription}"
do
	reject_unsafe_value "${pair%%:*}" "${pair#*:}"
done
case "$guest_ssh_port" in
	'' | *[!0-9]*) die "LOCALCLASH_ISTOREOS_SSH_PORT must be a positive integer" ;;
esac
[ "$guest_ssh_port" -gt 0 ] || die "LOCALCLASH_ISTOREOS_SSH_PORT must be a positive integer"

require_command ssh
mkdir -p "$(dirname "$guest_known_hosts")"

router_ssh_opts=(
	-o BatchMode=yes
	-o ConnectTimeout="${ssh_connect_timeout}"
	-o LogLevel="${ssh_log_level}"
)
guest_ssh_opts=(
	-p "${guest_ssh_port}"
	-o BatchMode=yes
	-o ConnectTimeout="${ssh_connect_timeout}"
	-o LogLevel="${ssh_log_level}"
	-o UserKnownHostsFile="${guest_known_hosts}"
	-o StrictHostKeyChecking=accept-new
)

subscription_sync() {
	local router_export_command guest_command router_rc guest_rc
	local -a pipeline_status

	ssh "${router_ssh_opts[@]}" "$router_ssh" \
		"test -f '${router_subscription}' && test ! -L '${router_subscription}' && test -s '${router_subscription}' && test -x '${router_core}' && command -v jsonfilter >/dev/null 2>&1" \
		>/dev/null \
		|| die "router subscription source or export tooling is unavailable"

	ssh "${guest_ssh_opts[@]}" "$guest_ssh" \
		"test -x '${guest_core}' && command -v jsonfilter >/dev/null 2>&1 && test -d '${guest_workdir}'" \
		>/dev/null \
		|| die "iStoreOS guest is not ready for product subscription setup"

	router_export_command="set -eu; umask 077; status='/tmp/localclash-real-subscription-export.\$\$'; cleanup() { rm -f \"\$status\"; }; trap cleanup EXIT INT TERM; cd '${router_workdir}'; '${router_core}' subscription get --json > \"\$status\" 2>/dev/null; configured=\$(jsonfilter -i \"\$status\" -e '@.status.configured' 2>/dev/null || true); source_count=\$(jsonfilter -i \"\$status\" -e '@.status.count' 2>/dev/null || true); uri_count=\$(jsonfilter -i \"\$status\" -e '@.status.uris[*]' 2>/dev/null | wc -l | tr -d ' '); test \"\$configured\" = true; case \"\$source_count:\$uri_count\" in *[!0-9:]*|0:*|*:0) exit 1;; esac; test \"\$source_count\" = \"\$uri_count\"; printf '{\"version\":1,\"uris\":'; jsonfilter -i \"\$status\" -e '@.status.uris'; printf '}\\n'"

	guest_command="set -eu; umask 077; input='/tmp/localclash-real-subscriptions.\$\$'; status='/tmp/localclash-real-subscription-status.\$\$'; provenance_dir='${guest_workdir}/.runtime/istoreos-real-scene'; provenance=\"\$provenance_dir/subscription.sha256\"; cleanup() { rm -f \"\$input\" \"\$status\"; }; trap cleanup EXIT INT TERM; cat > \"\$input\"; test -s \"\$input\"; cd '${guest_workdir}'; '${guest_core}' subscription set --input \"\$input\" --json >/dev/null 2>&1; '${guest_core}' subscription refresh --json >/dev/null 2>&1; '${guest_core}' subscription status --json > \"\$status\" 2>/dev/null; configured=\$(jsonfilter -i \"\$status\" -e '@.status.configured' 2>/dev/null || true); merged=\$(jsonfilter -i \"\$status\" -e '@.status.merged.exists' 2>/dev/null || true); sources=\$(jsonfilter -i \"\$status\" -e '@.status.sources[*].id' 2>/dev/null | wc -l | tr -d ' '); proxies=\$(jsonfilter -i \"\$status\" -e '@.status.merged.proxies_count' 2>/dev/null || true); test \"\$configured\" = true; test \"\$merged\" = true; case \"\$sources:\$proxies\" in *[!0-9:]*|0:*|*:0|'':*) exit 1;; esac; configured_sha=\$(sha256sum localclash-subscriptions.json | awk '{print \$1}'); mkdir -p \"\$provenance_dir\"; printf '%s\\n' \"\$configured_sha\" > \"\$provenance.tmp\"; chmod 600 \"\$provenance.tmp\"; mv \"\$provenance.tmp\" \"\$provenance\"; printf 'real subscription scene ready: configured=true merged=true sources=%s proxies=%s\\n' \"\$sources\" \"\$proxies\""

	set +e
	ssh "${router_ssh_opts[@]}" "$router_ssh" "$router_export_command" \
		| ssh "${guest_ssh_opts[@]}" "$guest_ssh" "$guest_command"
	pipeline_status=("${PIPESTATUS[@]}")
	router_rc="${pipeline_status[0]}"
	guest_rc="${pipeline_status[1]}"
	set -e

	[ "$router_rc" -eq 0 ] || die "failed to export every router subscription through the formal product contract"
	[ "$guest_rc" -eq 0 ] || die "failed to configure and refresh the real subscription in iStoreOS"
}

assert_update_ready() {
	local guest_command
	guest_command="set -eu; sub='/tmp/localclash-real-subscription-status.\$\$'; run='/tmp/localclash-real-runtime-status.\$\$'; takeover='/tmp/localclash-real-takeover-status.\$\$'; provenance='${guest_workdir}/.runtime/istoreos-real-scene/subscription.sha256'; cleanup() { rm -f \"\$sub\" \"\$run\" \"\$takeover\"; }; trap cleanup EXIT INT TERM; cd '${guest_workdir}'; test -s \"\$provenance\"; expected_sha=\$(cat \"\$provenance\"); configured_sha=\$(sha256sum localclash-subscriptions.json | awk '{print \$1}'); test \"\$configured_sha\" = \"\$expected_sha\"; '${guest_core}' subscription status --json > \"\$sub\" 2>/dev/null; '${guest_core}' runtime status --json > \"\$run\" 2>/dev/null; '${guest_helper}' call takeover_status > \"\$takeover\" 2>/dev/null; configured=\$(jsonfilter -i \"\$sub\" -e '@.status.configured' 2>/dev/null || true); merged=\$(jsonfilter -i \"\$sub\" -e '@.status.merged.exists' 2>/dev/null || true); running=\$(jsonfilter -i \"\$run\" -e '@.status.running' 2>/dev/null || true); effective=\$(jsonfilter -i \"\$takeover\" -e '@.status.effective' 2>/dev/null || true); dns_path=\$(jsonfilter -i \"\$takeover\" -e '@.status.dns.path' 2>/dev/null || true); test \"\$configured\" = true; test \"\$merged\" = true; test \"\$running\" = true; test \"\$effective\" = true; test \"\$dns_path\" = mihomo; printf 'one-click scene ready: subscription=real runtime=running takeover=effective dns_path=mihomo\\n'"

	ssh "${guest_ssh_opts[@]}" "$guest_ssh" "$guest_command" \
		|| die "iStoreOS one-click scene is not ready; fixture or stopped-runtime substitution is forbidden"
}

case "${1:-}" in
	subscription-sync) subscription_sync ;;
	assert-update-ready) assert_update_ready ;;
	-h | --help | help) usage ;;
	*) usage >&2; exit 2 ;;
esac
