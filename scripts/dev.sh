#!/usr/bin/env bash
# Test X-Costguard-Agent header tracking
# Run while the server is up: make run / docker compose up

BASE="http://localhost:8080"
PROVIDER="anthropic_primary"
MODEL="claude-haiku-4-5-20251001"
ADMIN="http://localhost:8080/admin"
ADMIN_KEY="gdHs-h45g-9b8e-4c1a-9c3d-2f5e6f7g8h9"   # from .env ADMIN_API_KEY

NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
MONTH_START=$(date -u +"%Y-%m-01T00:00:00Z")

COMMON_HEADERS=(
  -H "Content-Type: application/json"
  -H "X-Costguard-Provider: $PROVIDER"
  -H "X-Costguard-Team: backend"
  -H "X-Costguard-Project: chatbot"
  -H "X-Costguard-User: marco"
)

# ─── 1. Request from the router agent ────────────────────────────────────────
echo "=== 1. router agent (cheap classification call) ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  "${COMMON_HEADERS[@]}" \
  -H "X-Costguard-Agent: router" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Classify: support or sales? Reply with one word.\"}]}" \
  | jq '{model:.model, content:.choices[0].message.content, tokens:.usage}'

echo

# ─── 2. Request from the builder agent ───────────────────────────────────────
echo "=== 2. builder agent (expensive code-gen call) ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  "${COMMON_HEADERS[@]}" \
  -H "X-Costguard-Agent: builder" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Write a Go function that adds two ints.\"}]}" \
  | jq '{model:.model, content:.choices[0].message.content, tokens:.usage}'

echo

# ─── 3. Request with no agent header (backward compat) ───────────────────────
echo "=== 3. no agent header (backward compat) ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  "${COMMON_HEADERS[@]}" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Say hi.\"}]}" \
  | jq '{model:.model, content:.choices[0].message.content, tokens:.usage}'

echo

# ─── 4. Streaming request with agent header ───────────────────────────────────
echo "=== 4. comms agent — streaming ==="
curl -s -N -X POST "$BASE/v1/chat/completions" \
  "${COMMON_HEADERS[@]}" \
  -H "X-Costguard-Agent: comms" \
  -d "{\"model\":\"$MODEL\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"Count 1 to 3.\"}]}"

echo
echo

# ─── 5. Admin: spend by agent ─────────────────────────────────────────────────
echo "=== 5. GET /admin/usage/agents (this month) ==="
curl -s "$ADMIN/usage/agents?from=${MONTH_START}&to=${NOW}" \
  -H "Authorization: Bearer $ADMIN_KEY" \
  | jq .

echo

# ─── 6. Admin: spend by team (unchanged, sanity check) ───────────────────────
echo "=== 6. GET /admin/usage/teams (sanity check) ==="
curl -s "$ADMIN/usage/teams?from=${MONTH_START}&to=${NOW}" \
  -H "Authorization: Bearer $ADMIN_KEY" \
  | jq .