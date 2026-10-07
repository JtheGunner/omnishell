package cli_test

import (
	"bytes"
	"encoding/json"
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
// writers and the number of commands the runner ran.
func chattyEngine(t *testing.T) (*writers, *int) {
	t.Helper()
	got, calls := &writers{}, new(int)
	cli.SetRunnerForTest(nil) // setupModuleCLITest installed a mock; the factory below replaces it
	cli.SetRunnerFactoryForTest(func(stdout, stderr io.Writer) pkgmgr.Runner {
		got.out, got.err = stdout, stderr
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
