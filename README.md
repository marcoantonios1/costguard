# Costguard

Costguard is an AI gateway that adds:

- Multi-provider routing (OpenAI, Anthropic, ...)
- Cost tracking and budgeting
- Smart fallback across providers
- Model compatibility mapping
- Request caching
- Usage analytics and reporting

All without changing your application code.

---

# Why Costguard?

Most LLM applications eventually run into problems like:

- sending too many requests to expensive models
- regenerating identical responses repeatedly
- lacking clear visibility into token usage and cost
- having no policy layer between the app and the model provider

Costguard solves these problems by introducing a **thin gateway layer** that manages requests before they reach the model provider.

---

## 🏗 Architecture

Client → Costguard → Provider (OpenAI / Anthropic / ...)

Costguard acts as a proxy that:

- Selects a provider based on configuration and hints
- Tracks usage and cost
- Applies caching and budgets
- Falls back on failures

---

# Quick Example

Start the server:

```bash
go run ./cmd/api -config ./config.json
```

Send a request using the OpenAI API format:

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Provider: local_ollama" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d '{
    "model": "llama3.2",
    "messages": [
      {"role": "user", "content": "Say hello in two sentence"}
    ]
  }'
```

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Mode: private" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d '{
    "model": "llama3.2",
    "messages": [
      {"role": "user", "content": "Say hello in two sentence"}
    ]
  }'
```

Costguard injects the provider API key automatically.

Your application only needs to change the API base URL:

```
OPENAI_BASE_URL=http://localhost:8080
```

No other changes are required.

---

# Run with Docker

```bash
docker compose up --build
```

# Project Structure

```
costguard/
├── cmd/
│   └── api/                     # application entrypoint
│
├── internal/
│   ├── app/                     # application wiring
│   ├── cache/                   # cache interfaces and implementations
│   ├── config/                  # configuration loading
│   ├── gateway/                 # request pipeline
│   ├── logging/                 # structured logging
│   ├── providers/               # provider abstractions
│   │   └── openai/              # OpenAI provider client
│   ├── router/                  # model → provider routing
│   └── server/                  # HTTP infrastructure
│       └── openai/              # OpenAI-compatible HTTP layer
│
├── configs/                     # example configuration files
├── scripts/                     # development scripts
├── go.mod
└── README.md
```

---

## ✨ Features

### 🔀 Multi-Provider Routing

Route requests to different providers based on model or config.

### 🔁 Fallback System

Automatically retry with a fallback provider if the primary fails.

### 🔄 Model Compatibility Mapping

- Map models across providers when routing or fallback occurs.

Example:
claude-3-5-sonnet-latest → gpt-4o-mini

- If a model is not compatible with the selected provider and no mapping is defined, the request may fail.

### 🚫 Provider Availability Awareness

- Skip providers that are not configured or unavailable.

### 💾 Caching

- In-memory cache to reduce cost and latency.
- Cache keys include routing context (provider, mode, model) to prevent incorrect reuse across providers.

### 💰 Cost Tracking

- Track token usage and estimated cost per request.

### 📊 Budget Enforcement

- Global monthly budget
- Per-team and per-project limits

### 📧 Reporting & Alerts

- Scheduled reports
- Email notifications

### 🎯 Request-Level Routing Control

- Override provider or influence routing per request using headers.

---

# Configuration

Costguard uses a JSON configuration file.

Example:

```json
{
  "server": { "addr": ":8080" },

  "logging": {
    "level": "info",
    "json": true
  },

  "cache": {
    "enabled": true,
    "ttl": "30s",
    "max_keys": 10000
  },

  "routing": {
    "default_provider": "openai_primary",
    "fallback_provider": "openai_backup",
    "model_to_provider": {
      "gpt-4o-mini": "openai_primary"
    },
    "mode_to_provider": {
      "cheap": "google_primary",
      "balanced": "openai_primary",
      "best": "anthropic_primary",
      "private": "local_ollama"
    }
  },

  "providers": {
    "openai": {
      "openai_primary": {
        "base_url": "https://api.openai.com",
        "api_key": "env:OPENAI_API_KEY",
        "timeout": "60s"
      }
    }
  }
}
```

# Environment Variables

Sensitive configuration values such as API keys, database credentials, and SMTP passwords
should be provided through environment variables.

Costguard supports referencing environment variables in `config.json` using the `env:` prefix.

Example:

```
"api_key": "env:OPENAI_API_KEY"
```

### Create a `.env` file

Create a `.env` file in the project root:

```env
OPENAI_API_KEY=your_openai_api_key

COSTGUARD_DATABASE_URL=postgres://costguard:costguard@localhost:5432/costguard?sslmode=disable

COSTGUARD_SMTP_USERNAME=your_email@gmail.com
COSTGUARD_SMTP_PASSWORD=your_smtp_app_password
COSTGUARD_SMTP_FROM=your_email@gmail.com
COSTGUARD_ALERT_EMAIL_TO=alerts@example.com
```

Costguard automatically loads this file on startup.

### Important

Do **not commit `.env` to git**.

Add this to `.gitignore`:

```
.env
```

---

## 🛣 Roadmap

- [x] Multi-provider routing
- [x] Fallback system
- [x] Model compatibility mapping
- [x] Budget enforcement
- [x] Reporting system
- [ ] Smart routing (cost/performance optimization)
- [ ] Semantic caching
- [ ] Agent integration

---

# License

This project is licensed under the terms of the `LICENSE` file in the repository root.
