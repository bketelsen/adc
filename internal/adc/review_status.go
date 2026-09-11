package adc

func (e *Engine) reviewNeeds(task string) []map[string]string {
	owners := map[string]bool{}
	for _, d := range taskDocs(e.Store, task) {
		owners[d.Run] = true
	}
	reviews := taskReviews(e.Store, task)
	needs := []map[string]string{}
	for _, r := range taskRuns(e.Store, task) {
		if r.Parent == "" || r.Category == "review" || r.State != "complete" {
			continue
		}
		if r.Category != "implementation" && len(r.Code) == 0 && !owners[r.ID] {
			continue
		}
		if !e.hasCurrentReview(r, reviews) {
			needs = append(needs, map[string]string{"run": r.ID, "title": r.Title, "revision": e.revision(r)})
		}
	}
	return needs
}

func (e *Engine) hasCurrentReview(r Run, reviews []Review) bool {
	for _, review := range reviews {
		if review.Target == r.ID && review.Revision == e.revision(r) && CanReview(r.Model, review.Model) == nil {
			return review.Verdict == "pass"
		}
	}
	return false
}
