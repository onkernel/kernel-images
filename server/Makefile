SHELL := /bin/bash
.PHONY: oapi-generate build dev test clean

BIN_DIR ?= $(CURDIR)/bin
OAPI_CODEGEN ?= $(BIN_DIR)/oapi-codegen
RECORDING_DIR ?= $(CURDIR)/recordings

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

$(RECORDING_DIR):
	mkdir -p $(RECORDING_DIR)

$(OAPI_CODEGEN): | $(BIN_DIR)
	GOBIN=$(BIN_DIR) go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest

# Generate Go code from the OpenAPI spec
# 1. Convert 3.1 → 3.0 since oapi-codegen doesn't support 3.1 yet (https://github.com/oapi-codegen/oapi-codegen/issues/373)
# 2. Run oapi-codegen with our config
# 3. go mod tidy to pull deps
oapi-generate: $(OAPI_CODEGEN)
	pnpm i -g @apiture/openapi-down-convert
	openapi-down-convert --input openapi.yaml --output openapi-3.0.yaml
	$(OAPI_CODEGEN) -config ./oapi-codegen.yaml ./openapi-3.0.yaml
	go mod tidy

build: | $(BIN_DIR)
	go build -o $(BIN_DIR)/api ./cmd/api

dev: build $(RECORDING_DIR)
	OUTPUT_DIR=$(RECORDING_DIR) ./bin/api

test:
	go vet ./...
	go test -v -race ./...

clean:
	@rm -rf $(BIN_DIR)
	@rm -f openapi-3.0.yaml
	@echo "Clean complete"
