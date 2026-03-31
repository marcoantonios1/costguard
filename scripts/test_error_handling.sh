#!/usr/bin/env bash
# Test error handling: malformed bodies, wrong methods, missing fields, 4xx from upstream
# Usage: ./scripts/test_error_handling.sh
# Requires: server running, jq installed

set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
PROVIDER="${PROVIDER:-anthropic_primary}"
MODEL="${MODEL:-claude-haiku-4-5-20251001}"

check() {
  local label="$1" want="$2" got="$3"
  if [ "$got" = "$want" ]; then
    echo "  PASS — HTTP $got"
  else
    echo "  FAIL — got HTTP $got, want $want"
  fi
}

# 1. Wrong HTTP method on /v1/chat/completions
echo "=== 1. GET /v1/chat/completions → 405 ==="
GOT=$(curl -s -o /dev/null -w "%{http_code}" -X GET "$BASE/v1/chat/completions")
check "wrong method" 405 "$GOT"
echo

# 2. Malformed JSON body
echo "=== 2. Malformed JSON → 400 ==="
GOT=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: $PROVIDER" \
  -d "not json at all")
check "malformed json" 400 "$GOT"
echo

# 3. Missing model field
echo "=== 3. Missing model → error response ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: $PROVIDER" \
  -d '{"messages":[{"role":"user","content":"hi"}]}' | jq '{error:.error}'
echo

# 4. Missing messages field
echo "=== 4. Missing messages → error response ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: $PROVIDER" \
  -d "{\"model\":\"$MODEL\"}" | jq '{error:.error}'
echo

# 5. Unsupported path → 501
echo "=== 5. Unsupported path /v1/completions → 501 or 404 ==="
GOT=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "$BASE/v1/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: $PROVIDER" \
  -d "{\"model\":\"$MODEL\",\"prompt\":\"hi\"}")
echo "  HTTP $GOT"
echo

# 6. Empty body
echo "=== 6. Empty body → 400 ==="
GOT=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: $PROVIDER" \
  -d "")
check "empty body" 400 "$GOT"
echo

# 7. Unsupported role in messages
echo "=== 7. Unsupported message role → error ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: $PROVIDER" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"unknown_role\",\"content\":\"hi\"}]}" \
  | jq '{error:.error}'
echo
