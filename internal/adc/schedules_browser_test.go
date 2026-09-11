package adc

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestBrowserScheduleWorkflow(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for isolated browser qualification")
	}
	s, e, _, _, p := proposalFixture(t)
	p.Cadence = Cadence{Frequency: "interval", IntervalMinutes: 60}
	must(t, s.Put("proposal", p.Org, p.Task, p.State, p.ID, p))
	must(t, s.Put("connection", p.Org, "", "", "storage", Connection{ID: "storage", Org: p.Org, Name: "Storage"}))
	hash, err := bcrypt.GenerateFromPassword([]byte("ui-fixture-password"), bcrypt.MinCost)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','UI fixture','fixture',?); INSERT INTO memberships VALUES('owner','org')`, hash)
	must(t, err)
	w := NewWeb(s, e, false)
	mux := http.NewServeMux()
	mux.Handle("/", w.Handler())
	mux.HandleFunc("/fixture-schedule-due", func(rw http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		e.dispatchSchedules(time.Now().Add(2 * time.Hour))
		rw.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/schedules-check.cjs")
	must(t, err)
	output, err := exec.Command(node, script, server.URL, p.ID).CombinedOutput()
	t.Log(string(output))
	must(t, err)
	schedules := list[StandingSchedule](s, "schedule", p.Org)
	if len(schedules) != 1 || schedules[0].State != "active" || schedules[0].Runs != 1 {
		t.Fatal("schedule controls/history did not persist")
	}
}
