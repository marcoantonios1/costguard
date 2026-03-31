#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
TEAM="${TEAM:-backend}"
PROJECT="${PROJECT:-chatbot}"
USER_NAME="${USER_NAME:-marco}"

# Change these depending on what you want to test
MODEL_NON_STREAM="${MODEL_NON_STREAM:-llama3.2}"
MODEL_STREAM="${MODEL_STREAM:-llama3.2}"

# Main provider for normal test
PROVIDER_PRIMARY="${PROVIDER_PRIMARY:-local_ollama}"

# Optional fallback test:
# set this to a broken/non-existing provider mapping only if your config supports fallback
PROVIDER_BROKEN="${PROVIDER_BROKEN:-does_not_exist}"

echo "=================================================="
echo "1) NON-STREAM BASELINE"
echo "=================================================="

time curl -sS \
  -X POST "$BASE_URL/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: $PROVIDER_PRIMARY" \
  -H "X-Costguard-Team: $TEAM" \
  -H "X-Costguard-Project: $PROJECT" \
  -H "X-Costguard-User: $USER_NAME" \
  -d "{
    \"model\": \"$MODEL_NON_STREAM\",
    \"messages\": [
      {\"role\": \"user\", \"content\": \"Say hello in two sentences.\"}
    ]
  }" | jq .

echo
echo "=================================================="
echo "2) STREAMING REAL-TIME TEST"
echo "Expected: chunks should appear progressively, not all at once"
echo "=================================================="

START_TS=$(date +%s)

curl -N -sS \
  -X POST "$BASE_URL/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -H "X-Costguard-Provider: $PROVIDER_PRIMARY" \
  -H "X-Costguard-Mode: cheap" \
  -H "X-Costguard-Team: $TEAM" \
  -H "X-Costguard-Project: $PROJECT" \
  -H "X-Costguard-User: $USER_NAME" \
  -d "{
    \"model\": \"$MODEL_STREAM\",
    \"stream\": true,
    \"messages\": [
      {\"role\": \"user\", \"content\": \"Count from 1 to 20 slowly, one number at a time, and add a short word after each number.\"}
    ]
  }" | while IFS= read -r line; do
    NOW_TS=$(date +%s)
    DELTA=$((NOW_TS - START_TS))
    printf '[+%02ss] %s\n' "$DELTA" "$line"
  done

echo
echo "If the gateway is truly streaming-aware, you should see lines arrive over time."
echo "If everything prints only at the end, buffering is still happening somewhere."

echo
echo "=================================================="
echo "3) STREAM RESPONSE HEADERS TEST"
echo "Expected: Content-Type should be text/event-stream"
echo "=================================================="

curl -i -N -sS \
  -X POST "$BASE_URL/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -H "X-Costguard-Provider: $PROVIDER_PRIMARY" \
  -H "X-Costguard-Team: $TEAM" \
  -H "X-Costguard-Project: $PROJECT" \
  -H "X-Costguard-User: $USER_NAME" \
  -d "{
    \"model\": \"$MODEL_STREAM\",
    \"stream\": true,
    \"messages\": [
      {\"role\": \"user\", \"content\": \"Say hi as a streamed response.\"}
    ]
  }" | sed -n '1,30p'

echo
echo "Check these headers in the response:"
echo "  Content-Type: text/event-stream"
echo "Nice to also have:"
echo "  Cache-Control: no-cache"
echo "  Connection: keep-alive"

echo
echo "=================================================="
echo "4) FALLBACK TEST (only if your config fallback is enabled)"
echo "Expected: if primary fails before stream starts, fallback provider should answer"
echo "=================================================="

set +e
curl -N -sS \
  -X POST "$BASE_URL/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -H "X-Costguard-Provider: $PROVIDER_BROKEN" \
  -H "X-Costguard-Team: $TEAM" \
  -H "X-Costguard-Project: $PROJECT" \
  -H "X-Costguard-User: $USER_NAME" \
  -d "{
    \"model\": \"$MODEL_STREAM\",
    \"stream\": true,
    \"messages\": [
      {\"role\": \"user\", \"content\": \"If fallback works, answer with: fallback success.\"}
    ]
  }"
FALLBACK_EXIT=$?
set -e

echo
echo "curl exit code: $FALLBACK_EXIT"
echo "Now inspect Costguard logs for:"
echo "  provider_failed_try_fallback"
echo "  fallback_used"

echo
echo "=================================================="
echo "5) METERING CHECK"
echo "Expected: streaming request is metered after stream completes"
echo "=================================================="

echo "After finishing the stream, check:"
echo "  - Costguard logs for request_metered with streaming=true"
echo "  - usage_records table for the latest row"
echo
echo "Suggested SQL:"
cat <<'SQL'
SELECT
  request_id,
  provider,
  model,
  prompt_tokens,
  completion_tokens,
  total_tokens,
  estimated_cost_usd,
  cache_hit,
  status_code,
  timestamp_utc
FROM usage_records
ORDER BY timestamp_utc DESC
LIMIT 10;
SQL

echo
echo "For streaming requests, you want to confirm:"
echo "  - cache_hit = false"
echo "  - total_tokens > 0 (if provider sends usage)"
echo "  - status_code = 200"