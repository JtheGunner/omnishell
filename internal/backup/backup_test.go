package backup_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/backup"
)

func fixedTime() time.Time {
	return time.Date(2026, 9, 2, 22, 41, 3, 0, time.UTC)
}

func TestSaveCopiesFileIntoTimestampedDir(t *testing.T) {
	cfgDir := t.TempDir()
	src := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(src, []byte("rc contents"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := backup.NewSession(cfgDir, fixedTime())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	saved, err := s.Save(src)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	want := filepath.Join(cfgDir, "backups", "20260902T224103Z", ".zshrc")
	if saved != want {
		t.Fatalf("saved to %q, want %q", saved, want)
	}
	got, _ := os.ReadFile(saved)
	if string(got) != "rc contents" {
		t.Fatalf("backup content = %q", got)
	}
}

func TestSaveMissingFileIsNoop(t *testing.T) {
	s, err := backup.NewSession(t.TempDir(), fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Save(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Save of missing file: %v", err)
	}
	if saved != "" {
		t.Fatalf("saved = %q, want empty string", saved)
	}
}

func TestSaveRecordsEntryForMissingFile(t *testing.T) {
	s, err := backup.NewSession(t.TempDir(), fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := s.Save(missing); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.WriteManifest("apply", fixedTime()); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	m, err := backup.ReadManifest(s.Dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(m.Files) != 1 {
		t.Fatalf("Files = %+v, want 1 entry", m.Files)
	}
	f := m.Files[0]
	if f.OriginalPath != missing || f.ExistedBefore || f.BackupName != "" {
		t.Fatalf("entry = %+v, want {OriginalPath:%q ExistedBefore:false BackupName:\"\"}", f, missing)
	}
}

func TestSaveRecordsEntryForExistingFile(t *testing.T) {
	cfgDir := t.TempDir()
	src := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(src, []byte("rc contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := backup.NewSession(cfgDir, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(src); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.WriteManifest("apply", fixedTime()); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	m, err := backup.ReadManifest(s.Dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(m.Files) != 1 {
		t.Fatalf("Files = %+v, want 1 entry", m.Files)
	}
	f := m.Files[0]
	if f.OriginalPath != src || !f.ExistedBefore || f.BackupName != ".zshrc" {
		t.Fatalf("entry = %+v, want {OriginalPath:%q ExistedBefore:true BackupName:\".zshrc\"}", f, src)
	}
	if m.Kind != "apply" || m.Schema != backup.SchemaVersion {
		t.Fatalf("manifest header wrong: %+v", m)
	}
}

func TestSaveAccumulatesAcrossMultipleCalls(t *testing.T) {
	cfgDir := t.TempDir()
	srcDir := t.TempDir()
	a := filepath.Join(srcDir, "a")
	b := filepath.Join(srcDir, "b")
	if err := os.WriteFile(a, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := backup.NewSession(cfgDir, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(b); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteManifest("uninstall", fixedTime()); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	m, err := backup.ReadManifest(s.Dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(m.Files) != 2 {
		t.Fatalf("Files = %+v, want 2 entries (accumulation bug if only 1)", m.Files)
	}
}

func TestReadManifestMissingReturnsError(t *testing.T) {
	if _, err := backup.ReadManifest(t.TempDir()); err == nil {
		t.Fatal("ReadManifest on a dir with no manifest.json: want error, got nil")
	}
}
