//go:build linux

package adc

import (
	"context"
	"encoding/json"
	"fmt"

	copilot "github.com/github/copilot-sdk/go"
)

func (e *Engine) contributionImportTool(r Run) copilot.Tool {
	return copilot.DefineTool("adc_import_contribution", "Materialize an internally admitted candidate as a fresh source directory inside your protected workspace. Supply Contribution ID. This does not overwrite your checkout, run submitted code, commit, merge or publish. Integrate and validate under the original assignment's review and delivery gates.", func(in struct{ Contribution string }, _ copilot.ToolInvocation) (any, error) {
		s := e.Store
		s.mu.Lock()
		var c Contribution
		var current Run
		var task Assignment
		if s.Get(in.Contribution, &c) != nil || c.Org != r.Org || c.State != "admitted" || c.Verdict != "pass" || !c.Verified || s.Get(r.ID, &current) != nil || current.State != "running" || current.Execution != "protected" || current.Superseded || s.Get(r.Task, &task) != nil || task.State == "paused" || task.State == "cancelled" || task.State == "ready" {
			s.mu.Unlock()
			return nil, fmt.Errorf("an internally admitted candidate and active protected owner are required")
		}
		var p ContributionPacket
		var q ContributionQueue
		if s.Get(c.Packet, &p) != nil || s.Get(p.Queue, &q) != nil || p.Org != r.Org || q.Org != r.Org || p.Task != r.Task || q.Owner != r.Agent {
			s.mu.Unlock()
			return nil, fmt.Errorf("candidate does not belong to this accountable work")
		}
		files, err := candidateFiles(p, c)
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		payload, _ := json.Marshal(map[string]any{"ID": c.ID, "Files": files})
		x, err := runExecutor(current)
		if err != nil {
			return nil, err
		}
		// Server-selected relative destination and JSON stdin, never shell-interpolated
		// submitted paths or commands. Python isolated mode ignores source modules.
		script := `python3 -I -c 'import json,os,pathlib,sys,tempfile
p=json.load(sys.stdin)
parent=pathlib.Path("/workspace/contributions")
if parent.is_symlink(): raise RuntimeError("destination parent is a symlink")
parent.mkdir(exist_ok=True)
target=parent/p["ID"]
if target.exists() or target.is_symlink(): raise RuntimeError("destination exists; inspect the existing import")
with tempfile.TemporaryDirectory(dir=parent,prefix=".import-") as staging:
 for name,body in p["Files"].items():
  f=pathlib.Path(staging)/name
  f.parent.mkdir(parents=True,exist_ok=True)
  f.write_text(body,encoding="utf-8")
 os.rename(staging,target)
print(target)
'`
		result, err := x.Execute(context.Background(), workspaceCommand{Command: script, Stdin: string(payload), TimeoutSeconds: 30})
		if err != nil {
			return nil, err
		}
		if result.ExitCode != 0 {
			return nil, fmt.Errorf("candidate import: %s", clipped(result.Output, 1000))
		}
		return map[string]any{"Path": "/workspace/contributions/" + c.ID, "Source": p.Public.Source, "SourceRevision": p.Public.SourceRevision, "InternalReviewTask": c.Task, "Next": "Integrate and validate against current source. Existing artifact-bound independent review and publication approval still apply."}, nil
	})
}
