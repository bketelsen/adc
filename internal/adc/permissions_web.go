package adc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

type PermissionToolView struct {
	Tool   GatewayTool
	Policy ToolPolicy
	Stale  bool
	Schema string
}
type PermissionEntryView struct {
	Tool        GatewayTool
	Entry       AccessEntry
	Constraints string
	Arguments   string
}
type PermissionRequestView struct {
	Request      AccessRequest
	Entries      []PermissionEntryView
	Runs         []Run
	OneOperation bool
}
type PermissionPage struct {
	Connection string
	Tools      []PermissionToolView
	Requests   []PermissionRequestView
	Resolved   int
}

func (w *Web) permissionPage(org, connection string) PermissionPage {
	p := PermissionPage{Connection: connection}
	for _, tool := range list[GatewayTool](w.Store, "gateway-tool", org) {
		if connection != "" && tool.Connection != connection {
			continue
		}
		var policy ToolPolicy
		_ = w.Store.Get("policy-"+tool.ID, &policy)
		p.Tools = append(p.Tools, PermissionToolView{Tool: tool, Policy: policy, Stale: policy.Fingerprint != tool.Fingerprint, Schema: string(tool.Schema)})
	}
	for _, request := range list[AccessRequest](w.Store, "access-request", org) {
		if request.State != "pending" {
			p.Resolved++
			continue
		}
		view := PermissionRequestView{Request: request, OneOperation: true}
		for _, entry := range request.Entries {
			var tool GatewayTool
			_ = w.Store.Get(entry.Tool, &tool)
			b, _ := json.MarshalIndent(entry.Constraints, "", "  ")
			args := string(entry.Arguments)
			if args == "null" {
				args = ""
			}
			view.Entries = append(view.Entries, PermissionEntryView{Tool: tool, Entry: entry, Constraints: string(b), Arguments: args})
			if entry.Operation == "" || entry.ArgumentsHash == "" {
				view.OneOperation = false
			}
		}
		for _, id := range request.Runs {
			var run Run
			if w.Store.Get(id, &run) == nil {
				view.Runs = append(view.Runs, run)
			}
		}
		p.Requests = append(p.Requests, view)
	}
	return p
}

func (w *Web) permissionAction(r *http.Request, page Page) error {
	f := r.FormValue
	switch r.URL.Path {
	case "/permission-default":
		mode := f("mode")
		if mode != "protected" && mode != "advisory" {
			return fmt.Errorf("choose protected or advisory execution")
		}
		if mode == "protected" {
			if err := checkProtectedEnvironment(r.Context(), w.Store.Dir); err != nil {
				return err
			}
		}
		w.Store.mu.Lock()
		defer w.Store.mu.Unlock()
		var org Organization
		if w.Store.Get(page.Org.ID, &org) != nil || !w.Store.permissionHuman(page.User.ID, page.Org.ID) {
			return fmt.Errorf("organization unavailable")
		}
		if org.Execution != f("previous") {
			return fmt.Errorf("another human changed the execution default; reload before deciding")
		}
		org.Execution = mode
		return w.Store.Put("organization", org.ID, "", "", org.ID, org)
	case "/permission-edit":
		revision, err := strconv.Atoi(f("revision"))
		if err != nil {
			return fmt.Errorf("invalid request revision")
		}
		var request AccessRequest
		if w.Store.Get(f("request"), &request) != nil || request.Org != page.Org.ID {
			return fmt.Errorf("request unavailable")
		}
		constraints := make([][]ArgumentConstraint, len(request.Entries))
		for i := range request.Entries {
			if json.Unmarshal([]byte(f("constraints-"+strconv.Itoa(i))), &constraints[i]) != nil {
				return fmt.Errorf("each constraint list must be a JSON array")
			}
		}
		return w.Store.EditAccess(page.User.ID, page.Org.ID, request.ID, revision, constraints, f("answer"))
	case "/permission-bulk":
		var constraints []ArgumentConstraint
		if f("constraints") != "" {
			if json.Unmarshal([]byte(f("constraints")), &constraints) != nil {
				return fmt.Errorf("constraints must be a JSON array")
			}
		}
		changes := []PolicyChange{}
		for _, id := range r.Form["tools"] {
			revision, err := strconv.Atoi(f("revision-" + id))
			if err != nil {
				return fmt.Errorf("invalid policy revision")
			}
			changes = append(changes, PolicyChange{Policy: ToolPolicy{Tool: id, Fingerprint: f("fingerprint-" + id), Mode: f("mode"), Class: f("class"), Constraints: constraints}, Expected: revision})
		}
		return w.Store.SaveToolPolicies(page.User.ID, page.Org.ID, changes)
	case "/permission-discover":
		_, err := w.Store.DiscoverGateway(r.Context(), page.Org.ID, f("connection"))
		return err
	case "/permission-policy":
		// A single row update preserves the review revision. Group classification
		// can submit several selected rows atomically using the batch route below.
		var constraints []ArgumentConstraint
		if f("constraints") != "" {
			if err := json.Unmarshal([]byte(f("constraints")), &constraints); err != nil {
				return fmt.Errorf("constraints must be a JSON array")
			}
		}
		revision, err := strconv.Atoi(f("revision"))
		if err != nil {
			return fmt.Errorf("invalid policy revision")
		}
		return w.Store.SaveToolPolicy(page.User.ID, page.Org.ID, ToolPolicy{Tool: f("tool"), Fingerprint: f("fingerprint"), Mode: f("mode"), Class: f("class"), Constraints: constraints}, revision)
	case "/permission-resolve":
		revision, err := strconv.Atoi(f("revision"))
		if err != nil {
			return fmt.Errorf("invalid request revision")
		}
		return w.Store.ResolveAccess(page.User.ID, page.Org.ID, f("request"), revision, f("scope"), f("answer"))
	default:
		return fmt.Errorf("unknown permission action")
	}
}
