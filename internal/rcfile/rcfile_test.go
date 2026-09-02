package rcfile_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/rcfile"
)

func TestEnsureBlockAppendsWhenAbsent(t *testing.T) {
	in := "export PATH=/x\nalias g=git\n"
	out, changed := rcfile.EnsureBlock(in, "zsh", "$HOME/.config/omnishell/init.zsh")
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if !strings.HasPrefix(out, in) {
		t.Fatalf("existing content not preserved:\n%s", out)
	}
	if !rcfile.BlockPresent(out) {
		t.Fatal("block not present after EnsureBlock")
	}
	if !strings.Contains(out, `[[ -f "$HOME/.config/omnishell/init.zsh" ]] && source "$HOME/.config/omnishell/init.zsh"`) {
		t.Fatalf("source line wrong:\n%s", out)
	}
}

func TestEnsureBlockIdempotent(t *testing.T) {
	in := "code\n"
	once, _ := rcfile.EnsureBlock(in, "zsh", "$HOME/.config/omnishell/init.zsh")
	twice, changed := rcfile.EnsureBlock(once, "zsh", "$HOME/.config/omnishell/init.zsh")
	if changed || once != twice {
		t.Fatalf("second EnsureBlock changed the file:\n%s", twice)
	}
}

func TestEnsureBlockBashUsesSingleBracket(t *testing.T) {
	out, _ := rcfile.EnsureBlock("", "bash", "$HOME/.config/omnishell/init.bash")
	if !strings.Contains(out, `[ -f "$HOME/.config/omnishell/init.bash" ] && source`) {
		t.Fatalf("bash guard wrong:\n%s", out)
	}
}

func TestRemoveBlockRestoresOriginal(t *testing.T) {
	original := "export PATH=/x\nalias g=git\n"
	withBlock, _ := rcfile.EnsureBlock(original, "zsh", "$HOME/.config/omnishell/init.zsh")
	out, changed := rcfile.RemoveBlock(withBlock)
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if out != original {
		t.Fatalf("RemoveBlock did not restore original:\n%q\nwant\n%q", out, original)
	}
	if _, changed := rcfile.RemoveBlock(out); changed {
		t.Fatal("RemoveBlock on clean file reported a change")
	}
}
