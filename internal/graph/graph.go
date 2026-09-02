// Package graph computes the load order of active modules from their
// requires (hard) and after (soft) edges.
package graph

import (
	"fmt"
	"sort"

	"github.com/JtheGunner/omnishell/internal/module"
)

// MissingRequireError is returned when a required module is not active.
type MissingRequireError struct {
	Module   string
	Requires string
}

func (e MissingRequireError) Error() string {
	return fmt.Sprintf("module %q requires %q, which is not enabled", e.Module, e.Requires)
}

// CycleError is returned when the dependency edges contain a cycle.
type CycleError struct {
	Cycle []string
}

func (e CycleError) Error() string {
	return "dependency cycle among modules: " + joinArrow(e.Cycle)
}

func joinArrow(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += " -> "
		}
		out += id
	}
	return out
}

// Order returns the active module ids in load order.
func Order(active map[string]module.Manifest) ([]string, error) {
	deps := make(map[string]map[string]bool, len(active)) // node -> set of prerequisites
	for id := range active {
		deps[id] = map[string]bool{}
	}
	for id, mf := range active {
		for _, r := range mf.Requires {
			if _, ok := active[r]; !ok {
				return nil, MissingRequireError{Module: id, Requires: r}
			}
			deps[id][r] = true
		}
		for _, a := range mf.After {
			if _, ok := active[a]; ok {
				deps[id][a] = true
			}
		}
	}

	var order []string
	done := map[string]bool{}
	for len(order) < len(active) {
		ready := make([]string, 0)
		for id := range active {
			if done[id] {
				continue
			}
			ok := true
			for pre := range deps[id] {
				if !done[pre] {
					ok = false
					break
				}
			}
			if ok {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			remaining := make([]string, 0)
			for id := range active {
				if !done[id] {
					remaining = append(remaining, id)
				}
			}
			sort.Strings(remaining)
			return nil, CycleError{Cycle: remaining}
		}
		sort.Strings(ready)
		order = append(order, ready[0])
		done[ready[0]] = true
	}
	return order, nil
}
