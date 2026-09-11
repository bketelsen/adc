package adc

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

// Redaction covers configured secrets and recognizable credential forms. It is
// not a substitute for keeping secrets out of tool output and workspace notes.
type Redactor struct{ Values []string }

var credentialPattern = regexp.MustCompile(`(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{20,})`)
var assignmentPattern = regexp.MustCompile(`(?i)(\b(?:password|token|api[_-]?key|secret|authorization)\b["']?\s*[:=]\s*["']?)([^\s,"';]+)`)
var privateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`)

func (r Redactor) Text(text string) string {
	values := append([]string(nil), r.Values...)
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, value := range values {
		if value != "" {
			text = strings.ReplaceAll(text, value, "[redacted]")
		}
	}
	text = privateKeyPattern.ReplaceAllString(text, "[private key redacted]")
	text = credentialPattern.ReplaceAllString(text, "[credential redacted]")
	return assignmentPattern.ReplaceAllString(text, "${1}[redacted]")
}
func (r Redactor) JSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "[arguments unavailable]"
	}
	var decoded any
	if json.Unmarshal(data, &decoded) != nil {
		return "[arguments unavailable]"
	}
	var scrub func(any)
	scrub = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for key, item := range v {
				switch strings.ToLower(strings.ReplaceAll(key, "-", "_")) {
				case "password", "token", "access_token", "refresh_token", "api_key", "apikey", "secret", "client_secret", "authorization", "cookie", "private_key", "credentials":
					v[key] = "[redacted]"
				default:
					scrub(item)
				}
			}
		case []any:
			for _, item := range v {
				scrub(item)
			}
		}
	}
	scrub(decoded)
	data, _ = json.MarshalIndent(decoded, "", "  ")
	return r.Text(string(data))
}
func clipped(text string, limit int) string {
	if len(text) > limit {
		return text[:limit] + "\n[display truncated; inspect the original artifact for full evidence]"
	}
	return text
}

type ToolTrace struct{ ID, Org, Task, Run, Session, Call, Name, State, Arguments, Result, At string }

func traceID(session, call string) string { return "trace:" + digest(session+":"+call) }
func (e *Engine) toolStarted(r Run, session string, redact Redactor, event *copilot.ToolExecutionStartData) {
	trace := ToolTrace{ID: traceID(session, event.ToolCallID), Org: r.Org, Task: r.Task, Run: r.ID, Session: session, Call: event.ToolCallID, Name: event.ToolName, State: "running", Arguments: clipped(redact.JSON(event.Arguments), 8192), At: now()}
	_ = e.Store.Put("tooltrace", trace.Org, trace.Task, trace.State, trace.ID, trace)
	e.Store.Log(r.Org, r.Task, r.ID, "tool", event.ToolName)
}
func (e *Engine) toolCompleted(r Run, session string, redact Redactor, event *copilot.ToolExecutionCompleteData) {
	var trace ToolTrace
	if e.Store.Get(traceID(session, event.ToolCallID), &trace) != nil {
		return
	}
	trace.State = "complete"
	if !event.Success {
		trace.State = "failed"
	}
	var parts []string
	if event.Result != nil {
		parts = append(parts, event.Result.Content)
	}
	if event.Error != nil {
		parts = append(parts, event.Error.Message)
	}
	trace.Result = clipped(redact.Text(strings.Join(parts, "\n")), 16384)
	_ = e.Store.Put("tooltrace", trace.Org, trace.Task, trace.State, trace.ID, trace)
	e.Store.Log(r.Org, r.Task, r.ID, "tool-result", fmt.Sprintf("%s · %s", trace.Name, trace.State))
}
func taskTraces(s *Store, task string) []ToolTrace {
	records, err := s.Records("tooltrace", "")
	if err != nil {
		return nil
	}
	out := []ToolTrace{}
	for _, record := range records {
		if record.Parent != task {
			continue
		}
		var trace ToolTrace
		if json.Unmarshal(record.Data, &trace) == nil {
			out = append(out, trace)
		}
		if len(out) >= 100 {
			break
		}
	}
	return out
}

func (s *Store) interruptTraces(run, session string) {
	_, _ = s.db.Exec(`UPDATE records SET state='interrupted',data=json_set(data,'$.State','interrupted'),updated=? WHERE kind='tooltrace' AND state='running' AND json_extract(data,'$.Run')=? AND json_extract(data,'$.Session')=?`, now(), run, session)
}
