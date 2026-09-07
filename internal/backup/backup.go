// Package backup copies files into a per-run timestamped directory under
// <configDir>/backups/ before omnishell overwrites them, and records what it
// copied in a manifest.json so a later rollback can invert the change.
package backup

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
	"github.com/JtheGunner/omnishell/internal/buildinfo"
)

// SchemaVersion is the manifest schema this build writes and accepts.
const SchemaVersion = 1

// FileEntry records one file a session touched: where it lived, whether it
// existed before the session's run started, and (if it did) the name it was
// copied under inside the session directory.
type FileEntry struct {
	OriginalPath  string `json:"original_path"`
	ExistedBefore bool   `json:"existed_before"`
	BackupName    string `json:"backup_name,omitempty"`
}

// Manifest is the <session-dir>/manifest.json document: which run created
// the session and which files it captured.
type Manifest struct {
	Schema           int         `json:"schema"`
	Kind             string      `json:"kind"`
	CreatedAt        string      `json:"created_at"`
	OmnishellVersion string      `json:"omnishell_version"`
	Files            []FileEntry `json:"files"`
}

// Session is one backup directory for a single omnishell invocation.
type Session struct {
	Dir     string
	entries []FileEntry
}

// NewSession computes (but does not yet create) the backup directory
// <configDir>/backups/<RFC3339-basic-utc>/.
func NewSession(configDir string, now time.Time) (*Session, error) {
	stamp := now.UTC().Format("20060102T150405Z")
	return &Session{Dir: filepath.Join(configDir, "backups", stamp)}, nil
}

// Save copies path into the session directory, keeping its base name, and
// records the attempt as a FileEntry regardless of outcome. A missing source
// file is not an error: it is recorded with ExistedBefore=false, so a later
// rollback knows to delete rather than overwrite that path.
func (s *Session) Save(path string) (string, error) {
	in, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.entries = append(s.entries, FileEntry{OriginalPath: path, ExistedBefore: false})
			return "", nil
		}
		return "", fmt.Errorf("open %s for backup: %w", path, err)
	}
	defer func() { _ = in.Close() }() // read-only source; nothing to act on

	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir %s: %w", s.Dir, err)
	}
	base := filepath.Base(path)
	dst := filepath.Join(s.Dir, base)
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
	s.entries = append(s.entries, FileEntry{OriginalPath: path, ExistedBefore: true, BackupName: base})
	return dst, nil
}

// HasEntries reports whether the session has recorded at least one file via
// Save. Callers use it to skip writing a manifest for a run that changed
// nothing.
func (s *Session) HasEntries() bool {
	return len(s.entries) > 0
}

// WriteManifest writes <Dir>/manifest.json recording every file this session
// has captured via Save so far. Safe to call even after a partial run — it
// reflects whatever entries were recorded before the caller returned, so a
// run that fails partway through still leaves a manifest describing what it
// actually changed.
func (s *Session) WriteManifest(kind string, now time.Time) error {
	m := Manifest{
		Schema:           SchemaVersion,
		Kind:             kind,
		CreatedAt:        now.UTC().Format(time.RFC3339),
		OmnishellVersion: buildinfo.Version,
		Files:            s.entries,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return atomicfile.WriteFile(filepath.Join(s.Dir, "manifest.json"), append(data, '\n'), 0o644)
}

// ReadManifest reads and parses <sessionDir>/manifest.json.
func ReadManifest(sessionDir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(sessionDir, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest %s: %w", sessionDir, err)
	}
	return m, nil
}
