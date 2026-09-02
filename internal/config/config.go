// Package config reads (and, via edit.go, surgically edits) the
// user-facing ~/.config/omnishell/config.toml file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

// SchemaVersion is the config schema version this build understands.
const SchemaVersion = 1

// ErrNotFound is wrapped by Load when the config file does not exist.
var ErrNotFound = errors.New("config file not found")

// Error is a structured config/parse error; the CLI maps it to exit code 2.
type Error struct {
	Path string
	Line int
	Msg  string
}

func (e Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Msg)
}

// ModuleConfig is one [modules.<id>] table.
type ModuleConfig struct {
	Enabled bool
	Options map[string]any
}

// OmnishellSection is the [omnishell] table.
type OmnishellSection struct {
	Version int
	Shells  []string
}

// Config is the whole parsed file.
type Config struct {
	Omnishell OmnishellSection
	Modules   map[string]ModuleConfig
}

// Default is the config produced by `omnishell init`.
func Default() Config {
	return Config{
		Omnishell: OmnishellSection{Version: SchemaVersion},
		Modules:   map[string]ModuleConfig{},
	}
}

// raw mirrors the on-disk shape for decoding with strict key checking.
type raw struct {
	Omnishell struct {
		Version int      `toml:"version"`
		Shells  []string `toml:"shells"`
	} `toml:"omnishell"`
	Modules map[string]struct {
		Enabled bool           `toml:"enabled"`
		Options map[string]any `toml:"options"`
	} `toml:"modules"`
}

// Load parses the config file at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return Config{}, Error{Path: path, Msg: err.Error()}
	}

	var r raw
	md, derr := toml.Decode(string(data), &r)
	if derr != nil {
		return Config{}, Error{Path: path, Msg: derr.Error()}
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Config{}, Error{Path: path, Msg: "unknown key: " + undecoded[0].String()}
	}

	version := r.Omnishell.Version
	if version == 0 {
		version = SchemaVersion
	}
	if version != SchemaVersion {
		return Config{}, Error{
			Path: path,
			Msg:  fmt.Sprintf("unsupported config version %d (this omnishell understands version %d)", version, SchemaVersion),
		}
	}

	out := Config{
		Omnishell: OmnishellSection{Version: version, Shells: append([]string(nil), r.Omnishell.Shells...)},
		Modules:   make(map[string]ModuleConfig, len(r.Modules)),
	}
	for id, m := range r.Modules {
		opts := map[string]any{}
		for k, v := range m.Options {
			opts[k] = v
		}
		out.Modules[id] = ModuleConfig{Enabled: m.Enabled, Options: opts}
	}
	return out, nil
}
