#!/usr/bin/env bash
set -euo pipefail

ADDR="${LOCALCLASH_MCP_ADDR:-127.0.0.1:0}"
PATH_PREFIX="${LOCALCLASH_MCP_PATH:-/mcp}"
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/localclash-mcp-http.XXXXXX")"
server_pid=""

cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf -- "$test_dir"
}
trap cleanup EXIT
go build -C "$repo_dir" -o "$test_dir/localclash" .
"$test_dir/localclash" mcp --addr "$ADDR" --path "$PATH_PREFIX" >"$test_dir/server.log" 2>&1 &
server_pid=$!

endpoint=""
while [[ -z "$endpoint" ]]; do
  if ! kill -0 "$server_pid" 2>/dev/null; then
    cat "$test_dir/server.log" >&2
    exit 1
  fi
  endpoint="$(sed -n 's/^localClash MCP HTTP listening on //p' "$test_dir/server.log")"
  if [[ -z "$endpoint" ]]; then sleep 0.1; fi
done

response="$(
  curl --noproxy '*' -fsS \
    -H 'Content-Type: application/json' \
    -X POST "$endpoint" \
    --data '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"doctor","arguments":{}}}'
)"

printf '%s\n' "${response}"

if ! kill -0 "$server_pid" 2>/dev/null || ! jq -e '.result.structuredContent.status == "ok" and (.result.isError != true)' <<<"$response" >/dev/null; then
  echo "error: MCP HTTP doctor smoke test did not return status ok" >&2
  exit 1
fi
