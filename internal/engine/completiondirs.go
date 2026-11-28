package engine

import (
	"os"
	"path/filepath"
)

// completionDirCandidates lists where the zsh-completions module looks for its
// completion directory, in the order the module's zsh template tries them. The
// system paths are repeated in modules/builtin/zsh-completions/zsh.tmpl; a test
// keeps both lists identical. The vendor entry is the git fallback's checkout.
func completionDirCandidates(vendorDir string) []string {
	return []string{
		"/opt/homebrew/share/zsh-completions",
		"/usr/local/share/zsh-completions",
		"/usr/share/zsh-completions",
		filepath.Join(vendorDir, "zsh-completions", "src"),
	}
}

// writableCompletionDirs returns the existing candidate directories, and their
// parents, that are writable by group or others. compinit refuses such
// directories ("insecure directories") and skips their completions unless the
// user answers its prompt. A directory that does not exist is ignored; a
// parent shared by several candidates is reported once. It only reads modes.
func writableCompletionDirs(candidates []string) []string {
	var found []string
	seen := map[string]bool{}
	check := func(dir string) {
		if seen[dir] {
			return
		}
		seen[dir] = true
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return
		}
		if info.Mode().Perm()&0o022 != 0 {
			found = append(found, dir)
		}
	}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		check(dir)
		check(filepath.Dir(dir))
	}
	return found
}
