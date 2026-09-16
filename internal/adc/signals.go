package adc

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A signal is a steward telling a human something worth knowing: the NVMe
// pool is at 50%, last night's backup failed, a decision is coming up. It is
// keyed so repeated observations update one row instead of piling up, and a
// human acknowledges, snoozes or resolves it from the home page.
type Signal struct {
	ID, Org, Steward, Key, Severity, Message, Evidence, Suggestion, Task, Run string
	State, Created, Updated, SnoozedUntil, ResolvedBy                         string
	Count, Revision                                                           int
}

type signalInput struct {
	Key, Severity, Message, Evidence, Suggestion string
	Clear                                        bool
}

func signalID(steward, key string) string {
	return "signal:" + digest(steward + "\n" + strings.ToLower(strings.TrimSpace(key)))[:32]
}

func severityRank(v string) int {
	switch v {
	case "urgent":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

func (s *Store) stewardSignals(steward string) []Signal {
	out := []Signal{}
	for _, v := range list[Signal](s, "signal", "") {
		if v.Steward == steward {
			out = append(out, v)
		}
	}
	sortSignals(out)
	return out
}

func sortSignals(out []Signal) {
	sort.SliceStable(out, func(i, j int) bool {
		if severityRank(out[i].Severity) != severityRank(out[j].Severity) {
			return severityRank(out[i].Severity) < severityRank(out[j].Severity)
		}
		return out[i].Updated > out[j].Updated
	})
}

// openSignals lists what needs a human: open signals plus snoozed ones whose
// snooze has passed. Acknowledged and resolved signals stay on the steward page.
func (s *Store) openSignals(org string, at time.Time) []Signal {
	out := []Signal{}
	for _, v := range list[Signal](s, "signal", org) {
		if v.State == "open" || (v.State == "snoozed" && snoozeExpired(v, at)) {
			out = append(out, v)
		}
	}
	sortSignals(out)
	return out
}

func snoozeExpired(v Signal, at time.Time) bool {
	until, err := time.Parse(time.RFC3339Nano, v.SnoozedUntil)
	return err != nil || !at.Before(until)
}

func (s *Store) raiseSignal(v Steward, task, run string, in signalInput) (Signal, error) {
	in.Key = strings.TrimSpace(in.Key)
	if !boundedText(in.Key, factKeyLimit) {
		return Signal{}, fmt.Errorf("a signal needs a short stable Key (at most %d characters)", factKeyLimit)
	}
	id := signalID(v.Agent, in.Key)
	var old Signal
	exists := s.Get(id, &old) == nil
	if in.Clear {
		if !exists {
			return Signal{}, fmt.Errorf("no signal named %q to clear", in.Key)
		}
		if old.State == "resolved" {
			return old, nil
		}
		old.State, old.ResolvedBy, old.Updated, old.Revision = "resolved", "run:"+run, now(), old.Revision+1
		return old, s.Put("signal", old.Org, old.Steward, old.State, old.ID, old)
	}
	if in.Severity == "" {
		in.Severity = "info"
	}
	if in.Severity != "info" && in.Severity != "warning" && in.Severity != "urgent" {
		return Signal{}, fmt.Errorf("Severity is info, warning or urgent")
	}
	if !boundedText(in.Message, 600) || len(in.Evidence) > 2000 || len(in.Suggestion) > 600 {
		return Signal{}, fmt.Errorf("supply a Message up to 600 characters, Evidence up to 2000 and a Suggestion up to 600")
	}
	if err := s.checkOwnershipText(v.Org, in.Key, in.Message, in.Evidence, in.Suggestion); err != nil {
		return Signal{}, err
	}
	sig := Signal{ID: id, Org: v.Org, Steward: v.Agent, Key: in.Key, Severity: in.Severity, Message: strings.TrimSpace(in.Message), Evidence: in.Evidence, Suggestion: in.Suggestion, Task: task, Run: run, State: "open", Created: now(), Updated: now(), Count: 1, Revision: 1}
	if exists {
		sig.Created, sig.Count, sig.Revision = old.Created, old.Count+1, old.Revision+1
		sig.State, sig.SnoozedUntil = old.State, old.SnoozedUntil
		// A repeat of an acknowledged or snoozed signal stays quiet unless it
		// got worse or says something new; a resolved one reopens.
		changed := sig.Message != old.Message || severityRank(sig.Severity) < severityRank(old.Severity)
		if old.State == "resolved" || changed {
			sig.State, sig.SnoozedUntil = "open", ""
		}
	}
	return sig, s.Put("signal", sig.Org, sig.Steward, sig.State, sig.ID, sig)
}

// Human actions from the home or steward page. Caller holds Store.mu.
func (s *Store) signalAction(id string, revision int, action, user string, at time.Time) (Signal, error) {
	var v Signal
	if s.Get(id, &v) != nil {
		return Signal{}, fmt.Errorf("signal unavailable")
	}
	if v.Revision != revision {
		return v, fmt.Errorf("signal changed; reload before acting")
	}
	switch action {
	case "acknowledge":
		v.State, v.SnoozedUntil = "acknowledged", ""
	case "snooze":
		v.State, v.SnoozedUntil = "snoozed", at.Add(7*24*time.Hour).UTC().Format(time.RFC3339Nano)
	case "resolve":
		v.State, v.SnoozedUntil = "resolved", ""
	case "reopen":
		v.State, v.SnoozedUntil = "open", ""
	default:
		return v, fmt.Errorf("unknown signal action")
	}
	v.ResolvedBy, v.Updated, v.Revision = "human:"+user, now(), v.Revision+1
	return v, s.Put("signal", v.Org, v.Steward, v.State, v.ID, v)
}

func (w *Web) signalWebAction(r *http.Request, p Page) error {
	var v Signal
	if w.Store.Get(r.FormValue("id"), &v) != nil || v.Org != p.Org.ID {
		return fmt.Errorf("signal unavailable")
	}
	rev, err := strconv.Atoi(r.FormValue("revision"))
	if err != nil {
		return fmt.Errorf("signal revision required")
	}
	_, err = w.Store.signalAction(v.ID, rev, r.FormValue("action"), p.User.ID, time.Now())
	return err
}
