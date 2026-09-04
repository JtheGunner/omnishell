package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

// installedResponses pretends every package the built-in modules can ask for is
// already present, for whichever package manager pkgmgr.DetectManager actually
// picks on the host running the test: brew on darwin, apt on linux (the CI
// "test" job runs on ubuntu-latest, so both must be covered — a brew-only mock
// makes this pass on a developer's Mac and fail in CI). `brew list --versions
// <pkg>` and `dpkg-query -W -f=${Status} <pkg>` both report "installed", so
// pkgmgr's IsInstalled checks report true on either platform. This keeps
// `apply` on the clean, non-degraded path — no install command is ever issued
// and the git fallback never triggers — which is exactly what the happy-path
// journey wants to assert, on any OS the suite runs on.
func installedResponses() map[string]pkgmgr.MockResponse {
	pkgs := []string{
		"zsh-autosuggestions",
		"zsh-syntax-highlighting",
		"fzf",
		"eza",
		"bat",
		"fd",
		"zoxide",
	}
	out := map[string]pkgmgr.MockResponse{}
	for _, p := range pkgs {
		out["brew list --versions "+p] = pkgmgr.MockResponse{Out: []byte(p + " 1.0.0\n")}
		out["dpkg-query -W -f=${Status} "+p] = pkgmgr.MockResponse{Out: []byte("install ok installed\n")}
	}
	return out
}

// setupHome builds a hermetic $HOME with a pre-existing ~/.zshrc, installs the
// exec.LookPath seam (zsh + brew + git resolve; used only for shell/platform
// detection, not package-manager detection) and the pkgmgr.Runner seam, and
// returns a run() that drives cli.Execute capturing combined output.
func setupHome(t *testing.T) (home string, run func(args ...string) (int, string)) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("# existing zshrc\nexport EXISTING=1\n"), 0o644); err != nil {
		t.Fatalf("seed .zshrc: %v", err)
	}

	cli.SetLookPathForTest(func(bin string) (string, error) {
		switch bin {
		case "zsh", "brew", "git":
			return "/bin/" + bin, nil
		}
		return "", os.ErrNotExist
	})
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })

	// LookOK covers both brew (what pkgmgr.DetectManager picks on darwin) and
	// apt-get (what it picks first on linux), so package-manager detection
	// succeeds regardless of which OS this test binary runs on.
	cli.SetRunnerForTest(&pkgmgr.MockRunner{
		LookOK:    map[string]bool{"brew": true, "apt-get": true, "git": true},
		Responses: installedResponses(),
	})
	t.Cleanup(func() { cli.SetRunnerForTest(nil) })

	run = func(args ...string) (int, string) {
		var b bytes.Buffer
		code := cli.Execute(args, &b, &b)
		return code, b.String()
	}
	return home, run
}

func TestJourneyInitEnableApplyDoctorRemove(t *testing.T) {
	home, run := setupHome(t)

	if code, out := run("init"); code != 0 {
		t.Fatalf("init: %d %s", code, out)
	}
	for _, m := range []string{"completion", "history", "fzf", "autosuggestions", "syntax-highlighting"} {
		if code, out := run("enable", m); code != 0 {
			t.Fatalf("enable %s: %d %s", m, code, out)
		}
	}
	if code, out := run("apply", "--yes"); code != 0 {
		t.Fatalf("apply: %d %s", code, out)
	}

	initZsh := filepath.Join(home, ".config", "omnishell", "init.zsh")
	body, err := os.ReadFile(initZsh)
	if err != nil {
		t.Fatalf("init.zsh: %v", err)
	}
	s := string(body)
	idx := func(sub string) int { return strings.Index(s, sub) }
	for _, id := range []string{"completion", "history", "fzf", "autosuggestions", "syntax-highlighting"} {
		if idx("omnishell:"+id) < 0 {
			t.Fatalf("init.zsh missing section omnishell:%s:\n%s", id, s)
		}
	}
	// Assert only the orderings the dependency graph actually guarantees:
	//   history      after completion
	//   fzf          after completion, history
	//   autosuggest. after completion, history
	//   syntax-hl    after completion, history, fzf, autosuggestions
	// fzf vs autosuggestions is a genuine tie (same deps), so it is not asserted.
	deps := [][2]string{
		{"completion", "history"},
		{"history", "fzf"},
		{"history", "autosuggestions"},
		{"completion", "autosuggestions"},
		{"fzf", "syntax-highlighting"},
		{"autosuggestions", "syntax-highlighting"},
	}
	for _, d := range deps {
		if idx("omnishell:"+d[0]) >= idx("omnishell:"+d[1]) {
			t.Fatalf("section order wrong: %s must precede %s\n%s", d[0], d[1], s)
		}
	}

	zshrc, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if !strings.HasPrefix(string(zshrc), "# existing zshrc\nexport EXISTING=1\n") {
		t.Fatalf("existing .zshrc content disturbed:\n%s", zshrc)
	}
	if !strings.Contains(string(zshrc), "# >>> omnishell >>>") {
		t.Fatalf(".zshrc missing marker block:\n%s", zshrc)
	}

	if code, out := run("doctor"); code != 0 {
		t.Fatalf("doctor after apply: %d %s", code, out)
	}

	// Second apply is an idempotent no-op: init.zsh mtime must not move.
	info1, err := os.Stat(initZsh)
	if err != nil {
		t.Fatalf("stat init.zsh: %v", err)
	}
	if code, out := run("apply", "--yes"); code != 0 {
		t.Fatalf("second apply: %d %s", code, out)
	}
	info2, err := os.Stat(initZsh)
	if err != nil {
		t.Fatalf("stat init.zsh: %v", err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatalf("second apply rewrote init.zsh (mtime %v -> %v)", info1.ModTime(), info2.ModTime())
	}

	// Tamper the generated file -> doctor reports drift (exit 3).
	if err := os.WriteFile(initZsh, append(append([]byte{}, body...), []byte("\n# tamper\n")...), 0o644); err != nil {
		t.Fatalf("tamper init.zsh: %v", err)
	}
	if code, out := run("doctor"); code != 3 {
		t.Fatalf("doctor after tamper: exit %d, want 3; out:\n%s", code, out)
	}

	// apply --force repairs the drift.
	if code, out := run("apply", "--yes", "--force"); code != 0 {
		t.Fatalf("apply --force: %d %s", code, out)
	}
	if code, out := run("doctor"); code != 0 {
		t.Fatalf("doctor after repair: exit %d, want 0; out:\n%s", code, out)
	}

	// Remove one module: its section disappears, doctor stays clean.
	if code, out := run("remove", "fzf", "--yes"); code != 0 {
		t.Fatalf("remove fzf: %d %s", code, out)
	}
	body2, _ := os.ReadFile(initZsh)
	if strings.Contains(string(body2), "omnishell:fzf") {
		t.Fatalf("fzf section survived remove:\n%s", body2)
	}
	if code, out := run("doctor"); code != 0 {
		t.Fatalf("doctor after remove: exit %d, want 0; out:\n%s", code, out)
	}

	// uninstall restores the original ~/.zshrc byte-for-byte and drops init.zsh.
	if code, out := run("uninstall", "--yes"); code != 0 {
		t.Fatalf("uninstall: %d %s", code, out)
	}
	final, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if string(final) != "# existing zshrc\nexport EXISTING=1\n" {
		t.Fatalf("uninstall did not restore .zshrc:\n%q", final)
	}
	if _, err := os.Stat(initZsh); !os.IsNotExist(err) {
		t.Fatalf("init.zsh survived uninstall (err=%v)", err)
	}
}

func TestJourneyNoPackageManager(t *testing.T) {
	home, run := setupHome(t)

	// Override the seams: zsh present, but no package manager resolves at all.
	cli.SetLookPathForTest(func(bin string) (string, error) {
		if bin == "zsh" {
			return "/bin/zsh", nil
		}
		return "", os.ErrNotExist
	})
	cli.SetRunnerForTest(&pkgmgr.MockRunner{LookOK: map[string]bool{}})

	if code, out := run("init"); code != 0 {
		t.Fatalf("init: %d %s", code, out)
	}
	for _, m := range []string{"history", "completion", "fzf"} {
		if code, out := run("enable", m); code != 0 {
			t.Fatalf("enable %s: %d %s", m, code, out)
		}
	}

	// fzf needs a package and there is no manager -> degraded -> exit 1, but the
	// config-only modules still apply.
	code, out := run("apply", "--yes")
	if code != 1 {
		t.Fatalf("apply exit = %d, want 1 (degraded fzf); out:\n%s", code, out)
	}

	body, err := os.ReadFile(filepath.Join(home, ".config", "omnishell", "init.zsh"))
	if err != nil {
		t.Fatalf("init.zsh: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "omnishell:history") || !strings.Contains(got, "omnishell:completion") {
		t.Fatalf("expected history + completion applied:\n%s", got)
	}
	if strings.Contains(got, "omnishell:fzf") {
		t.Fatalf("fzf section should have been skipped (degraded):\n%s", got)
	}

	// Spec §1: apply run twice with no config change is a no-op — no writes, no
	// new backup — even when a module is permanently degraded.
	initZsh := filepath.Join(home, ".config", "omnishell", "init.zsh")
	backupsDir := filepath.Join(home, ".config", "omnishell", "backups")

	mtimeBefore := mustModTime(t, initZsh)
	backupsBefore := countEntries(t, backupsDir)

	code, out = run("apply", "--yes")
	if code != 1 {
		t.Fatalf("second apply exit = %d, want 1 (fzf still degraded); out:\n%s", code, out)
	}
	if got := mustModTime(t, initZsh); !got.Equal(mtimeBefore) {
		t.Fatalf("second apply rewrote init.zsh (mtime %v -> %v)", mtimeBefore, got)
	}
	if got := countEntries(t, backupsDir); got != backupsBefore {
		t.Fatalf("second apply created a new backup dir (%d -> %d)", backupsBefore, got)
	}

	code, out = run("doctor")
	if code != 3 {
		t.Fatalf("doctor exit = %d, want 3; out:\n%s", code, out)
	}
	if !strings.Contains(out, "module-degraded:fzf") {
		t.Fatalf("doctor should report module-degraded:fzf:\n%s", out)
	}
	if strings.Contains(out, "initfile-stale") {
		t.Fatalf("doctor should not report initfile-stale for a stably-degraded module:\n%s", out)
	}
	if strings.Contains(out, "pending-apply") {
		t.Fatalf("doctor should not report pending-apply for a stably-degraded module:\n%s", out)
	}
	if !strings.Contains(out, "[drift] ") {
		t.Fatalf("doctor output missing the [drift] line prefix:\n%s", out)
	}
}

func mustModTime(t *testing.T, path string) time.Time {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.ModTime()
}

func countEntries(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("readdir %s: %v", dir, err)
	}
	return len(entries)
}
