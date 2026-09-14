package adc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestInlineOutputPreservesStatusAndMarksIncompleteEvidence(t *testing.T) {
	for _, value := range []string{strings.Repeat("public source line\n", 4000), strings.Repeat("\x00\"\\", 16000), strings.Repeat("🦊", 10000)} {
		result := inlineWorkspaceResult(workspaceResult{Output: value, ExitCode: 7})
		raw, err := json.Marshal(result)
		must(t, err)
		if len(raw) > inlineToolBytes || !result.Truncated || result.ExitCode != 7 || !utf8.ValidString(result.Output) {
			t.Fatalf("invalid bounded result: %d", len(raw))
		}
		text := inlineToolText(value)
		if len(text) > inlineToolBytes || !json.Valid([]byte(text)) || !strings.Contains(text, "Truncated") {
			t.Fatal("invalid bounded tool text")
		}
	}
	small := workspaceResult{Output: "ok", ExitCode: 0}
	if inlineWorkspaceResult(small) != small || inlineToolText("ok") != "ok" {
		t.Fatal("ordinary results changed")
	}
}

func TestAdmissionTruncatedInlineOutputDoesNotVerify(t *testing.T) {
	s, e, _, p, _ := contributionFixture(t)
	c, _ := submitFixture(t, s, p)
	e.dispatchContributions(time.Now())
	must(t, s.Get(c.ID, &c))
	var r Run
	must(t, s.Get(c.Run, &r))
	setRunning(t, s, &r)
	_, err := call(t, e, r, "adc_candidate_check", map[string]any{"Command": "python3 -I -c 'print(\"x\" * 30000)'"})
	must(t, err)
	must(t, s.Get(c.ID, &c))
	if c.Verified {
		t.Fatal("partial inline output counted as complete validation")
	}
}

func TestInlineMalformedUTF8KeepsUsefulPreview(t *testing.T) {
	input := "prefix\xff useful evidence " + strings.Repeat("evidence ", 10000)
	result := inlineWorkspaceResult(workspaceResult{Output: input})
	if !result.Truncated || !strings.Contains(result.Output, "useful evidence") || !utf8.ValidString(result.Output) {
		t.Fatal("malformed byte erased the preview")
	}
}
