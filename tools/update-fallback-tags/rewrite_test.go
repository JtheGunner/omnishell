package main

import (
	"strings"
	"testing"
)

const starshipLike = `platforms = ["macos", "linux"]
shells    = ["zsh", "bash"]

[module]
id = "starship"

# a comment that must survive
[packages]
apt = ["starship"]

[[packages.fallback]]
type = "git"
repo = "https://github.com/starship/starship.git"
dest = "{{.VendorDir}}/starship"
ref  = "v1.25.0"
run  = ["cargo", "install"]
requires = ["cargo>=1.95"]
`

func TestRewriteRefsChangesOnlyTheRef(t *testing.T) {
	got, err := rewriteRefs(starshipLike, "v1.25.0", "v1.26.0")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(starshipLike, `ref  = "v1.25.0"`, `ref  = "v1.26.0"`, 1)
	if got != want {
		t.Fatalf("rewrite changed more than the ref:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRewriteRefsKeepsSpacingAndTrailingComment(t *testing.T) {
	in := "[[packages.fallback]]\nrepo = \"r\"\nref=\"v1.0.0\" # pinned\n"
	got, err := rewriteRefs(in, "v1.0.0", "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[[packages.fallback]]\nrepo = \"r\"\nref=\"v1.1.0\" # pinned\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRewriteRefsErrors(t *testing.T) {
	cases := map[string]string{
		"no fallback table":                      "[module]\nid = \"x\"\n",
		"no ref line":                            "[[packages.fallback]]\nrepo = \"r\"\n",
		"ref in a later table, not the fallback": "[[packages.fallback]]\nrepo = \"r\"\n\n[options.x]\nref = \"v1.0.0\"\n",
	}
	for name, in := range cases {
		if _, err := rewriteRefs(in, "v1.0.0", "v2.0.0"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestRewriteRefsOnlyMovesRefsEqualToTheOldOne(t *testing.T) {
	in := "[[packages.fallback]]\ntype = \"release\"\nref  = \"v1\"\n\n[[packages.fallback]]\ntype = \"git\"\nref  = \"v0.9\"\n"
	got, err := rewriteRefs(in, "v1", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `ref  = "v2"`) || !strings.Contains(got, `ref  = "v0.9"`) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRewriteRefsFailsWithoutAMatchingRef(t *testing.T) {
	if _, err := rewriteRefs("[[packages.fallback]]\nref = \"v1\"\n", "v9", "v10"); err == nil {
		t.Fatal("want an error when no fallback pins the old ref")
	}
}

func TestRewriteAssetSHAsReplacesThemInOrder(t *testing.T) {
	in := "[[packages.fallback.assets]]\nsha256 = \"" + strings.Repeat("1", 64) + "\" # amd64\n\n[[packages.fallback.assets]]\nsha256 = \"" + strings.Repeat("2", 64) + "\"\n"
	got, err := rewriteAssetSHAs(in, []string{strings.Repeat("a", 64), strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, strings.Repeat("a", 64)+"\" # amd64") || !strings.Contains(got, strings.Repeat("b", 64)) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRewriteAssetSHAsFailsOnACountMismatch(t *testing.T) {
	in := "[[packages.fallback.assets]]\nsha256 = \"" + strings.Repeat("1", 64) + "\"\n"
	if _, err := rewriteAssetSHAs(in, []string{"a", "b"}); err == nil {
		t.Fatal("want an error when the number of checksums differs from the number of assets")
	}
}
