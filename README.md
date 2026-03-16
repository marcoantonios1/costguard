# Costguard

Costguard is an **OpenAI-compatible AI gateway written in Go**.

It sits between your application and LLM providers and adds capabilities like:

- smart provider routing
- response caching
- usage and cost tracking
- provider fallback
- centralized logging

without requiring changes to your existing OpenAI client code.

Your application continues using the **OpenAI API format**, while Costguard handles the control layer.

Think of Costguard as **a lightweight API gateway for LLM traffic**.

---

# Why Costguard?

Most LLM applications eventually run into problems like:

- sending too many requests to expensive models
- regenerating identical responses repeatedly
- lacking clear visibility into token usage and cost
- having no policy layer between the app and the model provider

Costguard solves these problems by introducing a **thin gateway layer** that manages requests before they reach the model provider.

---

# High-Level Architecture

```
Your App
   │
   ▼
Costguard
   │
   ├── Router (model → provider)
   ├── Cache (reuse identical responses)
   ├── Metering (usage tracking & cost estimation)
   ▼
Provider Adapter
   │
   ▼
LLM Provider API
```

Your application talks to Costguard using **standard OpenAI API requests**.

---

# Quick Example

Start the server:

```bash
go run ./cmd/api -config ./config.json
```

Send a request using the OpenAI API format:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Costguard-Team: backend" \
  -H "X-Costguard-Project: chatbot" \
  -H "X-Costguard-User: marco" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      {"role":"user","content":"Say hello"}
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

# Design Goals

Costguard follows a few core design principles.

### OpenAI Compatibility

Existing OpenAI clients should work without modification.

### Single Binary

The gateway runs as a single Go binary for simplicity and easy deployment.

### Incremental Architecture

The codebase is structured so additional providers, policies, and infrastructure can be added without refactoring the core.

### Low Operational Overhead

The first versions should be easy to run locally and deploy anywhere.


# Development

Requirements:

```
Go 1.22+
```

Run locally:

```bash
go run ./cmd/api -config ./config.json
```

Test the health endpoint:

```bash
curl http://localhost:8080/healthz
```

Expected response:

```
ok
```

---

# License

This project is licensed under the terms of the `LICENSE` file in the repository root.