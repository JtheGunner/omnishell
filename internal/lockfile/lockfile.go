// Package lockfile reads and writes ~/.config/omnishell/state.lock.json,
// the machine-managed record of the last apply.
package lockfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
)

// SchemaVersion is the lockfile schema this build writes and accepts.
const SchemaVersion = 1

// PackageState records one package a module depends on.
type PackageState struct {
	Name                 string `json:"name"`
	Manager              string `json:"manager"`
	InstalledByOmnishell bool   `json:"installed_by_omnishell"`
}

// ModuleState is the recorded state of one module after apply.
type ModuleState struct {
	ModuleVersion  string         `json:"module_version"`
	Enabled        bool           `json:"enabled"`
	OptionsHash    string         `json:"options_hash"`
	ShellsRendered []string       `json:"shells_rendered"`
	Packages       []PackageState `json:"packages"`
	VendorPaths    []string       `json:"vendor_paths"`
	Status         string         `json:"status"`
}

// FileState records an init file's path and content hash.
type FileState struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
}

// RCState records whether the marker block is present in an rc file.
type RCState struct {
	Path         string `json:"path"`
	BlockPresent bool   `json:"block_present"`
}

// Lock is the whole state.lock.json document.
type Lock struct {
	Schema           int                    `json:"schema"`
	OmnishellVersion string                 `json:"omnishell_version"`
	LastApply        string                 `json:"last_apply"`
	Platform         string                 `json:"platform"`
	PackageManager   string                 `json:"package_manager"`
	Modules          map[string]ModuleState `json:"modules"`
	InitFiles        map[string]FileState   `json:"init_files"`
	RCFiles          map[string]RCState     `json:"rc_files"`
}

func empty() Lock {
	return Lock{
		Schema:    SchemaVersion,
		Modules:   map[string]ModuleState{},
		InitFiles: map[string]FileState{},
		RCFiles:   map[string]RCState{},
	}
}

// Load reads the lockfile. A missing file yields an initialised empty Lock
// and exists=false.
func Load(path string) (Lock, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return empty(), false, nil
		}
		return Lock{}, false, fmt.Errorf("read lockfile: %w", err)
	}
	var l Lock
	if err := json.Unmarshal(data, &l); err != nil {
		return Lock{}, false, fmt.Errorf("parse lockfile %s: %w", path, err)
	}
	if l.Schema != SchemaVersion {
		return Lock{}, false, fmt.Errorf("lockfile %s has schema %d, expected %d", path, l.Schema, SchemaVersion)
	}
	if l.Modules == nil {
		l.Modules = map[string]ModuleState{}
	}
	if l.InitFiles == nil {
		l.InitFiles = map[string]FileState{}
	}
	if l.RCFiles == nil {
		l.RCFiles = map[string]RCState{}
	}
	return l, true, nil
}

// Write serialises the lock atomically.
func (l Lock) Write(path string) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	return atomicfile.WriteFile(path, append(data, '\n'), 0o644)
}

// InstalledPackages returns only the packages omnishell installed for a module.
func (l Lock) InstalledPackages(moduleID string) []PackageState {
	var out []PackageState
	for _, p := range l.Modules[moduleID].Packages {
		if p.InstalledByOmnishell {
			out = append(out, p)
		}
	}
	return out
}
