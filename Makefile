BIN := omnishell
LDFLAGS := -X github.com/JtheGunner/omnishell/internal/buildinfo.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev) \
           -X github.com/JtheGunner/omnishell/internal/buildinfo.Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo none) \
           -X github.com/JtheGunner/omnishell/internal/buildinfo.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

ASSETS := build/assets

.PHONY: build test lint vet completions man dist-assets
build:
	go build -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/omnishell
test:
	go test ./... -race -count=1
vet:
	go vet ./...
lint:
	golangci-lint run

# Release assets: shell completions and man pages, generated from the command
# tree with `go run` so no prebuilt binary is required. GoReleaser calls
# `dist-assets` from its before-hook.
completions:
	@mkdir -p $(ASSETS)/completions
	go run ./cmd/omnishell completion bash > $(ASSETS)/completions/omnishell.bash
	go run ./cmd/omnishell completion zsh  > $(ASSETS)/completions/_omnishell
man:
	@mkdir -p $(ASSETS)/man
	go run ./cmd/omnishell docs man $(ASSETS)/man
dist-assets: completions man
