package render_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/render"
)

func baseCtx() render.Context {
	return render.Context{
		Options:   map[string]any{"ctrl_r": true, "default_opts": "--height 40%", "name": "it's fine"},
		Platform:  "macos",
		Shell:     "zsh",
		VendorDir: "/home/j/.config/omnishell/vendor",
		ConfigDir: "/home/j/.config/omnishell",
		Bin:       map[string]string{"fzf": "/opt/homebrew/bin/fzf"},
		Active:    map[string]bool{"completion": true},
	}
}

func TestRenderConditionalAndShellquote(t *testing.T) {
	tmpl := `{{ if .Options.ctrl_r }}bindkey ^R{{ end }}
export OPTS={{ .Options.default_opts | shellquote }}
name={{ .Options.name | shellquote }}
vendor={{ pathjoin .VendorDir "fzf" }}
{{ if has "completion" }}need-completion{{ end }}`
	got, err := render.Render(tmpl, baseCtx())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "bindkey ^R\n" +
		"export OPTS='--height 40%'\n" +
		"name='it'\\''s fine'\n" +
		"vendor=/home/j/.config/omnishell/vendor/fzf\n" +
		"need-completion\n"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderUnknownKeyIsError(t *testing.T) {
	_, err := render.Render(`{{ .Options.nope }}`, baseCtx())
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want a missing-key error mentioning nope", err)
	}
}

func TestHasItem(t *testing.T) {
	ctx := render.Context{Options: map[string]any{"replace": []string{"ls", "find"}}, Active: map[string]bool{}}
	got, err := render.Render(`{{ if has_item .Options.replace "ls" }}yes{{ end }}{{ if has_item .Options.replace "cat" }}CAT{{ end }}`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "yes\n" {
		t.Fatalf("got %q, want %q", got, "yes\n")
	}
}
