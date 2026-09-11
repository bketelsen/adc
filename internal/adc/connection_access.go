package adc

import "fmt"

// Connection names and IDs are enough to plan access. Never expose launch
// configuration, addresses, headers or sealed credentials in model context.
type ConnectionAccess struct {
	ID, Name string
	Granted  bool
}

func connectionAccess(s *Store, r Run) []ConnectionAccess {
	out := []ConnectionAccess{}
	for _, c := range list[Connection](s, "connection", r.Org) {
		out = append(out, ConnectionAccess{c.ID, c.Name, Subset([]string{c.ID}, r.Tools)})
	}
	return out
}

func requireConnections(s *Store, org string, required, granted []string) error {
	for _, id := range required {
		var c Connection
		if s.Get(id, &c) != nil || c.Org != org {
			return fmt.Errorf("required connection %s is not configured in this organization", id)
		}
		if !Subset([]string{id}, granted) {
			return fmt.Errorf("required connection %s (%s) is not granted to this run; select an authorized worker or report the missing grant with adc_blocked; delegation cannot expand access", c.Name, c.ID)
		}
	}
	return nil
}
