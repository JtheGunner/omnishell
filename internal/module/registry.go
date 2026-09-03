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
	malformed map[string]string // "display/path" -> reason
}

// LoadRegistry walks the built-in FS and the user modules directory.
// Each top-level folder in either location must contain a valid manifest.toml
// whose module.id equals the folder name; a user module with the same id as a
// built-in overrides it and is recorded in Overrides. builtin may be nil.
//
// A malformed built-in module is a hard error (they are compiled in — a bad one
// is a build bug). A malformed USER module dir (no manifest.toml, invalid
// manifest, folder/id mismatch) is skipped and recorded in Malformed() so one
// bad hand-written module cannot brick every command.
func LoadRegistry(builtin fs.FS, userDir string) (Registry, error) {
	r := Registry{
		byID:      map[string]Module{},
		overrides: map[string]bool{},
		malformed: map[string]string{},
	}

	if builtin != nil {
		if err := loadFrom(builtin, SourceBuiltin, "<builtin>", r.byID, nil, nil); err != nil {
			return Registry{}, err
		}
	}
	if userDir != "" {
		info, err := os.Stat(userDir)
		switch {
		case err == nil && info.IsDir():
			existed := map[string]bool{}
			for id := range r.byID {
				existed[id] = true
			}
			onLoad := func(id string) {
				if existed[id] {
					r.overrides[id] = true
				}
			}
			onMalformed := func(path, reason string) { r.malformed[path] = reason }
			if err := loadFrom(os.DirFS(userDir), SourceUser, userDir, r.byID, onLoad, onMalformed); err != nil {
				return Registry{}, err
			}
		case err == nil && !info.IsDir():
			return Registry{}, fmt.Errorf("user modules path %q is not a directory", userDir)
		case errors.Is(err, os.ErrNotExist):
			// No user modules dir at all — fine.
		default:
			return Registry{}, fmt.Errorf("stat user modules dir %q: %w", userDir, err)
		}
	}
	return r, nil
}

// loadFrom walks fsys's top-level folders. When onMalformed is non-nil a folder
// that fails to load is reported through it and skipped; when it is nil the
// first such failure is returned as an error (built-in modules).
func loadFrom(fsys fs.FS, src Source, display string, into map[string]Module,
	onLoad func(string), onMalformed func(path, reason string)) error {

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("read modules dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		folder := e.Name()
		bad := func(reason string) error {
			if onMalformed != nil {
				onMalformed(filepath.Join(display, folder), reason)
				return nil
			}
			return fmt.Errorf("module %q: %s", folder, reason)
		}

		sub, err := fs.Sub(fsys, folder)
		if err != nil {
			if berr := bad(err.Error()); berr != nil {
				return berr
			}
			continue
		}
		data, err := fs.ReadFile(sub, "manifest.toml")
		if err != nil {
			reason := "reading manifest.toml: " + err.Error()
			if errors.Is(err, fs.ErrNotExist) {
				reason = "missing manifest.toml"
			}
			if berr := bad(reason); berr != nil {
				return berr
			}
			continue
		}
		mf, err := ParseManifest(data)
		if err != nil {
			if berr := bad(err.Error()); berr != nil {
				return berr
			}
			continue
		}
		if err := ValidateManifest(mf); err != nil {
			if berr := bad(err.Error()); berr != nil {
				return berr
			}
			continue
		}
		if mf.Module.ID != folder {
			if berr := bad(fmt.Sprintf("folder %q declares id %q; they must match", folder, mf.Module.ID)); berr != nil {
				return berr
			}
			continue
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

// Malformed lists "<path>: <reason>" for every user module dir that was skipped
// because it could not be loaded, sorted by path.
func (r Registry) Malformed() []string {
	paths := make([]string, 0, len(r.malformed))
	for p := range r.malformed {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = p + ": " + r.malformed[p]
	}
	return out
}
