//go:build linux

package adc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func protectedCodePath(r Run, path string) (string, string, error) {
	var relative string
	if path == "/workspace" || strings.HasPrefix(path, "/workspace/") {
		relative = strings.TrimPrefix(strings.TrimPrefix(path, "/workspace"), "/")
	} else {
		var err error
		relative, err = filepath.Rel(r.Workspace, path)
		if err != nil {
			return "", "", err
		}
	}
	relative = filepath.Clean(relative)
	if relative == ".." || strings.HasPrefix(relative, "../") || filepath.IsAbs(relative) {
		return "", "", fmt.Errorf("code must be inside this run's /workspace")
	}
	return filepath.Join("/workspace", relative), filepath.Join(r.Workspace, relative), nil
}

func inspectProtectedCode(r Run, path string) (CodeEvidence, error) {
	inside, host, err := protectedCodePath(r, path)
	if err != nil {
		return CodeEvidence{}, err
	}
	x, err := runExecutor(r)
	if err != nil {
		return CodeEvidence{}, err
	}
	// All Git execution, including configuration and any hooks, stays inside
	// the worker boundary. No model-supplied repository is inspected by host Git.
	script := `import json,os,subprocess,sys
p=sys.argv[1]
def git(*args):
 return subprocess.check_output(['git','-c','core.fsmonitor=false','-c','core.hooksPath=/dev/null','-C',p,*args],text=True).strip()
root=git('rev-parse','--show-toplevel')
if os.path.realpath(root)!=os.path.realpath(p): raise RuntimeError('supply the repository root')
status=git('status','--porcelain=v1','--untracked-files=all','--ignore-submodules=none')
if status: raise RuntimeError('commit changes and resolve untracked files before review')
print(json.dumps({'commit':git('rev-parse','--verify','HEAD')}))`
	result, err := x.Execute(context.Background(), workspaceCommand{Command: "python3 -c " + shellQuote(script) + " " + shellQuote(inside), TimeoutSeconds: 15})
	if err != nil {
		return CodeEvidence{}, err
	}
	if result.ExitCode != 0 {
		return CodeEvidence{}, fmt.Errorf("isolated Git evidence check failed: %s", clipped(result.Output, 2000))
	}
	var evidence struct{ Commit string }
	if json.Unmarshal([]byte(result.Output), &evidence) != nil || (len(evidence.Commit) != 40 && len(evidence.Commit) != 64) {
		return CodeEvidence{}, fmt.Errorf("invalid Git evidence response")
	}
	for _, c := range evidence.Commit {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return CodeEvidence{}, fmt.Errorf("invalid commit identifier")
		}
	}
	return CodeEvidence{Path: host, Commit: evidence.Commit}, nil
}

func (e *Engine) checkoutProtectedCode(ctx context.Context, recipient Run, sourceID string, index int) (map[string]string, error) {
	var source Run
	if e.Store.Get(sourceID, &source) != nil || source.Org != recipient.Org || source.Task != recipient.Task || index < 0 || index >= len(source.Code) {
		return nil, fmt.Errorf("registered code unavailable in this assignment")
	}
	code := source.Code[index]
	current, err := inspectProtectedCode(source, code.Path)
	if err != nil {
		return nil, err
	}
	if current.Commit != code.Commit {
		return nil, fmt.Errorf("author changed the registered revision; request refreshed code evidence")
	}
	from, err := runExecutor(source)
	if err != nil {
		return nil, err
	}
	to, err := runExecutor(recipient)
	if err != nil {
		return nil, err
	}
	inside, _, err := protectedCodePath(source, code.Path)
	if err != nil {
		return nil, err
	}
	name := "review-" + ID() + ".bundle"
	result, err := from.Execute(ctx, workspaceCommand{Command: "git -C " + shellQuote(inside) + " bundle create " + shellQuote("/home/worker/"+name) + " " + shellQuote(code.Commit) + " HEAD", TimeoutSeconds: 60})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("could not export pinned code: %s", clipped(result.Output, 2000))
	}
	fromRoot, err := os.OpenRoot(from.Home)
	if err != nil {
		return nil, err
	}
	defer fromRoot.Close()
	defer fromRoot.Remove(name)
	file, err := fromRoot.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return nil, fmt.Errorf("review bundle is not a regular file under 256 MiB")
	}
	toRoot, err := os.OpenRoot(to.Home)
	if err != nil {
		return nil, err
	}
	defer toRoot.Close()
	out, err := toRoot.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer toRoot.Remove(name)
	_, copyErr := io.CopyN(out, file, info.Size())
	closeErr := out.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	destination := "/workspace/review-" + ID()
	result, err = to.Execute(ctx, workspaceCommand{Command: "git clone --no-checkout " + shellQuote("/home/worker/"+name) + " " + shellQuote(destination) + " && git -C " + shellQuote(destination) + " checkout --detach " + shellQuote(code.Commit), TimeoutSeconds: 60})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("could not import pinned code: %s", clipped(result.Output, 2000))
	}
	return map[string]string{"Path": destination, "Commit": code.Commit, "SourceRun": source.ID}, nil
}
