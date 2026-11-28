package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/modules"
)

func mkdirMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestWritableCompletionDirs(t *testing.T) {
	root := t.TempDir()
	mkdirMode(t, root, 0o755)

	safe := filepath.Join(root, "safe", "zsh-completions")
	mkdirMode(t, filepath.Dir(safe), 0o755)
	mkdirMode(t, safe, 0o755)

	groupWritable := filepath.Join(root, "group", "zsh-completions")
	mkdirMode(t, filepath.Dir(groupWritable), 0o755)
	mkdirMode(t, groupWritable, 0o775)

	parentWorldWritable := filepath.Join(root, "world", "zsh-completions")
	mkdirMode(t, filepath.Dir(parentWorldWritable), 0o777)
	mkdirMode(t, parentWorldWritable, 0o755)

	missing := filepath.Join(root, "missing", "zsh-completions")

	tests := []struct {
		name       string
		candidates []string
		want       []string
	}{
		{"private directory is fine", []string{safe}, nil},
		{"group-writable directory is reported", []string{groupWritable}, []string{groupWritable}},
		{"world-writable parent is reported, not the child", []string{parentWorldWritable},
			[]string{filepath.Dir(parentWorldWritable)}},
		{"missing directory is ignored", []string{missing}, nil},
		{"a shared parent is reported once", []string{
			filepath.Join(filepath.Dir(parentWorldWritable), "a"),
			parentWorldWritable,
		}, []string{filepath.Dir(parentWorldWritable)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// "a" does not exist; only the existing candidate counts.
			got := writableCompletionDirs(tc.candidates)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("writableCompletionDirs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCompletionDirCandidatesIncludeVendorSrc(t *testing.T) {
	got := completionDirCandidates("/v")
	want := filepath.Join("/v", "zsh-completions", "src")
	if got[len(got)-1] != want {
		t.Fatalf("last candidate = %q, want %q (candidates: %v)", got[len(got)-1], want, got)
	}
}

// The module template and the doctor check each carry the list of system
// directories; this keeps them from drifting apart.
func TestCompletionDirCandidatesMatchTemplate(t *testing.T) {
	body, err := fs.ReadFile(modules.FS(), "zsh-completions/zsh.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range completionDirCandidates("/v") {
		if strings.HasPrefix(dir, "/v") {
			continue
		}
		if !strings.Contains(string(body), dir) {
			t.Errorf("template does not list %s", dir)
		}
	}
}
