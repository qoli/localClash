#!/bin/sh

set -eu

skill_name="localclash-mcp-route-operator"
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "${script_dir}/.." && pwd)
source_dir="${repo_root}/.codex/skills/${skill_name}"

if [ -n "${CODEX_HOME:-}" ]; then
  codex_install_home="${CODEX_HOME}"
elif [ -n "${HOME:-}" ]; then
  codex_install_home="${HOME}/.codex"
else
  echo "error: CODEX_HOME and HOME are both unset" >&2
  exit 1
fi

install_root="${codex_install_home}/skills"
target_dir="${install_root}/${skill_name}"

if [ ! -f "${source_dir}/SKILL.md" ] || [ ! -f "${source_dir}/agents/openai.yaml" ]; then
  echo "error: incomplete companion skill source at ${source_dir}" >&2
  exit 1
fi

case "${1:-}" in
  "")
    ;;
  --check)
    if [ ! -d "${target_dir}" ]; then
      echo "not installed: ${target_dir}" >&2
      exit 1
    fi
    if diff -qr "${source_dir}" "${target_dir}" >/dev/null; then
      echo "up to date: ${target_dir}"
      exit 0
    fi
    echo "out of date: ${target_dir}" >&2
    diff -qr "${source_dir}" "${target_dir}" >&2 || true
    exit 1
    ;;
  *)
    echo "usage: $0 [--check]" >&2
    exit 2
    ;;
esac

mkdir -p "${install_root}"
stage_root=$(mktemp -d "${install_root}/.localclash-skill-install.XXXXXX")
next_dir="${stage_root}/${skill_name}"
previous_dir="${stage_root}/previous"
had_previous=0

cleanup() {
  rm -rf "${stage_root}"
}
trap cleanup EXIT HUP INT TERM

cp -R "${source_dir}" "${next_dir}"

if [ -e "${target_dir}" ] || [ -L "${target_dir}" ]; then
  mv "${target_dir}" "${previous_dir}"
  had_previous=1
fi

if mv "${next_dir}" "${target_dir}"; then
  echo "installed: ${target_dir}"
  exit 0
fi

if [ "${had_previous}" -eq 1 ] && [ ! -e "${target_dir}" ]; then
  mv "${previous_dir}" "${target_dir}"
fi
echo "error: failed to install ${skill_name}" >&2
exit 1
