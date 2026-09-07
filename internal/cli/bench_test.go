package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

// benchSetup runs init so config.toml exists, marks zsh as present, appends
// extraConfig to the [omnishell] table, and drops a placeholder init.zsh.
func benchSetup(t *testing.T, extraConfig string) (home, configDir string) {
	t.Helper()
	home, _ = setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)
	configDir = filepath.Join(home, ".config", "omnishell")

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if extraConfig != "" {
		p := filepath.Join(configDir, "config.toml")
		b, _ := os.ReadFile(p)
		if err := os.WriteFile(p, append(b, []byte(extraConfig)...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(configDir, "init.zsh"), []byte("# omnishell init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home, configDir
}

func fixedBench(baseline, sourced time.Duration) func(string, string, int) (time.Duration, time.Duration, error) {
	return func(string, string, int) (time.Duration, time.Duration, error) { return baseline, sourced, nil }
}

func TestBenchReportsAddedTimeAndWarnsOverBudget(t *testing.T) {
	benchSetup(t, "startup_budget_ms = 10\n")
	cli.SetBenchForTest(fixedBench(9*time.Millisecond, 51*time.Millisecond)) // +42ms
	defer cli.SetBenchForTest(nil)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"bench"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	s := out.String()
	if !strings.Contains(s, "+42ms") {
		t.Fatalf("missing added-time figure:\n%s", s)
	}
	if !strings.Contains(s, "budget") {
		t.Fatalf("expected an over-budget warning:\n%s", s)
	}
}

func TestBenchNoWarningWithinBudget(t *testing.T) {
	benchSetup(t, "startup_budget_ms = 500\n")
	cli.SetBenchForTest(fixedBench(9*time.Millisecond, 51*time.Millisecond))
	defer cli.SetBenchForTest(nil)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"bench"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if strings.Contains(out.String(), "budget") {
		t.Fatalf("unexpected budget warning:\n%s", out.String())
	}
}

func TestBenchJSON(t *testing.T) {
	benchSetup(t, "startup_budget_ms = 10\n")
	cli.SetBenchForTest(fixedBench(9*time.Millisecond, 51*time.Millisecond))
	defer cli.SetBenchForTest(nil)

	var out, errb bytes.Buffer
	cli.Execute([]string{"bench", "--json"}, &out, &errb)

	var got struct {
		BudgetMs int `json:"budget_ms"`
		Shells   []struct {
			Shell      string `json:"shell"`
			AddedMs    int    `json:"added_ms"`
			OverBudget bool   `json:"over_budget"`
		} `json:"shells"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if got.BudgetMs != 10 || len(got.Shells) != 1 || got.Shells[0].Shell != "zsh" ||
		got.Shells[0].AddedMs != 42 || !got.Shells[0].OverBudget {
		t.Fatalf("unexpected JSON: %+v", got)
	}
}

func TestBenchNoInitFile(t *testing.T) {
	_, configDir := benchSetup(t, "")
	if err := os.Remove(filepath.Join(configDir, "init.zsh")); err != nil {
		t.Fatal(err)
	}
	cli.SetBenchForTest(func(string, string, int) (time.Duration, time.Duration, error) {
		t.Fatal("benchRun must not run when there is no init file")
		return 0, 0, nil
	})
	defer cli.SetBenchForTest(nil)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"bench"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "no init file") {
		t.Fatalf("expected a 'no init file' line:\n%s", out.String())
	}
}

func TestBenchMissingConfigExitsTwo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cli.SetLookPathForTest(func(string) (string, error) { return "", os.ErrNotExist })
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	cli.SetRunnerForTest(&pkgmgr.MockRunner{})
	t.Cleanup(func() { cli.SetRunnerForTest(nil) })

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"bench"}, &out, &errb); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "omnishell init") {
		t.Fatalf("missing 'omnishell init' hint:\n%s", errb.String())
	}
}
