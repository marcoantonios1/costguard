#!/usr/bin/env bash
# Test tool calling (function calling) across providers
# Usage: ./scripts/test_tool_calling.sh
# Requires: server running, jq installed

set -euo pipefail

BASE="${BASE:-http://localhost:8080}"

WEATHER_TOOL='{
  "type": "function",
  "function": {
    "name": "get_weather",
    "description": "Get current weather for a city",
    "parameters": {
      "type": "object",
      "properties": {
        "location": {"type": "string", "description": "City name"},
        "unit":     {"type": "string", "enum": ["celsius","fahrenheit"]}
      },
      "required": ["location"]
    }
  }
}'

call() {
  local provider="$1" model="$2"
  curl -s -X POST "$BASE/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -H "X-Costguard-Provider: $provider" \
    -H "X-Costguard-Team: backend" \
    -H "X-Costguard-Project: chatbot" \
    -H "X-Costguard-User: marco" \
    -d "{
      \"model\": \"$model\",
      \"tools\": [$WEATHER_TOOL],
      \"tool_choice\": \"auto\",
      \"messages\": [{\"role\": \"user\", \"content\": \"What is the weather in London?\"}]
    }"
}

# ─── Anthropic ────────────────────────────────────────────────────────────────
echo "=== 1. Anthropic — tool call turn 1 ==="
RESP1=$(call anthropic_primary claude-haiku-4-5-20251001)
echo "$RESP1" | jq '{finish_reason:.choices[0].finish_reason, tool_calls:.choices[0].message.tool_calls}'
echo

CALL_ID=$(echo "$RESP1" | jq -r '.choices[0].message.tool_calls[0].id // empty')
CALL_NAME=$(echo "$RESP1" | jq -r '.choices[0].message.tool_calls[0].function.name // empty')
CALL_ARGS=$(echo "$RESP1" | jq -r '.choices[0].message.tool_calls[0].function.arguments // empty')
echo "  Tool call id=$CALL_ID name=$CALL_NAME args=$CALL_ARGS"
echo

if [ -n "$CALL_ID" ]; then
  echo "=== 2. Anthropic — send tool result (turn 2) ==="
  ASSISTANT_MSG=$(echo "$RESP1" | jq -c '.choices[0].message')
  curl -s -X POST "$BASE/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -H "X-Costguard-Provider: anthropic_primary" \
    -H "X-Costguard-Team: backend" \
    -H "X-Costguard-Project: chatbot" \
    -H "X-Costguard-User: marco" \
    -d "{
      \"model\": \"claude-haiku-4-5-20251001\",
      \"tools\": [$WEATHER_TOOL],
      \"messages\": [
        {\"role\": \"user\", \"content\": \"What is the weather in London?\"},
        $ASSISTANT_MSG,
        {\"role\": \"tool\", \"tool_call_id\": \"$CALL_ID\", \"content\": \"15°C and partly cloudy\"}
      ]
    }" | jq '{finish_reason:.choices[0].finish_reason, reply:.choices[0].message.content}'
  echo
fi

# ─── Gemini ───────────────────────────────────────────────────────────────────
echo "=== 3. Gemini — tool call ==="
call google_primary gemini-2.5-flash \
  | jq '{finish_reason:.choices[0].finish_reason, tool_calls:.choices[0].message.tool_calls}'
echo

# ─── tool_choice: required ────────────────────────────────────────────────────
echo "=== 4. Anthropic — tool_choice: required (must call a tool) ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: anthropic_primary" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{
    \"model\": \"claude-haiku-4-5-20251001\",
    \"tools\": [$WEATHER_TOOL],
    \"tool_choice\": \"required\",
    \"messages\": [{\"role\": \"user\", \"content\": \"Say hello.\"}]
  }" | jq '{finish_reason:.choices[0].finish_reason, tool_calls:.choices[0].message.tool_calls}'
echo

# ─── tool_choice: none ───────────────────────────────────────────────────────
echo "=== 5. Anthropic — tool_choice: none (must NOT call a tool) ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: anthropic_primary" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{
    \"model\": \"claude-haiku-4-5-20251001\",
    \"tools\": [$WEATHER_TOOL],
    \"tool_choice\": \"none\",
    \"messages\": [{\"role\": \"user\", \"content\": \"What is the weather in London?\"}]
  }" | jq '{finish_reason:.choices[0].finish_reason, content:.choices[0].message.content}'
echo

# ─── Streaming tool call ──────────────────────────────────────────────────────
echo "=== 6. Anthropic — streaming tool call ==="
curl -s -N -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: anthropic_primary" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{
    \"model\": \"claude-haiku-4-5-20251001\",
    \"stream\": true,
    \"tools\": [$WEATHER_TOOL],
    \"tool_choice\": \"auto\",
    \"messages\": [{\"role\": \"user\", \"content\": \"What is the weather in London?\"}]
  }" | while IFS= read -r line; do
    [[ "$line" == data:* ]] && echo "$line" | sed 's/^data: //' | jq -c '{fr:.choices[0].finish_reason,tc:.choices[0].delta.tool_calls}' 2>/dev/null || true
  done
echo
