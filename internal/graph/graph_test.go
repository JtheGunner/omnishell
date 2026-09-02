package graph_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/JtheGunner/omnishell/internal/graph"
	"github.com/JtheGunner/omnishell/internal/module"
)

func mf(id string, requires, after []string) module.Manifest {
	return module.Manifest{
		Module:   module.ModuleMeta{ID: id, Version: "1", Schema: 1},
		Requires: requires,
		After:    after,
	}
}

func TestOrderStableTopoSort(t *testing.T) {
	active := map[string]module.Manifest{
		"syntax-highlighting": mf("syntax-highlighting", nil, []string{"completion", "fzf", "autosuggestions"}),
		"autosuggestions":     mf("autosuggestions", nil, []string{"completion", "fzf"}),
		"fzf":                 mf("fzf", []string{"completion"}, nil),
		"completion":          mf("completion", nil, nil),
		"history":             mf("history", nil, []string{"completion"}),
	}
	got, err := graph.Order(active)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	want := []string{"completion", "fzf", "autosuggestions", "history", "syntax-highlighting"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v\nwant  %v", got, want)
	}
}

func TestOrderMissingRequire(t *testing.T) {
	active := map[string]module.Manifest{
		"fzf": mf("fzf", []string{"completion"}, nil),
	}
	_, err := graph.Order(active)
	var mre graph.MissingRequireError
	if !errors.As(err, &mre) || mre.Module != "fzf" || mre.Requires != "completion" {
		t.Fatalf("err = %v, want MissingRequireError{fzf, completion}", err)
	}
}

func TestOrderIgnoresInactiveAfter(t *testing.T) {
	active := map[string]module.Manifest{
		"fzf": mf("fzf", nil, []string{"completion"}), // completion not active
	}
	got, err := graph.Order(active)
	if err != nil || !reflect.DeepEqual(got, []string{"fzf"}) {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestOrderCycle(t *testing.T) {
	active := map[string]module.Manifest{
		"a": mf("a", []string{"b"}, nil),
		"b": mf("b", []string{"a"}, nil),
	}
	_, err := graph.Order(active)
	var ce graph.CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want CycleError", err)
	}
}
