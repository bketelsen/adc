//go:build !linux

package adc

import (
	"context"
	"fmt"
	copilot "github.com/github/copilot-sdk/go"
)

func (e *Engine) protectedTools(context.Context, Run) []copilot.Tool { return nil }
func checkProtectedEnvironment(context.Context, string) error {
	return fmt.Errorf("protected execution requires the qualified Linux executor")
}
func inspectProtectedCode(Run, string) (CodeEvidence, error) {
	return CodeEvidence{}, fmt.Errorf("protected execution requires the qualified Linux executor")
}
