package adc

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Called with the store's mutation lock held. A member can edit only their own
// portfolio; credential replacement waits until its active turns have yielded.
func (w *Web) saveAccount(r *http.Request, user User) error {
	s := w.Store
	f := r.FormValue
	limit, _ := strconv.Atoi(f("limit"))
	if limit < 1 || limit > 32 {
		return errors.New("Choose between 1 and 32 concurrent runs")
	}
	var old Account
	a := Account{ID: ID(), User: user.ID, Name: strings.TrimSpace(f("name")), Provider: "copilot", Local: f("local") == "on", Limit: limit}
	if a.Name == "" {
		return errors.New("Account label is required")
	}
	if id := f("id"); id != "" {
		if s.Get(id, &old) != nil || old.User != user.ID {
			return errors.New("Account unavailable")
		}
		a.ID = id
		a.Secret = old.Secret
	}
	provider := f("provider")
	if provider == "" && old.ID != "" {
		provider = old.Provider
	}
	a.Provider = providerName(provider)
	if err := validateProvider(a.Provider); err != nil {
		return err
	}
	if old.ID != "" && providerName(old.Provider) != a.Provider {
		return errors.New("create a separate account when changing providers")
	}
	if a.Provider == "selfhosted" {
		if a.Local {
			return errors.New("self-hosted connections use an API base and optional API key, not a local CLI identity")
		}
		base := f("base_url")
		if base == "" && old.ID != "" {
			base = old.BaseURL
		}
		var err error
		a.BaseURL, err = selfhostedBase(base)
		if err != nil {
			return err
		}
		changed := old.BaseURL != a.BaseURL || f("token") != "" || f("clear_token") == "on"
		if old.ID != "" && changed && w.Engine.accountActive(a.ID) {
			return errors.New("pause this account’s assignments and wait for active turns before replacing its endpoint or API key")
		}
		if f("clear_token") == "on" {
			a.Secret = ""
		} else if f("token") != "" {
			a.Secret, err = s.Seal(f("token"))
			if err != nil {
				return err
			}
		}
		if err := s.Put("account", "", a.User, "", a.ID, a); err != nil {
			return err
		}
		r.Form.Set("return", "/selfhosted-account?org="+r.URL.Query().Get("org")+"&id="+a.ID)
		return nil
	}
	if a.Provider == "codex" || a.Provider == "claude" {
		if a.Local || f("token") != "" {
			return errors.New("This provider uses its own sign-in; no token or local identity import")
		}
		a.Secret = ""
		if err := s.Put("account", "", a.User, "", a.ID, a); err != nil {
			return err
		}
		r.Form.Set("return", "/"+a.Provider+"-account?org="+r.URL.Query().Get("org")+"&id="+a.ID)
		return nil
	}
	if a.Local {
		var first string
		_ = s.db.QueryRow(`SELECT id FROM users ORDER BY rowid LIMIT 1`).Scan(&first)
		if first != user.ID {
			return errors.New("Only the installation owner can bind the local signed-in identity; connect your own token")
		}
		for _, existing := range list[Account](s, "account", "") {
			if existing.Local && providerName(existing.Provider) == a.Provider && existing.ID != a.ID {
				return errors.New("The local account is already connected")
			}
		}
		if f("token") != "" {
			return errors.New("Choose either the local identity or an access token")
		}
	}
	authChanged := old.ID == "" || old.Local != a.Local || f("token") != ""
	if old.ID != "" && authChanged && w.Engine.accountActive(a.ID) {
		return errors.New("Pause this account's assignments and wait for active turns to stop before replacing its credentials")
	}
	var err error
	if a.Local {
		a.Secret = ""
	} else if f("token") != "" {
		a.Secret, err = s.Seal(f("token"))
		if err != nil {
			return err
		}
	}
	if !a.Local {
		token, err := s.Unseal(a.Secret)
		if err != nil {
			return err
		}
		if token == "" {
			return errors.New("Provide a token or explicitly connect the local signed-in account")
		}
	}
	if err = s.Put("account", "", a.User, "", a.ID, a); err != nil {
		return err
	}
	if old.ID != "" && authChanged {
		w.Engine.resetClient(a.ID)
	}
	return nil
}
func (e *Engine) accountActive(account string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id := range e.active {
		var run Run
		var task Assignment
		if e.Store.Get(id, &run) == nil && e.Store.Get(run.Task, &task) == nil && (run.Account == account || (run.Account == "" && task.Account == account)) {
			return true
		}
	}
	return false
}
func (e *Engine) resetClient(account string) {
	e.clientMu.Lock()
	defer e.clientMu.Unlock()
	for _, key := range []string{account, account + ":protected"} {
		if c := e.clients[key]; c != nil {
			_ = c.Stop()
			delete(e.clients, key)
		}
	}
}
func (w *Web) changePassword(r *http.Request, user User) error {
	if r.FormValue("new_password") != r.FormValue("confirm_password") {
		return errors.New("New passwords do not match")
	}
	if len(r.FormValue("new_password")) < 12 {
		return errors.New("Use at least 12 characters for the new password")
	}
	var old []byte
	if w.Store.db.QueryRow(`SELECT password FROM users WHERE id=?`, user.ID).Scan(&old) != nil || bcrypt.CompareHashAndPassword(old, []byte(r.FormValue("current_password"))) != nil {
		return errors.New("Current password is incorrect")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(r.FormValue("new_password")), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := w.Store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE users SET password=? WHERE id=?`, hash, user.ID); err != nil {
		return err
	}
	keep := ""
	if c, err := r.Cookie("adc_session"); err == nil {
		keep = digest(c.Value)
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE user_id=? AND token<>?`, user.ID, keep); err != nil {
		return err
	}
	return tx.Commit()
}
func (w *Web) members(org string) []User {
	rows, err := w.Store.db.Query(`SELECT u.id,u.name,u.username FROM users u JOIN memberships m ON m.user_id=u.id WHERE m.org=? ORDER BY u.name`, org)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if rows.Scan(&u.ID, &u.Name, &u.Username) == nil {
			out = append(out, u)
		}
	}
	return out
}
