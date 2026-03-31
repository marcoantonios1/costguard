#!/usr/bin/env bash
# Test response caching: identical requests return a cached copy; streaming is never cached
# Usage: ./scripts/test_caching.sh
# Requires: server running, jq installed

set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
PROVIDER="${PROVIDER:-anthropic_primary}"
MODEL="${MODEL:-claude-haiku-4-5-20251001}"

BODY="{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"What is 2+2? Reply with just the number.\"}]}"

COMMON=(
  -X POST "$BASE/v1/chat/completions"
  -H "Content-Type: application/json"
  -H "X-Costguard-Provider: $PROVIDER"
  -H "X-Costguard-Team: backend"
  -H "X-Costguard-Project: chatbot"
  -H "X-Costguard-User: marco"
)

# 1. First request — populates cache
echo "=== 1. First request (cache miss expected) ==="
R1=$(curl -s "${COMMON[@]}" -d "$BODY")
echo "$R1" | jq '{model:.model, content:.choices[0].message.content, tokens:.usage.total_tokens}'
echo "  Check logs for: cache_miss"
echo

# 2. Identical second request — should hit cache
echo "=== 2. Second identical request (cache HIT expected) ==="
R2=$(curl -s "${COMMON[@]}" -d "$BODY")
echo "$R2" | jq '{model:.model, content:.choices[0].message.content, tokens:.usage.total_tokens}'
echo "  Check logs for: cache_hit"
echo

# 3. Compare responses
echo "=== 3. Responses match? ==="
C1=$(echo "$R1" | jq -c '{content:.choices[0].message.content}')
C2=$(echo "$R2" | jq -c '{content:.choices[0].message.content}')
if [ "$C1" = "$C2" ]; then
  echo "  YES — content identical (cache working)"
else
  echo "  NO  — content differs (cache miss or disabled)"
  echo "  R1: $C1"
  echo "  R2: $C2"
fi
echo

# 4. Streaming is never cached
echo "=== 4. Streaming request (must NOT be cached) ==="
curl -s -N "${COMMON[@]}" \
  -d "{\"model\":\"$MODEL\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"What is 2+2?\"}]}" \
  | head -5
echo
echo "  Check logs: should see cache_miss (never cache_hit) for streaming"
echo

# 5. Different body → different cache key → miss
echo "=== 5. Different body → cache miss ==="
curl -s "${COMMON[@]}" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"What is 3+3?\"}]}" \
  | jq '{content:.choices[0].message.content}'
echo "  Check logs: cache_miss"
echo
