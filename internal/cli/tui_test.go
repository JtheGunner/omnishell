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
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
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

// backendFor starts `omnishell tui` against a fake UI and returns the backend
// the command handed over, with the module registry already built.
func backendFor(t *testing.T) tui.Backend {
	t.Helper()
	got := tuiSpy(t, true)
	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errb.String())
	}
	if *got == nil {
		t.Fatal("the UI was not started")
	}
	return **got
}

func TestTUIBackendTogglesWriteConfigTomlAndKeepItsComments(t *testing.T) {
	cfgPath := setTestInit(t)
	cli.SetLookPathForTest(bashPresentLookPath)
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	b := backendFor(t)

	if err := b.Enable("fzf"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	c, err := config.Load(cfgPath)
	if err != nil || !c.Modules["fzf"].Enabled {
		t.Fatalf("fzf should be enabled: %+v err=%v", c.Modules, err)
	}

	if err := b.Disable("fzf"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	c, err = config.Load(cfgPath)
	if err != nil || c.Modules["fzf"].Enabled {
		t.Fatalf("fzf should be disabled: %+v err=%v", c.Modules, err)
	}

	src, err := os.ReadFile(cfgPath)
	if err != nil || !strings.Contains(string(src), "# Edit this file by hand") {
		t.Fatalf("the header comment must survive toggling (err=%v):\n%s", err, src)
	}
}

func TestTUIBackendRejectionIsAShortMessageWithoutTheConfigPath(t *testing.T) {
	cfgPath := setTestInit(t)
	cli.SetLookPathForTest(bashPresentLookPath) // zsh is absent, so zshonly cannot run
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	b := backendFor(t)
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	err = b.Enable("zshonly")

	if err == nil {
		t.Fatal("enabling a zsh-only module on a bash-only host must be rejected")
	}
	if !strings.Contains(err.Error(), `module "zshonly" only supports zsh`) {
		t.Fatalf("message = %q, want the reason", err)
	}
	if strings.Contains(err.Error(), cfgPath) {
		t.Fatalf("message %q must not start with the config path", err)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("config.toml must be untouched after a rejection (err=%v)", err)
	}
}

func TestTUIBackendReportsModulesThatCannotRunOnThisHost(t *testing.T) {
	setTestInit(t)
	cli.SetLookPathForTest(bashPresentLookPath)
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	b := backendFor(t)

	views, err := b.Modules()
	if err != nil {
		t.Fatalf("Modules: %v", err)
	}
	reasons := map[string]string{}
	for _, v := range views {
		reasons[v.ID] = v.Unavailable
	}
	if !strings.Contains(reasons["zshonly"], "needs zsh") {
		t.Fatalf("zshonly reason = %q, want it to explain the missing zsh", reasons["zshonly"])
	}
	if reasons["fzf"] != "" {
		t.Fatalf("fzf reason = %q, want none", reasons["fzf"])
	}
}

func TestTUIBackendUnknownModuleIsAnError(t *testing.T) {
	setTestInit(t)
	b := backendFor(t)

	if err := b.Enable("no-such-module"); err == nil || !strings.Contains(err.Error(), "unknown module") {
		t.Fatalf("err = %v, want unknown module", err)
	}
}

// The Bubble Tea program owns the terminal while the UI runs. Whatever a
// package manager prints while the UI probes it (brew and dpkg-query both
// write to stdout) must therefore never reach that terminal.
func TestTUIKeepsPackageManagerOutputOffTheScreen(t *testing.T) {
	setTestInit(t)
	cli.SetRunnerForTest(nil) // let the factory below build the runner
	var gotOut, gotErr io.Writer
	cli.SetRunnerFactoryForTest(func(stdout, stderr io.Writer) pkgmgr.Runner {
		gotOut, gotErr = stdout, stderr
		return &pkgmgr.MockRunner{}
	})
	t.Cleanup(func() { cli.SetRunnerFactoryForTest(nil) })
	tuiSpy(t, true)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errb.String())
	}

	if gotOut != io.Discard || gotErr != io.Discard {
		t.Fatalf("the runner must write to io.Discard, got stdout=%T stderr=%T", gotOut, gotErr)
	}
}
