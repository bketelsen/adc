//go:build linux

package adc

import (
	"os"
	"testing"
)

func TestIndependentContributionRuntimeAudit(t *testing.T) {
	if os.Getenv("ADC_RUNTIME_AUDIT") != "1" {
		t.Skip("set ADC_RUNTIME_AUDIT")
	}
	files := []string{"contribution_runtime.go", "contribution_runtime_test.go", "contribution_executor_linux.go", "contribution_executor_linux_test.go", "contributions.go", "contribution_review.go", "contribution_web.go", "templates/contributions.html"}
	prompt := `Focused independent review of the new installation-controlled contribution runtime. Existing P6 anonymous intake/isolated admission was reviewed previously. This change adds an optional Go toolchain/public module tree configured by the installation, pinned by digest in a human queue and immutable public packet. Candidate/model input cannot select host paths. Each command copies and hashes the curated runtime, mounts only that verified snapshot read-only, and keeps existing offline namespaces/cgroup limits/no credentials. Basic queues stay basic. Go profile uses fixed 1GiB memory, 256MiB tmp and 256MiB workspace; basic profile unchanged. We curated exact Go1.26.7 plus hashicorp/go-version v1.9.0 and successfully ran actual pinned Updex version tests and vet offline in 12.61 seconds. Tests cover runtime changes, symlinks, cancellation, no host secrets, read-only mount, blocked network. Assess concrete correctness/authority/boundary regressions. The trusted administrator must curate only public content, not their whole cache. Shared kernel isolation is explicitly not VM-grade. Read supplied source; at most two additional workspace calls, then adc_review_report with pass or actionable findings. No deployment or extra features requested.`
	for _, name := range files {
		b, err := os.ReadFile(name)
		must(t, err)
		prompt += "\nFILE " + name + "\n" + string(b) + "\nEND FILE\n"
	}
	b, err := os.ReadFile("../../cmd/adc/main.go")
	must(t, err)
	prompt += "\nCLI implementation (was outside the prior copied snapshot):\n" + string(b)
	prompt += "\nFollow-up: previous review found missing cheap runtime availability checks at intake. contributionQueueAvailable now calls contributionRuntimeAvailable, verifying installed ID, valid digest, and real configured directory; queue creation uses the same check. Hash drift is still checked against actual copied bytes per command, not by hashing 250MB on each public request. Check the correction and the new withdrawal-before-budget regression; avoid repeating unrelated prior audit."
	runIndependentCodeAudit(t, files, prompt, "contribution-independent-review.json")
}
