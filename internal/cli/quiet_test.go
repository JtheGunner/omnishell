package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

const probeNoise = "NOISE-FROM-PACKAGE-MANAGER"

// chattyRunner behaves like pkgmgr.ExecRunner as far as output goes: every
// command it runs writes a line to the writer it was built with, the way brew
// and dpkg-query print to the terminal. Every tool counts as installed and on
// PATH, so a package manager is detected and every package gets probed.
type chattyRunner struct {
	out   io.Writer
	calls *int
}

func (r chattyRunner) Run(name string, _ ...string) ([]byte, error) {
	*r.calls++
	_, _ = io.WriteString(r.out, probeNoise+" from "+name+"\n")
	return []byte("install ok installed"), nil
}

func (r chattyRunner) Look(name string) (string, error) { return "/usr/bin/" + name, nil }

// writers records what a command hands to the runner factory.
type writers struct{ out, err io.Writer }

// chattyEngine installs a runner factory that records the writers it is given
// and builds a chattyRunner on the stdout writer. It returns the recorded
// writers and the number of commands the runner ran. An engine builds its
// install runner first and a runner for package queries second, which is always
// quiet, so only the first call is recorded: those are the writers that decide
// whether installs and hooks are visible.
func chattyEngine(t *testing.T) (*writers, *int) {
	t.Helper()
	got, calls := &writers{}, new(int)
	cli.SetRunnerForTest(nil) // setupModuleCLITest installed a mock; the factory below replaces it
	cli.SetRunnerFactoryForTest(func(stdout, stderr io.Writer) pkgmgr.Runner {
		if got.out == nil {
			got.out, got.err = stdout, stderr
		}
		return chattyRunner{out: stdout, calls: calls}
	})
	t.Cleanup(func() { cli.SetRunnerFactoryForTest(nil) })
	return got, calls
}

func TestListJSONStaysValidWhenThePackageManagerPrintsToStdout(t *testing.T) {
	setTestInit(t)
	_, calls := chattyEngine(t)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"list", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errb.String())
	}

	if *calls == 0 {
		t.Fatal("setup: the package manager was never asked, so this test proves nothing")
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("list --json is not valid JSON (%v); it starts with:\n%.200s", err, out.String())
	}
	if len(rows) == 0 {
		t.Fatal("expected module rows")
	}
	if strings.Contains(errb.String(), probeNoise) {
		t.Fatalf("package manager output leaked to stderr:\n%.200s", errb.String())
	}
}

func TestListTableHasNoPackageManagerLines(t *testing.T) {
	setTestInit(t)
	_, calls := chattyEngine(t)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"list"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errb.String())
	}

	if *calls == 0 {
		t.Fatal("setup: the package manager was never asked, so this test proves nothing")
	}
	if strings.Contains(out.String(), probeNoise) {
		t.Fatalf("list output contains package manager lines:\n%.300s", out.String())
	}
	if !strings.HasPrefix(out.String(), "MODULE") {
		t.Fatalf("the table must be the first thing printed, got:\n%.200s", out.String())
	}
}

// Read-only commands ask the package manager questions; the answers are none of
// the user's business. Commands that install, remove or run hooks keep
// streaming the output, which is what the user is waiting for.
func TestRunnerWritersPerCommand(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		quiet bool
	}{
		{"list", []string{"list"}, true},
		{"list --json", []string{"list", "--json"}, true},
		{"diff", []string{"diff"}, true},
		{"apply --dry-run", []string{"apply", "--dry-run"}, true},
		{"doctor", []string{"doctor"}, true},
		{"apply", []string{"apply", "--yes"}, false},
		{"doctor --fix", []string{"doctor", "--fix", "--yes"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setTestInit(t)
			got, _ := chattyEngine(t)

			var out, errb bytes.Buffer
			cli.Execute(tc.args, &out, &errb) // the exit code does not matter here

			if got.out == nil {
				t.Fatal("the command never built an engine")
			}
			quiet := got.out == io.Discard && got.err == io.Discard
			if quiet != tc.quiet {
				t.Fatalf("runner writers: stdout=%T stderr=%T, want quiet=%v", got.out, got.err, tc.quiet)
			}
		})
	}
}

// Quieting the runner must not swallow the engine's own messages: they share
// the writers buildEngine is given.
func TestDiffStillWarnsAboutAnUnknownModule(t *testing.T) {
	cfgPath := setTestInit(t)
	chattyEngine(t)
	if err := config.SetEnabled(cfgPath, "ghost", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	var out, errb bytes.Buffer
	cli.Execute([]string{"diff"}, &out, &errb)

	if !strings.Contains(errb.String(), `unknown module "ghost"`) {
		t.Fatalf("the engine's warning is missing from stderr:\n%s", errb.String())
	}
}

// echoRunner prints the full command line of everything it runs to the writer
// it was built with, as brew and apt do on a terminal. Nothing is installed, so
// apply has to install; every query succeeds.
type echoRunner struct{ out io.Writer }

func (r echoRunner) Run(name string, args ...string) ([]byte, error) {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	_, _ = io.WriteString(r.out, "RAN "+line+"\n")
	switch {
	case name == "dpkg-query" || name == "rpm":
		return nil, errors.New("not installed")
	case strings.Contains(line, "apt-cache policy"):
		return []byte("Candidate: 1.0\n"), nil
	default:
		return nil, nil
	}
}

func (r echoRunner) Look(name string) (string, error) { return "/usr/bin/" + name, nil }

// isQueryLine reports whether a line echoed by echoRunner is one of the
// read-only questions the package manager is asked while the plan is computed.
func isQueryLine(line string) bool {
	for _, q := range []string{"brew info", "brew list", "dpkg-query", "apt-cache policy"} {
		if strings.Contains(line, q) {
			return true
		}
	}
	return false
}

func installLines(out string) (queries, installs []string) {
	for _, line := range strings.Split(out, "\n") {
		switch {
		case isQueryLine(line):
			queries = append(queries, line)
		case strings.Contains(line, "RAN ") && strings.Contains(line, "install"):
			installs = append(installs, line)
		}
	}
	return queries, installs
}

func echoEngine(t *testing.T) {
	t.Helper()
	cli.SetRunnerForTest(nil)
	cli.SetRunnerFactoryForTest(func(stdout, _ io.Writer) pkgmgr.Runner { return echoRunner{out: stdout} })
	t.Cleanup(func() { cli.SetRunnerFactoryForTest(nil) })
}

func TestApplyShowsInstallsButNotPackageManagerQueries(t *testing.T) {
	_, run := setupHome(t)
	echoEngine(t)

	for _, args := range [][]string{{"init"}, {"enable", "fzf"}, {"enable", "autosuggestions"}} {
		if code, out := run(args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, out)
		}
	}
	_, out := run("apply", "--yes") // the exit code does not matter here

	queries, installs := installLines(out)
	if len(queries) != 0 {
		t.Fatalf("package manager queries reached the terminal:\n%s", strings.Join(queries, "\n"))
	}
	if len(installs) == 0 {
		t.Fatalf("setup: no install output reached the terminal, so this test proves nothing:\n%.400s", out)
	}
}
