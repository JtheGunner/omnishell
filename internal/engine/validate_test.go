package engine_test

import (
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func validateEngine(t *testing.T) engine.Engine {
	return testEngine(t, &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}})
}

func cfgWithModules(m map[string]config.ModuleConfig) config.Config {
	return config.Config{
		Omnishell: config.OmnishellSection{Version: 1},
		Modules:   m,
	}
}

func TestValidateOKConfig(t *testing.T) {
	e := validateEngine(t)
	cfg := cfgWithModules(map[string]config.ModuleConfig{
		"completion": {Enabled: true},
		"fzf":        {Enabled: true},
	})
	if rep := e.Validate(cfg); !rep.OK() {
		t.Fatalf("expected no problems, got %+v", rep.Problems)
	}
}

func TestValidateUnknownModule(t *testing.T) {
	e := validateEngine(t)
	rep := e.Validate(cfgWithModules(map[string]config.ModuleConfig{"nope": {Enabled: true}}))
	if len(rep.Problems) != 1 || rep.Problems[0].Code != "unknown-module" || rep.Problems[0].Module != "nope" {
		t.Fatalf("want one unknown-module problem for nope, got %+v", rep.Problems)
	}
}

func TestValidateIgnoresDisabledModules(t *testing.T) {
	e := validateEngine(t)
	if rep := e.Validate(cfgWithModules(map[string]config.ModuleConfig{"nope": {Enabled: false}})); !rep.OK() {
		t.Fatalf("a disabled unknown module is not a problem, got %+v", rep.Problems)
	}
}

func TestValidateBadOptionValue(t *testing.T) {
	e := validateEngine(t)
	cfg := cfgWithModules(map[string]config.ModuleConfig{
		"fzf": {Enabled: true, Options: map[string]any{"bogus_key": 1}},
	})
	rep := e.Validate(cfg)
	if len(rep.Problems) != 1 || rep.Problems[0].Code != "bad-option" || rep.Problems[0].Module != "fzf" {
		t.Fatalf("want one bad-option problem for fzf, got %+v", rep.Problems)
	}
}

func TestValidateMissingRequire(t *testing.T) {
	e := validateEngine(t)
	rep := e.Validate(cfgWithModules(map[string]config.ModuleConfig{
		"needy": {Enabled: true}, // requires completion, which is not enabled
	}))
	if len(rep.Problems) != 1 || rep.Problems[0].Code != "missing-require" {
		t.Fatalf("want missing-require, got %+v", rep.Problems)
	}
}

func TestValidateConflict(t *testing.T) {
	e := validateEngine(t)
	rep := e.Validate(cfgWithModules(map[string]config.ModuleConfig{
		"fzf":        {Enabled: true},
		"conflictor": {Enabled: true}, // conflicts = ["fzf"]
	}))
	found := false
	for _, p := range rep.Problems {
		if p.Code == "conflict" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a conflict problem, got %+v", rep.Problems)
	}
}
