package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

const (
	testReleases = "https://example.test/releases"
	oldContent   = "old binary"
	newContent   = "#!/bin/sh\necho new\n"
)

type fakeResolver struct {
	tag string
	err error
}

func (f fakeResolver) Latest() (string, error) { return f.tag, f.err }

// fakeDownloader serves fixed bodies by URL and records every request.
type fakeDownloader struct {
	files map[string][]byte
	calls []string
}

func (f *fakeDownloader) Download(url string, dst io.Writer, _ int64) error {
	f.calls = append(f.calls, url)
	body, ok := f.files[url]
	if !ok {
		return fmt.Errorf("GET %s: 404 Not Found", url)
	}
	_, err := dst.Write(body)
	return err
}

type tarEntry struct {
	name string
	body string
}

func makeTarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

const testArchive = "omnishell_linux_amd64.tar.gz"

// fixture is a temp install dir with an "old" binary plus an updater wired to
// fakes. The release at v1.2.0 serves the given archive.
type fixture struct {
	exe string
	dl  *fakeDownloader
	out *bytes.Buffer
	u   Updater
}

func newFixture(t *testing.T, version string, archive []byte, checksums string) *fixture {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "omnishell")
	if err := os.WriteFile(exe, []byte(oldContent), 0o750); err != nil {
		t.Fatal(err)
	}
	base := testReleases + "/download/v1.2.0/"
	dl := &fakeDownloader{files: map[string][]byte{
		base + testArchive:     archive,
		base + "checksums.txt": []byte(checksums),
	}}
	out := &bytes.Buffer{}
	return &fixture{exe: exe, dl: dl, out: out, u: Updater{
		Version: version, GOOS: "linux", GOARCH: "amd64", ReleasesURL: testReleases,
		Resolver: fakeResolver{tag: "v1.2.0"}, Downloader: dl,
		Executable: func() (string, error) { return exe, nil }, Stdout: out,
	}}
}

func goodFixture(t *testing.T, version string) *fixture {
	t.Helper()
	archive := makeTarGz(t, tarEntry{"README.md", "readme"}, tarEntry{"omnishell", newContent})
	return newFixture(t, version, archive, sum(archive)+"  "+testArchive+"\n"+strings.Repeat("0", 64)+"  other.tar.gz\n")
}

func readExe(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertNoLeftovers(t *testing.T, exe string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("install dir has %d entries, want only the binary: %v", len(entries), entries)
	}
}

func TestUpdateReplacesBinaryAtomically(t *testing.T) {
	f := goodFixture(t, "1.1.0")
	if err := f.u.Update(); err != nil {
		t.Fatal(err)
	}
	if got := readExe(t, f.exe); got != newContent {
		t.Fatalf("binary content = %q, want new content", got)
	}
	info, err := os.Stat(f.exe)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("mode = %v, want the old binary's 0750", info.Mode().Perm())
	}
	assertNoLeftovers(t, f.exe)
	if !strings.Contains(f.out.String(), "1.1.0") || !strings.Contains(f.out.String(), "v1.2.0") {
		t.Fatalf("output %q should name both versions", f.out.String())
	}
}

func TestUpdateVersionComparison(t *testing.T) {
	tests := []struct {
		name, version string
		wantReplaced  bool
	}{
		{"older current updates", "1.1.9", true},
		{"older with v prefix updates", "v0.9.0", true},
		{"equal is up to date", "1.2.0", false},
		{"current ahead is up to date", "1.3.0", false},
		{"prerelease of latest updates", "1.2.0-rc.1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := goodFixture(t, tt.version)
			if err := f.u.Update(); err != nil {
				t.Fatal(err)
			}
			replaced := readExe(t, f.exe) == newContent
			if replaced != tt.wantReplaced {
				t.Fatalf("replaced = %v, want %v (output %q)", replaced, tt.wantReplaced, f.out.String())
			}
			if !tt.wantReplaced {
				if !strings.Contains(f.out.String(), "already up to date") {
					t.Fatalf("output %q lacks 'already up to date'", f.out.String())
				}
				if len(f.dl.calls) != 0 {
					t.Fatalf("up to date must not download, got %v", f.dl.calls)
				}
			}
		})
	}
}

func TestCheckReportsWithoutReplacing(t *testing.T) {
	tests := []struct {
		name, version, want string
	}{
		{"newer available", "1.1.0", "update available: current 1.1.0, latest v1.2.0"},
		{"up to date", "1.2.0", "already up to date (current 1.2.0, latest v1.2.0)"},
		{"ahead", "2.0.0", "already up to date"},
		{"dev build", "dev", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := goodFixture(t, tt.version)
			if err := f.u.Check(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(f.out.String(), tt.want) || !strings.Contains(f.out.String(), "v1.2.0") {
				t.Fatalf("output %q, want it to contain %q and the latest tag", f.out.String(), tt.want)
			}
			if got := readExe(t, f.exe); got != oldContent {
				t.Fatal("--check replaced the binary")
			}
			if len(f.dl.calls) != 0 {
				t.Fatalf("--check downloaded: %v", f.dl.calls)
			}
		})
	}
}

func TestCheckPropagatesLookupError(t *testing.T) {
	f := goodFixture(t, "1.1.0")
	f.u.Resolver = fakeResolver{err: errors.New("offline")}
	if err := f.u.Check(); err == nil {
		t.Fatal("want error")
	}
}

func TestUpdateRefusesDevBuild(t *testing.T) {
	for _, v := range []string{"dev", "", "not-a-version", "1.2"} {
		f := goodFixture(t, v)
		err := f.u.Update()
		if !errors.Is(err, ErrUnknownVersion) {
			t.Fatalf("version %q: err = %v, want ErrUnknownVersion", v, err)
		}
		if readExe(t, f.exe) != oldContent || len(f.dl.calls) != 0 {
			t.Fatalf("version %q: dev build must not download or replace", v)
		}
	}
}

func TestUpdateOnHomebrewPointsToBrew(t *testing.T) {
	for _, p := range []string{
		"/opt/homebrew/Cellar/omnishell/1.0.0/bin/omnishell",
		"/opt/homebrew/bin/omnishell",
		"/usr/local/Cellar/omnishell/1.0.0/bin/omnishell",
		"/home/linuxbrew/.linuxbrew/bin/omnishell",
		"/custom/prefix/Cellar/omnishell/1.0.0/bin/omnishell",
	} {
		f := goodFixture(t, "1.1.0")
		f.u.Executable = func() (string, error) { return p, nil }
		if err := f.u.Update(); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if !strings.Contains(f.out.String(), "brew upgrade") {
			t.Fatalf("%s: output %q should point to brew upgrade", p, f.out.String())
		}
		if len(f.dl.calls) != 0 || readExe(t, f.exe) != oldContent {
			t.Fatalf("%s: brew install must not download or replace", p)
		}
	}
}

func TestIsHomebrewPath(t *testing.T) {
	tests := map[string]bool{
		"/opt/homebrew/bin/omnishell":                 true,
		"/usr/local/Cellar/omnishell/1/bin/omnishell": true,
		"/usr/local/bin/omnishell":                    false,
		"/home/me/.local/bin/omnishell":               false,
		"/home/me/Cellar-notes/omnishell":             false,
		"/home/linuxbrew/.linuxbrew/bin/omnishell":    true,
		"/opt/homebrew-other/bin/omnishell":           false,
	}
	for path, want := range tests {
		if got := IsHomebrewPath(path); got != want {
			t.Errorf("IsHomebrewPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestUpdateFailsOnNonWritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := goodFixture(t, "1.1.0")
	dir := filepath.Dir(f.exe)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := f.u.Update()
	if err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("err = %v, want a 'not writable' error", err)
	}
	if len(f.dl.calls) != 0 {
		t.Fatalf("must fail before downloading, got %v", f.dl.calls)
	}
	if readExe(t, f.exe) != oldContent {
		t.Fatal("binary changed")
	}
}

func TestUpdateKeepsOldBinaryOnFailure(t *testing.T) {
	good := makeTarGz(t, tarEntry{"omnishell", newContent})
	tests := []struct {
		name      string
		archive   []byte
		checksums string
		wantErr   string
		mismatch  bool
	}{
		{"checksum mismatch", good, strings.Repeat("a", 64) + "  " + testArchive + "\n", "checksum mismatch", true},
		{"no matching line", good, sum(good) + "  other.tar.gz\n", "no checksum for " + testArchive, false},
		{"empty checksums", good, "", "no checksum for", false},
		{"non-hex hash", good, "xyz  " + testArchive + "\n", "malformed checksum line", false},
		{"short hash on the matching line", good, "abc123  " + testArchive + "\n", "malformed checksum line", false},
		{"tar path traversal", nil, "", "escapes", false},
		{"absolute path entry", nil, "", "escapes", false},
		{"binary missing from archive", nil, "", "not found in archive", false},
		{"not a gzip", []byte("garbage"), "", "read gzip", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive, checksums := tt.archive, tt.checksums
			switch tt.name {
			case "tar path traversal":
				archive = makeTarGz(t, tarEntry{"../omnishell", "evil"}, tarEntry{"omnishell", newContent})
			case "absolute path entry":
				archive = makeTarGz(t, tarEntry{"/tmp/omnishell", "evil"}, tarEntry{"omnishell", newContent})
			case "binary missing from archive":
				archive = makeTarGz(t, tarEntry{"README.md", "readme"})
			}
			if checksums == "" && tt.name != "empty checksums" {
				checksums = sum(archive) + "  " + testArchive + "\n"
			}
			f := newFixture(t, "1.1.0", archive, checksums)
			err := f.u.Update()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if tt.mismatch && !errors.Is(err, pkgmgr.ErrChecksumMismatch) {
				t.Fatalf("err = %v, want ErrChecksumMismatch", err)
			}
			if got := readExe(t, f.exe); got != oldContent {
				t.Fatalf("old binary changed to %q", got)
			}
			assertNoLeftovers(t, f.exe)
		})
	}
}

func TestUpdateKeepsOldBinaryWhenDownloadFails(t *testing.T) {
	f := goodFixture(t, "1.1.0")
	delete(f.dl.files, testReleases+"/download/v1.2.0/"+testArchive)
	if err := f.u.Update(); err == nil {
		t.Fatal("want error")
	}
	if readExe(t, f.exe) != oldContent {
		t.Fatal("binary changed")
	}
	assertNoLeftovers(t, f.exe)
}

func TestUpdateRejectsOversizedBinary(t *testing.T) {
	archive := makeTarGz(t, tarEntry{"omnishell", strings.Repeat("x", 64)})
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	err := extractBinary(path, "omnishell", io.Discard, 10)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want a size-limit error", err)
	}
}

func TestArchiveName(t *testing.T) {
	tests := []struct {
		goos, goarch, goarm, want string
		wantErr                   bool
	}{
		{"linux", "amd64", "", "omnishell_linux_amd64.tar.gz", false},
		{"darwin", "arm64", "", "omnishell_darwin_arm64.tar.gz", false},
		{"linux", "arm", "6", "omnishell_linux_armv6.tar.gz", false},
		{"linux", "arm", "7,softfloat", "omnishell_linux_armv7.tar.gz", false},
		{"linux", "arm", "", "", true},
		{"darwin", "arm", "7", "", true},
		{"linux", "386", "", "", true},
		{"windows", "amd64", "", "", true},
	}
	for _, tt := range tests {
		got, err := archiveName(tt.goos, tt.goarch, tt.goarm)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("archiveName(%s,%s,%s) = %q, %v; want %q (err %v)", tt.goos, tt.goarch, tt.goarm, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestSemverCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.10.0", "1.9.0", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0-rc.2", "1.0.0-rc.10", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0+build5", "1.0.0", 0},
	}
	for _, tt := range tests {
		a, err := parseSemver(tt.a)
		if err != nil {
			t.Fatal(err)
		}
		b, err := parseSemver(tt.b)
		if err != nil {
			t.Fatal(err)
		}
		if got := a.compare(b); got != tt.want {
			t.Errorf("compare(%s, %s) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
	for _, bad := range []string{"", "dev", "1.2", "1.2.x", "01.2.3", "1.2.3-", "1.2.3.4"} {
		if _, err := parseSemver(bad); err == nil {
			t.Errorf("parseSemver(%q) should fail", bad)
		}
	}
}

func TestRedirectResolver(t *testing.T) {
	serve := func(handler http.HandlerFunc) RedirectResolver {
		srv := httptest.NewTLSServer(handler)
		t.Cleanup(srv.Close)
		return RedirectResolver{URL: srv.URL + "/o/r/releases/latest", Client: srv.Client()}
	}
	t.Run("tag from relative redirect", func(t *testing.T) {
		r := serve(func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "/o/r/releases/tag/v1.4.0", http.StatusFound)
		})
		tag, err := r.Latest()
		if err != nil || tag != "v1.4.0" {
			t.Fatalf("tag = %q, err = %v", tag, err)
		}
	})
	t.Run("redirect to another host is refused", func(t *testing.T) {
		r := serve(func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "https://evil.example/o/r/releases/tag/v9.9.9", http.StatusFound)
		})
		if _, err := r.Latest(); err == nil || !strings.Contains(err.Error(), "unexpected redirect") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("redirect that is no release tag", func(t *testing.T) {
		r := serve(func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "/o/r/releases", http.StatusFound)
		})
		if _, err := r.Latest(); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("rate limited", func(t *testing.T) {
		r := serve(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
		if _, err := r.Latest(); err == nil || !strings.Contains(err.Error(), "rate limiting") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("plain http is refused", func(t *testing.T) {
		r := RedirectResolver{URL: "http://github.com/o/r/releases/latest", Client: http.DefaultClient}
		if _, err := r.Latest(); err == nil || !strings.Contains(err.Error(), "non-https") {
			t.Fatalf("err = %v", err)
		}
	})
}
