//go:build linux

package adc

import (
	"os"
	"testing"
)

func TestIndependentAreaCreationAudit(t *testing.T) {
	if os.Getenv("ADC_AREA_CREATION_AUDIT") != "1" {
		t.Skip("set ADC_AREA_CREATION_AUDIT")
	}
	files := []string{"area_creation.go", "area_creation_test.go", "ownership.go", "decisions.go", "model.go", "engine.go", "web.go", "provider_tools.go", "codex_run.go", "claude_client.go", "templates/areas.html", "templates/app.html"}
	prompt := `Review conversational area creation. The /area-proposals action uses the existing member/CSRF router and the human's own account, creates a temporary guide plus one restricted Kind=proposal assignment with AreaCreation=true atomically via assignmentWrites(assignment,guide). No permanent area/agent or work is created. Existing cross-provider proposal restrictions disable external/native tools; area conversations expose only adc_status/read_document/propose_area/finish/blocked. A dedicated area proposal tool validates an existing permanent owner, bounds/secret screening and duplicates; it calls submitDecision with ProposedArea. Replacement uses a fresh decision ID and resolved requests remain historical. Human approval revalidates and atomically saves area+decision+completed conversation using prepareArea; reject/refine save no area. Form creation still uses saveArea wrapper. Review concrete authorization, transaction, tool routing, stale/repeated approval, provider and human input issues. Do not demand implementation review on a human-approved configuration proposal. At most two focused helper reads, then adc_review_report PASS or actionable defects.`
	for _, file := range []string{"area_creation.go", "area_creation_test.go", "decisions.go"} {
		b, err := os.ReadFile(file)
		must(t, err)
		prompt += "\nFILE " + file + "\n" + string(b) + "\nEND FILE"
	}
	runIndependentCodeAudit(t, files, prompt, "area-creation-independent-review.json")
}
