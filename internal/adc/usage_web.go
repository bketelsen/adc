package adc

import (
	"bytes"
	"net/http"
	"strconv"
	"time"

	"github.com/starfederation/datastar-go/datastar"
)

func usageSelection(r *http.Request) (days, page int) {
	days = 30
	if d := r.URL.Query().Get("days"); d != "" {
		switch d {
		case "1":
			days = 1
		case "7":
			days = 7
		case "30":
			days = 30
		case "0":
			days = 0
		}
	}
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 0 || page > 1000000 {
		page = 0
	}
	return
}
func (w *Web) liveUsage(rw http.ResponseWriter, r *http.Request, p Page) {
	sse := datastar.NewSSE(rw, r)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if !w.member(p.User.ID, p.Org.ID) {
			return
		}
		report, err := w.Store.usageReport(p.Org.ID, p.Usage.Task, p.Usage.Days, p.Usage.Page, time.Now())
		if err != nil {
			return
		}
		p.Usage = report
		var b bytes.Buffer
		if w.templates.ExecuteTemplate(&b, "usage-report", p) != nil {
			return
		}
		if sse.PatchElements(b.String()) != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
