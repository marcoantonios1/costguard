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
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      {"role":"user","content":"Say hello"}
    ]
  }'
```

Your application can simply change the API base URL:

```
OPENAI_BASE_URL=http://localhost:8080
```

No other changes are required.

---

# Run with Docker

Build the image:

```bash
docker build -t costguard .
```

Run the container:

```bash
docker run --rm -p 8080:8080 \
  -e OPENAI_API_KEY="$OPENAI_API_KEY" \
  -v "$(pwd)/config.json:/config.json" \
  costguard
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

Environment variables can be referenced using:

```
env:VARIABLE_NAME
```

Example:

```
env:OPENAI_API_KEY
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