package adc

func (e *Engine) reviewNeeds(task string) []map[string]string {
	var t Assignment
	_ = e.Store.Get(task, &t)
	reviews := taskReviews(e.Store, task)
	needs := []map[string]string{}
	for _, r := range taskRuns(e.Store, task) {
		if r.Superseded || r.Parent == "" || r.Category == "review" || r.State != "complete" {
			continue
		}
		if !e.requiresIndependentReview(t, r) {
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
		if review.Stage != "candidate" && review.Target == r.ID && review.Revision == e.revision(r) && CanReview(r.Model, review.Model) == nil {
			return review.Verdict == "pass"
		}
	}
	return false
}
