package modedit

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/module"
)

// OptionView is one option of a module as a front end shows and edits it.
type OptionView struct {
	Key  string
	Type string // bool, int, string, enum, list<string> or list<enum>
	Help string
	// Values are the allowed values of an enum option, in manifest order.
	Values []string
	// Pattern is the regexp a string option must match; empty means any.
	Pattern string
	// Default is the manifest's default, as text.
	Default string
	// Value is what the option is right now, as text: the value in config.toml
	// when it sets one, otherwise the default.
	Value string
	// Set is true when config.toml sets the option explicitly.
	Set bool
	// Invalid is true when config.toml holds a value the schema rejects; Value
	// then shows it as written.
	Invalid bool
	// Editable is false for list options, which SetOption cannot take as one
	// raw string a user can sensibly type here.
	Editable bool
}

// Options returns the options of module id, sorted by key, with their current
// values. It reads only config.toml and the module's manifest, so it is cheap.
// A missing config.toml wraps config.ErrNotFound; an unknown module is a
// config.Error, as for Enable and SetOption.
func (ed Editor) Options(id string) ([]OptionView, error) {
	cfg, err := config.Load(ed.CfgPath)
	if err != nil {
		return nil, err
	}
	mod, ok := ed.Engine.Registry.Get(id)
	if !ok {
		return nil, config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf("unknown module %q", id)}
	}
	schema := mod.Manifest.Options
	defaults, err := module.ValidateOptions(schema, nil)
	if err != nil {
		return nil, config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf("module %q: %v", id, err)}
	}

	keys := make([]string, 0, len(schema))
	for key := range schema {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	configured := cfg.Modules[id].Options
	views := make([]OptionView, 0, len(keys))
	for _, key := range keys {
		spec := schema[key]
		v := OptionView{
			Key:      key,
			Type:     spec.Type,
			Help:     spec.Help,
			Values:   slices.Clone(spec.Values),
			Pattern:  spec.Pattern,
			Default:  optionText(defaults[key]),
			Editable: !strings.HasPrefix(spec.Type, "list<"),
		}
		v.Value = v.Default
		if raw, set := configured[key]; set {
			v.Set = true
			checked, verr := module.ValidateOptions(map[string]module.OptionSchema{key: spec}, map[string]any{key: raw})
			if verr != nil {
				v.Invalid = true
				v.Value = optionText(raw)
			} else {
				v.Value = optionText(checked[key])
			}
		}
		views = append(views, v)
	}
	return views, nil
}

// optionText renders an option value the way a user would type it.
func optionText(v any) string {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case string:
		return x
	case []string:
		return strings.Join(x, ",")
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = fmt.Sprint(item)
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(v)
	}
}
