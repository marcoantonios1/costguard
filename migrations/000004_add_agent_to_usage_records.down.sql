DROP INDEX IF EXISTS idx_usage_agent_timestamp;
ALTER TABLE usage_records DROP COLUMN IF EXISTS agent;
