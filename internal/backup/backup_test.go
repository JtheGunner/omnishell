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
