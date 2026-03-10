FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o costguard ./cmd/api

FROM gcr.io/distroless/base-debian12

WORKDIR /

COPY --from=builder /app/costguard /costguard

EXPOSE 8080

ENTRYPOINT ["/costguard"]
CMD ["-config", "/config.json"]