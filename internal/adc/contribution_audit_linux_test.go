//go:build linux

package adc

import (
	"os"
	"testing"
)

func TestIndependentContributionAudit(t *testing.T) {
	if os.Getenv("ADC_CONTRIBUTION_AUDIT") != "1" {
		t.Skip("set ADC_CONTRIBUTION_AUDIT")
	}
	files := []string{"contributions.go", "contribution_files.go", "contribution_review.go", "contribution_web.go", "contribution_executor_linux.go", "contribution_import_linux.go", "contributions_test.go", "contribution_executor_linux_test.go", "provider_tools.go", "protected_provider.go", "engine.go", "codex_run.go", "store.go", "web.go", "recovery.go", "workspace_executor_linux.go", "templates/contributions.html"}
	prompt := `Review this new P6 contribution implementation independently. Focus on actual correctness and hostile-input boundaries, not speculative features. Read the new contribution*.go files, then relevant provider/engine/web paths if needed. Public endpoints are DISABLED by default; a human owns each queue's public scope, source URL, funding portfolio, finite evaluation budget and cross-family reviewer. The permanent owner offers explicit text-source snapshots under that scope; semantic public-source honesty is advisory, secrets screened, no automatic private-task projection. Anonymous clients claim automatic receipts and submit replacements; no arbitrary URLs or archives are retrieved. Candidates bind a packet/source digest. Internal admission has only adc_candidate_check (cold offline bwrap namespace under systemd cgroup CPU/memory/pid/time limits) and adc_admission. No MCP/native host tools or organization context should enter that run. Candidate validation is not publication or original-task completion. Admitted sources may import into a fresh directory in an active protected owner's workspace, not overwrite an existing checkout. The original review/delivery gates still apply. Source and candidate text, logs and reported model are untrusted. Evaluate paths from HTTP to admission context/tools/execution, quota/replay/restart/revocation behavior, and owner wakeup without routine human intervention. We intentionally start with development text candidates, no external review privileges, no reputation, no production queue configured. Identify concrete exploitable/correctness flaws with minimal fixes; do not require authenticated public contributors or approvals per already-authorized packet. Finish with adc_review_report.`
	if os.Getenv("ADC_CONTRIBUTION_RECHECK") == "1" {
		prompt = `Focused re-review of your five concrete findings, at most THREE workspace calls then adc_review_report. Prior review found no route from public input to native host tools, MCP, private context or credentials; keep that conclusion unless changed source contradicts it. Corrections: (1) wakeContributors expires only open packets; received/reviewing candidates remain available while the owner resumes. (2) queue and packet claim budgets now use bounded rolling one-hour ClaimTimes, plus a five-second release cooldown; lifetime Claims are statistics only, so abuse cannot permanently exhaust a queue. Spent/MaxReviews remains the human-funded evaluation budget. (3) admitted import loads packet/queue directly and checks org/task/owner, without requiring open public intake; active protected task and exact admitted candidate remain required. (4) submission and listing use parsed timeAfter instead of RFC3339Nano string comparison. (5) source path dot is rejected. New deterministic regressions cover all five, plus exhausted admission returning to its owner without a human continuation decision. CompleteActivation preserves completed admission despite steering; recovery escalation for contribution-review does not create a pending human decision. Real Sol/Opus public donation and internal false-PASS rejection passed in 31.66 seconds, mobile UI passed, and aggregate process/memory/storage isolation tests passed. Inspect the actual corrections for correctness, concurrency/revocation regressions or new authority exposure, then pass or actionable findings. Do not repeat a broad source audit or add unrelated features.`
	}
	prompt += "\nAt most FOUR workspace calls if needed, then report concrete findings. The current primary source is supplied below; review it directly rather than repeatedly exploring the repository.\n"
	for _, name := range []string{"contributions.go", "contribution_files.go", "contribution_review.go", "contribution_web.go", "contribution_executor_linux.go", "contribution_import_linux.go", "provider_tools.go", "protected_provider.go"} {
		b, err := os.ReadFile(name)
		must(t, err)
		prompt += "\nFILE " + name + "\n" + string(b) + "\nEND FILE\n"
	}
	runIndependentCodeAudit(t, files, prompt, "contribution-independent-review.json")
}
