# Optional machine-specific executable paths and server settings.
-include Makefile.local

.DEFAULT_GOAL := build
GO ?= go
GOFMT ?= gofmt
ADC_ADDR ?= 127.0.0.1:8789
ADC_DATA ?= $(CURDIR)/.adc
COPILOT_CLI_PATH ?= copilot
ADC_CLAUDE_NODE ?= node
ADC_CLAUDE_RUNTIME_DIR ?= $(CURDIR)/runtime/claude

SERVER_ENV = COPILOT_CLI_PATH="$(COPILOT_CLI_PATH)" ADC_CLAUDE_NODE="$(ADC_CLAUDE_NODE)" ADC_CLAUDE_RUNTIME_DIR="$(ADC_CLAUDE_RUNTIME_DIR)"

.PHONY: build test verify run serve
build:
	"$(GO)" build -trimpath -o bin/adc ./cmd/adc
test:
	"$(GO)" test -race ./...
verify:
	@test -z "$$("$(GOFMT)" -l $$(find cmd internal -name '*.go'))"
	"$(GO)" vet ./...
	"$(GO)" test -race ./...
	"$(GO)" build -trimpath -o bin/adc ./cmd/adc
run: verify
	$(MAKE) serve
serve:
	exec env $(SERVER_ENV) ./bin/adc serve -addr "$(ADC_ADDR)" -data "$(ADC_DATA)"
