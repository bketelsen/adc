package adc

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func formRequest(path string, values url.Values) *http.Request {
	req := httptest.NewRequest("POST", "http://fixture"+path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = req.ParseForm()
	return req
}
func TestPortfolioEditPreservesSecretAndRejectsAnotherMember(t *testing.T) {
	s, e, _, _ := fixture(t)
	w := NewWeb(s, e, false)
	secret, err := s.Seal("private-fixture-token")
	must(t, err)
	a := Account{ID: "account", User: "owner", Name: "Original", Secret: secret, Limit: 2}
	must(t, s.Put("account", "", a.User, "", a.ID, a))
	req := formRequest("/accounts", url.Values{"id": {"account"}, "name": {"Updated"}, "limit": {"4"}})
	if w.saveAccount(req, User{ID: "someone-else"}) == nil {
		t.Fatal("another member edited the portfolio")
	}
	must(t, w.saveAccount(req, User{ID: "owner"}))
	must(t, s.Get(a.ID, &a))
	value, err := s.Unseal(a.Secret)
	must(t, err)
	if a.Limit != 4 || a.Name != "Updated" || value != "private-fixture-token" {
		t.Fatal("account edit lost settings or the saved credential")
	}
}
func TestLocalIdentityCannotBeBoundByAnotherMember(t *testing.T) {
	s, e, _, _ := fixture(t)
	w := NewWeb(s, e, false)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Owner','owner','fixture'); INSERT INTO users VALUES('other','Other','other','fixture')`)
	must(t, err)
	req := formRequest("/accounts", url.Values{"name": {"Local identity"}, "limit": {"2"}, "local": {"on"}})
	if w.saveAccount(req, User{ID: "other"}) == nil {
		t.Fatal("another member acquired the installation identity")
	}
}
func TestPasswordChangeChecksCurrentPasswordAndRevokesOtherSessions(t *testing.T) {
	s, e, _, _ := fixture(t)
	w := NewWeb(s, e, false)
	hash, err := bcrypt.GenerateFromPassword([]byte("old-fixture-password"), bcrypt.MinCost)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','Owner','owner',?)`, hash)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO sessions VALUES(?,'owner','2099-01-01'),(?,'owner','2099-01-01')`, digest("current-cookie"), digest("other-cookie"))
	must(t, err)
	values := url.Values{"current_password": {"incorrect"}, "new_password": {"new-fixture-password"}, "confirm_password": {"new-fixture-password"}}
	req := formRequest("/password", values)
	req.AddCookie(&http.Cookie{Name: "adc_session", Value: "current-cookie"})
	if w.changePassword(req, User{ID: "owner"}) == nil {
		t.Fatal("current password was not checked")
	}
	req.Form.Set("current_password", "old-fixture-password")
	must(t, w.changePassword(req, User{ID: "owner"}))
	must(t, s.db.QueryRow(`SELECT password FROM users WHERE id='owner'`).Scan(&hash))
	must(t, bcrypt.CompareHashAndPassword(hash, []byte("new-fixture-password")))
	var count int
	must(t, s.db.QueryRow(`SELECT count(*) FROM sessions WHERE user_id='owner'`).Scan(&count))
	if count != 1 {
		t.Fatal("other sessions were not revoked")
	}
	u, _ := w.user(req)
	if u.ID != "owner" {
		t.Fatal("current session unnecessarily lost")
	}
}
