# Costguard

Costguard is an AI gateway that adds cost tracking, budgeting, multi-provider routing, streaming, tool calling, and usage analytics to any LLM application — without changing your application code.

---

## Why Costguard?

Most LLM applications eventually run into:

- Runaway costs from expensive models with no spending limits
- Repeated identical API calls that could be cached
- No visibility into token usage by team, project, or agent
- No policy layer between the app and the model provider

Costguard solves these by sitting between your application and the model providers as a thin, OpenAI-compatible proxy.

---

## Architecture

```
Your App (OpenAI SDK / curl)
        │
        ▼
  Costguard Gateway  (:8080)
  ┌─────────────────────────────┐
  │  Routing → Budget check     │
  │  Cache lookup               │
  │  Provider selection         │
  │  Streaming / tool call      │
  │  Metering → DB              │
  └─────────────────────────────┘
        │
        ▼
  Provider  (OpenAI / Anthropic / Gemini / Local)
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

Route requests to OpenAI, Anthropic, Gemini, or any OpenAI-compatible endpoint (Ollama, vLLM, etc.) based on model name, a routing mode hint, or an explicit provider override.

### Fallback

If the primary provider fails, Costguard automatically retries with the configured fallback provider and maps the model name if needed.

### Caching

Identical requests return cached responses without hitting the upstream provider. Cache keys include routing context (provider, mode, model) to prevent cross-provider reuse. Streaming responses are never cached.

### Budget Enforcement

Hard monthly spending limits at the global, team, project, and agent level. Requests that would breach a limit receive an immediate `402 Payment Required`.

### Tool Calling

Full multi-turn tool calling is supported on Anthropic and Gemini. Requests use the standard OpenAI `tools` / `tool_calls` format — Costguard translates to each provider's native format.

### Streaming

Server-sent events (SSE) streaming is supported on all providers. Set `"stream": true` in the request body.

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

### Example: streaming tool call

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "stream": true,
    "tools": [{"type": "function", "function": {"name": "get_weather", "description": "Get weather", "parameters": {"type": "object", "properties": {"location": {"type": "string"}}, "required": ["location"]}}}],
    "messages": [{"role": "user", "content": "What is the weather in Paris?"}]
  }'
```

Note: streaming responses are not cached.

---

## Agent Integration

Costguard is designed to be an infrastructure layer for Agent OS and automated pipelines.

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

### Querying Provider Capabilities at Runtime

Before sending a request, an agent can query the provider catalog to find providers that support tools or streaming:

```bash
curl http://localhost:8080/admin/providers \
  -H "Authorization: Bearer $ADMIN_API_KEY"
```

Response:

```json
{
  "providers": [
    {
      "name": "anthropic_primary",
      "type": "anthropic",
      "kind": "cloud",
      "supports_tools": true,
      "supports_streaming": true,
      "supports_vision": true,
      "priority": 100,
      "tags": ["reasoning", "premium"],
      "enabled": true
    },
    {
      "name": "local_ollama",
      "type": "openai_compatible",
      "kind": "local",
      "supports_tools": false,
      "supports_streaming": true,
      "priority": 50,
      "tags": ["cheap", "private", "local"],
      "enabled": true
    }
  ]
}
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

### Provider Catalog

| Method | Path | Description |
|---|---|---|
| `GET` | `/admin/providers` | List all providers with capability metadata |
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

All error responses use the standard OpenAI error envelope regardless of which provider was used:

```json
{
  "error": {
    "message": "agent monthly budget exceeded",
    "type": "invalid_request_error"
  }
}
```

Common HTTP status codes:

| Status | Meaning |
|---|---|
| `400` | Malformed request |
| `402` | Budget exceeded (global, team, project, or agent) |
| `429` | Rate limit from upstream provider |
| `502` | Upstream provider error |

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
      "cheap":   "google_primary",
      "balanced":"openai_primary",
      "best":    "anthropic_primary",
      "private": "local_ollama"
    }
  },

  "providers": {
    "openai": {
      "openai_primary": {
        "base_url": "https://api.openai.com",
        "api_key":  "env:OPENAI_API_KEY",
        "timeout":  "60s",
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
        "base_url":           "https://api.anthropic.com",
        "api_key":            "env:ANTHROPIC_API_KEY",
        "anthropic_version":  "2023-06-01",
        "timeout":            "60s",
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
        "base_url": "http://host.docker.internal:11434",
        "api_key":  "",
        "timeout":  "60s",
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
│   ├── budget/                # budget enforcement service
│   ├── cache/                 # in-memory cache
│   ├── config/                # config loading
│   ├── gateway/               # request pipeline (routing, caching, metering, streaming)
│   ├── logging/               # structured logging
│   ├── providers/             # provider abstractions and catalog
│   │   ├── anthropic/         # Anthropic adapter (tools + streaming)
│   │   ├── gemini/            # Gemini adapter (tools + streaming)
│   │   ├── openai/            # OpenAI adapter
│   │   └── openaicompat/      # Generic OpenAI-compatible adapter (Ollama, vLLM)
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
- [x] Budget enforcement (global, team, project, agent)
- [x] Reporting system
- [x] Tool calling (OpenAI, Anthropic, Gemini)
- [x] Streaming (all providers)
- [x] Agent integration (X-Costguard-Agent, per-agent budget, usage API)
- [ ] Smart routing (cost/performance optimization)
- [ ] Semantic caching

---

## License

This project is licensed under the terms of the `LICENSE` file in the repository root.
