#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: LIFELEDGER_API_TOKEN=<token> scripts/smoke.sh <base-url> [--chat]" >&2
  exit 2
}

[[ $# -ge 1 ]] || usage
base="${1%/}"
chat="${2:-}"
token="${LIFELEDGER_API_TOKEN:?set LIFELEDGER_API_TOKEN to the API token of the server}"
failures=0

pass() { printf '  ok    %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; failures=$((failures + 1)); }

request() {
  local method="$1" path="$2" auth="$3" body="${4:-}"
  local args=(-sS -o "$tmp/body" -D "$tmp/headers" -w '%{http_code}' -X "$method" --max-time 120)
  [[ "$auth" == yes ]] && args+=(-H "Authorization: Bearer $token")
  [[ -n "$body" ]] && args+=(-H 'Content-Type: application/json' --data "$body")
  curl "${args[@]}" "$base$path" || echo 000
}

expect() {
  local name="$1" got="$2" want="$3" pattern="${4:-}"
  if [[ "$got" != "$want" ]]; then
    fail "$name (HTTP $got, want $want)"
  elif [[ -n "$pattern" ]] && ! grep -Eq "$pattern" "$tmp/body"; then
    fail "$name (body has no match for $pattern)"
  else
    pass "$name"
  fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "LifeLedger smoke test: $base"

expect "health endpoint" "$(request GET /healthz no)" 200 '"status":"ok"'

code="$(request GET / no)"
expect "web app" "$code" 200 '<ll-root'
if grep -qi '^content-security-policy:' "$tmp/headers"; then pass "security headers"; else fail "security headers (no Content-Security-Policy)"; fi

expect "API rejects a missing token" "$(request GET /api/v1/overview no)" 401 '"unauthorized"'
expect "API overview" "$(request GET /api/v1/overview yes)" 200 '"today":"[0-9]{4}-[0-9]{2}-[0-9]{2}"'
expect "ledger" "$(request GET '/api/v1/obligations?limit=5' yes)" 200 '"items":'
expect "memory" "$(request GET '/api/v1/memories?limit=5' yes)" 200 '"items":'
expect "notifications" "$(request GET '/api/v1/notifications?limit=5' yes)" 200 '"items":'

if [[ "$chat" == --chat ]]; then
  code="$(request POST /api/v1/chat yes '{"message":"Which bills are due in the next 7 days? Answer in one sentence."}')"
  expect "agent reply from Nemotron" "$code" 200 '"reply":"[^"]+'
  id="$(grep -Eo '"conversation":\{"id":[0-9]+' "$tmp/body" | grep -Eo '[0-9]+$' || true)"
  if [[ -n "$id" ]]; then
    expect "delete the test conversation" "$(request DELETE "/api/v1/conversations/$id" yes)" 204
  fi
fi

if [[ $failures -gt 0 ]]; then
  echo "$failures check(s) failed"
  exit 1
fi
echo "all checks passed"
