package module

import (
	"errors"
	"io/fs"
	"path"
)

// Source says where a module came from.
type Source string

const (
	SourceBuiltin Source = "builtin"
	SourceUser    Source = "user"
)

// Module is a manifest plus access to its folder contents.
type Module struct {
	Manifest Manifest
	FS       fs.FS  // rooted at the module folder
	Root     string // display path
	Source   Source
}

// Template returns the body of <shell>.tmpl, or ("", false, nil) if absent.
func (m Module) Template(shell string) (string, bool, error) {
	data, err := fs.ReadFile(m.FS, shell+".tmpl")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(data), true, nil
}

// HasHook reports whether hooks/<name>.sh exists.
func (m Module) HasHook(name string) bool {
	_, err := fs.Stat(m.FS, path.Join("hooks", name+".sh"))
	return err == nil
}

// ReadHook returns the bytes of hooks/<name>.sh.
func (m Module) ReadHook(name string) ([]byte, error) {
	return fs.ReadFile(m.FS, path.Join("hooks", name+".sh"))
}
