package adc

import "fmt"

func providerName(value string) string {
	if value == "" {
		return "copilot"
	}
	return value
}
func validateProvider(value string) error {
	if p := providerName(value); p != "copilot" && p != "codex" && p != "claude" {
		return fmt.Errorf("choose Copilot, Codex or Claude")
	}
	return nil
}
func (s *Store) fundingAccount(t Assignment, provider string) (Account, error) {
	for _, id := range []string{t.Account, t.ExtraAccount} {
		if id == "" {
			continue
		}
		var a Account
		if s.Get(id, &a) != nil || a.User != t.Creator {
			return Account{}, fmt.Errorf("choose subscriptions from the assignment owner's portfolio")
		}
		if providerName(a.Provider) == providerName(provider) {
			return a, nil
		}
	}
	return Account{}, fmt.Errorf("this assignment has no approved %s subscription; select one from the same human's portfolio", providerName(provider))
}
func (s *Store) runAccount(t Assignment, r Run) (Account, error) {
	a, err := s.fundingAccount(t, r.Provider)
	if err != nil {
		return a, err
	}
	if r.Account != "" && a.ID != r.Account {
		return Account{}, fmt.Errorf("run funding changed; restore the approved subscription")
	}
	return a, nil
}
func (s *Store) bindRunAccount(t Assignment, r *Run) error {
	a, err := s.fundingAccount(t, r.Provider)
	if err == nil {
		r.Account = a.ID
		r.Provider = providerName(r.Provider)
	}
	return err
}
