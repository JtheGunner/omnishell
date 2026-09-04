// Package backup copies files into a per-run timestamped directory under
// <configDir>/backups/ before omnishell overwrites them.
package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Session is one backup directory for a single omnishell invocation.
type Session struct {
	Dir string
}

// NewSession computes (but does not yet create) the backup directory
// <configDir>/backups/<RFC3339-basic-utc>/.
func NewSession(configDir string, now time.Time) (Session, error) {
	stamp := now.UTC().Format("20060102T150405Z")
	return Session{Dir: filepath.Join(configDir, "backups", stamp)}, nil
}

// Save copies path into the session directory, keeping its base name.
// A missing source file is not an error and returns ("", nil).
func (s Session) Save(path string) (string, error) {
	in, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("open %s for backup: %w", path, err)
	}
	defer func() { _ = in.Close() }() // read-only source; nothing to act on

	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir %s: %w", s.Dir, err)
	}
	dst := filepath.Join(s.Dir, filepath.Base(path))
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("create backup file %s: %w", dst, err)
	}
	defer func() { _ = out.Close() }() // best-effort; the checked Close below covers the success path
	if _, err := io.Copy(out, in); err != nil {
		return "", fmt.Errorf("copy to backup %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close backup file %s: %w", dst, err)
	}
	return dst, nil
}
