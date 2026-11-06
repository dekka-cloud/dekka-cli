SPEC_URL ?= https://api.dekka.cloud/openapi.json
SPEC     := openapi/openapi.json
BIN      := bin/dekka

.PHONY: build test lint generate sync-spec check-generated

build:
	go build -o $(BIN) ./cmd/dekka

test:
	go test -race ./...

lint:
	golangci-lint run

# Regenerate the API client from the vendored OpenAPI document.
generate:
	go generate ./internal/api

# Replace the vendored document with the one the public API serves, then regenerate.
sync-spec:
	curl -fsSL --proto '=https' $(SPEC_URL) -o $(SPEC)
	go run ./internal/tools/jsonindent $(SPEC)
	$(MAKE) generate

# Fail when the generated client does not match the vendored document.
check-generated: generate
	git diff --exit-code -- internal/api
