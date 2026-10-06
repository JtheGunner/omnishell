// Package install tests install.sh end to end against a stub curl, so no test
// touches the network.
package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubCurl answers the three kinds of request install.sh makes: a HEAD request
// for the latest-release redirect, and downloads from a fixture directory. It
// logs every URL it is asked for.
const stubCurl = `#!/bin/sh
url=""
out=""
head=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
    -I|-sI|-sSI|-fsSI) head=1 ;;
    http*) url="$1" ;;
  esac
  shift
done
echo "$url" >> "$STUB_LOG"
if [ -n "$head" ]; then
  printf 'HTTP/2 %s\r\n' "$STUB_LATEST_STATUS"
  [ -z "$STUB_LATEST_LOCATION" ] || printf 'Location: %s\r\n' "$STUB_LATEST_LOCATION"
  printf '\r\n'
  exit 0
fi
cp "$STUB_FIXTURES/$(basename "$url")" "$out"
`

// stubUname reports STUB_UNAME_M / STUB_UNAME_S for `uname -m` / `uname -s` and
// defers to the real uname otherwise, so a test can pretend to be another machine.
const stubUname = `#!/bin/sh
if [ "$1" = "-m" ] && [ -n "${STUB_UNAME_M:-}" ]; then echo "$STUB_UNAME_M"; exit 0; fi
if [ "$1" = "-s" ] && [ -n "${STUB_UNAME_S:-}" ]; then echo "$STUB_UNAME_S"; exit 0; fi
exec /usr/bin/uname "$@"
`

type fixture struct {
	binDir     string
	home       string
	fixtures   string
	logFile    string
	tarball    string
	installDir string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	arch := map[string]string{"amd64": "amd64", "arm64": "arm64"}[runtime.GOARCH]
	if arch == "" || (runtime.GOOS != "linux" && runtime.GOOS != "darwin") {
		t.Skipf("install.sh does not support %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	root := t.TempDir()
	f := &fixture{
		binDir:     filepath.Join(root, "stubbin"),
		home:       filepath.Join(root, "home"),
		fixtures:   filepath.Join(root, "fixtures"),
		logFile:    filepath.Join(root, "curl.log"),
		tarball:    fmt.Sprintf("omnishell_%s_%s.tar.gz", runtime.GOOS, arch),
		installDir: filepath.Join(root, "installed"),
	}
	for _, dir := range []string{f.binDir, f.home, f.fixtures} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.binDir, "curl"), []byte(stubCurl), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(f.binDir, "uname"), []byte(stubUname), 0o755); err != nil {
		t.Fatal(err)
	}

	archive := buildTarball(t)
	if err := os.WriteFile(filepath.Join(f.fixtures, f.tarball), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), f.tarball)
	if err := os.WriteFile(filepath.Join(f.fixtures, "checksums.txt"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

// addArchive makes the stub server answer for another archive name, with its
// checksum appended to checksums.txt.
func (f *fixture) addArchive(t *testing.T, name string) {
	t.Helper()
	archive := buildTarball(t)
	if err := os.WriteFile(filepath.Join(f.fixtures, name), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), name)
	sums := filepath.Join(f.fixtures, "checksums.txt")
	existing, err := os.ReadFile(sums)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sums, append(existing, line...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildTarball(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho stub\n")
	if err := tw.WriteHeader(&tar.Header{Name: "omnishell", Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// run executes install.sh and returns its combined output, the URLs the stub
// curl was asked for and the exit error (nil on success).
func (f *fixture) run(t *testing.T, env ...string) (string, []string, error) {
	t.Helper()
	script, err := filepath.Abs("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	cmd.Env = append([]string{
		"PATH=" + f.binDir + ":" + os.Getenv("PATH"),
		"HOME=" + f.home,
		"OMNISHELL_BIN_DIR=" + f.installDir,
		"STUB_LOG=" + f.logFile,
		"STUB_FIXTURES=" + f.fixtures,
	}, env...)
	out, runErr := cmd.CombinedOutput()

	var urls []string
	if raw, err := os.ReadFile(f.logFile); err == nil {
		urls = strings.Fields(string(raw))
	}
	return string(out), urls, runErr
}

func mustNotContainAPI(t *testing.T, urls []string) {
	t.Helper()
	for _, u := range urls {
		if strings.Contains(u, "api.github.com") {
			t.Errorf("install.sh requested the GitHub API: %s", u)
		}
	}
}

func TestInstall_ResolvesLatestTagFromRedirect(t *testing.T) {
	f := newFixture(t)
	out, urls, err := f.run(t,
		"STUB_LATEST_STATUS=302",
		"STUB_LATEST_LOCATION=https://github.com/JtheGunner/omnishell/releases/tag/v1.2.3",
	)
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	mustNotContainAPI(t, urls)
	want := "https://github.com/JtheGunner/omnishell/releases/download/v1.2.3/" + f.tarball
	if !contains(urls, want) {
		t.Errorf("expected download of %s, got %v", want, urls)
	}
	if _, err := os.Stat(filepath.Join(f.installDir, "omnishell")); err != nil {
		t.Errorf("binary not installed: %v", err)
	}
	if !strings.Contains(out, "checksum OK") {
		t.Errorf("checksum was not verified:\n%s", out)
	}
}

func TestInstall_VersionOverrideSkipsLookup(t *testing.T) {
	for _, version := range []string{"v0.5.0", "0.5.0"} {
		t.Run(version, func(t *testing.T) {
			f := newFixture(t)
			out, urls, err := f.run(t, "OMNISHELL_VERSION="+version)
			if err != nil {
				t.Fatalf("install.sh failed: %v\n%s", err, out)
			}
			mustNotContainAPI(t, urls)
			for _, u := range urls {
				if strings.HasSuffix(u, "/releases/latest") {
					t.Errorf("latest lookup was not skipped: %s", u)
				}
			}
			want := "https://github.com/JtheGunner/omnishell/releases/download/v0.5.0/" + f.tarball
			if !contains(urls, want) {
				t.Errorf("expected download of %s, got %v", want, urls)
			}
		})
	}
}

func TestInstall_LatestLookupFailureNamesHTTPCause(t *testing.T) {
	for _, status := range []string{"403", "429"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t)
			out, _, err := f.run(t, "STUB_LATEST_STATUS="+status)
			if err == nil {
				t.Fatalf("expected failure, got success:\n%s", out)
			}
			for _, want := range []string{"could not determine latest release", "HTTP " + status, "OMNISHELL_VERSION"} {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestInstall_MissingLocationHeaderFails(t *testing.T) {
	f := newFixture(t)
	out, _, err := f.run(t, "STUB_LATEST_STATUS=200")
	if err == nil {
		t.Fatalf("expected failure, got success:\n%s", out)
	}
	if !strings.Contains(out, "could not determine latest release") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestInstall_ChecksumMismatchStillAborts(t *testing.T) {
	f := newFixture(t)
	bad := strings.Repeat("0", 64) + "  " + f.tarball + "\n"
	if err := os.WriteFile(filepath.Join(f.fixtures, "checksums.txt"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err := f.run(t, "OMNISHELL_VERSION=v0.5.0")
	if err == nil {
		t.Fatalf("expected checksum failure, got success:\n%s", out)
	}
	if !strings.Contains(out, "checksum verification failed") {
		t.Errorf("unexpected output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(f.installDir, "omnishell")); err == nil {
		t.Error("binary was installed despite checksum mismatch")
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestInstall_MapsMachineToArchive(t *testing.T) {
	cases := map[string]string{
		"armv6l":  "omnishell_linux_armv6.tar.gz",
		"armv7l":  "omnishell_linux_armv7.tar.gz",
		"armv8l":  "omnishell_linux_armv7.tar.gz",
		"x86_64":  "omnishell_linux_amd64.tar.gz",
		"aarch64": "omnishell_linux_arm64.tar.gz",
	}
	for machine, archive := range cases {
		t.Run(machine, func(t *testing.T) {
			f := newFixture(t)
			f.addArchive(t, archive)
			out, urls, err := f.run(t, "OMNISHELL_VERSION=v9.9.9", "STUB_UNAME_S=Linux", "STUB_UNAME_M="+machine)
			if err != nil {
				t.Fatalf("install.sh failed: %v\n%s", err, out)
			}
			want := "https://github.com/JtheGunner/omnishell/releases/download/v9.9.9/" + archive
			if !contains(urls, want) {
				t.Fatalf("no request for %s; requested %v", want, urls)
			}
		})
	}
}

func TestInstall_RefusesUnsupportedMachines(t *testing.T) {
	for _, machine := range []string{"armv5tel", "armv7", "arm", "mips"} {
		t.Run(machine, func(t *testing.T) {
			f := newFixture(t)
			out, _, err := f.run(t, "OMNISHELL_VERSION=v9.9.9", "STUB_UNAME_S=Linux", "STUB_UNAME_M="+machine)
			if err == nil || !strings.Contains(out, "unsupported arch: "+machine) {
				t.Fatalf("err = %v, output = %q, want an unsupported-arch failure", err, out)
			}
		})
	}
}

func TestInstall_RefusesArm32OnMacOS(t *testing.T) {
	f := newFixture(t)
	out, _, err := f.run(t, "OMNISHELL_VERSION=v9.9.9", "STUB_UNAME_S=Darwin", "STUB_UNAME_M=armv7l")
	if err == nil || !strings.Contains(out, "unsupported OS for armv7") {
		t.Fatalf("err = %v, output = %q, want an unsupported-OS failure", err, out)
	}
}
