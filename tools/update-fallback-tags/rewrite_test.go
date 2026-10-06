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

func TestRewriteRefChangesOnlyTheRef(t *testing.T) {
	got, err := rewriteRef(starshipLike, "v1.26.0")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(starshipLike, `ref  = "v1.25.0"`, `ref  = "v1.26.0"`, 1)
	if got != want {
		t.Fatalf("rewrite changed more than the ref:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRewriteRefKeepsSpacingAndTrailingComment(t *testing.T) {
	in := "[[packages.fallback]]\nrepo = \"r\"\nref=\"v1.0.0\" # pinned\n"
	got, err := rewriteRef(in, "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[[packages.fallback]]\nrepo = \"r\"\nref=\"v1.1.0\" # pinned\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRewriteRefErrors(t *testing.T) {
	cases := map[string]string{
		"no fallback table":                      "[module]\nid = \"x\"\n",
		"no ref line":                            "[[packages.fallback]]\nrepo = \"r\"\n",
		"ref in a later table, not the fallback": "[[packages.fallback]]\nrepo = \"r\"\n\n[options.x]\nref = \"v1.0.0\"\n",
	}
	for name, in := range cases {
		if _, err := rewriteRef(in, "v2.0.0"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
