package module

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Registry is the merged set of built-in and user modules.
type Registry struct {
	byID      map[string]Module
	overrides map[string]bool
}

// LoadRegistry walks the built-in FS and the user modules directory.
// Each top-level folder in either location must contain a valid manifest.toml
// whose module.id equals the folder name; a user module with the same id as a
// built-in overrides it and is recorded in Overrides. builtin may be nil.
func LoadRegistry(builtin fs.FS, userDir string) (Registry, error) {
	r := Registry{byID: map[string]Module{}, overrides: map[string]bool{}}

	if builtin != nil {
		if err := loadFrom(builtin, SourceBuiltin, "<builtin>", r.byID, nil); err != nil {
			return Registry{}, err
		}
	}
	if userDir != "" {
		if info, err := os.Stat(userDir); err == nil && info.IsDir() {
			existed := map[string]bool{}
			for id := range r.byID {
				existed[id] = true
			}
			if err := loadFrom(os.DirFS(userDir), SourceUser, userDir, r.byID, func(id string) {
				if existed[id] {
					r.overrides[id] = true
				}
			}); err != nil {
				return Registry{}, err
			}
		}
	}
	return r, nil
}

func loadFrom(fsys fs.FS, src Source, display string, into map[string]Module, onLoad func(string)) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("read modules dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		folder := e.Name()
		sub, err := fs.Sub(fsys, folder)
		if err != nil {
			return err
		}
		data, err := fs.ReadFile(sub, "manifest.toml")
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("module %q: missing manifest.toml", folder)
			}
			return fmt.Errorf("module %q: reading manifest.toml: %w", folder, err)
		}
		mf, err := ParseManifest(data)
		if err != nil {
			return fmt.Errorf("module %q: %w", folder, err)
		}
		if err := ValidateManifest(mf); err != nil {
			return fmt.Errorf("module %q: %w", folder, err)
		}
		if mf.Module.ID != folder {
			return fmt.Errorf("module folder %q declares id %q; they must match", folder, mf.Module.ID)
		}
		into[folder] = Module{
			Manifest: mf,
			FS:       sub,
			Root:     filepath.Join(display, folder),
			Source:   src,
		}
		if onLoad != nil {
			onLoad(folder)
		}
	}
	return nil
}

// All returns every module, sorted by id.
func (r Registry) All() []Module {
	out := make([]Module, 0, len(r.byID))
	for _, m := range r.byID {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Manifest.Module.ID < out[j].Manifest.Module.ID
	})
	return out
}

// Get returns the module with the given id.
func (r Registry) Get(id string) (Module, bool) {
	m, ok := r.byID[id]
	return m, ok
}

// Overrides lists ids where a user module shadows a built-in, sorted.
func (r Registry) Overrides() []string {
	out := make([]string, 0, len(r.overrides))
	for id := range r.overrides {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
