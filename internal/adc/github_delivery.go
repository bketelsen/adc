package adc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type GitHubDelivery struct {
	ID, Org, Task, Run, SourceRun, Connection, Repository, Branch, Base                              string
	Commit, PreviousCommit, RemoteCommit, SourceRevision, State, Lease, Created, Updated, URL, Error string
	Number, Generation                                                                               int
}
type githubPull struct {
	Number         int
	State, HTMLURL string // API uses html_url, decoded separately below.
	Draft          bool
	Head           struct {
		Ref, SHA string
		Repo     struct {
			FullName string `json:"full_name"`
		}
	}
	Base struct{ Ref string }
}

// Caller holds Store.mu. Rechecked immediately before each remote mutation.
func (g *githubGateway) deliverySource(ctx context.Context, args githubArguments, pin string) (Run, Run, CodeEvidence, error) {
	r, err := g.caller(ctx)
	if err != nil {
		return r, Run{}, CodeEvidence{}, err
	}
	actor, _ := ctx.Value(githubActorKey{}).(githubActor)
	var tool GatewayTool
	if g.store.Get(digest(r.Org+":"+g.connection.ID+":github_draft_pr"), &tool) != nil {
		return r, Run{}, CodeEvidence{}, fmt.Errorf("discover GitHub delivery before use")
	}
	if _, err = g.store.authorizeGateway(r.ID, tool, actor.Operation, actor.Arguments); err != nil {
		return r, Run{}, CodeEvidence{}, err
	}
	var task Assignment
	_ = g.store.Get(r.Task, &task)
	var source Run
	if !task.Publication || g.store.Get(args.SourceRun, &source) != nil || source.Org != r.Org || source.Task != r.Task || source.Execution != "protected" || source.State != "complete" || source.Superseded || args.Index < 0 || args.Index >= len(source.Code) {
		return r, source, CodeEvidence{}, fmt.Errorf("publication authorization and a completed protected source artifact in this assignment are required")
	}
	e := &Engine{Store: g.store}
	if !e.hasCurrentReview(source, taskReviews(g.store, r.Task)) || e.milestoneMissing(source) != "" {
		return r, source, CodeEvidence{}, fmt.Errorf("source needs current independent review and all required evidence before delivery")
	}
	if p, step, ok := e.plannedStep(source); ok {
		view := e.inspectPlan(p)
		ready := false
		for _, candidate := range view.Steps {
			if candidate.Key == step.Key {
				ready = candidate.State == "complete"
			}
		}
		if !ready {
			return r, source, CodeEvidence{}, fmt.Errorf("planned source is not complete with its designated review and current prerequisites")
		}
	}
	code := source.Code[args.Index]
	if code.Commit != args.Commit || !gitObjectID(args.Commit) || (pin != "" && e.revision(source) != pin) {
		return r, source, code, fmt.Errorf("source commit or reviewed evidence changed; refresh the delivery proposal")
	}
	if err = verifyCode(source); err != nil {
		return r, source, code, err
	}
	return r, source, code, nil
}
func (g *githubGateway) findPull(ctx context.Context, args githubArguments, branch string) (*githubPull, error) {
	query := url.Values{"state": {"all"}, "head": {args.Owner + ":" + branch}, "base": {args.Ref}, "per_page": {"100"}}
	raw, _, err := g.request(ctx, "GET", "/repos/"+args.Owner+"/"+args.Repository+"/pulls?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		githubPull
		URL string `json:"html_url"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return nil, fmt.Errorf("invalid GitHub pull response")
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("multiple pull requests use this delivery branch; reconcile before publishing")
	}
	if len(rows) == 0 {
		return nil, nil
	}
	p := rows[0].githubPull
	p.HTMLURL = rows[0].URL
	if p.Head.Ref != branch || p.Base.Ref != args.Ref || !strings.EqualFold(p.Head.Repo.FullName, args.Owner+"/"+args.Repository) {
		return nil, fmt.Errorf("GitHub pull identity differs from the approved repository/head/base")
	}
	if p.State != "open" || !p.Draft {
		return nil, fmt.Errorf("existing pull request is closed or no longer a draft; it will not be reopened or modified automatically")
	}
	return &p, nil
}
func (g *githubGateway) saveDelivery(v GitHubDelivery) error {
	v.Updated = now()
	v.Error = (Redactor{Values: []string{g.token, base64.StdEncoding.EncodeToString([]byte("x-access-token:" + g.token))}}).Text(v.Error)
	return g.store.Put("github-delivery", v.Org, v.Task, v.State, v.ID, v)
}

// Caller holds Store.mu. A late result cannot overwrite a recovery attempt.
func (g *githubGateway) ownsDelivery(v GitHubDelivery) error {
	var current GitHubDelivery
	if g.store.Get(v.ID, &current) != nil || current.Generation != v.Generation || current.State != "in-flight" {
		return fmt.Errorf("delivery lease was superseded; inspect its current outcome")
	}
	return nil
}
func (g *githubGateway) deliver(ctx context.Context, args githubArguments) (result any, err error) {
	if strings.TrimSpace(args.Title) == "" || len(args.Title) > 240 || strings.TrimSpace(args.Body) == "" || len(args.Body) > 16000 {
		return nil, fmt.Errorf("provide a bounded draft PR title and body")
	}
	s := g.store
	s.mu.Lock()
	r, source, code, err := g.deliverySource(ctx, args, "")
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	key := "github-delivery:" + digest(g.connection.ID+":"+r.Task+":"+strings.ToLower(args.Owner+"/"+args.Repository)+":"+source.ID)
	var previous GitHubDelivery
	_ = s.Get(key, &previous)
	lease, leaseErr := time.Parse(time.RFC3339Nano, previous.Lease)
	if previous.State == "in-flight" && (leaseErr != nil || time.Now().Before(lease)) {
		s.mu.Unlock()
		return nil, fmt.Errorf("delivery is still in flight; reconcile after its lease expires")
	}
	if previous.ID != "" && previous.Base != args.Ref {
		s.mu.Unlock()
		return nil, fmt.Errorf("delivery base is already fixed; use a newly scoped source run for a different base")
	}
	e := &Engine{Store: s}
	v := GitHubDelivery{ID: key, Org: r.Org, Task: r.Task, Run: r.ID, SourceRun: source.ID, Connection: g.connection.ID, Repository: args.Owner + "/" + args.Repository, Branch: "adc/" + digest(r.Task + ":" + source.ID)[:32], Base: args.Ref, Commit: code.Commit, PreviousCommit: previous.Commit, RemoteCommit: previous.RemoteCommit, SourceRevision: e.revision(source), State: "in-flight", Lease: time.Now().Add(90 * time.Second).UTC().Format(time.RFC3339Nano), Created: previous.Created, Generation: previous.Generation + 1}
	if v.Created == "" {
		v.Created = now()
	}
	if err = g.saveDelivery(v); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	mutationAttempted := false
	markMutation := func() {
		mutationAttempted = true
		actor, _ := ctx.Value(githubActorKey{}).(githubActor)
		if actor.MutationAttempted != nil {
			*actor.MutationAttempted = true
		}
	}
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if leaseErr := g.ownsDelivery(v); leaseErr != nil {
			err = leaseErr
			return
		}
		v.Lease = ""
		if err != nil {
			v.State = "failed"
			if mutationAttempted {
				v.State = "uncertain"
			}
			v.Error = clipped(err.Error(), 4000)
		}
		if saveErr := g.saveDelivery(v); saveErr != nil {
			err = fmt.Errorf("delivery recording failed; reconcile the same operation before retrying")
		}
	}()
	dir, err := g.bare(ctx)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	bundle, err := githubExportBundle(ctx, source, code, dir)
	if err != nil {
		return nil, err
	}
	if _, err = g.git(ctx, dir, "fetch", "--no-tags", bundle, code.Commit+":refs/heads/artifact"); err != nil {
		return nil, err
	}
	remote := g.remote(args.Owner, args.Repository)
	if _, err = g.git(ctx, dir, "fetch", "--no-tags", remote, "refs/heads/"+args.Ref+":refs/heads/base"); err != nil {
		return nil, err
	}
	if _, err = g.git(ctx, dir, "merge-base", "--is-ancestor", "refs/heads/base", code.Commit); err != nil {
		return nil, fmt.Errorf("current base is not contained in the reviewed commit; update the artifact and repeat its checks/review")
	}
	ref := "refs/heads/" + v.Branch
	headText, err := g.git(ctx, dir, "ls-remote", "--heads", remote, ref)
	if err != nil {
		return nil, err
	}
	head := ""
	if fields := strings.Fields(headText); len(fields) == 2 && fields[1] == ref {
		head = fields[0]
	} else if headText != "" {
		return nil, fmt.Errorf("unexpected remote branch response")
	}
	allowedHead := v.RemoteCommit
	if allowedHead == "" {
		allowedHead = v.PreviousCommit
	}
	if (head != "" && head != code.Commit && head != allowedHead) || (head == "" && v.RemoteCommit != "") {
		return nil, fmt.Errorf("remote delivery branch changed or was deleted outside ADC; reconcile without overwriting it")
	}
	pull, err := g.findPull(ctx, args, v.Branch)
	if err != nil {
		return nil, err
	}
	if pull != nil && pull.Head.SHA != head {
		return nil, fmt.Errorf("pull head and remote branch disagree; obtain a fresh observation")
	}
	admit := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := g.ownsDelivery(v); err != nil {
			return err
		}
		_, _, _, err := g.deliverySource(ctx, args, v.SourceRevision)
		return err
	}
	if head != code.Commit {
		if head != "" {
			if _, err = g.git(ctx, dir, "fetch", "--no-tags", remote, ref+":refs/heads/previous"); err != nil {
				return nil, err
			}
			if _, err = g.git(ctx, dir, "merge-base", "--is-ancestor", head, code.Commit); err != nil {
				return nil, fmt.Errorf("delivery update must preserve the existing branch's commits; refresh the reviewed artifact")
			}
		}
		if err = admit(); err != nil {
			return nil, err
		}
		markMutation()
		if _, err = g.git(ctx, dir, "push", "--force-with-lease="+ref+":"+head, remote, code.Commit+":"+ref); err != nil {
			return nil, err
		}
	}
	v.RemoteCommit = code.Commit
	s.mu.Lock()
	err = g.ownsDelivery(v)
	if err == nil {
		err = g.saveDelivery(v)
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err = admit(); err != nil {
		return nil, err
	}
	markMutation()
	if pull == nil {
		body := args.Body + "\n\n<!-- adc:" + key + " -->"
		_, status, postErr := g.request(ctx, "POST", "/repos/"+args.Owner+"/"+args.Repository+"/pulls", map[string]any{"title": args.Title, "body": body, "head": v.Branch, "base": args.Ref, "draft": true})
		if postErr != nil && status != 422 {
			return nil, postErr
		}
		// Read back even after a validation/duplicate response. The observed
		// exact draft is the result, not a promise or the POST status alone.
		pull, err = g.findPull(ctx, args, v.Branch)
		if err != nil {
			return nil, err
		}
	} else {
		if _, _, err = g.request(ctx, "PATCH", fmt.Sprintf("/repos/%s/%s/pulls/%d", args.Owner, args.Repository, pull.Number), map[string]any{"title": args.Title, "body": args.Body + "\n\n<!-- adc:" + key + " -->"}); err != nil {
			return nil, err
		}
		pull, err = g.findPull(ctx, args, v.Branch)
		if err != nil {
			return nil, err
		}
	}
	if pull == nil || pull.Head.SHA != code.Commit || pull.HTMLURL == "" {
		return nil, fmt.Errorf("draft PR has not yet been observed at the exact reviewed commit; reconcile the same operation")
	}
	v.State = "complete"
	v.URL = pull.HTMLURL
	v.Number = pull.Number
	return map[string]any{"url": v.URL, "number": v.Number, "branch": v.Branch, "commit": v.Commit, "draft": true, "source_run": v.SourceRun, "reviewed_revision": v.SourceRevision}, nil
}

// Only this built-in adapter has deterministic branch identity, head leases and
// draft reconciliation. Generic MCP mutations retain their uncertain-outcome stop.
func (s *Store) retryableGitHubDelivery(tool GatewayTool, prior GatewayOperation) bool {
	var c Connection
	if tool.Name != "github_draft_pr" || s.Get(tool.Connection, &c) != nil || c.Transport != "github" {
		return false
	}
	if prior.State == "uncertain" || prior.State == "failed" {
		return true
	}
	created, err := time.Parse(time.RFC3339Nano, prior.Created)
	return prior.State == "in-flight" && err == nil && time.Since(created) > 90*time.Second
}
