//go:build !linux

package adc

import (
	"context"
	"fmt"
)

type contributionExecutor struct{}

func (contributionExecutor) Execute(context.Context, map[string]string, workspaceCommand) (workspaceResult, error) {
	return workspaceResult{}, fmt.Errorf("contribution execution requires qualified Linux isolation")
}
