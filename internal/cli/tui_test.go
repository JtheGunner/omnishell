package cli_test

import (
	"bytes"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/tui"
)

// tuiSpy swaps the terminal check and the UI runner for the test's duration.
// It returns a pointer to the backend the command handed to the UI (nil if the
// UI never started).
func tuiSpy(t *testing.T, isTerminal bool) **tui.Backend {
	t.Helper()
	var got *tui.Backend
	cli.SetTUIForTest(
		func(io.Reader, io.Writer) bool { return isTerminal },
		func(b tui.Backend, _ io.Reader, _ io.Writer) error {
			got = &b
			return nil
		},
	)
	t.Cleanup(func() { cli.SetTUIForTest(nil, nil) })
	return &got
}

func TestTUIRefusesWithoutAnInteractiveTerminal(t *testing.T) {
	setTestInit(t)
	var started bool
	cli.SetTUIForTest(nil, func(tui.Backend, io.Reader, io.Writer) error { started = true; return nil })
	t.Cleanup(func() { cli.SetTUIForTest(nil, nil) })

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"tui"}, &out, &errb)

	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "needs an interactive terminal") {
		t.Fatalf("stderr should explain the terminal requirement, got: %s", errb.String())
	}
	if out.Len() != 0 || started {
		t.Fatalf("nothing may start or print: stdout=%q started=%v", out.String(), started)
	}
}

func TestTUIWithoutConfigHintsAtInitAndNeverStarts(t *testing.T) {
	setupModuleCLITest(t) // no init: config.toml does not exist
	got := tuiSpy(t, true)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"tui"}, &out, &errb)

	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "run 'omnishell init' first") {
		t.Fatalf("stderr should carry the init hint, got: %s", errb.String())
	}
	if *got != nil {
		t.Fatal("the UI must not start without a config")
	}
}

func TestTUIWithMalformedConfigFailsBeforeStarting(t *testing.T) {
	cfgPath := setTestInit(t)
	if err := os.WriteFile(cfgPath, []byte("not = [toml"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	got := tuiSpy(t, true)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"tui"}, &out, &errb)

	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	if *got != nil {
		t.Fatal("the UI must not start with a malformed config")
	}
}

func TestTUIHandsTheFixtureModulesToTheUI(t *testing.T) {
	setTestInit(t)
	got := tuiSpy(t, true)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	if *got == nil {
		t.Fatal("the UI was not started")
	}

	views, err := (**got).Modules()
	if err != nil {
		t.Fatalf("Modules: %v", err)
	}
	ids := make([]string, len(views))
	for i, v := range views {
		ids[i] = v.ID
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("module ids must be sorted for display, got %v", ids)
	}
	for _, want := range []string{"completion", "fzf", "modern-aliases", "zshonly"} {
		if !slices.Contains(ids, want) {
			t.Fatalf("module %q missing from %v", want, ids)
		}
	}
}

func TestTUIRejectsArguments(t *testing.T) {
	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui", "extra"}, &out, &errb); code == 0 {
		t.Fatal("tui takes no arguments")
	}
}
