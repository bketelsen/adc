package adc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func selfhostedBase(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("use an HTTP(S) API base URL without embedded credentials, query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	if u.Path == "" {
		u.Path = "/v1"
	}
	if strings.Contains(u.Path, "..") || strings.ContainsAny(u.Path, "\r\n") {
		return "", fmt.Errorf("invalid API base path")
	}
	return u.String(), nil
}
func (s *Store) selfhostedRequest(ctx context.Context, a Account, path string, body any) (json.RawMessage, error) {
	if providerName(a.Provider) != "selfhosted" {
		return nil, fmt.Errorf("self-hosted account required")
	}
	base, err := selfhostedBase(a.BaseURL)
	if err != nil {
		return nil, err
	}
	token, err := s.Unseal(a.Secret)
	if err != nil {
		return nil, fmt.Errorf("self-hosted API key unavailable")
	}
	method := "GET"
	var input io.Reader
	if body != nil {
		method = "POST"
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("provider redirects are not followed; configure its final API base")
	}}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("self-hosted request failed: %s", (Redactor{Values: []string{token}}).Text(err.Error()))
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (16<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("self-hosted response interrupted")
	}
	if len(raw) > 16<<20 {
		return nil, fmt.Errorf("self-hosted response exceeds 16 MiB")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var reported struct{ Error json.RawMessage }
		detail := ""
		if json.Unmarshal(raw, &reported) == nil && len(reported.Error) > 0 {
			var message struct{ Message string }
			if json.Unmarshal(reported.Error, &message) == nil {
				detail = message.Message
			} else {
				_ = json.Unmarshal(reported.Error, &detail)
			}
		}
		detail = clipped((Redactor{Values: []string{token}}).Text(detail), 1000)
		if detail != "" {
			return nil, fmt.Errorf("self-hosted provider returned HTTP %d: %s", res.StatusCode, detail)
		}
		return nil, fmt.Errorf("self-hosted provider returned HTTP %d; check its availability, API base and account configuration", res.StatusCode)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("self-hosted endpoint did not return JSON; check the API base URL")
	}
	return raw, nil
}
func (e *Engine) selfhostedModels(ctx context.Context, a Account) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	raw, err := e.Store.selfhostedRequest(ctx, a, "/models", nil)
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Data []struct {
			ID     string
			Labels []string
		}
	}
	if json.Unmarshal(raw, &catalog) != nil || catalog.Data == nil {
		return nil, fmt.Errorf("provider returned an invalid model catalog")
	}
	out := []Model{}
	seen := map[string]bool{}
	for _, m := range catalog.Data {
		if len(m.ID) > 200 || strings.TrimSpace(m.ID) == "" || strings.ContainsAny(m.ID, "\r\n") || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		family := Family(m.ID)
		if family == "" {
			continue
		}
		nonChat := false
		if !Subset([]string{"chat"}, m.Labels) {
			for _, label := range m.Labels {
				switch label {
				case "embeddings", "embedding", "reranking", "reranker", "transcription", "tts", "image", "classification", "realtime-transcription":
					nonChat = true
				}
			}
		}
		if nonChat {
			continue
		}
		out = append(out, Model{ID: m.ID, Name: m.ID, Family: family})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no recognized model families available for agent work; use a family-identifying model ID and a chat model with function calling")
	}
	return out, nil
}

func validateSelfhostedExecution(t Assignment, provider string) error {
	if providerName(provider) == "selfhosted" && t.Kind != "proposal" && t.Execution != "protected" {
		return fmt.Errorf("self-hosted agents currently require protected assignments for workspace and mediated MCP tools; select protected execution for this work")
	}
	return nil
}
