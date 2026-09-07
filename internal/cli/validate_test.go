package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

const badFzfConfig = `[omnishell]
version = 1

[modules.fzf]
enabled = true

[modules.fzf.options]
bogus = 1
`

func TestValidateGoodConfigExitsZero(t *testing.T) {
	setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"validate"}, &out, &errb); code != 0 {
		t.Fatalf("validate exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "valid") {
		t.Fatalf("output missing 'valid':\n%s", out.String())
	}
}

func TestValidateBadConfigExitsTwo(t *testing.T) {
	_, cfgPath := setupModuleCLITest(t)
	if err := os.WriteFile(cfgPath, []byte(badFzfConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := cli.Execute([]string{"validate"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit %d, want 2\nout=%s\nerr=%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "bad-option") {
		t.Fatalf("output missing 'bad-option':\n%s", out.String())
	}
}

func TestValidateJSONOutput(t *testing.T) {
	_, cfgPath := setupModuleCLITest(t)
	if err := os.WriteFile(cfgPath, []byte(badFzfConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	cli.Execute([]string{"validate", "--json"}, &out, &errb)

	var got struct {
		Valid    bool `json:"valid"`
		Problems []struct {
			Module  string `json:"module"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"problems"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if got.Valid || len(got.Problems) == 0 || got.Problems[0].Code != "bad-option" || got.Problems[0].Module != "fzf" {
		t.Fatalf("unexpected JSON: %+v", got)
	}
}

func TestValidateMissingConfigExitsTwo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cli.SetLookPathForTest(func(string) (string, error) { return "", os.ErrNotExist })
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	cli.SetRunnerForTest(&pkgmgr.MockRunner{})
	t.Cleanup(func() { cli.SetRunnerForTest(nil) })

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"validate"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "omnishell init") {
		t.Fatalf("missing 'omnishell init' hint:\n%s", errb.String())
	}
}
