package adc

import (
	"bytes"
	"net/http"
	"time"

	"github.com/starfederation/datastar-go/datastar"
)

func scheduleWhen(s StandingSchedule) string {
	t, err := time.Parse(time.RFC3339Nano, s.NextAt)
	if err != nil {
		return s.NextAt
	}
	loc := time.UTC
	if s.Cadence.Frequency != "interval" {
		if zone, err := time.LoadLocation(s.Cadence.Timezone); err == nil {
			loc = zone
		}
	}
	return t.In(loc).Format("Jan 2, 2006 · 15:04 MST")
}
func scheduleHistory(tasks []Assignment, id string) []Assignment {
	out := []Assignment{}
	for _, task := range tasks {
		if task.Schedule == id {
			out = append(out, task)
			if len(out) == 10 {
				break
			}
		}
	}
	return out
}
func (w *Web) liveSchedules(rw http.ResponseWriter, r *http.Request, p Page) {
	sse := datastar.NewSSE(rw, r)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	last := ""
	for {
		if !w.member(p.User.ID, p.Org.ID) {
			return
		}
		p.Schedules = list[StandingSchedule](w.Store, "schedule", p.Org.ID)
		p.Tasks = list[Assignment](w.Store, "assignment", p.Org.ID)
		var b bytes.Buffer
		if w.templates.ExecuteTemplate(&b, "schedule-list", p) != nil {
			return
		}
		current := b.String()
		if current != last {
			if sse.PatchElements(current) != nil {
				return
			}
			last = current
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
