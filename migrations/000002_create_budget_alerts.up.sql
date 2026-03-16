CREATE TABLE IF NOT EXISTS budget_alerts (
    id BIGSERIAL PRIMARY KEY,
    period_start TIMESTAMPTZ NOT NULL,
    threshold_percent INTEGER NOT NULL,
    alert_type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_budget_alerts_period_threshold_type
ON budget_alerts (period_start, threshold_percent, alert_type);