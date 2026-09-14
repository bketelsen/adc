package adc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// This route never uses session cookies or organization membership. It exposes
// only deliberately public packets and receipt-bound fixed status fields.
func (w *Web) publicContributions(rw http.ResponseWriter, r *http.Request) {
	if !w.PublicContributions {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	var requestBody []byte
	if r.Method == "POST" {
		_ = http.NewResponseController(rw).SetReadDeadline(time.Now().Add(10 * time.Second))
		var err error
		requestBody, err = io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
		if err != nil || len(requestBody) > 2<<20 {
			http.Error(rw, "bounded JSON required", 400)
			return
		}
	}
	_ = http.NewResponseController(rw).SetWriteDeadline(time.Now().Add(15 * time.Second))
	decode := func(dst any) error {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			return fmt.Errorf("application/json required")
		}
		d := json.NewDecoder(bytes.NewReader(requestBody))
		d.DisallowUnknownFields()
		if err := d.Decode(dst); err != nil {
			return err
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return fmt.Errorf("one JSON object required")
		}
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/public/"), "/")
	if len(parts) < 2 || len(parts) > 3 || len(parts[1]) != 32 {
		http.NotFound(rw, r)
		return
	}
	s := w.Store
	s.mu.Lock()
	var response any
	status := http.StatusOK
	defer func() { s.mu.Unlock(); rw.WriteHeader(status); _ = json.NewEncoder(rw).Encode(response) }()
	fail := func(code int, message string) { status = code; response = map[string]string{"Error": message} }
	send := func(v any) { response = v }
	if parts[0] == "queues" && len(parts) == 2 && r.Method == "GET" {
		var q ContributionQueue
		if s.Get(parts[1], &q) != nil || !s.contributionQueueAvailable(q) {
			fail(404, "queue unavailable")
			return
		}
		items := []PublicPacket{}
		for _, p := range list[ContributionPacket](s, "contribution-packet", q.Org) {
			if p.Queue != q.ID || p.State != "open" {
				continue
			}
			if _, _, err := s.publicPacket(p.ID); err != nil {
				continue
			}
			if p.ClaimHash != "" && timeAfter(p.ClaimUntil, time.Now()) {
				continue
			}
			pub := p.Public
			pub.Files = nil
			items = append(items, pub)
		}
		send(map[string]any{"Protocol": 1, "Scope": q.Scope, "Work": items, "Instructions": "GET /public/packets/{ID}; POST /public/packets/{ID}/claim with {}. Keep the returned Receipt. Submit file replacements with its Bearer authorization. Never send credentials."})
		return
	}
	receipt := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if parts[0] == "submissions" && len(parts) == 2 && r.Method == "GET" {
		var c Contribution
		if s.Get(parts[1], &c) != nil || !receiptMatches(c.ReceiptHash, receipt) {
			fail(404, "submission unavailable")
			return
		}
		send(contributionPublicStatus(c))
		return
	}
	if parts[0] != "packets" {
		fail(404, "operation unavailable")
		return
	}
	p, _, err := s.publicPacket(parts[1])
	if err != nil {
		fail(404, "work unavailable")
		return
	}
	if len(parts) == 2 && r.Method == "GET" {
		send(p.Public)
		return
	}
	if len(parts) != 3 || r.Method != "POST" {
		fail(405, "unsupported operation")
		return
	}
	switch parts[2] {
	case "claim":
		var in struct{}
		if decode(&in) != nil {
			fail(400, "valid JSON required")
			return
		}
		v, err := s.claimContribution(p.ID, time.Now())
		if err != nil {
			fail(409, err.Error())
			return
		}
		send(v)
	case "release":
		var in struct{}
		if decode(&in) != nil {
			fail(400, "valid JSON required")
			return
		}
		if !receiptMatches(p.ClaimHash, receipt) || p.State != "open" {
			fail(409, "claim unavailable")
			return
		}
		p.ClaimHash, p.ClaimUntil = "", ""
		p.ClaimReady = time.Now().Add(5 * time.Second).UTC().Format(time.RFC3339Nano)
		if s.Put("contribution-packet", p.Org, p.Task, p.State, p.ID, p) != nil {
			fail(500, "could not release claim")
			return
		}
		send(map[string]string{"State": "released"})
	case "submit":
		var in contributionSubmission
		if decode(&in) != nil {
			fail(400, "valid bounded JSON required")
			return
		}
		c, err := s.submitContribution(p.ID, receipt, in, time.Now())
		if err != nil {
			fail(409, err.Error())
			return
		}
		send(contributionPublicStatus(c))
	default:
		fail(404, "operation unavailable")
	}
}

type ContributionPage struct {
	Enabled bool
	Queues  []ContributionQueue
	Packets []ContributionPacket
	Results []Contribution
	More    bool
}

func (w *Web) contributionPage(p *Page) {
	p.View, p.Title = "contributions", "Contributed work"
	p.Contributions = ContributionPage{Enabled: w.PublicContributions, Queues: list[ContributionQueue](w.Store, "contribution-queue", p.Org.ID), Packets: list[ContributionPacket](w.Store, "contribution-packet", p.Org.ID), Results: list[Contribution](w.Store, "contribution", p.Org.ID)}
	if len(p.Contributions.Packets) > 30 {
		p.Contributions.Packets = p.Contributions.Packets[:30]
		p.Contributions.More = true
	}
	if len(p.Contributions.Results) > 30 {
		p.Contributions.Results = p.Contributions.Results[:30]
		p.Contributions.More = true
	}
}
func (w *Web) contributionAction(r *http.Request, p Page) error {
	s := w.Store
	if r.FormValue("action") == "close-packet" {
		var packet ContributionPacket
		if s.Get(r.FormValue("id"), &packet) != nil || packet.Org != p.Org.ID {
			return fmt.Errorf("packet unavailable")
		}
		packet.State = "closed"
		return s.Put("contribution-packet", packet.Org, packet.Task, packet.State, packet.ID, packet)
	}
	if r.FormValue("action") == "pause" {
		var q ContributionQueue
		if s.Get(r.FormValue("id"), &q) != nil || q.Org != p.Org.ID {
			return fmt.Errorf("queue unavailable")
		}
		q.State = "paused"
		return s.Put("contribution-queue", q.Org, q.Area, q.State, q.ID, q)
	}
	if r.FormValue("action") != "create" {
		return fmt.Errorf("unsupported contribution action")
	}
	if len(list[ContributionQueue](s, "contribution-queue", p.Org.ID)) >= 8 {
		return fmt.Errorf("at most eight contribution queues per organization")
	}
	var area Area
	var reviewer Agent
	var owner Agent
	var account Account
	if s.Get(r.FormValue("area"), &area) != nil || area.Org != p.Org.ID || s.Get(area.Owner, &owner) != nil || s.Get(r.FormValue("reviewer"), &reviewer) != nil || reviewer.Org != p.Org.ID || s.Get(r.FormValue("account"), &account) != nil || account.User != p.User.ID || providerName(account.Provider) != providerName(reviewer.Provider) {
		return fmt.Errorf("choose an area, independent reviewer and matching subscription from your portfolio")
	}
	if providerName(reviewer.Provider) == "selfhosted" || CanReview(owner.Model, reviewer.Model) != nil {
		return fmt.Errorf("admission needs an internally owned reviewer from a different family than the owner; local provider qualification is deferred")
	}
	max, err := strconv.Atoi(r.FormValue("budget"))
	if err != nil || max < 1 || max > 20 {
		return fmt.Errorf("choose 1–20 internal evaluations")
	}
	q := ContributionQueue{ID: ID(), Org: p.Org.ID, Area: area.ID, Owner: area.Owner, Creator: p.User.ID, Account: account.ID, Reviewer: reviewer.ID, Provider: providerName(reviewer.Provider), Model: reviewer.Model, Scope: r.FormValue("scope"), Source: r.FormValue("source"), State: "active", MaxReviews: max, MaxPackets: 20, Created: now()}
	if !boundedText(q.Scope, 3000) || !publicSource(q.Source) || r.FormValue("disclosure") != "yes" {
		return fmt.Errorf("approve a specific public scope and credential-free HTTPS source")
	}
	if err := s.checkOwnershipText(q.Org, q.Scope, q.Source); err != nil {
		return err
	}
	return s.Put("contribution-queue", q.Org, q.Area, q.State, q.ID, q)
}
