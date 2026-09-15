package adc

import (
	"fmt"
	"sort"
	"strings"
)

type PlanGraphNode struct {
	Key, Title, Owner, State, Reason, Dependencies string
	Lines                                          []string
	X, Y                                           int
}
type PlanGraphEdge struct{ From, To, Path string }
type PlanGraph struct {
	Nodes                                                               []PlanGraphNode
	Edges                                                               []PlanGraphEdge
	Width, Height, Complete, Active, Blocked, Waiting, Stopped, Omitted int
}

// Layout is derived from prerequisite edges, not the order of plan rows. The
// durable execution graph remains authoritative; this is a read-only projection.
func planGraph(p ExecutionPlan) PlanGraph {
	g := PlanGraph{Width: 320, Height: 180}
	byKey := map[string]PlanStep{}
	for _, s := range p.Steps {
		byKey[s.Key] = s
	}
	ranks := map[string]int{}
	visiting := map[string]bool{}
	var rank func(string) int
	rank = func(key string) int {
		if n, ok := ranks[key]; ok {
			return n
		}
		if visiting[key] {
			return 0
		} // A corrupt historical graph must not recurse forever.
		visiting[key] = true
		n := 0
		for _, dep := range byKey[key].DependsOn {
			if _, ok := byKey[dep]; ok {
				if x := rank(dep) + 1; x > n {
					n = x
				}
			}
		}
		visiting[key] = false
		ranks[key] = n
		return n
	}
	columns := map[int][]PlanStep{}
	maxRank := 0
	for _, s := range p.Steps {
		n := rank(s.Key)
		columns[n] = append(columns[n], s)
		if n > maxRank {
			maxRank = n
		}
	}
	positions := map[string]PlanGraphNode{}
	for col := 0; col <= maxRank; col++ {
		steps := columns[col]
		center := func(s PlanStep) float64 {
			sum, n := 0, 0
			for _, d := range s.DependsOn {
				if v, ok := positions[d]; ok {
					sum += v.Y
					n++
				}
			}
			if n == 0 {
				return 0
			}
			return float64(sum) / float64(n)
		}
		sort.SliceStable(steps, func(i, j int) bool { return center(steps[i]) < center(steps[j]) })
		for row, s := range steps {
			state := s.State
			if p.State == "draft" {
				state = "draft"
			}
			switch state {
			case "omitted":
				g.Omitted++
			case "complete":
				g.Complete++
			case "running", "review", "queued":
				g.Active++
			case "blocked":
				g.Blocked++
			case "cancelled", "paused", "failed":
				g.Stopped++
			default:
				g.Waiting++
			}
			n := PlanGraphNode{Key: s.Key, Title: s.Title, Owner: s.Worker.Name, State: state, Reason: s.Reason, Dependencies: strings.Join(s.DependsOn, " "), X: 28 + col*310, Y: 28 + row*154, Lines: graphLines(s.Title)}
			positions[s.Key] = n
			g.Nodes = append(g.Nodes, n)
			if n.Y+156 > g.Height {
				g.Height = n.Y + 156
			}
		}
	}
	g.Width = 56 + maxRank*310 + 240
	for _, s := range p.Steps {
		to := positions[s.Key]
		for _, dep := range s.DependsOn {
			from, ok := positions[dep]
			if !ok {
				continue
			}
			x1, y1, x2, y2 := from.X+240, from.Y+60, to.X, to.Y+60
			mid := (x1 + x2) / 2
			g.Edges = append(g.Edges, PlanGraphEdge{dep, s.Key, fmt.Sprintf("M %d %d C %d %d, %d %d, %d %d", x1, y1, mid, y1, mid, y2, x2, y2)})
		}
	}
	return g
}

func graphLines(title string) []string {
	r := []rune(title)
	var lines []string
	for len(r) > 0 && len(lines) < 2 {
		n := len(r)
		if n > 29 {
			n = 29
			for i := 29; i > 16; i-- {
				if r[i] == ' ' {
					n = i
					break
				}
			}
		}
		line := strings.TrimSpace(string(r[:n]))
		r = []rune(strings.TrimSpace(string(r[n:])))
		if len(lines) == 1 && len(r) > 0 {
			line += "…"
		}
		lines = append(lines, line)
	}
	return lines
}
