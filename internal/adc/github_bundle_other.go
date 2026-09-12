//go:build !linux

package adc

import (
	"context"
	"fmt"
)

func githubExportBundle(context.Context, Run, CodeEvidence, string) (string, error) {
	return "", fmt.Errorf("mediated Git requires protected Linux execution")
}
func githubImportBundle(context.Context, Run, string, string) (string, error) {
	return "", fmt.Errorf("mediated Git requires protected Linux execution")
}
