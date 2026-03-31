#!/usr/bin/env bash
# Test /admin/budget/status and budget enforcement
# Usage: ./scripts/test_budget.sh
# Requires: server running, jq installed

set -euo pipefail

ADMIN="${ADMIN:-http://localhost:8080/admin}"
ADMIN_KEY="${ADMIN_KEY:-gdHs-h45g-9b8e-4c1a-9c3d-2f5e6f7g8h9}"
BASE="${BASE:-http://localhost:8080}"
PROVIDER="${PROVIDER:-anthropic_primary}"
MODEL="${MODEL:-claude-haiku-4-5-20251001}"

# 1. Budget status
echo "=== 1. GET /admin/budget/status ==="
curl -s "$ADMIN/budget/status" \
  -H "Authorization: Bearer $ADMIN_KEY" | jq .
echo

# 2. Normal request — should succeed (budget not exhausted)
echo "=== 2. Normal request — expect 200 ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: $PROVIDER" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}")
echo "  HTTP $STATUS (want 200)"
echo

# 3. Budget status again — confirm spend increased
echo "=== 3. Budget status after request ==="
curl -s "$ADMIN/budget/status" \
  -H "Authorization: Bearer $ADMIN_KEY" | jq '{
    monthly_limit:    .monthly_limit_usd,
    monthly_spent:    .monthly_spent_usd,
    monthly_percent:  .monthly_percent,
    team_limits:      .teams,
    project_limits:   .projects
  }'
echo

# 4. Wrong HTTP method — expect 405
echo "=== 4. POST /admin/budget/status → 405 ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "$ADMIN/budget/status" \
  -H "Authorization: Bearer $ADMIN_KEY")
echo "  HTTP $STATUS (want 405)"
echo
