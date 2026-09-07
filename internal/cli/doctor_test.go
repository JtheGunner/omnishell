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

func TestDoctorFixRepairsMissingInitFile(t *testing.T) {
	_, configDir := applyForDoctor(t)
	initFile := filepath.Join(configDir, "init.zsh")
	if err := os.Remove(initFile); err != nil {
		t.Fatalf("remove init.zsh: %v", err)
	}

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"doctor", "--fix", "--yes"}, &out, &errb)
	if code != 0 {
		t.Fatalf("doctor --fix exit %d, want 0\nstdout: %s\nstderr: %s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "fixed") {
		t.Fatalf("expected a 'fixed' line:\n%s", out.String())
	}
	if _, err := os.Stat(initFile); err != nil {
		t.Fatalf("init.zsh not regenerated: %v", err)
	}
}

func TestDoctorFixRefusesHandEditedInitFile(t *testing.T) {
	_, configDir := applyForDoctor(t)
	initFile := filepath.Join(configDir, "init.zsh")
	data, _ := os.ReadFile(initFile)
	tampered := append(data, []byte("\n# tampered\n")...)
	if err := os.WriteFile(initFile, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"doctor", "--fix", "--yes"}, &out, &errb)
	if code != 3 {
		t.Fatalf("doctor --fix exit %d, want 3\nstdout: %s\nstderr: %s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String()+errb.String(), "cannot auto-fix") {
		t.Fatalf("expected a 'cannot auto-fix' message:\nout: %s\nerr: %s", out.String(), errb.String())
	}
	now, _ := os.ReadFile(initFile)
	if string(now) != string(tampered) {
		t.Fatalf("--fix modified a hand-edited init file")
	}
}

func TestDoctorFixDeclinedPromptChangesNothing(t *testing.T) {
	_, configDir := applyForDoctor(t)
	initFile := filepath.Join(configDir, "init.zsh")
	if err := os.Remove(initFile); err != nil {
		t.Fatal(err)
	}
	cli.SetPromptForTest(func(string) bool { return false })
	defer cli.SetPromptForTest(nil)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"doctor", "--fix"}, &out, &errb)
	if code != 3 {
		t.Fatalf("doctor --fix (declined) exit %d, want 3", code)
	}
	if _, err := os.Stat(initFile); !os.IsNotExist(err) {
		t.Fatalf("init.zsh was regenerated despite a declined prompt (err=%v)", err)
	}
}

func TestDoctorFixOnCleanSetupIsNoop(t *testing.T) {
	_, _ = applyForDoctor(t)
	var out, errb bytes.Buffer
	code := cli.Execute([]string{"doctor", "--fix", "--yes"}, &out, &errb)
	if code != 0 {
		t.Fatalf("doctor --fix on a clean setup exit %d, want 0: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "no drift detected") {
		t.Fatalf("expected 'no drift detected':\n%s", out.String())
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
