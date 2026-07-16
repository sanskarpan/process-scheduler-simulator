# Process Scheduler Simulator — Make targets.
# Keep commands runnable on a fresh checkout: `make ci`.

BINARY  := bin/scheduler-server
PKG     := github.com/sanskar/scheduler-simulator
GOFLAGS := -trimpath
LDFLAGS := -s -w
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS += -X main.buildVersion=$(VERSION)

.PHONY: all build run test test-race test-verbose lint vet fmt tidy \
        benchmark docker build-all clean ci cover vuln help

all: build

## build: compile the server binary into ./bin
build:
	mkdir -p bin
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/server

## run: build and run the server (PORT env overrides port)
run: build
	PORT=${PORT:-8082} ./$(BINARY)

## test: run the test suite
test:
	go test ./...

## test-race: run tests with the race detector
test-race:
	go test -race -count=1 ./...

## test-verbose: run tests with -v
test-verbose:
	go test -race -count=1 -v ./...

## lint: run golangci-lint (install v2.1.6 if missing; must match CI)
lint:
	@command -v golangci-lint >/dev/null 2>&1 || \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v2.1.6
	golangci-lint run ./... --timeout 5m

## vet: go vet
vet:
	go vet ./...

## fmt: gofmt + goimports
fmt:
	@gofmt -w .
	@command -v goimports >/dev/null 2>&1 && goimports -w . || true

## tidy: go mod tidy
tidy:
	go mod tidy

## benchmark: run benchmarks
benchmark:
	go test -run='^$$' -bench=. -benchmem -count=2 ./internal/simulator/...

## docker: build the docker image
docker:
	docker build -t scheduler-simulator:$(VERSION) .

## build-all: cross-compile for linux/darwin (amd64 + arm64)
build-all:
	mkdir -p bin
	GOOS=linux   GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(PKG)-linux-amd64   ./cmd/server
	GOOS=linux   GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(PKG)-linux-arm64   ./cmd/server
	GOOS=darwin  GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(PKG)-darwin-amd64  ./cmd/server
	GOOS=darwin  GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(PKG)-darwin-arm64  ./cmd/server

## cover: run tests and open HTML coverage report
cover:
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@pct=$$(go tool cover -func=coverage.out | awk '/^total/{gsub(/%/,"",$3); print $$3}'); \
	  echo "Coverage: $${pct}%"

## vuln: check dependencies for known vulnerabilities
vuln:
	@command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

## clean: remove build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html

## ci: the full pipeline CI runs (build + vet + lint + race tests + coverage + vuln)
ci: build vet lint test-race cover vuln benchmark
	@echo "CI pipeline complete."

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
