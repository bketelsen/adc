package adc

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
)

type OwnerRequestPage struct {
	Requests                      []OwnerRequest
	Total, Offset, Next, Previous int
}

func (w *Web) ownerRequestPage(r *http.Request, p *Page) {
	q := list[OwnerRequest](w.Store, "owner-request", p.Org.ID)
	sort.SliceStable(q, func(i, j int) bool { return requestOpen(q[i]) && !requestOpen(q[j]) })
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset >= len(q) {
		offset = 0
	}
	end := offset + 40
	if end > len(q) {
		end = len(q)
	}
	view := OwnerRequestPage{Total: len(q), Offset: offset, Next: -1, Previous: -1, Requests: q[offset:end]}
	if end < len(q) {
		view.Next = end
	}
	if offset > 0 {
		view.Previous = offset - 40
		if view.Previous < 0 {
			view.Previous = 0
		}
	}
	p.Coordination = view
	p.View, p.Title = "coordination", "Owner coordination"
}
func (w *Web) ownerRequestAction(r *http.Request, p Page) error {
	var q OwnerRequest
	if r.FormValue("action") != "cancel" || w.Store.Get(r.FormValue("id"), &q) != nil || q.Org != p.Org.ID {
		return fmt.Errorf("request unavailable")
	}
	revision, err := strconv.Atoi(r.FormValue("revision"))
	if err != nil || revision != q.Revision {
		return fmt.Errorf("request changed; reload before cancelling")
	}
	if !requestOpen(q) {
		return fmt.Errorf("request already closed")
	}
	reason := r.FormValue("reason")
	if !boundedText(reason, 1500) {
		return fmt.Errorf("record why this request is being cancelled")
	}
	if err = w.Store.checkOwnershipText(q.Org, reason); err != nil {
		return err
	}
	old := q
	q.State, q.WaitReason = "cancelled", "Human cancelled: "+reason
	if err = w.Store.Batch(ownerRequestWrites(old, q)...); err != nil {
		return err
	}
	w.Store.Log(q.Org, q.SourceTask, "", "human", "Cancelled owner request: "+q.Subject+"; "+reason)
	return nil
}
