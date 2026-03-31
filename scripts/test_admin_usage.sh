#!/usr/bin/env bash
# Test all /admin/usage/* endpoints
# Usage: ./scripts/test_admin_usage.sh
# Requires: server running, jq installed

set -euo pipefail

ADMIN="${ADMIN:-http://localhost:8080/admin}"
ADMIN_KEY="${ADMIN_KEY:-gdHs-h45g-9b8e-4c1a-9c3d-2f5e6f7g8h9}"
BASE="${BASE:-http://localhost:8080}"
PROVIDER="${PROVIDER:-anthropic_primary}"
MODEL="${MODEL:-claude-haiku-4-5-20251001}"

NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
MONTH_START=$(date -u +"%Y-%m-01T00:00:00Z")

auth() { echo -H "Authorization: Bearer $ADMIN_KEY"; }

# Seed a couple of requests so there is something to report on
echo "=== Seeding: two chat requests ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: $PROVIDER" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Say hi.\"}]}" > /dev/null
echo "  request 1 done"

curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: $PROVIDER" \
  -H "X-Costguard-Team: mobile" \
  -H "X-Costguard-Project: analytics" \
  -H "X-Costguard-User: alice" \
  -H "X-Costguard-Agent: router" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Say hi.\"}]}" > /dev/null
echo "  request 2 done"
echo

# 1. Summary
echo "=== 1. GET /admin/usage/summary ==="
curl -s "$ADMIN/usage/summary?from=${MONTH_START}&to=${NOW}" \
  -H "Authorization: Bearer $ADMIN_KEY" | jq .
echo

# 2. By team
echo "=== 2. GET /admin/usage/teams ==="
curl -s "$ADMIN/usage/teams?from=${MONTH_START}&to=${NOW}" \
  -H "Authorization: Bearer $ADMIN_KEY" | jq .
echo

# 3. By project
echo "=== 3. GET /admin/usage/projects ==="
curl -s "$ADMIN/usage/projects" \
  -H "Authorization: Bearer $ADMIN_KEY" | jq .
echo

# 4. By agent
echo "=== 4. GET /admin/usage/agents ==="
curl -s "$ADMIN/usage/agents?from=${MONTH_START}&to=${NOW}" \
  -H "Authorization: Bearer $ADMIN_KEY" | jq .
echo

# 5. Auth check — wrong key should be rejected
echo "=== 5. Auth: wrong key → 401 ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  "$ADMIN/usage/summary?from=${MONTH_START}&to=${NOW}" \
  -H "Authorization: Bearer wrong-key")
echo "  HTTP $STATUS (want 401)"
echo

# 6. Missing date range
echo "=== 6. Missing from/to → error ==="
curl -s "$ADMIN/usage/summary" \
  -H "Authorization: Bearer $ADMIN_KEY" | cat
echo
