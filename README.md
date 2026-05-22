# Costguard

Costguard is an AI gateway that adds cost tracking, budgeting, multi-provider routing, streaming, tool calling, and usage analytics to any LLM application — without changing your application code.

---

## Why Costguard?

Most LLM applications eventually run into:

- Runaway costs from expensive models with no spending limits
- Repeated identical API calls that could be cached
- No visibility into token usage by team, project, or agent
- No policy layer between the app and the model provider
- No automatic fallback when a provider is down or rate-limited

Costguard solves these by sitting between your application and the model providers as a thin, OpenAI-compatible proxy.

---

## Architecture

```
Your App (OpenAI SDK / curl)
        │
        ▼
  Costguard Gateway  (:8080)
  ┌───────────────────────────────────────┐
  │  Routing → Budget check               │
  │  Cache lookup                         │
  │  Provider selection                   │
  │  Circuit breaker gate                 │
  │  Retry with backoff                   │
  │  Health recording                     │
  │  Streaming / tool call                │
  │  Metering → DB                        │
  └───────────────────────────────────────┘
        │              │ (fallback)
        ▼              ▼
   Primary         Fallback
   Provider        Provider
```

---

## Quick Start

**1. Start the server:**

```bash
go run ./cmd/api -config ./config.json
```

Or with Docker:

```bash
docker compose up --build
```

**2. Point your application at Costguard:**

```bash
OPENAI_BASE_URL=http://localhost:8080
```

No other application changes are required.

**3. Send a request:**

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

---

## Features

### Multi-Provider Routing

Route requests to OpenAI, Anthropic, Gemini, or any OpenAI-compatible endpoint (Ollama, vLLM, etc.) based on model name, a routing hint, or an explicit provider override.

### Fallback

If the primary provider fails, Costguard automatically retries with the configured fallback provider and maps the model name if needed.

### Per-Provider Retry Policy

Configure per-provider retry behaviour with exponential backoff. Retries are attempted automatically before falling back to the secondary provider.

```json
"retry": {
  "max_attempts": 3,
  "retry_on_5xx": true,
  "retry_on_timeout": true,
  "initial_backoff": "500ms",
  "max_backoff": "10s"
}
```

- **`max_attempts`** — total attempts including the first (default: 1 = no retry).
- **`retry_on_5xx`** — retry on `5xx` upstream responses.
- **`retry_on_timeout`** — retry on network timeouts.
- **`initial_backoff`** / **`max_backoff`** — exponential backoff with ±10% jitter, capped at `max_backoff`.
- **429 Retry-After** — honoured automatically; the delay is capped at `max_backoff × 3`.

Each retry attempt is logged with `provider_retry_attempt` and includes the attempt number, reason, and backoff applied. When all attempts are exhausted on a retryable error, `provider_retry_exhausted` is logged and the fallback fires.

### Circuit Breaker

A per-provider three-state circuit breaker (`closed` → `open` → `half-open`) prevents cascading failures by short-circuiting calls to unhealthy providers without waiting for a timeout.

```json
"breaker": {
  "failure_threshold": 5,
  "cooldown_seconds": 30
}
```

- **Closed** — all requests pass through. Consecutive relevant failures are counted.
- **Open** — requests are rejected immediately (no upstream call). Transitions to half-open after `cooldown_seconds`.
- **Half-open** — one probe request is allowed. Success → closed. Failure → re-opens.

Only `upstream_failure` and `provider_unavailable` errors count toward the threshold. Authentication errors, invalid request errors, and rate limits do **not** trip the breaker — these are caller or quota issues, not provider health signals.

When the breaker opens, Costguard falls through to the fallback provider automatically. State transitions are logged:

| Log event | Meaning |
|---|---|
| `circuit_breaker_tripped` | Threshold reached; breaker opened |
| `circuit_breaker_open` | Request blocked; fallback will fire |
| `circuit_breaker_closed` | Probe succeeded; breaker closed |

### Provider Health Monitoring

Every upstream call records an outcome (success/failure, latency, error category) in a fixed-size ring buffer (last 100 calls per provider). Live stats are available from the admin health endpoint.

### Error Taxonomy

All upstream errors are normalised to one of five categories, returned in the `error.category` field of every error response:

| Category | Meaning | Typical HTTP status |
|---|---|---|
| `auth` | Bad or missing API key | 401, 403 |
| `invalid_request` | Malformed request | 400, 422 |
| `rate_limit` | Quota or rate limit exceeded | 429 |
| `provider_unavailable` | Network error, timeout, or circuit open | 502, 503, 504 |
| `upstream_failure` | Generic provider-side error | 5xx |

Example error response:

```json
{
  "error": {
    "message": "Incorrect API key provided.",
    "type": "invalid_api_key",
    "category": "auth"
  }
}
```

### Caching

Identical requests return cached responses without hitting the upstream provider. Cache keys include routing context (provider, mode, model) to prevent cross-provider reuse. Streaming responses are never cached.

### Budget Enforcement

Hard monthly spending limits at the global, team, project, and agent level. Requests that would breach a limit receive an immediate `402 Payment Required`.

### Tool Calling

Full multi-turn tool calling is supported on Anthropic and Gemini. Requests use the standard OpenAI `tools` / `tool_calls` format — Costguard translates to each provider's native format.

### Streaming

Server-sent events (SSE) streaming is supported on all providers. Set `"stream": true` in the request body.

### Vision & Multimodal

Send image content using the standard OpenAI `image_url` format — Costguard translates it to each provider's native wire format (Anthropic `source` blocks, Gemini `inlineData` parts).

**Provider support:**

| Provider | Image inputs | Token estimation |
|---|---|---|
| OpenAI | ✅ | tile-based (detail level) |
| Anthropic | ✅ | tile-based (Anthropic formula) |
| Gemini | ✅ | trusts `usageMetadata` |
| OpenAI-compatible (Ollama, vLLM) | opt-in | — |

Because some providers do not include image tokens in their usage response, Costguard estimates vision tokens client-side and adds them to the recorded prompt token count. This ensures budget enforcement accounts for the true cost of vision requests.

OpenAI-compatible providers reject image content by default. Set `allow_multimodal: true` to pass image blocks through:

```json
"openai_compatible": {
  "local_ollama": {
    "base_url": "http://host.docker.internal:11434",
    "allow_multimodal": true
  }
}
```

### Per-Agent Cost Tracking

Tag requests with `X-Costguard-Agent` to track and limit spend per agent or automated pipeline.

### Usage Analytics

Query spend by team, project, agent, provider, or model via the Admin API.

---

## Request Headers

All headers are optional. Omit any that are not needed.

| Header | Purpose |
|---|---|
| `X-Costguard-Provider` | Pin request to a specific named provider (e.g. `anthropic_primary`) |
| `X-Costguard-Mode` | Routing hint: `cheap`, `balanced`, `best`, `private` (configurable) |
| `X-Costguard-Team` | Tag request with a team name for cost tracking and budget enforcement |
| `X-Costguard-Project` | Tag request with a project name for cost tracking and budget enforcement |
| `X-Costguard-Agent` | Tag request with an agent name for per-agent cost tracking and budget enforcement |
| `X-Costguard-User` | Tag request with an end-user identifier (informational) |

### Example: routing mode hint

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Mode: cheap" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -d '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "Hi"}]}'
```

### Example: pin to a specific provider

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: anthropic_primary" \
  -d '{"model": "claude-sonnet-4-6", "messages": [{"role": "user", "content": "Hi"}]}'
```

---

## Tool Calling

Tool calling uses the standard OpenAI request format. Costguard translates to each provider's native wire format automatically.

**Provider support:**

| Provider | Tool Calling |
|---|---|
| OpenAI | ✅ |
| Anthropic | ✅ |
| Gemini | ✅ |
| OpenAI-compatible (Ollama, vLLM) | depends on model |

### Example: single-turn tool call

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Agent: my-agent" \
  -d '{
    "model": "claude-sonnet-4-6",
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "get_weather",
          "description": "Get current weather for a location",
          "parameters": {
            "type": "object",
            "properties": {
              "location": {"type": "string", "description": "City name"}
            },
            "required": ["location"]
          }
        }
      }
    ],
    "messages": [
      {"role": "user", "content": "What is the weather in London?"}
    ]
  }'
```

### Example: multi-turn conversation with tool call history

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Agent: my-agent" \
  -d '{
    "model": "claude-sonnet-4-6",
    "tools": [{"type": "function", "function": {"name": "get_weather", "description": "Get weather", "parameters": {"type": "object", "properties": {"location": {"type": "string"}}, "required": ["location"]}}}],
    "messages": [
      {"role": "user", "content": "What is the weather in London?"},
      {"role": "assistant", "content": null, "tool_calls": [
        {"id": "tc_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"location\": \"London\"}"}}
      ]},
      {"role": "tool", "tool_call_id": "tc_1", "content": "15°C, cloudy"},
      {"role": "user", "content": "What about Paris?"}
    ]
  }'
```

Assistant messages with `null` content and `tool_calls` are handled correctly on all providers.

---

## Streaming

Set `"stream": true` to receive a server-sent events (SSE) response. The response uses the standard OpenAI SSE chunk format regardless of which provider handles the request.

**Provider support:**

| Provider | Streaming |
|---|---|
| OpenAI | ✅ |
| Anthropic | ✅ |
| Gemini | ✅ |
| OpenAI-compatible (Ollama, vLLM) | ✅ |

### Example: streaming text response

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "stream": true,
    "messages": [{"role": "user", "content": "Count to five slowly."}]
  }'
```

Note: streaming responses are not cached.

---

## Vision & Multimodal

Send images using the standard OpenAI multipart content format. Costguard transforms the request to each provider's native format automatically.

**Example: image URL**

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "messages": [{
      "role": "user",
      "content": [
        {"type": "text", "text": "What is in this image?"},
        {"type": "image_url", "image_url": {"url": "https://example.com/photo.png"}}
      ]
    }]
  }'
```

**Example: base64 data URI**

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{
      "role": "user",
      "content": [
        {"type": "text", "text": "Describe this chart."},
        {"type": "image_url", "image_url": {"url": "data:image/png;base64,<base64data>", "detail": "high"}}
      ]
    }]
  }'
```

The `detail` field (`"low"`, `"high"`, `"auto"`) is used for OpenAI token estimation and forwarded as-is to OpenAI. It is ignored for other providers.

**Vision token estimation:**

| Provider | Formula |
|---|---|
| Anthropic | Resize to 1568×1568 → count 512px tiles → `tiles × 765 + 65` per image |
| OpenAI `detail=low` | 85 tokens flat |
| OpenAI `detail=high/auto` | Fit within 2048px → scale shortest side to 768px → count 512px tiles → `tiles × 170 + 85` |
| Gemini | Trusts `usageMetadata.promptTokenCount` from the upstream response |

---

## Agent Integration

Costguard is designed to be an infrastructure layer for agent-based and automated pipelines.

### Identifying Requests

Tag every agent request with `X-Costguard-Agent`:

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Agent: data-pipeline" \
  -H "X-Costguard-Team: backend" \
  -d '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "Summarize this."}]}'
```

### Per-Agent Budget Enforcement

Configure a monthly USD limit per agent in `config.json`:

```json
"budget": {
  "agents": {
    "ci-bot": 10,
    "data-pipeline": 15
  }
}
```

When an agent's budget is exceeded, Costguard returns `402 Payment Required` immediately — no upstream call is made.

### Querying Provider Health at Runtime

Before routing a request, an agent can query live provider health including circuit-breaker state:

```bash
curl http://localhost:8080/admin/providers/health \
  -H "Authorization: Bearer $ADMIN_API_KEY"
```

Response:

```json
{
  "providers": [
    {
      "name": "anthropic_primary",
      "status": "enabled",
      "total": 150,
      "successes": 148,
      "failures": 2,
      "success_rate": 0.987,
      "avg_latency_ms": 843.2,
      "last_error": "",
      "breaker_state": "closed",
      "breaker_consecutive_failures": 0
    },
    {
      "name": "openai_primary",
      "status": "enabled",
      "total": 0,
      "successes": 0,
      "failures": 0,
      "success_rate": -1,
      "avg_latency_ms": -1,
      "breaker_state": "closed",
      "breaker_consecutive_failures": 0
    }
  ]
}
```

`success_rate` and `avg_latency_ms` are `-1` when no data has been recorded yet.

### Querying Provider Capabilities

```bash
curl http://localhost:8080/admin/providers \
  -H "Authorization: Bearer $ADMIN_API_KEY"
```

Use `supports_tools: true` to find providers for tool-calling requests, and `supports_streaming: true` for streaming requests.

### Querying Available Routing Modes

```bash
curl http://localhost:8080/admin/routing/modes \
  -H "Authorization: Bearer $ADMIN_API_KEY"
```

Response:

```json
{
  "modes": {
    "best": "anthropic_primary",
    "balanced": "openai_primary",
    "cheap": "google_primary",
    "private": "local_ollama"
  }
}
```

Pass the mode name in `X-Costguard-Mode` to influence routing without hard-coding provider names.

---

## Admin API

All admin endpoints require `Authorization: Bearer <ADMIN_API_KEY>`.

### Provider Catalog & Health

| Method | Path | Description |
|---|---|---|
| `GET` | `/admin/providers` | List all providers with capability metadata |
| `GET` | `/admin/providers/health` | Live health stats and circuit-breaker state per provider |
| `GET` | `/admin/providers/{name}/models` | List supported models for a named provider |
| `GET` | `/admin/routing/modes` | List routing mode → provider mappings |

### Usage & Spend

| Method | Path | Query Params | Description |
|---|---|---|---|
| `GET` | `/admin/usage/summary` | `from`, `to` (RFC3339) | Total spend for period |
| `GET` | `/admin/usage/teams` | `from`, `to` | Spend grouped by team |
| `GET` | `/admin/usage/projects` | — | Spend grouped by project (current month) |
| `GET` | `/admin/usage/agents` | `from`, `to` | Spend grouped by agent |

### Budget

| Method | Path | Description |
|---|---|---|
| `GET` | `/admin/budget/status` | Current month spend vs budget |

### Reports

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/reports/monthly/send` | Trigger a monthly cost report email |

### Provider health response fields

| Field | Type | Description |
|---|---|---|
| `total` | int | Total calls in the tracking window (last 100) |
| `successes` | int | Successful calls |
| `failures` | int | Failed calls |
| `success_rate` | float | 0.0–1.0; `-1` if no data |
| `avg_latency_ms` | float | Average latency of successful calls; `-1` if no data |
| `last_error` | string | Error category of the most recent failure |
| `breaker_state` | string | `"closed"`, `"open"`, or `"half_open"` |
| `breaker_consecutive_failures` | int | Current consecutive relevant failure count |
| `breaker_trip_time` | string | RFC3339 timestamp of the most recent trip; omitted if never tripped |

### Usage query example

```bash
curl "http://localhost:8080/admin/usage/agents?from=2026-04-01T00:00:00Z&to=2026-05-01T00:00:00Z" \
  -H "Authorization: Bearer $ADMIN_API_KEY"
```

Response:

```json
{
  "from": "2026-04-01T00:00:00Z",
  "to": "2026-05-01T00:00:00Z",
  "agents": [
    {"agent": "data-pipeline", "total_cost_usd": 4.23},
    {"agent": "ci-bot", "total_cost_usd": 1.07}
  ]
}
```

---

## Error Responses

All error responses use the standard OpenAI error envelope regardless of which provider was used. A `category` field is always present to classify the error:

```json
{
  "error": {
    "message": "Incorrect API key provided.",
    "type": "invalid_api_key",
    "category": "auth"
  }
}
```

| Category | Meaning | Typical HTTP status |
|---|---|---|
| `auth` | Bad or missing API key | 401, 403 |
| `invalid_request` | Malformed or unsupported request | 400, 422 |
| `rate_limit` | Quota or rate limit exceeded | 429 |
| `provider_unavailable` | Network error, timeout, or circuit open | 502, 503, 504 |
| `upstream_failure` | Generic provider-side error | 5xx |

Common HTTP status codes:

| Status | Meaning |
|---|---|
| `400` | Malformed request |
| `402` | Budget exceeded (global, team, project, or agent) |
| `429` | Rate limit from upstream provider |
| `502` | Upstream provider error or all providers unavailable |

---

## Configuration Reference

```json
{
  "server": { "addr": ":8080" },
  "logging": { "level": "info", "json": true },

  "cache": {
    "enabled": true,
    "ttl": "30s",
    "max_keys": 10000
  },

  "database": {
    "driver": "postgres",
    "dsn": "env:COSTGUARD_DATABASE_URL"
  },

  "admin": {
    "api_key": "env:ADMIN_API_KEY"
  },

  "budget": {
    "enabled": true,
    "monthly_usd": 100,
    "teams":    { "backend": 40, "mobile": 30 },
    "projects": { "chatbot": 25, "analytics": 20 },
    "agents":   { "ci-bot": 10, "data-pipeline": 15 }
  },

  "routing": {
    "default_provider": "anthropic_primary",
    "fallback_provider": "openai_primary",
    "model_to_provider": {
      "gpt-4o-mini":       "openai_primary",
      "claude-sonnet-4-6": "anthropic_primary",
      "gemini-2.5-flash":  "google_primary"
    },
    "model_compatibility": {
      "gpt-4o-mini": {
        "anthropic_primary": "claude-sonnet-4-6",
        "google_primary":    "gemini-2.5-flash"
      }
    },
    "mode_to_provider": {
      "cheap":    "google_primary",
      "balanced": "openai_primary",
      "best":     "anthropic_primary",
      "private":  "local_ollama"
    }
  },

  "providers": {
    "openai": {
      "openai_primary": {
        "base_url": "https://api.openai.com",
        "api_key":  "env:OPENAI_API_KEY",
        "timeout":  "60s",
        "retry": {
          "max_attempts":    2,
          "retry_on_5xx":    true,
          "retry_on_timeout": false,
          "initial_backoff": "500ms",
          "max_backoff":     "10s"
        },
        "breaker": {
          "failure_threshold": 5,
          "cooldown_seconds":  30
        },
        "metadata": {
          "kind": "cloud",
          "supports_tools":      true,
          "supports_streaming":  true,
          "supports_vision":     true,
          "supports_embeddings": false,
          "priority": 95,
          "tags": ["premium", "general"]
        }
      }
    },
    "anthropic": {
      "anthropic_primary": {
        "base_url":          "https://api.anthropic.com",
        "api_key":           "env:ANTHROPIC_API_KEY",
        "anthropic_version": "2023-06-01",
        "timeout":           "60s",
        "retry": {
          "max_attempts":    3,
          "retry_on_5xx":    true,
          "retry_on_timeout": true,
          "initial_backoff": "500ms",
          "max_backoff":     "10s"
        },
        "breaker": {
          "failure_threshold": 3,
          "cooldown_seconds":  60
        },
        "metadata": {
          "kind": "cloud",
          "supports_tools":      true,
          "supports_streaming":  true,
          "supports_vision":     true,
          "supports_embeddings": false,
          "priority": 100,
          "tags": ["reasoning", "premium"]
        }
      }
    },
    "gemini": {
      "google_primary": {
        "base_url": "https://generativelanguage.googleapis.com",
        "api_key":  "env:GEMINI_API_KEY",
        "timeout":  "60s",
        "metadata": {
          "kind": "cloud",
          "supports_tools":      true,
          "supports_streaming":  true,
          "supports_vision":     true,
          "supports_embeddings": false,
          "priority": 90,
          "tags": ["fast", "cheap", "general"]
        }
      }
    },
    "openai_compatible": {
      "local_ollama": {
        "base_url":         "http://host.docker.internal:11434",
        "api_key":          "",
        "timeout":          "60s",
        "allow_multimodal": false,
        "breaker": {
          "disabled": true
        },
        "metadata": {
          "kind": "local",
          "supports_tools":      false,
          "supports_streaming":  true,
          "supports_vision":     false,
          "supports_embeddings": false,
          "priority": 50,
          "tags": ["cheap", "private", "local"]
        }
      }
    }
  }
}
```

### Provider fields

| Field | Type | Description |
|---|---|---|
| `base_url` | string | Upstream API base URL |
| `api_key` | string | API key (use `env:VAR_NAME` to read from environment) |
| `timeout` | duration | Per-request timeout (e.g. `"60s"`) |
| `allow_multimodal` | bool | OpenAI-compatible only — pass image blocks through (default: `false`) |

### `retry` fields

| Field | Type | Default | Description |
|---|---|---|---|
| `max_attempts` | int | `1` | Total attempts including the first. `1` = no retry. |
| `retry_on_5xx` | bool | `false` | Retry on upstream 5xx responses |
| `retry_on_timeout` | bool | `false` | Retry on network timeouts |
| `initial_backoff` | duration | `"500ms"` | Backoff before the first retry |
| `max_backoff` | duration | `"10s"` | Maximum backoff (backoff is capped here) |

### `breaker` fields

| Field | Type | Default | Description |
|---|---|---|---|
| `failure_threshold` | int | `5` | Consecutive relevant failures before opening |
| `cooldown_seconds` | int | `30` | Seconds to stay open before allowing a probe |
| `disabled` | bool | `false` | Disable the breaker entirely for this provider |

### Provider metadata fields

| Field | Type | Description |
|---|---|---|
| `kind` | string | `"cloud"` or `"local"` |
| `supports_tools` | bool | Provider handles `tools` / `tool_calls` |
| `supports_streaming` | bool | Provider supports SSE streaming |
| `supports_vision` | bool | Provider accepts image content parts |
| `supports_embeddings` | bool | Provider has an embeddings endpoint |
| `priority` | int | Higher = preferred when multiple providers match |
| `tags` | string[] | Arbitrary labels for filtering or display |
| `supported_models` | string[] | If set, router only routes listed models to this provider |

---

## Environment Variables

Use the `env:` prefix in `config.json` to reference environment variables. Create a `.env` file in the project root (do **not** commit it):

```env
OPENAI_API_KEY=...
ANTHROPIC_API_KEY=...
GEMINI_API_KEY=...

ADMIN_API_KEY=...

COSTGUARD_DATABASE_URL=postgres://costguard:costguard@localhost:5432/costguard?sslmode=disable

EMAIL_USERNAME=you@gmail.com
EMAIL_PASSWORD=your_smtp_app_password
EMAIL_FROM=you@gmail.com
EMAIL_TO=alerts@example.com
```

---

## Project Structure

```
costguard/
├── cmd/api/                   # entrypoint
├── internal/
│   ├── app/                   # application wiring
│   ├── breaker/               # per-provider circuit breaker (three-state machine + registry)
│   ├── budget/                # budget enforcement service
│   ├── cache/                 # in-memory cache
│   ├── config/                # config loading
│   ├── gateway/               # request pipeline (routing, breaker, retry, caching, metering, streaming)
│   ├── health/                # per-provider outcome ring buffer and health snapshots
│   ├── logging/               # structured logging
│   ├── providers/             # provider abstractions and catalog
│   │   ├── anthropic/         # Anthropic adapter (tools + streaming + vision)
│   │   ├── gemini/            # Gemini adapter (tools + streaming + vision)
│   │   ├── openai/            # OpenAI adapter (vision)
│   │   ├── openaicompat/      # Generic OpenAI-compatible adapter (Ollama, vLLM)
│   │   └── openaiformat/      # Shared request parsing and vision token estimation
│   ├── router/                # model → provider routing logic
│   ├── server/                # HTTP infrastructure, admin API
│   └── usage/                 # usage records and spend queries
├── migrations/                # Postgres migrations
├── scripts/                   # manual test scripts
├── config.json                # example configuration
└── docker-compose.yml
```

---

## Roadmap

- [x] Multi-provider routing
- [x] Fallback system
- [x] Model compatibility mapping
- [x] Cost-aware and priority-based provider selection
- [x] Budget enforcement (global, team, project, agent)
- [x] Reporting system
- [x] Tool calling (OpenAI, Anthropic, Gemini)
- [x] Streaming (all providers)
- [x] Agent integration (X-Costguard-Agent, per-agent budget, usage API)
- [x] Vision & multimodal (image inputs on all providers, client-side token estimation)
- [x] Per-provider retry policy with exponential backoff and 429 Retry-After support
- [x] Normalized error taxonomy with `category` field on all error responses
- [x] Per-provider health tracking (ring buffer, live stats in admin health endpoint)
- [x] Per-provider circuit breaker (closed/open/half-open, breaker state in admin health endpoint)
- [ ] Semantic caching
- [ ] Router-level circuit awareness (skip open-breaker providers at selection time)

---

## License

This project is licensed under the terms of the `LICENSE` file in the repository root.
