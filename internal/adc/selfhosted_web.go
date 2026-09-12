package adc

import (
	"fmt"
	"net/http"
)

type SelfhostedAccountPage struct {
	Account Account
	Error   string
	HasKey  bool
}

func (w *Web) selfhostedAccountPage(r *http.Request, p *Page) error {
	var a Account
	if w.Store.Get(r.URL.Query().Get("id"), &a) != nil || a.User != p.User.ID || providerName(a.Provider) != "selfhosted" {
		return fmt.Errorf("account unavailable")
	}
	p.View = "selfhosted-account"
	p.Title = "Self-hosted connection"
	p.Selfhosted.HasKey = a.Secret != ""
	models, err := w.Engine.selfhostedModels(r.Context(), a)
	if err != nil {
		p.Selfhosted.Error = err.Error()
	} else {
		p.Models = models
	}
	a.Secret = ""
	p.Selfhosted.Account = a
	return nil
}
