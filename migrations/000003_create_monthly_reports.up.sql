CREATE TABLE IF NOT EXISTS monthly_reports (
    id BIGSERIAL PRIMARY KEY,
    period_start TIMESTAMPTZ NOT NULL,
    report_type TEXT NOT NULL,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_monthly_reports_period_type
ON monthly_reports (period_start, report_type);