//go:build !linux

package adc

import (
	"context"
	"fmt"
)

func verifyRepositoryBase(r Run, repo RepositoryEvidence) error {
	if r.Execution == "protected" {
		return fmt.Errorf("protected execution requires Linux")
	}
	_, err := gitOutput(repo.Path, "merge-base", "--is-ancestor", repo.Base, repo.Commit)
	return err
}
func executeValidationCommand(context.Context, Run, string) (string, int, error) {
	return "", -1, fmt.Errorf("observed commands require protected Linux execution")
}
