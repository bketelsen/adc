package adc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type ConnectionPage struct {
	Connection                 Connection
	Revision, ArgsJSON, Scopes string
	EnvKeys, HeaderKeys        []string
}

func (w *Web) connectionPage(id string, p *Page) error {
	var c Connection
	if w.Store.Get(id, &c) != nil || c.Org != p.Org.ID {
		return errors.New("Connection unavailable")
	}
	v := ConnectionPage{Revision: connectionRevision(c), Scopes: strings.Join(c.Args, "\n")}
	args, _ := json.Marshal(c.Args)
	v.ArgsJSON = string(args)
	if c.Args == nil {
		v.ArgsJSON = "[]"
	}
	for key := range c.Env {
		v.EnvKeys = append(v.EnvKeys, key)
	}
	for key := range c.Headers {
		v.HeaderKeys = append(v.HeaderKeys, key)
	}
	sort.Strings(v.EnvKeys)
	sort.Strings(v.HeaderKeys)
	c.Env, c.Headers = nil, nil
	v.Connection = c
	p.Connection = v
	p.View, p.Title = "connection", "Edit connection"
	return nil
}

// Called under Store.mu by the authenticated, organization-scoped action route.
func (w *Web) saveConnection(r *http.Request, org string) error {
	s, f := w.Store, r.FormValue
	x := Connection{ID: ID(), Org: org, Name: strings.TrimSpace(f("name")), Transport: f("transport"), Command: strings.TrimSpace(f("command")), URL: strings.TrimSpace(f("url")), Env: map[string]string{}, Headers: map[string]string{}}
	var old Connection
	if id := f("id"); id != "" {
		if s.Get(id, &old) != nil || old.Org != org {
			return errors.New("Connection unavailable")
		}
		if f("revision") != connectionRevision(old) {
			return errors.New("Connection changed since you opened it; reload before saving")
		}
		if x.Transport != old.Transport {
			return errors.New("Create a new connection to use a different transport")
		}
		x.ID = old.ID
		for k, v := range old.Env {
			x.Env[k] = v
		}
		for k, v := range old.Headers {
			x.Headers[k] = v
		}
	}
	if x.Name == "" {
		return errors.New("Connection name is required")
	}
	switch x.Transport {
	case "github":
		x.Command, x.URL = "", ""
		x.Args = strings.Fields(f("github_scopes"))
		if len(x.Args) == 0 || len(x.Args) > 100 {
			return errors.New("List 1–100 allowed owner/repository or owner/* scopes")
		}
		for _, scope := range x.Args {
			owner, repo, ok := strings.Cut(scope, "/")
			if !ok || (repo != "*" && !validGitHubRepository(owner, repo)) || !validGitHubRepository(owner, "scope") {
				return errors.New("Use owner/repository or owner/* for each GitHub scope")
			}
		}
		if token := strings.TrimSpace(f("github_token")); token != "" {
			sealed, err := s.Seal(token)
			if err != nil {
				return err
			}
			x.Headers["GitHubToken"] = sealed
		}
		if x.Headers["GitHubToken"] == "" {
			return errors.New("Provide a GitHub access token for this connection")
		}
	case "stdio", "http":
		if x.Transport == "stdio" {
			if x.Command == "" {
				return errors.New("Command is required")
			}
			if err := json.Unmarshal([]byte(f("args")), &x.Args); err != nil {
				return errors.New("Arguments must be a JSON array")
			}
		} else {
			u, err := url.Parse(x.URL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
				return errors.New("Enter an HTTP(S) MCP URL without credentials or a fragment")
			}
		}
		for key, dst := range map[string]map[string]string{"env": x.Env, "headers": x.Headers} {
			if err := s.patchConnectionSecrets(key, f(key), dst); err != nil {
				return err
			}
		}
	default:
		return errors.New("Choose stdio or HTTP")
	}
	if old.ID != "" && connectionRevision(old) != connectionRevision(x) {
		// State plus active handles covers interrupted/recovering and stopping turns.
		w.Engine.mu.Lock()
		defer w.Engine.mu.Unlock()
		for _, run := range list[Run](s, "run", org) {
			_, active := w.Engine.active[run.ID]
			if (active || run.State == "running") && Subset([]string{x.ID}, run.Tools) {
				return errors.New("A worker is using this connection; wait for its turn to finish or pause its assignment before saving")
			}
		}
	}
	return s.Put("connection", x.Org, "", "", x.ID, x)
}

// Empty input preserves values. A JSON string sets a value; null removes it.
// Existing ciphertext is copied unchanged, so a no-op edit does not stale grants.
func (s *Store) patchConnectionSecrets(field, input string, dst map[string]string) error {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	var patch map[string]*string
	if json.Unmarshal([]byte(input), &patch) != nil || patch == nil {
		return fmt.Errorf("%s must be a JSON object with string values (or null to remove a value)", field)
	}
	for key, value := range patch {
		if key == "" || strings.ContainsAny(key, "\x00\r\n") || (field == "env" && strings.Contains(key, "=")) {
			return fmt.Errorf("%s contains an invalid name", field)
		}
		if value == nil {
			delete(dst, key)
			continue
		}
		if strings.ContainsRune(*value, 0) || (field == "headers" && strings.ContainsAny(*value, "\r\n")) {
			return fmt.Errorf("%s contains an invalid value", field)
		}
		sealed, err := s.Seal(*value)
		if err != nil {
			return err
		}
		dst[key] = sealed
	}
	return nil
}
