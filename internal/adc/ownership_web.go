package adc

import (
	"fmt"
	"net/http"
	"strconv"
)

// Uses the normal authenticated organization/CSRF boundary and Store.mu.
func (w *Web) areaAction(r *http.Request, p Page) error {
	if action := r.FormValue("action"); action == "note" || action == "discover" {
		var a Area
		if w.Store.Get(r.FormValue("id"), &a) != nil || a.Org != p.Org.ID {
			return fmt.Errorf("area unavailable")
		}
		rev, err := strconv.Atoi(r.FormValue("revision"))
		if err != nil {
			return fmt.Errorf("area revision required")
		}
		if action == "note" {
			_, err = w.Store.noteArea(a, "human:"+p.User.ID, r.FormValue("selection"), r.FormValue("message"), rev)
			return err
		}
		if rev != a.Revision {
			return fmt.Errorf("area changed; reload before discovery")
		}
		t := Assignment{ID: ID(), Creator: p.User.ID, Account: r.FormValue("account"), ExtraAccount: r.FormValue("extra_account"), Prompt: r.FormValue("brief"), Execution: r.FormValue("execution")}
		writes, err := w.Engine.discoveryWrites(a, t)
		if err != nil {
			return err
		}
		if err = w.Store.Batch(writes...); err != nil {
			return err
		}
		r.Form.Set("return", "/task?org="+a.Org+"&id="+t.ID)
		return nil
	}

	if r.FormValue("action") == "cancel-obligation" {
		var o Obligation
		if w.Store.Get(r.FormValue("obligation"), &o) != nil || o.Org != p.Org.ID {
			return fmt.Errorf("obligation unavailable")
		}
		rev, err := strconv.Atoi(r.FormValue("revision"))
		if err != nil || rev != o.Revision {
			return fmt.Errorf("obligation changed; reload before cancelling")
		}
		if o.State == "resolved" || o.State == "cancelled" {
			return fmt.Errorf("obligation already closed")
		}
		if !boundedText(r.FormValue("reason"), 2000) {
			return fmt.Errorf("record why this obligation is being cancelled")
		}
		if err := w.Store.checkOwnershipText(o.Org, r.FormValue("reason")); err != nil {
			return err
		}
		o.State, o.Note, o.Revision = "cancelled", r.FormValue("reason"), o.Revision+1
		writes := []Write{{"obligation", o.Org, o.SourceTask, o.State, o.ID, o}}
		if o.Task != "" {
			var t Assignment
			if w.Store.Get(o.Task, &t) == nil && t.State != "ready" && t.State != "cancelled" {
				t.State = "paused"
				writes = append(writes, Write{"assignment", t.Org, "", t.State, t.ID, t})
			}
		}
		if err := w.Store.Batch(writes...); err != nil {
			return err
		}
		if o.Task != "" {
			w.Engine.CancelTask(o.Task)
		}
		w.Store.Log(o.Org, o.SourceTask, "", "human", "Cancelled follow-up obligation: "+o.Outcome+"; "+o.Note)
		return nil
	}
	a := Area{ID: r.FormValue("id"), Org: p.Org.ID, Owner: r.FormValue("owner"), Name: r.FormValue("name"), Intent: r.FormValue("intent")}
	rev, _ := strconv.Atoi(r.FormValue("revision"))
	if a.ID != "" {
		var old Area
		if w.Store.Get(a.ID, &old) != nil || old.Org != p.Org.ID {
			return fmt.Errorf("area unavailable")
		}
		a.Summary, a.Source, a.UnderstandingKind, a.ObservedAt = old.Summary, old.Source, old.UnderstandingKind, old.ObservedAt
		a.PublicIntent, a.PublicSource = old.PublicIntent, old.PublicSource
		a.CompletionMode = old.CompletionMode
		if r.FormValue("public_edit") == "yes" {
			a.PublicIntent = r.FormValue("public_intent")
			a.PublicSource = r.FormValue("public_source")
		}
	}
	if _, present := r.Form["completion_mode"]; present {
		a.CompletionMode = r.FormValue("completion_mode")
	}
	_, err := w.Store.saveArea(a, rev, "human:"+p.User.ID)
	return err
}
