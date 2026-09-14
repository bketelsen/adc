//go:build !linux

package adc

import (
	"fmt"
	copilot "github.com/github/copilot-sdk/go"
)

func (e *Engine) contributionImportTool(r Run) copilot.Tool {
	return copilot.DefineTool("adc_import_contribution", "Requires qualified Linux execution", func(struct{}, copilot.ToolInvocation) (string, error) {
		return "", fmt.Errorf("qualified Linux execution required")
	})
}
