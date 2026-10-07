#!/usr/bin/env bash
set -euo pipefail

SERVER_NAME="${LOCALCLASH_MCP_CLI_SERVER:-localclash}"
ADDR="${LOCALCLASH_MCP_CLI_ADDR:-127.0.0.1:18765}"
PATH_PREFIX="${LOCALCLASH_MCP_CLI_PATH:-/mcp}"
USE_EXISTING="${LOCALCLASH_MCP_CLI_USE_EXISTING:-0}"
LOG_FILE="$(mktemp "${TMPDIR:-/tmp}/localclash-mcp-cli.XXXXXX.log")"
CONFIG_FILE="$(mktemp "${TMPDIR:-/tmp}/localclash-mcp-cli.XXXXXX.json")"
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -f "${LOG_FILE}" "${CONFIG_FILE}"
}
trap cleanup EXIT

cat >"${CONFIG_FILE}" <<JSON
{
  "mcpServers": {
    "${SERVER_NAME}": {
      "url": "http://${ADDR}${PATH_PREFIX}"
    }
  }
}
JSON

if [[ "${USE_EXISTING}" != "1" ]]; then
  go run . mcp --addr "${ADDR}" --path "${PATH_PREFIX}" >"${LOG_FILE}" 2>&1 &
  SERVER_PID=$!
fi

for _ in $(seq 1 80); do
  if curl -fsS "http://${ADDR}/health" >/dev/null 2>&1; then
    break
  fi
  if [[ -n "${SERVER_PID}" ]] && ! kill -0 "${SERVER_PID}" 2>/dev/null; then
    cat "${LOG_FILE}" >&2
    exit 1
  fi
  sleep 0.1
done

curl -fsS "http://${ADDR}/health" >/dev/null

echo "mcp-cli ping ${SERVER_NAME}"
uvx mcp-cli ping \
  --config-file "${CONFIG_FILE}" \
  --server "${SERVER_NAME}" \
  --quiet \
  "${SERVER_NAME}"

echo "mcp-cli tools --raw"
tools_output="$(
  uvx mcp-cli tools \
    --config-file "${CONFIG_FILE}" \
    --server "${SERVER_NAME}" \
    --quiet \
    --raw
)"
printf '%s\n' "${tools_output}" | grep -o '"name": "[^"]*"' | sed 's/^/  /'

for expected_tool in doctor tools_list config_render subscriptions_status custom_sites_list custom_sites_transact; do
  if ! grep -q "\"name\": \"${expected_tool}\"" <<<"${tools_output}"; then
    echo "error: mcp-cli tools output did not include ${expected_tool}" >&2
    printf '%s\n' "${tools_output}" >&2
    exit 1
  fi
done

echo "mcp-cli interactive execute doctor"
doctor_output="$(
  printf 'execute doctor {}\nexit\n' | uvx mcp-cli interactive \
    --config-file "${CONFIG_FILE}" \
    --server "${SERVER_NAME}" \
    --quiet
)"
printf '%s\n' "${doctor_output}" | grep -m 1 '"status": "ok"' || true

if ! grep -q '"status": "ok"' <<<"${doctor_output}"; then
  echo "error: mcp-cli doctor execution did not return status ok" >&2
  printf '%s\n' "${doctor_output}" >&2
  exit 1
fi

echo "mcp-cli compatibility smoke test passed"
