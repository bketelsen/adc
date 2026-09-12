//go:build linux

package adc

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func githubCopyFile(root *os.Root, name, destination string) error {
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return fmt.Errorf("Git bundle must be a regular file under 256 MiB")
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.CopyN(out, file, info.Size())
	return err
}
func githubExportBundle(ctx context.Context, r Run, code CodeEvidence, dir string) (string, error) {
	x, err := runExecutor(r)
	if err != nil {
		return "", err
	}
	inside, _, err := protectedCodePath(r, code.Path)
	if err != nil {
		return "", err
	}
	name := "delivery-" + ID() + ".bundle"
	root, err := os.OpenRoot(x.Home)
	if err != nil {
		return "", err
	}
	defer root.Close()
	defer root.Remove(name)
	result, err := x.Execute(ctx, workspaceCommand{Command: "git -c core.hooksPath=/dev/null -c core.fsmonitor=false -C " + shellQuote(inside) + " bundle create " + shellQuote("/home/worker/"+name) + " " + shellQuote(code.Commit) + " HEAD", TimeoutSeconds: 45})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("could not export registered commit: %s", clipped(result.Output, 2000))
	}
	destination := filepath.Join(dir, "source.bundle")
	if err = githubCopyFile(root, name, destination); err != nil {
		return "", err
	}
	return destination, nil
}
func githubImportBundle(ctx context.Context, r Run, bundle, commit string) (string, error) {
	x, err := runExecutor(r)
	if err != nil {
		return "", err
	}
	source, err := os.OpenRoot(filepath.Dir(bundle))
	if err != nil {
		return "", err
	}
	defer source.Close()
	// The destination home is installation-owned; open via a root handle to
	// avoid following a replaced workspace/home directory while copying.
	home, err := os.OpenRoot(x.Home)
	if err != nil {
		return "", err
	}
	defer home.Close()
	name := "fetch-" + ID() + ".bundle"
	defer home.Remove(name)
	from, err := source.Open(filepath.Base(bundle))
	if err != nil {
		return "", err
	}
	defer from.Close()
	info, err := from.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return "", fmt.Errorf("Git bundle exceeds the 256 MiB limit")
	}
	to, err := home.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, copyErr := io.CopyN(to, from, info.Size())
	closeErr := to.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	destination := "/workspace/github-" + ID()
	result, err := x.Execute(ctx, workspaceCommand{Command: "git clone --no-checkout " + shellQuote("/home/worker/"+name) + " " + shellQuote(destination) + " && git -C " + shellQuote(destination) + " checkout --detach " + shellQuote(commit), TimeoutSeconds: 45})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("could not import repository bundle: %s", clipped(result.Output, 2000))
	}
	evidence, err := inspectProtectedCode(r, destination)
	if err != nil {
		return "", err
	}
	if evidence.Commit != commit {
		return "", fmt.Errorf("imported commit differs from the fetched commit")
	}
	return destination, nil
}
