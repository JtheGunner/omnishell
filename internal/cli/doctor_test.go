package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
)

// applyForDoctor runs init + enable completion + apply --yes with zsh present
// and returns the config dir. It fails the test if apply does not exit 0.
func applyForDoctor(t *testing.T) (home, configDir string) {
	t.Helper()
	home, _ = setupModuleCLITest(t)
	configDir = filepath.Join(home, ".config", "omnishell")
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"apply", "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("apply exit %d: %s\n%s", code, errb.String(), out.String())
	}
	return home, configDir
}

func TestDoctorCommandCleanExitsZero(t *testing.T) {
	_, _ = applyForDoctor(t)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"doctor"}, &out, &errb)
	if code != 0 {
		t.Fatalf("doctor exit %d, want 0 (stdout: %s / stderr: %s)", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "no drift detected") {
		t.Fatalf("doctor stdout missing clean line:\n%s", out.String())
	}
}

func TestDoctorCommandDetectsDriftExitsThree(t *testing.T) {
	_, configDir := applyForDoctor(t)

	initFile := filepath.Join(configDir, "init.zsh")
	data, err := os.ReadFile(initFile)
	if err != nil {
		t.Fatalf("read init.zsh: %v", err)
	}
	if err := os.WriteFile(initFile, append(data, []byte("\n# tampered\n")...), 0o644); err != nil {
		t.Fatalf("tamper init.zsh: %v", err)
	}

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"doctor"}, &out, &errb)
	if code != 3 {
		t.Fatalf("doctor exit %d, want 3 (stdout: %s / stderr: %s)", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "initfile-edited:") {
		t.Fatalf("doctor stdout missing hand-edit finding:\n%s", out.String())
	}
}

func TestDoctorCommandNeverAppliedExitsThree(t *testing.T) {
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
	code := cli.Execute([]string{"doctor"}, &out, &errb)
	if code != 3 {
		t.Fatalf("doctor exit %d, want 3 (stdout: %s / stderr: %s)", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "never-applied") {
		t.Fatalf("doctor stdout missing never-applied finding:\n%s", out.String())
	}
}
