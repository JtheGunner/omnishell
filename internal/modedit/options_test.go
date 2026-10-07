package modedit_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/platform"
)

// newOptionsEditor builds an Editor over the modules in testdata/modules-options
// with cfgBody as config.toml (no file at all when cfgBody is empty).
func newOptionsEditor(t *testing.T, cfgBody string) (modedit.Editor, string) {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules-options")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if cfgBody != "" {
		if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	return modedit.Editor{
		Engine: engine.Engine{
			Platform: platform.Info{OS: platform.Linux},
			Registry: reg,
			Manager:  forbiddenManager{},
		},
		CfgPath: cfgPath,
	}, cfgPath
}

func optionByKey(t *testing.T, opts []modedit.OptionView, key string) modedit.OptionView {
	t.Helper()
	for _, o := range opts {
		if o.Key == key {
			return o
		}
	}
	t.Fatalf("no option %q in %+v", key, opts)
	return modedit.OptionView{}
}

const baseConfig = "[omnishell]\nversion = 1\n"

func TestOptionsAreSortedAndShowTheirDefaultsWhenNothingIsSet(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig)

	opts, err := ed.Options("tuned")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	keys := make([]string, len(opts))
	for i, o := range opts {
		keys[i] = o.Key
	}
	if want := []string{"extras", "label", "retries", "theme", "verbose"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	for _, o := range opts {
		if o.Set || o.Invalid || o.Value != o.Default {
			t.Fatalf("%s: set=%v invalid=%v value=%q default=%q, want the default and nothing set", o.Key, o.Set, o.Invalid, o.Value, o.Default)
		}
	}
	want := map[string]string{"extras": "a,b", "label": "work", "retries": "3", "theme": "dark", "verbose": "false"}
	for key, def := range want {
		if got := optionByKey(t, opts, key).Default; got != def {
			t.Fatalf("%s default = %q, want %q", key, got, def)
		}
	}
}

func TestOptionsCarryTheirSchema(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig)

	opts, err := ed.Options("tuned")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	theme := optionByKey(t, opts, "theme")
	if theme.Type != "enum" || !reflect.DeepEqual(theme.Values, []string{"dark", "light", "solarized"}) || theme.Help != "Colour theme" || !theme.Editable {
		t.Fatalf("theme = %+v", theme)
	}
	label := optionByKey(t, opts, "label")
	if label.Type != "string" || label.Pattern != "^[a-z]+$" {
		t.Fatalf("label = %+v", label)
	}
	if optionByKey(t, opts, "extras").Editable {
		t.Fatal("a list option must not be editable as one text")
	}
	for _, key := range []string{"verbose", "retries", "label", "theme"} {
		if !optionByKey(t, opts, key).Editable {
			t.Fatalf("%s must be editable", key)
		}
	}
}

func TestOptionsShowTheValuesConfigTomlSets(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig+`
[modules.tuned.options]
verbose = true
retries = 7
theme   = "light"
label   = "home"
extras  = ["x", "y", "z"]
`)

	opts, err := ed.Options("tuned")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	want := map[string]string{"verbose": "true", "retries": "7", "theme": "light", "label": "home", "extras": "x,y,z"}
	for key, value := range want {
		o := optionByKey(t, opts, key)
		if o.Value != value || !o.Set || o.Invalid {
			t.Fatalf("%s = %+v, want value %q, set, valid", key, o, value)
		}
	}
}

func TestOptionsFollowSetOption(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig)

	if err := ed.SetOption("tuned", "retries", "9"); err != nil {
		t.Fatalf("SetOption: %v", err)
	}
	opts, err := ed.Options("tuned")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	if o := optionByKey(t, opts, "retries"); o.Value != "9" || !o.Set {
		t.Fatalf("retries = %+v, want 9 and set", o)
	}
	if o := optionByKey(t, opts, "verbose"); o.Set {
		t.Fatal("an option nobody touched must stay unset")
	}
}

// A value the schema rejects (a hand edit) must still be shown, as written and
// flagged, so the user can see and fix it; the other options stay usable.
func TestOptionsFlagAValueTheSchemaRejects(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig+"[modules.tuned.options]\nretries = \"many\"\ntheme = \"light\"\n")

	opts, err := ed.Options("tuned")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	if o := optionByKey(t, opts, "retries"); !o.Invalid || o.Value != "many" || !o.Set {
		t.Fatalf("retries = %+v, want it flagged invalid and shown as written", o)
	}
	if o := optionByKey(t, opts, "theme"); o.Invalid || o.Value != "light" {
		t.Fatalf("theme = %+v, want the valid value untouched", o)
	}
}

func TestOptionsIgnoreKeysTheSchemaDoesNotKnow(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig+"[modules.tuned.options]\nbogus = 1\n")

	opts, err := ed.Options("tuned")
	if err != nil || len(opts) != 5 {
		t.Fatalf("err=%v len=%d, want the five declared options", err, len(opts))
	}
}

func TestOptionsOfAModuleWithoutOptionsIsEmptyNotNil(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig)

	opts, err := ed.Options("bare")

	if err != nil || opts == nil || len(opts) != 0 {
		t.Fatalf("opts=%#v err=%v, want an empty, non-nil slice", opts, err)
	}
}

func TestOptionsErrors(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig)
	var cfgErr config.Error
	if _, err := ed.Options("nope"); !errors.As(err, &cfgErr) {
		t.Fatalf("unknown module: err = %v, want a config.Error", err)
	}

	missing, _ := newOptionsEditor(t, "")
	if _, err := missing.Options("tuned"); !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("missing config: err = %v, want ErrNotFound", err)
	}

	broken, _ := newOptionsEditor(t, "not = [toml")
	if _, err := broken.Options("tuned"); err == nil || errors.Is(err, config.ErrNotFound) {
		t.Fatalf("malformed config: err = %v, want a parse error", err)
	}
}
