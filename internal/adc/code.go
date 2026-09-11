package adc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CodeEvidence binds an agent's result to a clean, committed working tree.
// This checks declared artifacts; broad shell access remains advisory.
type CodeEvidence struct{ Path, Commit string }

func gitOutput(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git evidence check failed for %s: %w", dir, err)
	}
	return strings.TrimSpace(string(b)), nil
}

func inspectCode(path string) (CodeEvidence, error) {
	root, err := filepath.EvalSymlinks(path)
	if err != nil {
		return CodeEvidence{}, err
	}
	top, err := gitOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return CodeEvidence{}, err
	}
	top, err = filepath.EvalSymlinks(top)
	if err != nil || top != root {
		return CodeEvidence{}, fmt.Errorf("supply the repository working tree root")
	}
	status, err := gitOutput(root, "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return CodeEvidence{}, err
	}
	if status != "" {
		return CodeEvidence{}, fmt.Errorf("commit the intended changes and resolve untracked files before recording review evidence for %s", root)
	}
	commit, err := gitOutput(root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return CodeEvidence{}, err
	}
	return CodeEvidence{Path: root, Commit: commit}, nil
}

func (e *Engine) captureCode(r *Run, path string) (CodeEvidence, error) {
	if r.Execution == "protected" {
		evidence, err := inspectProtectedCode(*r, path)
		if err != nil {
			return CodeEvidence{}, err
		}
		for i, old := range r.Code {
			if old.Path == evidence.Path {
				r.Code[i] = evidence
				return evidence, nil
			}
		}
		r.Code = append(r.Code, evidence)
		return evidence, nil
	}
	workspace, err := filepath.EvalSymlinks(r.Workspace)
	if err != nil {
		return CodeEvidence{}, err
	}
	evidence, err := inspectCode(path)
	if err != nil {
		return CodeEvidence{}, err
	}
	relative, err := filepath.Rel(workspace, evidence.Path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return CodeEvidence{}, fmt.Errorf("code must be in this run's isolated workspace")
	}
	for i, existing := range r.Code {
		if existing.Path == evidence.Path {
			r.Code[i] = evidence
			return evidence, nil
		}
	}
	r.Code = append(r.Code, evidence)
	return evidence, nil
}

func verifyCode(r Run) error {
	for _, evidence := range r.Code {
		var current CodeEvidence
		var err error
		if r.Execution == "protected" {
			current, err = inspectProtectedCode(r, evidence.Path)
		} else {
			current, err = inspectCode(evidence.Path)
		}
		if err != nil {
			return err
		}
		if current.Commit != evidence.Commit {
			return fmt.Errorf("code changed since recorded evidence at %s; refresh the artifact and independent review", evidence.Path)
		}
	}
	return nil
}
