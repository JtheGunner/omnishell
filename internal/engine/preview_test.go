package engine_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

const previewConfig = "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n"

// previewFixture is a converged-able setup: a bash-only host with the
// completion module enabled and nothing applied yet.
type previewFixture struct {
	e                 engine.Engine
	home              string
	cfgPath, lockPath string
}

func newPreviewFixture(t *testing.T, cfgBody string) previewFixture {
	t.Helper()
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	f := previewFixture{
		e:        applyEngine(t, home, mgr, &out),
		home:     home,
		cfgPath:  filepath.Join(home, ".config", "omnishell", "config.toml"),
		lockPath: filepath.Join(home, ".config", "omnishell", "state.lock.json"),
	}
	writeConfig(t, f.cfgPath, cfgBody)
	return f
}

func (f previewFixture) load(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(f.cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

// previewThenApply returns what Preview says about the next apply and whether
// a real apply then changed anything.
func (f previewFixture) previewThenApply(t *testing.T) (needsApply, changed bool) {
	t.Helper()
	cfg := f.load(t)
	pv, err := f.e.Preview(cfg, f.lockPath)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	res, err := f.e.Apply(cfg, f.cfgPath, f.lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return pv.NeedsApply, res.Changed
}

// The preview is only worth showing if it agrees with apply about whether
// there is anything to do, in every state apply distinguishes.
func TestPreviewNeedsApplyAgreesWithApply(t *testing.T) {
	f := newPreviewFixture(t, previewConfig)

	needs, changed := f.previewThenApply(t)
	if !needs || !changed {
		t.Fatalf("fresh config: preview needs=%v, apply changed=%v, want both true", needs, changed)
	}

	needs, changed = f.previewThenApply(t)
	if needs || changed {
		t.Fatalf("converged: preview needs=%v, apply changed=%v, want both false", needs, changed)
	}

	// A deleted init file is not drift to a plain apply (only Refresh and doctor
	// notice it), so the preview must not promise a change either.
	if err := os.Remove(filepath.Join(f.home, ".config", "omnishell", "init.bash")); err != nil {
		t.Fatal(err)
	}
	needs, changed = f.previewThenApply(t)
	if needs != changed {
		t.Fatalf("deleted init file: preview needs=%v but apply changed=%v", needs, changed)
	}

	writeConfig(t, f.cfgPath, previewConfig+"[modules.fzf]\nenabled=true\n")
	needs, changed = f.previewThenApply(t)
	if !needs || !changed {
		t.Fatalf("after enabling fzf: preview needs=%v, apply changed=%v, want both true", needs, changed)
	}
}

func TestPreviewTextIsThePlanApplyWouldShow(t *testing.T) {
	f := newPreviewFixture(t, previewConfig)
	cfg := f.load(t)

	pv, err := f.e.Preview(cfg, f.lockPath)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	dry, err := f.e.Apply(cfg, f.cfgPath, f.lockPath, engine.ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Apply dry run: %v", err)
	}

	if strings.TrimRight(dry.PlanText, "\n") != pv.Text {
		t.Fatalf("preview text differs from the dry-run plan:\n--- preview ---\n%s\n--- dry run ---\n%s", pv.Text, dry.PlanText)
	}
	if !strings.Contains(pv.Text, "completion") {
		t.Fatalf("the plan should mention the enabled module:\n%s", pv.Text)
	}
}

func TestPreviewAppendsAWarningPerUnknownModule(t *testing.T) {
	f := newPreviewFixture(t, previewConfig+"[modules.ghost]\nenabled=true\n")

	pv, err := f.e.Preview(f.load(t), f.lockPath)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	if !strings.HasSuffix(pv.Text, `warning: unknown module "ghost" in config (ignored)`) {
		t.Fatalf("the warning should end the text:\n%s", pv.Text)
	}
}

func TestPreviewChangesNothingOnDisk(t *testing.T) {
	f := newPreviewFixture(t, previewConfig)
	snapshot := func() map[string]string {
		files := map[string]string{}
		err := filepath.WalkDir(f.home, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				data, rerr := os.ReadFile(path)
				if rerr != nil {
					return rerr
				}
				files[path] = string(data)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		return files
	}
	before := snapshot()

	if _, err := f.e.Preview(f.load(t), f.lockPath); err != nil {
		t.Fatalf("Preview: %v", err)
	}

	after := snapshot()
	if len(before) != len(after) {
		t.Fatalf("files before=%d after=%d: Preview must not create or remove files", len(before), len(after))
	}
	for path, content := range before {
		if after[path] != content {
			t.Fatalf("%s changed", path)
		}
	}
}

func TestPreviewReportsAConfigErrorForAnInvalidOption(t *testing.T) {
	f := newPreviewFixture(t, previewConfig+"[modules.fzf]\nenabled=true\n[modules.fzf.options]\nctrl_r=\"yes please\"\n")

	_, err := f.e.Preview(f.load(t), f.lockPath)

	var cfgErr engine.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err = %v, want an engine.ConfigError", err)
	}
}

func TestPreviewReportsAMalformedLockfile(t *testing.T) {
	f := newPreviewFixture(t, previewConfig)
	if err := os.WriteFile(f.lockPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := f.e.Preview(f.load(t), f.lockPath)

	if err == nil || !strings.Contains(err.Error(), "lockfile") {
		t.Fatalf("err = %v, want a lockfile error", err)
	}
}
