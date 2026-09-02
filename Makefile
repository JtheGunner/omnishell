BIN := omnishell
LDFLAGS := -X github.com/JtheGunner/omnishell/internal/buildinfo.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev) \
           -X github.com/JtheGunner/omnishell/internal/buildinfo.Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo none) \
           -X github.com/JtheGunner/omnishell/internal/buildinfo.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: build test lint vet
build:
	go build -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/omnishell
test:
	go test ./... -race -count=1
vet:
	go vet ./...
lint:
	golangci-lint run
