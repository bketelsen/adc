//go:build linux

package adc

import (
	"context"
	"fmt"
)

func verifyRepositoryBase(r Run, repo RepositoryEvidence) error {
	if r.Execution != "protected" {
		_, err := gitOutput(repo.Path, "merge-base", "--is-ancestor", repo.Base, repo.Commit)
		return err
	}
	inside, _, err := protectedCodePath(r, repo.Path)
	if err != nil {
		return err
	}
	x, err := runExecutor(r)
	if err != nil {
		return err
	}
	result, err := x.Execute(context.Background(), workspaceCommand{Command: "git -c core.fsmonitor=false -c core.hooksPath=/dev/null -C " + shellQuote(inside) + " merge-base --is-ancestor " + shellQuote(repo.Base) + " " + shellQuote(repo.Commit), TimeoutSeconds: 15})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("repository base must be an ancestor of the registered output commit")
	}
	return nil
}
func executeValidationCommand(ctx context.Context, r Run, command string) (string, int, error) {
	x, err := runExecutor(r)
	if err != nil {
		return "", -1, err
	}
	result, err := x.Execute(ctx, workspaceCommand{Command: command, TimeoutSeconds: 300})
	return result.Output, result.ExitCode, err
}
