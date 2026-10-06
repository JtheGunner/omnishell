package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const guardScript = "../../.github/scripts/enable-automerge.sh"

// runGuard runs the auto-merge guard with a fake `gh` first on PATH and returns
// the recorded gh invocations. The fake answers `gh api` with apiOut and, when
// apiFails is set, exits non-zero after printing it (as gh does for an HTTP
// error: the error body goes to stdout).
func runGuard(t *testing.T, apiOut string, apiFails bool) []string {
	t.Helper()
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "gh.log")
	fake := `#!/bin/sh
echo "$*" >> "$GH_LOG"
if [ "$1" = "api" ]; then
  printf '%s' "$FAKE_API_OUT"
  [ "$FAKE_API_FAILS" = "1" ] && exit 1
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	fails := "0"
	if apiFails {
		fails = "1"
	}
	cmd := exec.Command("sh", guardScript)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GH_LOG="+logPath,
		"FAKE_API_OUT="+apiOut,
		"FAKE_API_FAILS="+fails,
		"PR=7", "REPO=o/r",
		"GITHUB_STEP_SUMMARY="+filepath.Join(t.TempDir(), "summary.md"),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("guard script failed: %v\n%s", err, out)
	}
	raw, _ := os.ReadFile(logPath)
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func anyCall(calls []string, sub string) bool {
	for _, c := range calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func TestGuardEnablesAutoMergeOnlyWithRequiredChecks(t *testing.T) {
	calls := runGuard(t, "2", false)
	if !anyCall(calls, "pr merge 7 --repo o/r --auto --squash") {
		t.Fatalf("auto-merge must be requested when main has required checks: %v", calls)
	}
}

func TestGuardFailsClosed(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		apiFails bool
	}{
		{"no required checks", "0", false},
		{"empty answer", "", false},
		{"non-numeric answer", "oops", false},
		{"api error with the error body on stdout", `{"message":"Server Error","status":"500"}`, true},
		{"api error with no body", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := runGuard(t, tc.out, tc.apiFails)
			if anyCall(calls, "--auto --squash") {
				t.Fatalf("auto-merge must not be requested without a confirmed check count: %v", calls)
			}
			if !anyCall(calls, "pr merge 7 --repo o/r --disable-auto") {
				t.Fatalf("a failed guard must withdraw any earlier auto-merge: %v", calls)
			}
		})
	}
}
