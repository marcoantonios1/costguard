# -----------------------------
# Build stage
# -----------------------------
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o costguard ./cmd/api

# -----------------------------
# Runtime stage
# -----------------------------
FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /app/costguard .
COPY --from=builder /app/config.json ./config.json
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/.env ./.env

EXPOSE 8080

ENTRYPOINT ["/app/costguard"]