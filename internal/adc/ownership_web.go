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
