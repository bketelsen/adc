package adc

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

type ClaudeAccountPage struct {
	Account             Account
	Status              codexAccountStatus
	LoginCommand, Error string
}

func shellArgument(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (w *Web) claudeAccountPage(r *http.Request, p *Page) error {
	var a Account
	if w.Store.Get(r.URL.Query().Get("id"), &a) != nil || a.User != p.User.ID || a.Provider != "claude" {
		return fmt.Errorf("account unavailable")
	}
	p.View, p.Title, p.Claude.Account = "claude-account", "Claude connection", a
	_, _, cli, err := claudePaths()
	if err != nil {
		p.Claude.Error = err.Error()
		return nil
	}
	dir, err := filepath.Abs(filepath.Join(w.Store.Dir, "providers", "claude", a.ID))
	if err != nil {
		return err
	}
	p.Claude.LoginCommand = "env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u CLAUDE_CODE_OAUTH_TOKEN CLAUDE_CONFIG_DIR=" + shellArgument(dir) + " " + shellArgument(cli) + " auth login --claudeai"
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c, err := w.Engine.claudeClient(ctx, a)
	if err == nil {
		err = c.Call(ctx, "account/read", nil, &p.Claude.Status)
	}
	if err == nil && p.Claude.Status.Account != nil {
		p.Models, err = w.Engine.claudeModels(ctx, a)
	}
	if err != nil {
		p.Claude.Error = err.Error()
	}
	return nil
}
func (w *Web) claudeLogout(r *http.Request, p Page) error {
	var a Account
	if w.Store.Get(r.FormValue("id"), &a) != nil || a.User != p.User.ID || a.Provider != "claude" {
		return fmt.Errorf("account unavailable")
	}
	if w.Engine.accountActive(a.ID) {
		return fmt.Errorf("pause affected work and wait for active Claude runs before changing sign-in")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c, err := w.Engine.claudeClient(ctx, a)
	if err != nil {
		return err
	}
	if err = c.Call(ctx, "account/logout", nil, nil); err != nil {
		return err
	}
	r.Form.Set("return", "/claude-account?org="+p.Org.ID+"&id="+a.ID)
	return nil
}
