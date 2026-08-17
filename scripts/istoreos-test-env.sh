#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runtime_dir="${LOCALCLASH_ISTOREOS_RUNTIME_DIR:-${repo_root}/.runtime/istoreos-qemu}"

# Keep the default image immutable and reproducible. Override all three values
# together when deliberately testing another official iStoreOS build.
firmware_name="${LOCALCLASH_ISTOREOS_FIRMWARE_NAME:-istoreos-24.10.8-2026073111-x86-64-squashfs-combined.img.gz}"
firmware_sha256="${LOCALCLASH_ISTOREOS_FIRMWARE_SHA256:-2ce609e2625f9ba67723ec29b0b509baa300c6b74f528596490d950909e09a9c}"
firmware_url="${LOCALCLASH_ISTOREOS_FIRMWARE_URL:-https://fw.koolcenter.com/iStoreOS/x86_64/${firmware_name}}"

archive_path="${runtime_dir}/${firmware_name}"
base_path="${runtime_dir}/istoreos-base.img"
overlay_path="${runtime_dir}/istoreos-test.qcow2"
pid_path="${runtime_dir}/qemu.pid"
monitor_path="${runtime_dir}/qemu-monitor.sock"
serial_path="${runtime_dir}/serial-console.sock"
qemu_log_path="${runtime_dir}/qemu.log"

luci_port="${LOCALCLASH_ISTOREOS_LUCI_PORT:-18089}"
ssh_port="${LOCALCLASH_ISTOREOS_SSH_PORT:-12223}"
mcp_port="${LOCALCLASH_ISTOREOS_MCP_PORT:-18766}"
controller_port="${LOCALCLASH_ISTOREOS_CONTROLLER_PORT:-19091}"
vnc_display="${LOCALCLASH_ISTOREOS_VNC_DISPLAY:-2}"

usage() {
	cat <<EOF
usage: scripts/istoreos-test-env.sh <command>

commands:
  prepare   Download and verify the pinned firmware, then create a qcow2 overlay
  start     Start the isolated iStoreOS VM
  wait      Wait until the LuCI HTTP endpoint answers
  status    Show the VM process and local endpoints
  console   Attach to the iStoreOS serial console (exit with Ctrl-C)
  stop      Stop the VM process
  reset     Stop the VM and replace only its writable overlay

Local endpoints:
  LuCI:       http://127.0.0.1:${luci_port}/
  SSH:        ssh -p ${ssh_port} root@127.0.0.1 (after configuring root auth)
  MCP:        http://127.0.0.1:${mcp_port}/mcp
  Controller: http://127.0.0.1:${controller_port}/
  VNC:        vnc://127.0.0.1:$((5900 + vnc_display))

Runtime files stay under:
  ${runtime_dir}
EOF
}

need_command() {
	command -v "$1" >/dev/null 2>&1 || {
		printf 'missing required command: %s\n' "$1" >&2
		exit 1
	}
}

sha256_file() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		sha256sum "$1" | awk '{print $1}'
	fi
}

read_pid() {
	[ -f "$pid_path" ] || return 1
	local pid
	pid="$(tr -dc '0-9' < "$pid_path")"
	[ -n "$pid" ] || return 1
	printf '%s\n' "$pid"
}

is_running() {
	local pid
	pid="$(read_pid)" || return 1
	kill -0 "$pid" 2>/dev/null
}

prepare() {
	need_command curl
	need_command gzip
	need_command qemu-img
	is_running && {
		printf 'iStoreOS VM is running; its prepared image is already in use\n'
		return 0
	}
	mkdir -p "$runtime_dir"

	if [ ! -f "$archive_path" ] || [ "$(sha256_file "$archive_path")" != "$firmware_sha256" ]; then
		printf 'Downloading pinned iStoreOS firmware: %s\n' "$firmware_name"
		curl -fL --retry 3 --continue-at - -o "${archive_path}.part" "$firmware_url"
		mv "${archive_path}.part" "$archive_path"
	fi

	local actual_sha256
	actual_sha256="$(sha256_file "$archive_path")"
	[ "$actual_sha256" = "$firmware_sha256" ] || {
		printf 'firmware SHA-256 mismatch: got %s, want %s\n' "$actual_sha256" "$firmware_sha256" >&2
		exit 1
	}

	if [ ! -s "$base_path" ]; then
		printf 'Expanding immutable base image\n'
		local gzip_rc
		set +e
		gzip -dc "$archive_path" > "${base_path}.part"
		gzip_rc=$?
		set -e
		case "$gzip_rc" in
			0) ;;
			2) printf 'gzip reported trailing vendor data; accepting the verified first gzip member\n' ;;
			*) printf 'firmware decompression failed with exit code %s\n' "$gzip_rc" >&2; exit "$gzip_rc" ;;
		esac
		[ -s "${base_path}.part" ] || {
			printf 'firmware decompression produced an empty image\n' >&2
			exit 1
		}
		mv "${base_path}.part" "$base_path"
		chmod 444 "$base_path"
	fi

	if [ ! -f "$overlay_path" ]; then
		qemu-img create -f qcow2 -F raw -b "$base_path" "$overlay_path"
	fi

	printf 'Prepared %s\n' "$runtime_dir"
	qemu-img info --backing-chain "$overlay_path"
}

start() {
	need_command qemu-system-x86_64
	is_running && {
		printf 'iStoreOS VM is already running (pid %s)\n' "$(read_pid)"
		return 0
	}
	[ -f "$overlay_path" ] || prepare
	rm -f "$pid_path" "$monitor_path"
	: > "$qemu_log_path"

	qemu-system-x86_64 \
		-name localclash-istoreos-test \
		-machine q35,accel=tcg,usb=off \
		-cpu max \
		-smp 2 \
		-m 2048 \
		-drive "file=${overlay_path},format=qcow2,if=ide,index=0,media=disk" \
		-netdev user,id=wan,net=10.0.2.0/24,host=10.0.2.2,dhcpstart=10.0.2.15 \
		-device virtio-net-pci,netdev=wan,mac=02:16:3e:1a:b3:20 \
		-netdev "user,id=lan,net=192.168.101.0/24,host=192.168.101.2,dhcpstart=192.168.101.100,hostfwd=tcp:127.0.0.1:${luci_port}-192.168.101.1:80,hostfwd=tcp:127.0.0.1:${ssh_port}-192.168.101.1:22,hostfwd=tcp:127.0.0.1:${mcp_port}-192.168.101.1:8765,hostfwd=tcp:127.0.0.1:${controller_port}-192.168.101.1:9090" \
		-device virtio-net-pci,netdev=lan,mac=02:16:3e:1a:b3:21 \
		-serial "unix:${serial_path},server=on,wait=off" \
		-monitor "unix:${monitor_path},server=on,wait=off" \
		-vnc "127.0.0.1:${vnc_display}" \
		-display none \
		-daemonize \
		-pidfile "$pid_path" \
		-D "$qemu_log_path"

	printf 'Started iStoreOS VM (pid %s)\n' "$(read_pid)"
	status
}

wait_ready() {
	need_command curl
	local attempt
	for attempt in $(seq 1 180); do
		is_running || {
			printf 'iStoreOS VM exited before LuCI became ready; inspect %s\n' "$qemu_log_path" >&2
			exit 1
		}
		if curl -fsS --max-time 2 "http://127.0.0.1:${luci_port}/" >/dev/null 2>&1; then
			printf 'LuCI is ready: http://127.0.0.1:%s/\n' "$luci_port"
			return 0
		fi
		sleep 2
	done
	printf 'timed out waiting for LuCI; use VNC or inspect %s\n' "$serial_path" >&2
	exit 1
}

status() {
	if is_running; then
		printf 'state: running (pid %s)\n' "$(read_pid)"
	else
		printf 'state: stopped\n'
	fi
	printf 'firmware: %s\n' "$firmware_name"
	printf 'LuCI: http://127.0.0.1:%s/\n' "$luci_port"
	printf 'SSH: ssh -p %s root@127.0.0.1 (after configuring root auth)\n' "$ssh_port"
	printf 'MCP: http://127.0.0.1:%s/mcp\n' "$mcp_port"
	printf 'Controller: http://127.0.0.1:%s/\n' "$controller_port"
	printf 'VNC: vnc://127.0.0.1:%s\n' "$((5900 + vnc_display))"
}

console() {
	need_command nc
	is_running || {
		printf 'iStoreOS VM is not running\n' >&2
		exit 1
	}
	[ -S "$serial_path" ] || {
		printf 'serial console is unavailable: %s\n' "$serial_path" >&2
		exit 1
	}
	printf 'Attaching to iStoreOS serial console; press Ctrl-C to detach\n' >&2
	nc -U "$serial_path"
}

stop() {
	local pid
	pid="$(read_pid)" || {
		printf 'iStoreOS VM is already stopped\n'
		return 0
	}
	if ! kill -0 "$pid" 2>/dev/null; then
		rm -f "$pid_path" "$monitor_path"
		printf 'iStoreOS VM is already stopped\n'
		return 0
	fi
	local process_command
	process_command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
	case "$process_command" in
		*qemu-system-x86_64*"$overlay_path"*) ;;
		*) printf 'refusing to stop unexpected pid %s: %s\n' "$pid" "$process_command" >&2; exit 1 ;;
	esac
	kill -TERM "$pid"
	local attempt
	for attempt in $(seq 1 30); do
		kill -0 "$pid" 2>/dev/null || break
		sleep 1
	done
	if kill -0 "$pid" 2>/dev/null; then
		printf 'VM did not stop after 30 seconds; leaving pid %s running\n' "$pid" >&2
		exit 1
	fi
	rm -f "$pid_path" "$monitor_path"
	printf 'Stopped iStoreOS VM\n'
}

reset_overlay() {
	stop
	[ -f "$base_path" ] || prepare
	rm -f "$overlay_path"
	qemu-img create -f qcow2 -F raw -b "$base_path" "$overlay_path"
	printf 'Reset writable overlay; immutable firmware base was preserved\n'
}

case "${1:-}" in
	prepare) prepare ;;
	start) start ;;
	wait) wait_ready ;;
	status) status ;;
	console) console ;;
	stop) stop ;;
	reset) reset_overlay ;;
	*) usage; exit 2 ;;
esac
