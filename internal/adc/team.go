package adc

import "fmt"

type CategoryDefault struct{ Org, Category, Agent string }

// PrepareTeam resolves proposal-local role references before any permanent agent
// is created. All roles are validated as a graph and committed atomically.
func PrepareTeam(s *Store, org string, proposal []Agent) ([]Agent, error) {
	if len(proposal) == 0 || len(proposal) > 30 {
		return nil, fmt.Errorf("a team proposal must contain 1–30 agents")
	}
	agents := append([]Agent(nil), proposal...)
	refs := map[string]string{}
	for i := range agents {
		a := &agents[i]
		if _, exists := refs[a.Name]; exists {
			return nil, fmt.Errorf("duplicate agent name %s", a.Name)
		}
		id := ID()
		refs[a.Name] = id
		if a.ID != "" && a.ID != a.Name {
			if _, exists := refs[a.ID]; exists {
				return nil, fmt.Errorf("ambiguous role reference %s", a.ID)
			}
			refs[a.ID] = id
		}
		a.ID = id
		a.Org = org
		if a.Authority == "" {
			a.Authority = "draft"
		}
	}
	byID := map[string]Agent{}
	for _, a := range list[Agent](s, "agent", org) {
		byID[a.ID] = a
	}
	for i := range agents {
		a := &agents[i]
		if id, ok := refs[a.ReportsTo]; ok {
			a.ReportsTo = id
		}
		byID[a.ID] = *a
	}
	for _, a := range agents {
		check := a
		check.ReportsTo = ""
		if err := validateAgent(s, check); err != nil {
			return nil, err
		}
		seen := map[string]bool{a.ID: true}
		next := a.ReportsTo
		for next != "" {
			boss, ok := byID[next]
			if !ok {
				return nil, fmt.Errorf("unknown supervisor for %s", a.Name)
			}
			if seen[next] {
				return nil, fmt.Errorf("reporting cycle involving %s", a.Name)
			}
			seen[next] = true
			next = boss.ReportsTo
		}
	}
	return agents, nil
}

func SaveAgent(s *Store, a Agent, makeDefault bool) error {
	if err := validateAgent(s, a); err != nil {
		return err
	}
	writes := []Write{{"agent", a.Org, a.ReportsTo, "", a.ID, a}}
	var old Agent
	if s.Get(a.ID, &old) == nil && old.Category != a.Category {
		key := "default:" + a.Org + ":" + old.Category
		var previous CategoryDefault
		if s.Get(key, &previous) == nil && previous.Agent == a.ID {
			writes = append(writes, Write{"retired-default", a.Org, "", "", key, previous})
		}
	}
	id := "default:" + a.Org + ":" + a.Category
	var existing CategoryDefault
	if makeDefault || s.Get(id, &existing) != nil {
		writes = append(writes, Write{"default", a.Org, "", "", id, CategoryDefault{a.Org, a.Category, a.ID}})
	}
	return s.Batch(writes...)
}

// RoleTree is a presentation of accountability, not a restriction on collaboration.
type RoleTree struct {
	Agent    Agent
	Children []RoleTree
}

func TeamTree(agents []Agent) []RoleTree {
	children := map[string][]Agent{}
	ids := map[string]bool{}
	for _, a := range agents {
		ids[a.ID] = true
	}
	for _, a := range agents {
		parent := a.ReportsTo
		if !ids[parent] {
			parent = ""
		}
		children[parent] = append(children[parent], a)
	}
	var walk func(string, map[string]bool) []RoleTree
	walk = func(parent string, seen map[string]bool) []RoleTree {
		out := []RoleTree{}
		for _, a := range children[parent] {
			if seen[a.ID] {
				continue
			}
			seen[a.ID] = true
			out = append(out, RoleTree{a, walk(a.ID, seen)})
		}
		return out
	}
	return walk("", map[string]bool{})
}
