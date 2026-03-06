# Costguard

Costguard is an OpenAI-compatible AI gateway built in Go.

It sits between your application and an LLM provider so you can add routing, caching, usage tracking, and later cost controls without changing your app code.

## Current status

The project is in **Phase A**.

What exists right now:
- single-binary Go service
- HTTP server with graceful shutdown
- `GET /healthz` health endpoint
- starter project structure for config, gateway, providers, router, cache, metering, and observability

What does **not** exist yet:
- `/v1/chat/completions`
- provider forwarding
- routing logic
- response caching
- token / cost metering

That is intentional. The project is being built in clean phases.

## Why Costguard?

LLM apps often have four problems:
- they send too many requests to expensive models
- they regenerate the same response repeatedly
- they do not measure usage and cost clearly
- they have no policy layer between the app and the model provider

Costguard is meant to solve that with a small gateway that is easy to run and easy to extend.

## High-level architecture

```text
Your App -> Costguard -> LLM Provider
```

Phase A starts with OpenAI-compatible HTTP behavior first, then adds the optimization layers behind it.

## Design goals

- **OpenAI-compatible** so existing apps can adopt it easily
- **Single binary** for simple local development and deployment
- **Incremental architecture** so Phase B can add more providers and policies cleanly
- **Low operational overhead** for the first version
- **Production-minded foundation** even while the MVP stays small

## Project structure

```text
costguard/
├── cmd/
│   └── api/                  # application entrypoint
├── internal/
│   ├── cache/                # cache interfaces and implementations
│   ├── config/               # config models and loading
│   ├── gateway/              # OpenAI-compatible gateway handlers
│   ├── metering/             # usage and cost estimation
│   ├── observability/        # logging / request tracing
│   ├── providers/            # provider abstractions and implementations
│   ├── router/               # model/provider selection rules
│   └── server/               # HTTP server and infrastructure handlers
├── configs/                  # example config files
├── scripts/                  # development scripts
├── go.mod
└── README.md
```

## What Commit 1 implements

Commit 1 establishes the first runnable baseline.

### Included
- `cmd/api/main.go`
- `internal/server/http.go`
- `internal/server/health.go`

### Behavior
- starts an HTTP server
- listens on `:8080` by default
- reads `PORT` from the environment if provided
- exposes `GET /healthz`
- shuts down gracefully on `SIGINT` and `SIGTERM`

## Run locally

Requirements:
- Go 1.22+

Start the server:

```bash
go run ./cmd/api
```

Use a custom port:

```bash
PORT=9090 go run ./cmd/api
```

Test the health endpoint:

```bash
curl -i http://localhost:8080/healthz
```

Expected response:

```json
{"status":"ok"}
```

## Phase plan

## Phase A — developer tool

Goal: build the smallest useful OpenAI-compatible gateway.

Planned order:
1. health endpoint and server bootstrap
2. env-based config loader
3. provider interface
4. OpenAI provider passthrough
5. `/v1/chat/completions`
6. basic routing rules
7. in-memory cache
8. usage and cost metering
9. request logging polish

## Phase B — multi-provider + policy layer

Planned additions:
- multiple providers such as OpenAI, Gemini, Claude, and others
- provider/model routing rules
- Redis cache
- budget guardrails
- project/team quotas
- better auditability

## Phase C — managed platform

Possible future direction:
- hosted control plane
- dashboard and analytics
- tenant management
- billing and usage controls

## Roadmap notes

A good rule for this repo:
- build the smallest complete slice first
- keep extension points, but do not overbuild them early
- optimize for correctness and clarity before cleverness

## Configuration

Right now the running baseline only uses one environment variable:

```bash
PORT=8080
```

A dedicated config package already exists in the structure and will be wired in next.

Planned early config values:
- `PORT`
- `OPENAI_API_KEY`
- `OPENAI_BASE_URL`
- `LOG_LEVEL`

## Dockerfile — now or later?

Create a **simple dev Dockerfile after Commit 2 or Commit 3**, not at the very end.

That is the best timing because:
- after Commit 1, the app is barely more than a health server
- after Commit 2 or 3, the project has enough shape to containerize meaningfully
- doing it too late increases the chance that local-only assumptions sneak into the code

So the recommendation is:
- **do not make Docker your next task**
- finish config first
- then add a small Dockerfile once the app can load config and start cleanly

## Suggested next step

The next implementation step should be:

### Commit 2 — env-based config loader

Add:
- config structs
- env loading
- validation for required values later used by providers
- wire config into `main.go`

After that, move to the provider abstraction and OpenAI passthrough.

## License

This repository includes a `LICENSE` file at the project root.
