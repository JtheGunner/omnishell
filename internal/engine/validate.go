// Package engine: Validate checks a config against the module registry's
// option schemas and dependency rules without computing a plan or touching the
// system — a CI-friendly "is this config.toml still valid" gate.
package engine

import (
	"errors"
	"sort"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/graph"
	"github.com/JtheGunner/omnishell/internal/module"
)

// ValidateProblem is one issue found in a config: a module id plus a short
// code and a human message. Module is empty for whole-config problems.
type ValidateProblem struct {
	Module  string `json:"module"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidateReport is the full set of problems from one Validate call.
type ValidateReport struct {
	Problems []ValidateProblem `json:"problems"`
}

// OK reports whether the config passed validation.
func (r ValidateReport) OK() bool { return len(r.Problems) == 0 }

// Validate checks every enabled module in cfg against the registry: the id
// must resolve, its option values must satisfy the schema, and the set of
// enabled modules must form a valid dependency graph (no missing `requires`,
// no `conflicts` pair, no cycle). It uses only e.Registry — no platform,
// package manager, lockfile or filesystem.
func (e Engine) Validate(cfg config.Config) ValidateReport {
	var rep ValidateReport
	add := func(mod, code, msg string) {
		rep.Problems = append(rep.Problems, ValidateProblem{Module: mod, Code: code, Message: msg})
	}

	ids := make([]string, 0, len(cfg.Modules))
	for id, mc := range cfg.Modules {
		if mc.Enabled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	active := map[string]module.Manifest{}
	for _, id := range ids {
		mod, ok := e.Registry.Get(id)
		if !ok {
			add(id, "unknown-module", "no registered module provides this id")
			continue
		}
		if _, err := module.ValidateOptions(mod.Manifest.Options, cfg.Modules[id].Options); err != nil {
			add(id, "bad-option", err.Error())
		}
		active[id] = mod.Manifest
	}

	// graph.Order surfaces the first missing-require / conflict / cycle it
	// finds; report that one.
	if _, err := graph.Order(active); err != nil {
		var mre graph.MissingRequireError
		var cfe graph.ConflictError
		var ce graph.CycleError
		switch {
		case errors.As(err, &mre):
			add(mre.Module, "missing-require", err.Error())
		case errors.As(err, &cfe):
			add(cfe.Module, "conflict", err.Error())
		case errors.As(err, &ce):
			add("", "cycle", err.Error())
		default:
			add("", "graph", err.Error())
		}
	}

	return rep
}
