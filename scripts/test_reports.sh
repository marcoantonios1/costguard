#!/usr/bin/env bash
# Test /admin/reports/monthly/send
# Usage: ./scripts/test_reports.sh
# Requires: server running, email configured in config.json / .env

set -euo pipefail

ADMIN="${ADMIN:-http://localhost:8080/admin}"
ADMIN_KEY="${ADMIN_KEY:-gdHs-h45g-9b8e-4c1a-9c3d-2f5e6f7g8h9}"

# 1. Trigger monthly report
echo "=== 1. POST /admin/reports/monthly/send ==="
curl -s -X POST "$ADMIN/reports/monthly/send" \
  -H "Authorization: Bearer $ADMIN_KEY" | jq .
echo
echo "  Check email inbox and Costguard logs for:"
echo "    monthly_report_sent  (success)"
echo "    monthly_report_failed (failure — check SMTP config)"
echo

# 2. Wrong method → 405
echo "=== 2. GET /admin/reports/monthly/send → 405 ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -X GET "$ADMIN/reports/monthly/send" \
  -H "Authorization: Bearer $ADMIN_KEY")
echo "  HTTP $STATUS (want 405)"
echo

# 3. No auth → 401
echo "=== 3. No Authorization header → 401 ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "$ADMIN/reports/monthly/send")
echo "  HTTP $STATUS (want 401)"
echo
