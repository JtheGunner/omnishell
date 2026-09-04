// Package atomicfile writes files atomically: a temp file in the destination
// directory is written, flushed, chmod-ed, then renamed over the target.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile atomically writes data to path with the given permissions,
// creating parent directories (0o755) as needed.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op if the rename succeeded

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // best-effort: the write error above is what we report
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close() // best-effort: the sync error above is what we report
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file over %s: %w", path, err)
	}
	return nil
}
