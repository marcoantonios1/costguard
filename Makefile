BINARY     = costguard
CMD        = ./cmd/api
BUILD_DIR  = ./bin

.PHONY: all build run test lint fmt vet tidy clean down logs

all: build

## build: compile the binary to ./bin/costguard
build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY) $(CMD)

## run: build Docker images and start all services (docker compose up --build)
run:
	docker compose up --build

## run-d: same as run but detached
run-d:
	docker compose up --build -d

## down: stop and remove containers
down:
	docker compose down

## logs: tail logs for all services
logs:
	docker compose logs -f

## test: run all tests
test:
	go test ./... -v -count=1

## test-short: run tests skipping long-running ones
test-short:
	go test ./... -short -count=1

## test-cover: run tests with coverage report
test-cover:
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: run golangci-lint (must be installed)
lint:
	golangci-lint run ./...

## fmt: format all Go source files
fmt:
	gofmt -w .

## vet: run go vet
vet:
	go vet ./...

## tidy: tidy and verify go modules
tidy:
	go mod tidy
	go mod verify

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR) coverage.out coverage.html

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
