//go:build linux

package adc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// workspaceExecutor runs model-controlled commands in the qualified Linux profile.
// Paths are installation-owned configuration, never arguments from a model.
var workspaceNetworkPaths = []string{"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/ssl/certs", "/etc/pki/ca-trust/extracted", "/etc/pki/tls/certs"}

type workspaceExecutor struct {
	Workspace, Home string
}

type workspaceCommand struct {
	Command        string
	Stdin          string
	TimeoutSeconds int
}

type workspaceResult struct {
	Output    string
	ExitCode  int
	Truncated bool
}

// cappedOutput drains output even after the cap; a noisy child cannot grow ADC's
// memory or deadlock a pipe. stdout and stderr may write concurrently.
type cappedOutput struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := (1 << 20) - b.buf.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.buf.Write(p)
	return n, nil
}

// Open the trusted workspace directory without following a replaced final
// symlink, then bind the descriptor. Rename races cannot redirect that mount.
func workspaceDirectory(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, fmt.Errorf("executor directories must be normalized absolute paths")
	}
	fd, err := unix.Open(path, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open executor directory: %w", err)
	}
	return os.NewFile(uintptr(fd), path), nil
}

func (x workspaceExecutor) Execute(ctx context.Context, in workspaceCommand) (workspaceResult, error) {
	if strings.TrimSpace(in.Command) == "" || len(in.Command) > 128<<10 || len(in.Stdin) > 2<<20 {
		return workspaceResult{}, fmt.Errorf("provide a command of at most 128 KiB and stdin of at most 2 MiB")
	}
	if in.TimeoutSeconds < 0 || in.TimeoutSeconds > 300 {
		return workspaceResult{}, fmt.Errorf("timeout must be 1–300 seconds")
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 60
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds)*time.Second)
	defer cancel()
	work, err := workspaceDirectory(x.Workspace)
	if err != nil {
		return workspaceResult{}, err
	}
	defer work.Close()
	home, err := workspaceDirectory(x.Home)
	if err != nil {
		return workspaceResult{}, err
	}
	defer home.Close()
	wi, err := work.Stat()
	if err != nil {
		return workspaceResult{}, err
	}
	hi, err := home.Stat()
	if err != nil {
		return workspaceResult{}, err
	}
	if os.SameFile(wi, hi) {
		return workspaceResult{}, fmt.Errorf("workspace and private home must differ")
	}
	// Only distribution executables are used here; never resolve a launcher from
	// a worker-controlled PATH. Missing prerequisites are failures, not fallback.
	for _, binary := range []string{"/usr/bin/bwrap", "/usr/bin/prlimit", "/usr/bin/bash"} {
		if _, err := os.Stat(binary); err != nil {
			return workspaceResult{}, fmt.Errorf("isolated execution requires %s: %w", binary, err)
		}
	}
	args := []string{"--nofile=256:256", "--fsize=268435456:268435456", "--cpu=300:300", "--", "/usr/bin/bwrap",
		"--unshare-user", "--unshare-pid", "--unshare-ipc", "--unshare-uts", "--unshare-cgroup-try",
		"--disable-userns", "--cap-drop", "ALL", "--new-session", "--die-with-parent",
		"--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin", "--symlink", "usr/sbin", "/sbin",
		"--symlink", "usr/lib", "/lib", "--symlink", "usr/lib64", "/lib64",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--dir", "/etc",
		"--bind-fd", "3", "/workspace", "--bind-fd", "4", "/home/worker",
		"--clearenv", "--setenv", "HOME", "/home/worker", "--setenv", "PATH", "/usr/bin:/bin",
		"--setenv", "LANG", "C.UTF-8", "--setenv", "TMPDIR", "/tmp",
		"--setenv", "GIT_CONFIG_NOSYSTEM", "1", "--setenv", "GIT_TERMINAL_PROMPT", "0",
		"--setenv", "XDG_CACHE_HOME", "/home/worker/.cache", "--setenv", "XDG_CONFIG_HOME", "/home/worker/.config"}
	// Copy only standard networking/trust configuration into the namespace.
	// Never bind /etc wholesale: installations often keep secrets there.
	for _, path := range workspaceNetworkPaths {
		if _, err := os.Stat(path); err == nil {
			args = append(args, "--ro-bind", path, path)
		}
	}
	args = append(args, "--chdir", "/workspace", "/usr/bin/bash", "--noprofile", "--norc", "-c", in.Command)
	cmd := exec.CommandContext(ctx, "/usr/bin/prlimit", args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	cmd.Dir = "/"
	cmd.ExtraFiles = []*os.File{work, home}
	cmd.Stdin = strings.NewReader(in.Stdin)
	output := &cappedOutput{}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	result := workspaceResult{Output: output.buf.String(), Truncated: output.truncated}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return result, fmt.Errorf("workspace execution ended: %w", ctx.Err())
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return result, nil
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return result, fmt.Errorf("isolated execution unavailable: %w", err)
	}
	return result, nil
}
