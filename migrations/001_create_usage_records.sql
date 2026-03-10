CREATE TABLE IF NOT EXISTS usage_records (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    timestamp_utc TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost_usd NUMERIC(12, 6) NOT NULL DEFAULT 0,
    price_found BOOLEAN NOT NULL DEFAULT FALSE,
    cache_hit BOOLEAN NOT NULL DEFAULT FALSE,
    team TEXT,
    project TEXT,
    user_name TEXT,
    path TEXT NOT NULL,
    status_code INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_usage_timestamp
ON usage_records (timestamp_utc);

CREATE INDEX IF NOT EXISTS idx_usage_team_timestamp
ON usage_records (team, timestamp_utc);

CREATE INDEX IF NOT EXISTS idx_usage_project_timestamp
ON usage_records (project, timestamp_utc);

CREATE INDEX IF NOT EXISTS idx_usage_provider_timestamp
ON usage_records (provider, timestamp_utc);

CREATE INDEX IF NOT EXISTS idx_usage_model_timestamp
ON usage_records (model, timestamp_utc);