ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS agent TEXT;

CREATE INDEX IF NOT EXISTS idx_usage_agent_timestamp
ON usage_records (agent, timestamp_utc);
