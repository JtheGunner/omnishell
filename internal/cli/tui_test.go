package cli_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/modedit"
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
		func(b tui.Backend, _ io.Reader, _ io.Writer) (tui.Result, error) {
			got = &b
			return tui.Result{}, nil
		},
	)
	t.Cleanup(func() { cli.SetTUIForTest(nil, nil) })
	return &got
}

func TestTUIRefusesWithoutAnInteractiveTerminal(t *testing.T) {
	setTestInit(t)
	var started bool
	cli.SetTUIForTest(nil, func(tui.Backend, io.Reader, io.Writer) (tui.Result, error) { started = true; return tui.Result{}, nil })
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

func TestTUIBackendStatusesFollowTheConfigAfterAToggle(t *testing.T) {
	setTestInit(t)
	cli.SetLookPathForTest(bashPresentLookPath)
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	b := backendFor(t)

	if err := b.Enable("fzf"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	statuses, err := b.Statuses()
	if err != nil {
		t.Fatalf("Statuses: %v", err)
	}

	if statuses["fzf"] != modedit.StatusEnabled || statuses["zshonly"] != modedit.StatusDisabled {
		t.Fatalf("statuses fzf=%q zshonly=%q, want enabled and disabled", statuses["fzf"], statuses["zshonly"])
	}
}

func TestTUIBackendPlanDescribesTheConfigAsItIsNow(t *testing.T) {
	setTestInit(t)
	cli.SetLookPathForTest(bashPresentLookPath)
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	b := backendFor(t)

	before, err := b.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !strings.HasPrefix(before.Text, "Plan (") || !before.NeedsApply {
		t.Fatalf("a fresh init has work for apply, got needs=%v:\n%s", before.NeedsApply, before.Text)
	}
	if strings.Contains(before.Text, "fzf") {
		t.Fatalf("fzf is not enabled yet:\n%s", before.Text)
	}

	if err := b.Enable("fzf"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	after, err := b.Plan()
	if err != nil {
		t.Fatalf("Plan after enabling: %v", err)
	}
	if !strings.Contains(after.Text, "fzf") {
		t.Fatalf("the plan must include what was just toggled:\n%s", after.Text)
	}
}

func TestTUIBackendPlanReportsAMalformedLockfile(t *testing.T) {
	cfgPath := setTestInit(t)
	b := backendFor(t)
	lockPath := filepath.Join(filepath.Dir(cfgPath), "state.lock.json")
	if err := os.WriteFile(lockPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	_, err := b.Plan()

	if err == nil || !strings.Contains(err.Error(), "lockfile") {
		t.Fatalf("err = %v, want a lockfile error", err)
	}
}

// handoffRun makes the fake UI report the given result, as the real one does
// when the user leaves the plan screen.
func handoffRun(t *testing.T, result tui.Result, runErr error) {
	t.Helper()
	cli.SetTUIForTest(
		func(io.Reader, io.Writer) bool { return true },
		func(tui.Backend, io.Reader, io.Writer) (tui.Result, error) { return result, runErr },
	)
	t.Cleanup(func() { cli.SetTUIForTest(nil, nil) })
}

// askedQuestions replaces the confirmation prompt with one that records the
// questions and answers with answer.
func askedQuestions(t *testing.T, answer bool) *[]string {
	t.Helper()
	var asked []string
	cli.SetPromptForTest(func(question string) bool {
		asked = append(asked, question)
		return answer
	})
	t.Cleanup(func() { cli.SetPromptForTest(nil) })
	return &asked
}

// zshHostWithCompletion is a host with zsh, initialised and with the completion
// module enabled, ready for a first apply. It returns the init file's path.
func zshHostWithCompletion(t *testing.T) string {
	t.Helper()
	home, _ := setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	var out, errb bytes.Buffer
	for _, args := range [][]string{{"init"}, {"enable", "completion"}} {
		if code := cli.Execute(args, &out, &errb); code != 0 {
			t.Fatalf("%v exit %d: %s", args, code, errb.String())
		}
	}
	return filepath.Join(home, ".config", "omnishell", "init.zsh")
}

func TestTUIDoesNotApplyUnlessTheUserConfirmedThePlan(t *testing.T) {
	initFile := zshHostWithCompletion(t)
	handoffRun(t, tui.Result{}, nil)
	asked := askedQuestions(t, true)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errb.String())
	}

	if len(*asked) != 0 {
		t.Fatalf("apply must not run, but it asked %v", *asked)
	}
	if _, err := os.Stat(initFile); !os.IsNotExist(err) {
		t.Fatalf("init.zsh must not exist (err=%v)", err)
	}
}

// The plan screen is a preview. The prompt of the real apply stays the gate: a
// "no" there must change nothing, and the exit code is apply's own.
func TestTUIHandsOverToApplyWhichStillAsksBeforeChangingAnything(t *testing.T) {
	initFile := zshHostWithCompletion(t)
	handoffRun(t, tui.Result{ApplyRequested: true}, nil)
	asked := askedQuestions(t, false)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"tui"}, &out, &errb)

	if code != 1 {
		t.Fatalf("exit = %d, want 1 (apply was declined); stderr: %s", code, errb.String())
	}
	if !reflect.DeepEqual(*asked, []string{"Proceed?"}) {
		t.Fatalf("apply must ask once before it changes anything, asked %v", *asked)
	}
	if !strings.Contains(errb.String(), "aborted") {
		t.Fatalf("stderr = %q, want apply's own 'aborted'", errb.String())
	}
	if !strings.Contains(out.String(), "Plan (") {
		t.Fatalf("apply shows its plan before it asks; stdout:\n%s", out.String())
	}
	if _, err := os.Stat(initFile); !os.IsNotExist(err) {
		t.Fatalf("a declined apply must write nothing (err=%v)", err)
	}
}

func TestTUIHandoffRunsTheRealApplyWhenTheUserSaysYes(t *testing.T) {
	initFile := zshHostWithCompletion(t)
	handoffRun(t, tui.Result{ApplyRequested: true}, nil)
	askedQuestions(t, true)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errb.String())
	}

	body, err := os.ReadFile(initFile)
	if err != nil || !strings.Contains(string(body), "# >>> omnishell:completion") {
		t.Fatalf("apply did not write the completion section (err=%v):\n%s", err, body)
	}
}

func TestTUIHandoffStreamsInstallOutputLikeApplyDoes(t *testing.T) {
	zshHostWithCompletion(t)
	handoffRun(t, tui.Result{ApplyRequested: true}, nil)
	askedQuestions(t, false)
	cli.SetRunnerForTest(nil)
	var runnerWriters []io.Writer
	cli.SetRunnerFactoryForTest(func(stdout, _ io.Writer) pkgmgr.Runner {
		runnerWriters = append(runnerWriters, stdout)
		return &pkgmgr.MockRunner{}
	})
	t.Cleanup(func() { cli.SetRunnerFactoryForTest(nil) })

	var out, errb bytes.Buffer
	cli.Execute([]string{"tui"}, &out, &errb)

	if len(runnerWriters) < 2 {
		t.Fatalf("want an engine for the UI and one for apply, got %d", len(runnerWriters))
	}
	if runnerWriters[0] != io.Discard {
		t.Fatal("the UI's engine must stay quiet")
	}
	if last := runnerWriters[len(runnerWriters)-1]; last == io.Discard {
		t.Fatal("the apply that follows the UI must stream installs and hooks to the terminal")
	}
}

func TestTUIDoesNotApplyWhenTheUIFailed(t *testing.T) {
	zshHostWithCompletion(t)
	handoffRun(t, tui.Result{ApplyRequested: true}, errors.New("terminal went away"))
	asked := askedQuestions(t, true)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"tui"}, &out, &errb)

	if code != 1 || !strings.Contains(errb.String(), "terminal went away") {
		t.Fatalf("exit=%d stderr=%q, want the UI's error", code, errb.String())
	}
	if len(*asked) != 0 {
		t.Fatalf("apply must not run after a failed UI, asked %v", *asked)
	}
}

func TestTUIBackendOptionsAndSetOptionRoundTripThroughConfigToml(t *testing.T) {
	cfgPath := setTestInit(t)
	b := backendFor(t)

	before, err := b.Options("fzf")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}
	if len(before) != 1 || before[0].Key != "ctrl_r" || before[0].Value != "true" || before[0].Set || !before[0].Editable {
		t.Fatalf("options = %+v, want ctrl_r true, unset and editable", before)
	}

	if err := b.SetOption("fzf", "ctrl_r", "false"); err != nil {
		t.Fatalf("SetOption: %v", err)
	}
	after, err := b.Options("fzf")
	if err != nil {
		t.Fatalf("Options after: %v", err)
	}
	if after[0].Value != "false" || !after[0].Set {
		t.Fatalf("ctrl_r = %+v, want false and set", after[0])
	}
	src, err := os.ReadFile(cfgPath)
	if err != nil || !strings.Contains(string(src), "ctrl_r = false") || !strings.Contains(string(src), "# Edit this file by hand") {
		t.Fatalf("config.toml must hold the option and keep its comments (err=%v):\n%s", err, src)
	}
}

func TestTUIBackendRejectedOptionIsAShortMessageAndWritesNothing(t *testing.T) {
	cfgPath := setTestInit(t)
	b := backendFor(t)
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	err = b.SetOption("fzf", "ctrl_r", "maybe")

	if err == nil || !strings.Contains(err.Error(), "invalid value for fzf.ctrl_r") {
		t.Fatalf("err = %v, want the validation message", err)
	}
	if strings.Contains(err.Error(), cfgPath) {
		t.Fatalf("message %q must not start with the config path", err)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("config.toml must be untouched (err=%v)", err)
	}
}

func TestTUIBackendOptionsMarksListOptionsAsNotEditable(t *testing.T) {
	setTestInit(t)
	b := backendFor(t)

	opts, err := b.Options("modern-aliases")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	if len(opts) != 1 || opts[0].Type != "list<enum>" || opts[0].Editable {
		t.Fatalf("options = %+v, want one list option that is not editable", opts)
	}
}

func TestTUIBackendOptionsOfAnUnknownModuleIsAnError(t *testing.T) {
	setTestInit(t)
	b := backendFor(t)

	if _, err := b.Options("no-such-module"); err == nil || !strings.Contains(err.Error(), "unknown module") {
		t.Fatalf("err = %v, want unknown module", err)
	}
}

// The plan said there was something to apply, but the config changed in
// between and apply finds nothing to do: that must be said, not left silent.
func TestTUIHandoffSaysWhenApplyFindsNothingToDo(t *testing.T) {
	zshHostWithCompletion(t)
	handoffRun(t, tui.Result{ApplyRequested: true}, nil)
	askedQuestions(t, true)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui"}, &out, &errb); code != 0 {
		t.Fatalf("first run: exit = %d (stderr: %s)", code, errb.String())
	}
	if strings.Contains(out.String(), "Nothing to apply") {
		t.Fatalf("the first apply had work to do, but said otherwise:\n%s", out.String())
	}

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"tui"}, &out, &errb); code != 0 {
		t.Fatalf("second run: exit = %d (stderr: %s)", code, errb.String())
	}
	if !strings.Contains(out.String(), "Nothing to apply: no module or package changes are planned.") {
		t.Fatalf("a hand-off that finds nothing to do must say so; stdout:\n%s", out.String())
	}
}

// A help line longer than a classic 80-column terminal wraps in the middle of a
// word.
func TestTUIHelpFitsAnEightyColumnTerminal(t *testing.T) {
	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui", "--help"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errb.String())
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if n := utf8.RuneCountInString(line); n > 80 {
			t.Errorf("help line is %d characters wide: %q", n, line)
		}
	}
}
