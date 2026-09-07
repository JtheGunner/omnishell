package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
)

func TestDocsManWritesPages(t *testing.T) {
	dir := t.TempDir()

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"docs", "man", dir}, &out, &errb); code != 0 {
		t.Fatalf("docs man exit %d: %s", code, errb.String())
	}

	for _, name := range []string{"omnishell.1", "omnishell-apply.1", "omnishell-doctor.1"} {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestDocsManRejectsMissingDir(t *testing.T) {
	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"docs", "man"}, &out, &errb); code == 0 {
		t.Fatal("docs man with no directory argument should fail")
	}
}

func TestDocsIsHidden(t *testing.T) {
	var out, errb bytes.Buffer
	cli.Execute([]string{"--help"}, &out, &errb)
	if bytes.Contains(out.Bytes(), []byte("docs")) {
		t.Fatalf("the docs command should be hidden from --help:\n%s", out.String())
	}
}
