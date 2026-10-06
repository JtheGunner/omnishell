package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const releaseGuardScript = "../../.github/scripts/verify-release-tag.sh"

// gitIn runs git in dir and returns its trimmed stdout.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// releaseRepo builds a repository with one commit on main (mirrored as
// refs/remotes/origin/main, as actions/checkout leaves it) and one commit that
// only exists on a side branch. It returns the repo dir and both commit ids.
func releaseRepo(t *testing.T, withOriginMain bool) (dir, onMain, onSide string) {
	t.Helper()
	dir = t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "on main")
	onMain = gitIn(t, dir, "rev-parse", "HEAD")
	if withOriginMain {
		gitIn(t, dir, "update-ref", "refs/remotes/origin/main", onMain)
	}
	gitIn(t, dir, "checkout", "-q", "-b", "release-prep")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "only on a branch")
	onSide = gitIn(t, dir, "rev-parse", "HEAD")
	return dir, onMain, onSide
}

// runReleaseGuard runs the guard for the commit sha and returns its output and
// whether it exited zero.
func runReleaseGuard(t *testing.T, dir, sha string) (string, bool) {
	t.Helper()
	script, err := filepath.Abs(releaseGuardScript)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GITHUB_SHA="+sha, "GITHUB_REF_NAME=v9.9.9")
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func TestReleaseGuardAcceptsACommitOnMain(t *testing.T) {
	dir, onMain, _ := releaseRepo(t, true)
	if out, ok := runReleaseGuard(t, dir, onMain); !ok {
		t.Fatalf("a tag on a commit that is on main must be released:\n%s", out)
	}
}

func TestReleaseGuardRefusesACommitThatIsNotOnMain(t *testing.T) {
	dir, _, onSide := releaseRepo(t, true)
	out, ok := runReleaseGuard(t, dir, onSide)
	if ok {
		t.Fatal("a tag on a commit that is not on main must not be released")
	}
	if !strings.Contains(out, "not on main") {
		t.Fatalf("the refusal must say why:\n%s", out)
	}
}

func TestReleaseGuardFailsClosedWithoutOriginMain(t *testing.T) {
	dir, onMain, _ := releaseRepo(t, false)
	if out, ok := runReleaseGuard(t, dir, onMain); ok {
		t.Fatalf("without origin/main the guard cannot confirm anything and must refuse:\n%s", out)
	}
}
