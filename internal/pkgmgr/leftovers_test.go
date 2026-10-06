package pkgmgr_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

const toolRepo = "https://github.com/o/tool.git"

func gitFB() module.Fallback {
	return module.Fallback{Type: "git", Repo: toolRepo, Dest: "{{.VendorDir}}/tool", Ref: "v1"}
}

// buildSandbox lays out an old Cargo build under a vendor dir with a space in
// its name: a clone with a target/ dir, plus cargo metadata that also lists
// another tool.
func buildSandbox(t *testing.T, remote string) pkgmgr.FallbackContext {
	t.Helper()
	vendor := filepath.Join(t.TempDir(), "my vendor")
	tree := filepath.Join(vendor, "tool")
	for _, dir := range []string{filepath.Join(tree, ".git"), filepath.Join(tree, "target")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitConfig := "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = " + remote + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	writeFile(t, filepath.Join(tree, ".git", "config"), gitConfig)
	writeFile(t, filepath.Join(tree, "target", "artifact"), "x")
	ours := "tool 1.0.0 (path+file://" + tree + ")"
	other := "other 2.0.0 (path+file://" + filepath.Join(vendor, "other") + ")"
	writeFile(t, filepath.Join(vendor, ".crates.toml"),
		"[v1]\n\""+ours+"\" = [\"tool\"]\n\""+other+"\" = [\"other\"]\n")
	writeFile(t, filepath.Join(vendor, ".crates2.json"),
		`{"installs":{"`+ours+`":{"version_req":null},"`+other+`":{"version_req":null}},"extra":1}`)
	return pkgmgr.FallbackContext{VendorDir: vendor, Platform: "linux"}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func kinds(ls []pkgmgr.Leftover) map[pkgmgr.LeftoverKind]int {
	m := map[pkgmgr.LeftoverKind]int{}
	for _, l := range ls {
		m[l.Kind]++
	}
	return m
}

func TestFindLeftoversListsOnlyThisBuild(t *testing.T) {
	ctx := buildSandbox(t, toolRepo)
	found, skipped := pkgmgr.FindLeftovers(gitFB(), ctx)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v", skipped)
	}
	got := kinds(found)
	if got[pkgmgr.LeftoverTree] != 1 || got[pkgmgr.LeftoverCrateEntry] != 2 || len(found) != 3 {
		t.Fatalf("found = %+v, want the tree and one entry in each metadata file", found)
	}
}

func TestRemoveLeftoversKeepsOtherToolsMetadata(t *testing.T) {
	ctx := buildSandbox(t, toolRepo)
	found, _ := pkgmgr.FindLeftovers(gitFB(), ctx)
	for _, l := range found {
		if err := pkgmgr.RemoveLeftover(l); err != nil {
			t.Fatalf("RemoveLeftover(%+v): %v", l, err)
		}
	}
	if _, err := os.Stat(filepath.Join(ctx.VendorDir, "tool")); !os.IsNotExist(err) {
		t.Fatalf("source tree still exists (err %v)", err)
	}
	toml, err := os.ReadFile(filepath.Join(ctx.VendorDir, ".crates.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(toml), "tool 1.0.0") || !strings.Contains(string(toml), "other 2.0.0") || !strings.Contains(string(toml), "[v1]") {
		t.Fatalf(".crates.toml after cleanup:\n%s", toml)
	}
	raw, err := os.ReadFile(filepath.Join(ctx.VendorDir, ".crates2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Installs map[string]json.RawMessage `json:"installs"`
		Extra    int                        `json:"extra"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf(".crates2.json is no longer valid JSON: %v\n%s", err, raw)
	}
	if len(doc.Installs) != 1 || doc.Extra != 1 {
		t.Fatalf(".crates2.json after cleanup = %s", raw)
	}
	for key := range doc.Installs {
		if !strings.HasPrefix(key, "other 2.0.0") {
			t.Fatalf("remaining install = %q, want the other tool's", key)
		}
	}
}

func TestFindLeftoversLeavesAForeignCloneAlone(t *testing.T) {
	ctx := buildSandbox(t, "https://github.com/someone-else/tool.git")
	found, _ := pkgmgr.FindLeftovers(gitFB(), ctx)
	if kinds(found)[pkgmgr.LeftoverTree] != 0 {
		t.Fatalf("a clone of another repository was listed for deletion: %+v", found)
	}
}

func TestFindLeftoversLeavesANonCloneAlone(t *testing.T) {
	ctx := buildSandbox(t, toolRepo)
	if err := os.RemoveAll(filepath.Join(ctx.VendorDir, "tool", ".git")); err != nil {
		t.Fatal(err)
	}
	found, _ := pkgmgr.FindLeftovers(gitFB(), ctx)
	if kinds(found)[pkgmgr.LeftoverTree] != 0 {
		t.Fatalf("a directory that is not a git clone was listed for deletion: %+v", found)
	}
}

func TestFindLeftoversIgnoresAPathOutsideTheVendorDir(t *testing.T) {
	ctx := buildSandbox(t, toolRepo)
	fb := gitFB()
	fb.Dest = filepath.Join(t.TempDir(), "elsewhere")
	found, _ := pkgmgr.FindLeftovers(fb, ctx)
	if len(found) != 0 {
		t.Fatalf("found = %+v for a dest outside the vendor dir", found)
	}
}

func TestFindLeftoversReportsUnparsableMetadata(t *testing.T) {
	ctx := buildSandbox(t, toolRepo)
	writeFile(t, filepath.Join(ctx.VendorDir, ".crates2.json"), "{not json")
	found, skipped := pkgmgr.FindLeftovers(gitFB(), ctx)
	if kinds(found)[pkgmgr.LeftoverCrateEntry] != 1 {
		t.Fatalf("found = %+v, want only the .crates.toml entry", found)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], ".crates2.json") {
		t.Fatalf("skipped = %v, want a note naming .crates2.json", skipped)
	}
}

func TestFindLeftoversWithNothingToClean(t *testing.T) {
	ctx := pkgmgr.FallbackContext{VendorDir: filepath.Join(t.TempDir(), "my vendor"), Platform: "linux"}
	found, skipped := pkgmgr.FindLeftovers(gitFB(), ctx)
	if len(found) != 0 || len(skipped) != 0 {
		t.Fatalf("found=%+v skipped=%v on an empty vendor dir", found, skipped)
	}
}
