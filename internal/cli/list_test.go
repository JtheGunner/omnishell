package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestListShowsEnabledFixtureModule(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	configDir := filepath.Join(home, ".config", "omnishell")
	installFixtureModules(t, configDir)

	cli.SetLookPathForTest(func(string) (string, error) { return "", os.ErrNotExist })
	defer cli.SetLookPathForTest(nil)
	cli.SetRunnerForTest(&pkgmgr.MockRunner{})
	defer cli.SetRunnerForTest(nil)

	// init to create the config file.
	var initOut, initErr bytes.Buffer
	if code := cli.Execute([]string{"init"}, &initOut, &initErr); code != 0 {
		t.Fatalf("init exit %d: %s", code, initErr.String())
	}

	// Before enabling: module is disabled.
	var out1, err1 bytes.Buffer
	if code := cli.Execute([]string{"list"}, &out1, &err1); code != 0 {
		t.Fatalf("list exit %d: %s", code, err1.String())
	}
	if !strings.Contains(out1.String(), "completion") || !strings.Contains(out1.String(), "disabled") {
		t.Fatalf("expected completion/disabled row, got:\n%s", out1.String())
	}

	// Enable it in the config.
	cfgPath := filepath.Join(configDir, "config.toml")
	if err := config.SetEnabled(cfgPath, "completion", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	var out2, err2 bytes.Buffer
	if code := cli.Execute([]string{"list"}, &out2, &err2); code != 0 {
		t.Fatalf("list exit %d: %s", code, err2.String())
	}
	line := findRow(out2.String(), "completion")
	if line == "" {
		t.Fatalf("no completion row:\n%s", out2.String())
	}
	if !strings.Contains(line, "enabled") {
		t.Fatalf("completion row not enabled: %q", line)
	}
	if !strings.Contains(line, "n/a") {
		t.Fatalf("completion (no packages) should show n/a: %q", line)
	}
	if !strings.Contains(line, "compinit") {
		t.Fatalf("completion row missing description: %q", line)
	}

	// --json path.
	var jout, jerr bytes.Buffer
	if code := cli.Execute([]string{"list", "--json"}, &jout, &jerr); code != 0 {
		t.Fatalf("list --json exit %d: %s", code, jerr.String())
	}
	var rows []map[string]string
	if err := json.Unmarshal(jout.Bytes(), &rows); err != nil {
		t.Fatalf("json: %v\n%s", err, jout.String())
	}
	var completionRow map[string]string
	for _, r := range rows {
		if r["module"] == "completion" {
			completionRow = r
		}
	}
	if completionRow == nil || completionRow["status"] != "enabled" {
		t.Fatalf("unexpected json rows: %+v", rows)
	}
	if completionRow["description"] != "compinit" {
		t.Fatalf("unexpected description in json row: %+v", completionRow)
	}
}

func TestListWithoutConfigIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	installFixtureModules(t, filepath.Join(home, ".config", "omnishell"))

	cli.SetLookPathForTest(func(string) (string, error) { return "", os.ErrNotExist })
	defer cli.SetLookPathForTest(nil)
	cli.SetRunnerForTest(&pkgmgr.MockRunner{})
	defer cli.SetRunnerForTest(nil)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"list"}, &out, &errb); code != 0 {
		t.Fatalf("list exit %d: %s", code, errb.String())
	}
	line := findRow(out.String(), "completion")
	if !strings.Contains(line, "—") {
		t.Fatalf("missing config should render status as em dash: %q", line)
	}
}

func findRow(table, id string) string {
	for _, l := range strings.Split(table, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), id) {
			return l
		}
	}
	return ""
}
