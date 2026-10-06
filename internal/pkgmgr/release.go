package pkgmgr

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/JtheGunner/omnishell/internal/module"
)

// MaxReleaseBytes caps both a release download and the file extracted from it.
const MaxReleaseBytes int64 = 512 << 20

// ReleaseContext locates a release install: the vendor directory and the host
// OS and architecture used to pick an asset.
type ReleaseContext struct {
	VendorDir string
	OS        string
	Arch      string
}

// BinPath is where a release fallback's binary is installed.
func (c ReleaseContext) BinPath(fb module.Fallback) string {
	return filepath.Join(c.VendorDir, "bin", fb.Bin)
}

// ReleaseInstalled reports whether the fallback's binary exists. It has no side
// effects.
func ReleaseInstalled(fb module.Fallback, c ReleaseContext) bool {
	info, err := os.Stat(c.BinPath(fb))
	return err == nil && info.Mode().IsRegular()
}

// ReleaseInstall describes a finished install: where the binary landed and the
// checksum of the asset it came from.
type ReleaseInstall struct {
	BinPath string
	SHA256  string
}

// InstallRelease downloads the asset for ctx.OS/ctx.Arch, verifies its SHA-256,
// extracts the binary and renames it into place. Any failure leaves a previous
// binary untouched and removes every temp file.
func InstallRelease(fb module.Fallback, ctx ReleaseContext, d Downloader) (ReleaseInstall, error) {
	if fb.Type != "release" {
		return ReleaseInstall{}, fmt.Errorf("unsupported fallback type %q", fb.Type)
	}
	if d == nil {
		return ReleaseInstall{}, errors.New("no downloader configured")
	}
	asset, ok := fb.AssetFor(ctx.OS, ctx.Arch)
	if !ok {
		return ReleaseInstall{}, fmt.Errorf("no release asset for %s/%s", ctx.OS, ctx.Arch)
	}
	url, err := asset.RenderURL(fb.Ref)
	if err != nil {
		return ReleaseInstall{}, fmt.Errorf("render asset url: %w", err)
	}
	binPath := ctx.BinPath(fb)
	binDir := filepath.Dir(binPath)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return ReleaseInstall{}, fmt.Errorf("create %s: %w", binDir, err)
	}
	archive, err := downloadVerified(d, url, asset, ctx.VendorDir)
	if err != nil {
		return ReleaseInstall{}, err
	}
	defer func() { _ = os.Remove(archive) }()
	if err := writeBinary(archive, asset, binDir, binPath); err != nil {
		return ReleaseInstall{}, err
	}
	return ReleaseInstall{BinPath: binPath, SHA256: asset.SHA256}, nil
}

// downloadVerified downloads url into a temp file under dir, hashing it on the
// way, and returns the file's path once the checksum matches the asset's pin.
func downloadVerified(d Downloader, url string, asset module.Asset, dir string) (string, error) {
	tmp, err := os.CreateTemp(dir, ".release-download-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	name := tmp.Name()
	fail := func(err error) (string, error) {
		_ = os.Remove(name)
		return "", err
	}
	hasher := sha256.New()
	downloadErr := d.Download(url, io.MultiWriter(tmp, hasher), MaxReleaseBytes)
	closeErr := tmp.Close()
	if downloadErr != nil {
		return fail(fmt.Errorf("download %s: %w", url, downloadErr))
	}
	if closeErr != nil {
		return fail(fmt.Errorf("close %s: %w", name, closeErr))
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != asset.SHA256 {
		return fail(fmt.Errorf("checksum mismatch for %s/%s (expected %s, got %s)", asset.OS, asset.Arch, asset.SHA256, got))
	}
	return name, nil
}

// writeBinary writes the asset's binary to a temp file next to binPath, marks
// it executable and renames it over binPath.
func writeBinary(archive string, asset module.Asset, binDir, binPath string) error {
	tmp, err := os.CreateTemp(binDir, ".release-bin-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // no-op once the rename succeeded
	writeErr := copyOut(archive, asset, tmp)
	closeErr := tmp.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", name, closeErr)
	}
	if err := os.Chmod(name, 0o755); err != nil {
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	if err := os.Rename(name, binPath); err != nil {
		return fmt.Errorf("install %s: %w", binPath, err)
	}
	return nil
}

func copyOut(archive string, asset module.Asset, w io.Writer) error {
	if format := asset.Archive(); format != "" {
		return extractMember(archive, format, asset.Member, w, MaxReleaseBytes)
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return copyLimited(w, f, MaxReleaseBytes)
}
