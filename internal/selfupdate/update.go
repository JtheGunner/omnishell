// Package selfupdate replaces the running omnishell binary with the latest
// GitHub release, verifying the archive against checksums.txt first. Every
// outside dependency (network, executable path, host platform) is injected so
// tests never touch the real binary or the network.
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

// ReleasesURL is the releases page of the omnishell repository.
const ReleasesURL = "https://github.com/JtheGunner/omnishell/releases"

// BrewUpgradeCommand is what Homebrew installs are pointed to.
const BrewUpgradeCommand = "brew upgrade jthegunner/tap/omnishell"

const (
	binaryName       = "omnishell"
	maxChecksumBytes = 1 << 20
)

// ErrUnknownVersion reports that the running binary has no parseable version,
// so it cannot be compared with a release.
var ErrUnknownVersion = errors.New("current version is unknown")

// Updater holds everything a self-update needs.
type Updater struct {
	// Version is the running binary's version (buildinfo.Version).
	Version string
	// GOOS, GOARCH and GoARM describe the host; GoARM is the raw GOARM setting.
	GOOS, GOARCH, GoARM string
	// ReleasesURL is the releases page, without a trailing slash.
	ReleasesURL string
	Resolver    LatestResolver
	Downloader  pkgmgr.Downloader
	// Executable returns the symlink-resolved path of the running binary.
	Executable func() (string, error)
	Stdout     io.Writer
}

// ResolveExecutable returns the running binary's path with symlinks resolved.
func ResolveExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate running binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", exe, err)
	}
	return resolved, nil
}

// Check reports whether a newer release exists. It never replaces anything.
func (u Updater) Check() error {
	latest, err := u.Resolver.Latest()
	if err != nil {
		return err
	}
	current, curErr := parseSemver(u.Version)
	if curErr != nil {
		u.printf("current version %q is unknown; latest release is %s\n", u.Version, latest)
		return nil
	}
	newer, err := isNewer(current, latest)
	if err != nil {
		return err
	}
	if newer {
		u.printf("update available: current %s, latest %s; run 'omnishell self-update'\n", u.Version, latest)
		return nil
	}
	u.printf("omnishell is already up to date (current %s, latest %s)\n", u.Version, latest)
	return nil
}

// Update replaces the running binary with the latest release when that is
// strictly newer. On any error the old binary is left untouched.
func (u Updater) Update() error {
	exe, err := u.Executable()
	if err != nil {
		return err
	}
	if IsHomebrewPath(exe) {
		u.printf("omnishell was installed with Homebrew; update it with: %s\n", BrewUpgradeCommand)
		return nil
	}
	current, curErr := parseSemver(u.Version)
	if curErr != nil {
		return fmt.Errorf("%w (%q): self-update only works on release builds; 'omnishell self-update --check' still shows the latest release", ErrUnknownVersion, u.Version)
	}
	latest, err := u.Resolver.Latest()
	if err != nil {
		return err
	}
	newer, err := isNewer(current, latest)
	if err != nil {
		return err
	}
	if !newer {
		u.printf("omnishell is already up to date (current %s, latest %s)\n", u.Version, latest)
		return nil
	}
	archive, err := archiveName(u.GOOS, u.GOARCH, u.GoARM)
	if err != nil {
		return err
	}
	if err := u.replace(exe, latest, archive); err != nil {
		return err
	}
	u.printf("updated omnishell from %s to %s (%s)\n", u.Version, latest, exe)
	return nil
}

func (u Updater) replace(exe, tag, archive string) error {
	info, err := os.Stat(exe)
	if err != nil {
		return fmt.Errorf("stat %s: %w", exe, err)
	}
	// The temp file next to the binary doubles as the writability probe, so a
	// read-only directory fails before anything is downloaded.
	dir := filepath.Dir(exe)
	staged, err := os.CreateTemp(dir, ".omnishell-update-*")
	if err != nil {
		return fmt.Errorf("%s is not writable: %w; run omnishell with enough permissions (it never uses sudo)", dir, err)
	}
	stagedName := staged.Name()
	defer func() { _ = os.Remove(stagedName) }() // no-op once the rename succeeded
	defer func() { _ = staged.Close() }()

	work, err := os.MkdirTemp("", "omnishell-update-*")
	if err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	base := u.ReleasesURL + "/download/" + tag + "/"
	var sums strings.Builder
	if err := u.Downloader.Download(base+"checksums.txt", &sums, maxChecksumBytes); err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}
	want, err := expectedChecksum(sums.String(), archive)
	if err != nil {
		return err
	}
	archivePath, err := u.downloadVerified(base+archive, want, work)
	if err != nil {
		return err
	}
	if err := extractBinary(archivePath, binaryName, staged, pkgmgr.MaxReleaseBytes); err != nil {
		return err
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("close %s: %w", stagedName, err)
	}
	if err := os.Chmod(stagedName, info.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod %s: %w", stagedName, err)
	}
	if err := os.Rename(stagedName, exe); err != nil {
		return fmt.Errorf("replace %s: %w", exe, err)
	}
	return nil
}

func (u Updater) downloadVerified(url, want, dir string) (string, error) {
	path := filepath.Join(dir, "archive.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", path, err)
	}
	hasher := sha256.New()
	dlErr := u.Downloader.Download(url, io.MultiWriter(f, hasher), pkgmgr.MaxReleaseBytes)
	closeErr := f.Close()
	if dlErr != nil {
		return "", fmt.Errorf("download %s: %w", url, dlErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close %s: %w", path, closeErr)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != want {
		return "", fmt.Errorf("%w for %s (expected %s, got %s)", pkgmgr.ErrChecksumMismatch, url, want, got)
	}
	return path, nil
}

func isNewer(current semver, latestTag string) (bool, error) {
	latest, err := parseSemver(latestTag)
	if err != nil {
		return false, fmt.Errorf("latest release tag: %w", err)
	}
	return latest.compare(current) > 0, nil
}

func (u Updater) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(u.Stdout, format, args...)
}
