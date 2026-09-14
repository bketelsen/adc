//go:build linux

package adc

import (
	"os"
	"strings"
	"testing"
)

func TestIndependentSupervisorCodeHandoffAudit(t *testing.T) {
	if os.Getenv("ADC_CODE_HANDOFF_AUDIT") != "1" {
		t.Skip("set ADC_CODE_HANDOFF_AUDIT")
	}
	files := []string{"code.go", "code_handoff_test.go", "engine.go", "review_status.go"}
	prompt := `Review a narrow completion recovery fix independently. In the live contribution pilot a supervisor retained registered code, then correctly delegated exact-commit finalization to a worker with independent review. The old unconditional root.Code guard permanently prevents completion even after that handoff. The new helper accepts only exact nonempty commits adopted by a completed nonsuperseded direct nonreview worker with clean pinned code and a current final PASS independent of BOTH supervisor and worker. The normal root/worker verifyCode, pending work/decision, plan and document gates in adc_finish remain afterward. No evidence is cleared, authority is unchanged, no arbitrary root self-review allowed. Check stale/wrong-commit/multiple-artifact/review-family regressions and remaining completion guards. Tests exercise the actual adc_finish with real local Git trees. One helper read at most, then adc_review_report with concrete blocking findings or PASS. No speculative broader architecture.`
	for _, file := range files {
		b, err := os.ReadFile(file)
		must(t, err)
		if file == "engine.go" {
			start := strings.Index(string(b), `copilot.DefineTool("adc_finish"`)
			end := strings.Index(string(b)[start:], "\n\ttools = append")
			if end < 0 {
				end = len(b) - start
			}
			b = b[start : start+end]
		}
		prompt += "\nFILE " + file + "\n" + string(b) + "\nEND FILE\n"
	}
	runIndependentCodeAudit(t, files, prompt, "supervisor-code-handoff-independent-review.json")
}
