package pkgmgr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
	"github.com/JtheGunner/omnishell/internal/module"
)

// LeftoverKind says what a Leftover is.
type LeftoverKind string

const (
	// LeftoverTree is the cloned source tree (with its target/ directory).
	LeftoverTree LeftoverKind = "source-tree"
	// LeftoverCrateEntry is this build's entry in cargo's install metadata.
	LeftoverCrateEntry LeftoverKind = "crates-entry"
)

// Leftover is one piece of an old Cargo build that a release install makes
// obsolete. Path is the tree or the metadata file; Key names the metadata entry.
type Leftover struct {
	Kind LeftoverKind
	Path string
	Key  string
}

// Describe words the leftover for plan and log output.
func (l Leftover) Describe() string {
	if l.Kind == LeftoverCrateEntry {
		return fmt.Sprintf("%s (cargo install entry %q)", l.Path, l.Key)
	}
	return l.Path
}

// FindLeftovers lists what an earlier `cargo install --path <dest> --root
// <vendor>` build of gitFB left behind, without changing anything. A tree is
// listed only when it lives inside the vendor dir and is a git clone of
// gitFB.Repo. Cargo metadata entries are matched by their source path, so
// another tool's entries are never touched. Things it cannot judge (an
// unparsable metadata file) are left alone and described in skipped.
func FindLeftovers(gitFB module.Fallback, ctx FallbackContext) (found []Leftover, skipped []string) {
	dest, err := renderPath(gitFB.Dest, ctx)
	if err != nil || dest == "" {
		return nil, nil
	}
	// Resolve "." and ".." before judging the path: "<vendor>/.." has the
	// vendor dir as its parent, yet names the vendor dir's own parent.
	dest = filepath.Clean(dest)
	if filepath.Dir(dest) != filepath.Clean(ctx.VendorDir) || dest == filepath.Clean(ctx.VendorDir) {
		return nil, nil
	}
	if populatedDir(dest) && isCloneOf(dest, gitFB.Repo) {
		found = append(found, Leftover{Kind: LeftoverTree, Path: dest})
	}
	suffix := "(path+file://" + dest + ")"
	tomlPath := filepath.Join(ctx.VendorDir, ".crates.toml")
	if keys, err := crateKeysTOML(tomlPath); err != nil {
		skipped = append(skipped, fmt.Sprintf("cannot read %s: %v", tomlPath, err))
	} else {
		found = append(found, entriesWithSuffix(tomlPath, keys, suffix)...)
	}
	jsonPath := filepath.Join(ctx.VendorDir, ".crates2.json")
	if keys, err := crateKeysJSON(jsonPath); err != nil {
		skipped = append(skipped, fmt.Sprintf("cannot parse %s: %v", jsonPath, err))
	} else {
		found = append(found, entriesWithSuffix(jsonPath, keys, suffix)...)
	}
	return found, skipped
}

// RemoveLeftover deletes one leftover. Metadata files are rewritten atomically
// without the entry; they are never deleted.
func RemoveLeftover(l Leftover) error {
	switch l.Kind {
	case LeftoverTree:
		return os.RemoveAll(l.Path)
	case LeftoverCrateEntry:
		if strings.HasSuffix(l.Path, ".json") {
			return removeCrateEntryJSON(l.Path, l.Key)
		}
		return removeCrateEntryTOML(l.Path, l.Key)
	}
	return fmt.Errorf("unknown leftover kind %q", l.Kind)
}

func entriesWithSuffix(path string, keys []string, suffix string) []Leftover {
	var out []Leftover
	for _, key := range keys {
		if strings.HasSuffix(key, suffix) {
			out = append(out, Leftover{Kind: LeftoverCrateEntry, Path: path, Key: key})
		}
	}
	return out
}

// isCloneOf reports whether dir is a git clone whose origin is repo. It reads
// .git/config directly, so no command runs.
func isCloneOf(dir, repo string) bool {
	f, err := os.Open(filepath.Join(dir, ".git", "config"))
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	inOrigin := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if !inOrigin {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok && strings.TrimSpace(key) == "url" {
			return normalizeRepoURL(value) == normalizeRepoURL(repo)
		}
	}
	return false
}

func normalizeRepoURL(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	return strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
}

// crateKeysTOML returns the install keys of a .crates.toml: the quoted keys at
// the start of a line. A missing file has no keys.
func crateKeysTOML(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, line := range strings.Split(string(data), "\n") {
		if key, ok := tomlLineKey(line); ok {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func tomlLineKey(line string) (string, bool) {
	if !strings.HasPrefix(line, `"`) {
		return "", false
	}
	end := strings.Index(line[1:], `"`)
	if end < 0 {
		return "", false
	}
	return line[1 : 1+end], true
}

func removeCrateEntryTOML(path, key string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	kept := lines[:0]
	for _, line := range lines {
		if k, ok := tomlLineKey(line); ok && k == key {
			continue
		}
		kept = append(kept, line)
	}
	return atomicfile.WriteFile(path, []byte(strings.Join(kept, "\n")), info.Mode().Perm())
}

type cratesDoc map[string]json.RawMessage

func readCratesJSON(path string) (cratesDoc, map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var doc cratesDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	var installs map[string]json.RawMessage
	if raw, ok := doc["installs"]; ok {
		if err := json.Unmarshal(raw, &installs); err != nil {
			return nil, nil, err
		}
	}
	return doc, installs, nil
}

func crateKeysJSON(path string) ([]string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	_, installs, err := readCratesJSON(path)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(installs))
	for k := range installs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func removeCrateEntryJSON(path, key string) error {
	doc, installs, err := readCratesJSON(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	delete(installs, key)
	rawInstalls, err := json.Marshal(installs)
	if err != nil {
		return err
	}
	doc["installs"] = rawInstalls
	out, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, out, info.Mode().Perm())
}
