//go:build linux

package adc

import (
	"os"
	"testing"
)

func TestIndependentInlineToolOutputAudit(t *testing.T) {
	if os.Getenv("ADC_OUTPUT_AUDIT") != "1" {
		t.Skip("set ADC_OUTPUT_AUDIT")
	}
	files := []string{"tool_output.go", "tool_output_test.go", "provider_tools.go", "protected_tools_linux.go"}
	prompt := `Review this narrow ADC fix independently. A real protected Copilot owner repeatedly dumped ~47KiB source reads; the provider offloaded output to its private /tmp, invisible to the protected workspace. ADC now limits encoded model-facing results to 12KiB, gives explicit truncation/narrow-read guidance, preserves workspace exit codes and sets Truncated=true. Generic oversized tool results get a JSON preview envelope; small results and tool outcomes are unchanged. Errors are redacted before bounding. Crucially adc_candidate_check bounds its result before considering successful nontruncated validation, so an incomplete preview cannot newly satisfy admission. Existing Verified evidence from prior successful commands remains valid for the unchanged pinned candidate. Inspect concrete correctness, error semantics and permission/evidence regressions. No private credentials or new tools granted. At most two helper reads, then adc_review_report. No speculative unrelated features.`
	if os.Getenv("ADC_OUTPUT_RECHECK") == "1" {
		prompt = "Focused final re-review: your previous verdict passed inline bounds, linear malformed UTF-8 handling, idempotent wait rejoin and non-extending deadlines. We fixed your optional stale WaitRun finding: terminal contribution results clear WaitRun before returning, and expired waits clear WaitRun and close only still-open packets (received/submitted candidates remain alive). Check these precise changes and the regression, with at most ONE helper read, then adc_review_report. Other source is unchanged. No new permission or funding changes."
	}
	for _, name := range files {
		b, err := os.ReadFile(name)
		must(t, err)
		prompt += "\nFILE " + name + "\n" + string(b) + "\nEND FILE\n"
	}
	prompt += "\nCorrection to prior review: malformed UTF-8 is now normalized linearly after slicing (strings.ToValidUTF8), with a regression proving useful evidence survives an invalid byte. Also review a directly observed contribution wait recovery fix: after steering reactivated a waiter, a repeated adc_wait_contribution used to fail because WaitUntil was set, causing an unnecessary adc_blocked human decision. waitContribution now idempotently rejoins the original deadline (never extends); terminal results and expired deadlines return actionable continuation without yielding. providerTools now yields adc_wait_contribution only when the run actually left running, like adc_wait. Tests cover active rejoin, original deadline, expiry, and completed admission. No completed work is revived and no permission/funding changes occur."
	runIndependentCodeAudit(t, files, prompt, "tool-output-independent-review.json")
}
