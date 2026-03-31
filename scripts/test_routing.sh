#!/usr/bin/env bash
# Test routing: provider hint, mode hint, model-to-provider mapping, fallback
# Usage: ./scripts/test_routing.sh
# Requires: server running, jq installed

set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
MODEL_ANTHROPIC="${MODEL_ANTHROPIC:-claude-haiku-4-5-20251001}"
MODEL_GEMINI="${MODEL_GEMINI:-gemini-2.5-flash}"
MODEL_OPENAI="${MODEL_OPENAI:-gpt-4o-mini}"

post() {
  local extra=("$@")
  curl -s -X POST "$BASE/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -H "X-Costguard-Team: backend" \
    -H "X-Costguard-Project: chatbot" \
    -H "X-Costguard-User: marco" \
    "${extra[@]}" \
    -d "{\"model\":\"$MODEL_ANTHROPIC\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}"
}

# ─── 1. Explicit provider hint ────────────────────────────────────────────────
echo "=== 1. X-Costguard-Provider: anthropic_primary ==="
post -H "X-Costguard-Provider: anthropic_primary" \
  | jq '{model:.model, finish:.choices[0].finish_reason}'
echo "  Check logs: provider_hint_accepted, provider=anthropic_primary"
echo

# ─── 2. Model-based auto-routing (anthropic model → anthropic_primary) ────────
echo "=== 2. Auto-route by model ($MODEL_ANTHROPIC) ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{\"model\":\"$MODEL_ANTHROPIC\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" \
  | jq '{model:.model, finish:.choices[0].finish_reason}'
echo "  Check logs: route_selected, provider=anthropic_primary"
echo

# ─── 3. Mode hint: cheap → google_primary ─────────────────────────────────────
echo "=== 3. X-Costguard-Mode: cheap → google_primary ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Mode: cheap" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{\"model\":\"$MODEL_GEMINI\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" \
  | jq '{model:.model, finish:.choices[0].finish_reason}'
echo "  Check logs: mode_hint_accepted, mode=cheap, provider=google_primary"
echo

# ─── 4. Mode hint: best → anthropic_primary ───────────────────────────────────
echo "=== 4. X-Costguard-Mode: best → anthropic_primary ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Mode: best" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{\"model\":\"$MODEL_ANTHROPIC\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" \
  | jq '{model:.model, finish:.choices[0].finish_reason}'
echo

# ─── 5. Model compatibility rewrite ───────────────────────────────────────────
echo "=== 5. Model rewrite: send gpt-4o-mini to anthropic_primary ==="
echo "   (config maps gpt-4o-mini → claude-sonnet-4-6 on anthropic)"
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: anthropic_primary" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{\"model\":\"$MODEL_OPENAI\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" \
  | jq '{model:.model, finish:.choices[0].finish_reason}'
echo "  Check logs: model_rewritten_for_provider"
echo

# ─── 6. Invalid provider hint → 400 ──────────────────────────────────────────
echo "=== 6. Unknown provider hint → 400 ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: no_such_provider" \
  -d "{\"model\":\"$MODEL_ANTHROPIC\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" \
  | jq '{error:.error.message}'
echo

# ─── 7. Invalid mode hint → 400 ──────────────────────────────────────────────
echo "=== 7. Unknown mode hint → 400 ==="
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Mode: turbo_ultra" \
  -d "{\"model\":\"$MODEL_ANTHROPIC\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" \
  | jq '{error:.error.message}'
echo

# ─── 8. Fallback (primary unreachable) ───────────────────────────────────────
echo "=== 8. Fallback: broken provider → fallback to openai_primary ==="
set +e
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: does_not_exist" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d "{\"model\":\"$MODEL_ANTHROPIC\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" \
  | jq '{model:.model, error:.error.message}'
set -e
echo "  Check logs: provider_failed_try_fallback / fallback_used or fallback_failed"
echo
