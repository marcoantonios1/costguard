#!/usr/bin/env bash
# Test /admin/providers and /admin/routing/modes
# Usage: ./scripts/test_providers_and_modes.sh
# Requires: server running, jq installed

set -euo pipefail

ADMIN="${ADMIN:-http://localhost:8080/admin}"
ADMIN_KEY="${ADMIN_KEY:-gdHs-h45g-9b8e-4c1a-9c3d-2f5e6f7g8h9}"
BASE="${BASE:-http://localhost:8080}"
MODEL="${MODEL:-claude-haiku-4-5-20251001}"

# 1. List all registered providers
echo "=== 1. GET /admin/providers ==="
curl -s "$ADMIN/providers" \
  -H "Authorization: Bearer $ADMIN_KEY" | jq '.providers[] | {name,type,kind,supports_streaming,supports_tools,priority}'
echo

# 2. List mode → provider mappings
echo "=== 2. GET /admin/routing/modes ==="
curl -s "$ADMIN/routing/modes" \
  -H "Authorization: Bearer $ADMIN_KEY" | jq .
echo

# 3. Use mode hint: cheap → google_primary
echo "=== 3. X-Costguard-Mode: cheap → google_primary ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Mode: cheap" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{\"model\":\"gemini-2.5-flash\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" \
  | jq '{model:.model, finish:.choices[0].finish_reason}'
echo

# 4. Use mode hint: best → anthropic_primary
echo "=== 4. X-Costguard-Mode: best → anthropic_primary ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Mode: best" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" \
  | jq '{model:.model, finish:.choices[0].finish_reason}'
echo

# 5. Unknown mode → 400
echo "=== 5. Unknown mode → 400 ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Mode: nonexistent" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}")
echo "  HTTP $STATUS (want 400)"
echo

# 6. Unknown provider hint → 400
echo "=== 6. Unknown provider hint → 400 ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: does_not_exist" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" \
  | jq '{error:.error.message}'
echo
