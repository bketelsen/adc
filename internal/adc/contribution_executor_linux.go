//go:build linux

package adc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// contributionExecutor is deliberately separate from ordinary protected work.
// Candidate programs get no network, persistent HOME, ADC tools or host worktree.
// systemd supplies aggregate memory/process limits; bubblewrap supplies mounts
// and namespaces. A missing user manager or isolation primitive is a hard error.
type contributionExecutor struct{}

func (contributionExecutor) Execute(ctx context.Context, files map[string]string, in workspaceCommand) (workspaceResult, error) {
	if err := validateContributionFiles(files); err != nil {
		return workspaceResult{}, err
	}
	if strings.TrimSpace(in.Command) == "" || len(in.Command) > 32<<10 || in.Stdin != "" {
		return workspaceResult{}, fmt.Errorf("provide a command up to 32 KiB; stdin is unavailable")
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 30
	}
	if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 120 {
		return workspaceResult{}, fmt.Errorf("contribution command timeout must be 1–120 seconds")
	}
	for _, name := range []string{"/usr/bin/systemd-run", "/usr/bin/systemctl", "/usr/bin/bwrap", "/usr/bin/bash"} {
		if _, err := os.Stat(name); err != nil {
			return workspaceResult{}, fmt.Errorf("contribution isolation requires %s", name)
		}
	}
	source, err := os.MkdirTemp("", "adc-public-source-")
	if err != nil {
		return workspaceResult{}, err
	}
	defer os.RemoveAll(source)
	for name, body := range files {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return workspaceResult{}, err
		}
		if err = os.WriteFile(path, []byte(body), 0600); err != nil {
			return workspaceResult{}, err
		}
	}
	unit := "adc-contribution-" + ID()
	// The service cannot outlive its call. Stop the entire cgroup before deleting
	// source, even if the IPC client disconnects while the service still runs.
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(stopCtx, "/usr/bin/systemctl", "--user", "stop", unit+".service").Run()
	}()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds+10)*time.Second)
	defer cancel()
	args := []string{"--user", "--quiet", "--wait", "--pipe", "--collect", "--unit=" + unit, "--service-type=exec",
		"-p", "CPUQuota=100%", "-p", "MemoryMax=512M", "-p", "MemorySwapMax=0", "-p", "TasksMax=64", "-p", fmt.Sprintf("RuntimeMaxSec=%d", in.TimeoutSeconds), "-p", "LimitCORE=0", "-p", "NoNewPrivileges=yes", "-p", "KillMode=control-group",
		"/usr/bin/bwrap", "--unshare-all", "--unshare-user", "--disable-userns", "--cap-drop", "ALL", "--new-session", "--die-with-parent",
		"--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin", "--symlink", "usr/sbin", "/sbin", "--symlink", "usr/lib", "/lib", "--symlink", "usr/lib64", "/lib64",
		"--proc", "/proc", "--dev", "/dev", "--dir", "/etc", "--size", "268435456", "--tmpfs", "/workspace", "--size", "16777216", "--tmpfs", "/tmp", "--dir", "/home", "--size", "16777216", "--tmpfs", "/home/worker",
		"--ro-bind", source, "/source", "--clearenv", "--setenv", "HOME", "/home/worker", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "LANG", "C.UTF-8", "--setenv", "TMPDIR", "/tmp", "--setenv", "GIT_CONFIG_NOSYSTEM", "1", "--setenv", "GIT_TERMINAL_PROMPT", "0",
		"--chdir", "/workspace", "/usr/bin/bash", "--noprofile", "--norc", "-c", `cp -r /source/. /workspace/ && exec /usr/bin/bash --noprofile --norc -c "$1"`, "adc-contribution", in.Command}
	cmd := exec.CommandContext(ctx, "/usr/bin/systemd-run", args...)
	// systemd-run needs the invoking user's bus, but none of this environment is
	// passed through bubblewrap's clearenv to candidate code.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "XDG_RUNTIME_DIR=" + os.Getenv("XDG_RUNTIME_DIR")}
	out := &cappedOutput{}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	result := workspaceResult{Output: out.buf.String(), Truncated: out.truncated, ExitCode: -1}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return result, nil
	}
	return result, err
}
