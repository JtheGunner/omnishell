package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner answers `git ls-remote --tags --refs <repo>` from a table.
type fakeRunner struct {
	tags  map[string]string // repo -> ls-remote output
	fail  map[string]error  // repo -> error
	calls []string
}

func (f *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	repo := args[len(args)-1]
	if err := f.fail[repo]; err != nil {
		return nil, err
	}
	return []byte(f.tags[repo]), nil
}

func lsRemote(tags ...string) string {
	var b strings.Builder
	for _, t := range tags {
		_, _ = fmt.Fprintf(&b, "0123456789abcdef0123456789abcdef01234567\trefs/tags/%s\n", t)
	}
	return b.String()
}

const fallbackManifest = `platforms = ["linux"]
shells    = ["bash"]

[module]
id      = %q
name    = %q
version = "1.0.0"
schema  = 1

[[packages.fallback]]
type = "git"
repo = %q
dest = "{{.VendorDir}}/%s"
ref  = %q
`

func writeModule(t *testing.T, dir, id, repo, ref string) string {
	t.Helper()
	path := filepath.Join(dir, id, "manifest.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(fallbackManifest, id, id, repo, id, ref)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUpdateRewritesANewerTag(t *testing.T) {
	dir := t.TempDir()
	a := writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	b := writeModule(t, dir, "beta", "https://example.com/beta.git", "2.0.0")
	beforeB, _ := os.ReadFile(b)
	r := &fakeRunner{tags: map[string]string{
		"https://example.com/alpha.git": lsRemote("v1.0.0", "v1.1.0", "weekly"),
		"https://example.com/beta.git":  lsRemote("2.0.0"),
	}}

	res, err := Update(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 || res.Changes[0] != (Change{Module: "alpha", Repo: "https://example.com/alpha.git", Old: "v1.0.0", New: "v1.1.0"}) {
		t.Fatalf("Changes = %+v", res.Changes)
	}
	gotA, _ := os.ReadFile(a)
	if !strings.Contains(string(gotA), `ref  = "v1.1.0"`) {
		t.Fatalf("alpha not rewritten:\n%s", gotA)
	}
	if gotB, _ := os.ReadFile(b); string(gotB) != string(beforeB) {
		t.Fatal("beta must stay byte-identical")
	}
}

func TestUpdateRecordsAFailurePerModuleAndContinues(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	writeModule(t, dir, "beta", "https://example.com/beta.git", "v2.0.0")
	r := &fakeRunner{
		fail: map[string]error{"https://example.com/alpha.git": errors.New("exit status 128")},
		tags: map[string]string{"https://example.com/beta.git": lsRemote("v2.1.0")},
	}
	res, err := Update(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failures) != 1 || res.Failures[0].Module != "alpha" {
		t.Fatalf("Failures = %+v", res.Failures)
	}
	if len(res.Changes) != 1 || res.Changes[0].Module != "beta" {
		t.Fatalf("Changes = %+v", res.Changes)
	}
}

func TestUpdateTreatsAnEmptyTagListAsNoChange(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	res, err := Update(dir, &fakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 0 || len(res.Failures) != 0 {
		t.Fatalf("res = %+v, want no change and no failure", res)
	}
}

func TestUpdateIgnoresModulesWithoutFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain", "manifest.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := "platforms = [\"linux\"]\nshells = [\"bash\"]\n\n[module]\nid = \"plain\"\nname = \"plain\"\nversion = \"1.0.0\"\nschema = 1\n"
	if err := os.WriteFile(path, []byte(plain), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{}
	res, err := Update(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 0 || len(res.Changes) != 0 || len(res.Failures) != 0 {
		t.Fatalf("a module without a fallback must be skipped: calls=%v res=%+v", r.calls, res)
	}
}

func TestUpdateLeavesANonVersionRefAlone(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "main")
	r := &fakeRunner{tags: map[string]string{"https://example.com/alpha.git": lsRemote("v9.9.9")}}
	res, err := Update(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 0 || len(res.Failures) != 0 {
		t.Fatalf("a branch ref must not be upgraded: %+v", res)
	}
}

func TestUpdateReportsAFallbackWithoutRef(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alpha", "manifest.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "platforms = [\"linux\"]\nshells = [\"bash\"]\n\n[module]\nid = \"alpha\"\nname = \"alpha\"\nversion = \"1.0.0\"\nschema = 1\n\n[[packages.fallback]]\ntype = \"git\"\nrepo = \"https://example.com/alpha.git\"\ndest = \"{{.VendorDir}}/alpha\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Update(dir, &fakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failures) != 1 {
		t.Fatalf("Failures = %+v, want one", res.Failures)
	}
}

func TestSameMajor(t *testing.T) {
	cases := []struct {
		name    string
		changes []Change
		want    bool
	}{
		{"no changes", nil, false},
		{"minor and patch bumps", []Change{{Old: "v1.25.0", New: "v1.26.0"}, {Old: "2.68.0", New: "2.69.0"}}, true},
		{"one major bump", []Change{{Old: "v1.25.0", New: "v1.26.0"}, {Old: "v18.9.0", New: "v19.0.0"}}, false},
	}
	for _, tc := range cases {
		if got := sameMajor(tc.changes); got != tc.want {
			t.Errorf("%s: sameMajor = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSummary(t *testing.T) {
	if got := summary(Result{}); got != "" {
		t.Fatalf("empty result must give an empty summary, got %q", got)
	}
	got := summary(Result{
		Changes:  []Change{{Module: "starship", Old: "v1.25.0", New: "v1.26.0"}},
		Failures: []Failure{{Module: "mise", Err: errors.New("exit status 128")}},
	})
	for _, want := range []string{
		"| `starship` | `v1.25.0` | `v1.26.0` |",
		"`requires`",
		"CI does not build the fallbacks",
		"`mise`: exit status 128",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
}
