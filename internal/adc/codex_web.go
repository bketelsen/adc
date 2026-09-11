package adc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type CodexAccountPage struct {
	Account Account
	Status  codexAccountStatus
	Login   codexLogin
	Error   string
}

func (w *Web) codexAccountPage(r *http.Request, p *Page) error {
	var a Account
	if w.Store.Get(r.URL.Query().Get("id"), &a) != nil || a.User != p.User.ID || providerName(a.Provider) != "codex" {
		return fmt.Errorf("account unavailable")
	}
	p.View = "codex-account"
	p.Title = "Codex connection"
	p.Codex.Account = a
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, err := w.Engine.codexClient(ctx, a)
	if err == nil {
		err = c.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &p.Codex.Status)
	}
	if err != nil {
		p.Codex.Error = err.Error()
	}
	w.Engine.clientMu.Lock()
	p.Codex.Login = w.Engine.codexLogins[a.ID]
	w.Engine.clientMu.Unlock()
	if p.Codex.Status.Account != nil {
		p.Codex.Login = codexLogin{}
		p.Models, err = w.Engine.codexModels(ctx, a)
		if err != nil {
			p.Codex.Error = err.Error()
		}
	}
	return nil
}
func (w *Web) codexLoginAction(r *http.Request, p Page) error {
	var a Account
	if w.Store.Get(r.FormValue("id"), &a) != nil || a.User != p.User.ID || providerName(a.Provider) != "codex" {
		return fmt.Errorf("account unavailable")
	}
	if w.Engine.accountActive(a.ID) {
		return fmt.Errorf("pause affected work and wait for active Codex runs before changing sign-in")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c, err := w.Engine.codexClient(ctx, a)
	if err != nil {
		return err
	}
	switch r.FormValue("action") {
	case "login":
		var login codexLogin
		if err = c.Call(ctx, "account/login/start", map[string]string{"type": "chatgptDeviceCode"}, &login); err != nil {
			return err
		}
		u, err := url.Parse(login.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() != "auth.openai.com" || login.Code == "" {
			return fmt.Errorf("Codex returned an unexpected sign-in destination")
		}
		w.Engine.clientMu.Lock()
		w.Engine.codexLogins[a.ID] = login
		w.Engine.clientMu.Unlock()
	case "logout":
		if err = c.Call(ctx, "account/logout", nil, nil); err != nil {
			return err
		}
		w.Engine.clientMu.Lock()
		delete(w.Engine.codexLogins, a.ID)
		w.Engine.clientMu.Unlock()
	default:
		return fmt.Errorf("unknown sign-in action")
	}
	r.Form.Set("return", "/codex-account?org="+p.Org.ID+"&id="+a.ID)
	return nil
}
