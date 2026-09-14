# Optional machine-specific executable paths and server settings.
-include Makefile.local

.DEFAULT_GOAL := build
GO ?= go
GOFMT ?= gofmt
ADC_ADDR ?= 127.0.0.1:8789
ADC_PUBLIC_CONTRIBUTIONS ?= false
ADC_CONTRIBUTION_RUNTIME ?=
ADC_CONTRIBUTION_RUNTIME_ID ?=
ADC_DATA ?= $(CURDIR)/.adc
COPILOT_CLI_PATH ?= copilot
ADC_CLAUDE_NODE ?= node
ADC_CLAUDE_RUNTIME_DIR ?= $(CURDIR)/runtime/claude

SERVER_ENV = ADC_CONTRIBUTION_RUNTIME="$(ADC_CONTRIBUTION_RUNTIME)" ADC_CONTRIBUTION_RUNTIME_ID="$(ADC_CONTRIBUTION_RUNTIME_ID)" COPILOT_CLI_PATH="$(COPILOT_CLI_PATH)" ADC_CLAUDE_NODE="$(ADC_CLAUDE_NODE)" ADC_CLAUDE_RUNTIME_DIR="$(ADC_CLAUDE_RUNTIME_DIR)"

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
	exec env $(SERVER_ENV) ./bin/adc serve -addr "$(ADC_ADDR)" -data "$(ADC_DATA)" -public-contributions="$(ADC_PUBLIC_CONTRIBUTIONS)"

# Keep development serving independent of the terminal/session that started it.
.PHONY: start stop status logs
start: build
	systemd-run --user --unit=adc-development --collect --service-type=exec --property=WorkingDirectory="$(CURDIR)" --property=Restart=on-failure --property=RestartSec=3 /usr/bin/make serve
stop:
	systemctl --user stop adc-development
status:
	systemctl --user status adc-development
logs:
	journalctl --user -u adc-development -n 80 --no-pager
