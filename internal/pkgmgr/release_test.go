package pkgmgr_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

type fakeDownloader struct {
	body []byte
	err  error
	urls []string
}

func (f *fakeDownloader) Download(url string, dst io.Writer, _ int64) error {
	f.urls = append(f.urls, url)
	if f.err != nil {
		return f.err
	}
	_, err := dst.Write(f.body)
	return err
}

func sumHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func tarGzBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
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

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func releaseFB(url, member, sha string) module.Fallback {
	return module.Fallback{
		Type: "release", Repo: "https://example.com/tool", Ref: "v1.2.3", Bin: "tool",
		Assets: []module.Asset{{OS: "linux", Arch: "amd64", URL: url, SHA256: sha, Member: member}},
	}
}

// ctxIn uses a vendor directory whose name contains a space.
func ctxIn(t *testing.T) pkgmgr.ReleaseContext {
	t.Helper()
	return pkgmgr.ReleaseContext{VendorDir: filepath.Join(t.TempDir(), "my vendor"), OS: "linux", Arch: "amd64"}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// assertNoTemps fails when an install left a ".release-*" temp file behind.
func assertNoTemps(t *testing.T, ctx pkgmgr.ReleaseContext) {
	t.Helper()
	for _, dir := range []string{ctx.VendorDir, filepath.Join(ctx.VendorDir, "bin")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".release-") {
				t.Fatalf("temp file %s left in %s", e.Name(), dir)
			}
		}
	}
}

func TestInstallReleaseFromATarGz(t *testing.T) {
	body := tarGzBytes(t, map[string]string{"tool/bin/tool": "#!/bin/sh\necho tool\n", "tool/README": "x"})
	fb := releaseFB("https://example.com/{{.Ref}}/tool.tar.gz", "tool/bin/tool", sumHex(body))
	ctx := ctxIn(t)
	dl := &fakeDownloader{body: body}

	res, err := pkgmgr.InstallRelease(fb, ctx, dl)
	if err != nil {
		t.Fatalf("InstallRelease: %v", err)
	}
	want := filepath.Join(ctx.VendorDir, "bin", "tool")
	if res.BinPath != want || res.SHA256 != sumHex(body) {
		t.Fatalf("result = %+v, want path %s and sha %s", res, want, sumHex(body))
	}
	if got := readText(t, want); got != "#!/bin/sh\necho tool\n" {
		t.Fatalf("binary content = %q", got)
	}
	info, err := os.Stat(want)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("binary mode = %v (err %v), want 0755", info.Mode().Perm(), err)
	}
	if len(dl.urls) != 1 || dl.urls[0] != "https://example.com/v1.2.3/tool.tar.gz" {
		t.Fatalf("downloaded %v, want the rendered url once", dl.urls)
	}
	assertNoTemps(t, ctx)
	if !pkgmgr.ReleaseInstalled(fb, ctx) {
		t.Fatal("ReleaseInstalled = false after a successful install")
	}
}

func TestInstallReleaseFromAZip(t *testing.T) {
	body := zipBytes(t, map[string]string{"x86_64-unknown-linux-musl/tool": "zip-binary", "other/tool": "no"})
	fb := releaseFB("https://example.com/{{.Version}}/tool.zip", "x86_64-unknown-linux-musl/tool", sumHex(body))
	ctx := ctxIn(t)

	res, err := pkgmgr.InstallRelease(fb, ctx, &fakeDownloader{body: body})
	if err != nil {
		t.Fatalf("InstallRelease: %v", err)
	}
	if got := readText(t, res.BinPath); got != "zip-binary" {
		t.Fatalf("binary content = %q", got)
	}
	assertNoTemps(t, ctx)
}

func TestInstallReleaseFromARawBinary(t *testing.T) {
	body := []byte("raw-binary")
	fb := releaseFB("https://example.com/tool-linux-amd64", "", sumHex(body))
	ctx := ctxIn(t)

	res, err := pkgmgr.InstallRelease(fb, ctx, &fakeDownloader{body: body})
	if err != nil {
		t.Fatalf("InstallRelease: %v", err)
	}
	if got := readText(t, res.BinPath); got != "raw-binary" {
		t.Fatalf("binary content = %q", got)
	}
	assertNoTemps(t, ctx)
}

func TestInstallReleaseKeepsThePreviousBinaryOnFailure(t *testing.T) {
	good := tarGzBytes(t, map[string]string{"tool/bin/tool": "new"})
	noMember := tarGzBytes(t, map[string]string{"tool/README": "x"})
	cases := []struct {
		name string
		dl   *fakeDownloader
		sha  string
		want string
	}{
		{"checksum mismatch", &fakeDownloader{body: good}, strings.Repeat("0", 64), "checksum mismatch"},
		{"member missing from the archive", &fakeDownloader{body: noMember}, sumHex(noMember), `member "tool/bin/tool" not found`},
		{"download error", &fakeDownloader{err: errors.New("connection reset")}, sumHex(good), "connection reset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := ctxIn(t)
			bin := filepath.Join(ctx.VendorDir, "bin", "tool")
			if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
				t.Fatal(err)
			}
			fb := releaseFB("https://example.com/tool.tar.gz", "tool/bin/tool", tc.sha)

			_, err := pkgmgr.InstallRelease(fb, ctx, tc.dl)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			if got := readText(t, bin); got != "old" {
				t.Fatalf("previous binary replaced by %q", got)
			}
			assertNoTemps(t, ctx)
		})
	}
}

func TestInstallReleaseChecksumMessageNamesBothHashes(t *testing.T) {
	body := []byte("payload")
	fb := releaseFB("https://example.com/tool", "", strings.Repeat("0", 64))
	_, err := pkgmgr.InstallRelease(fb, ctxIn(t), &fakeDownloader{body: body})
	if err == nil {
		t.Fatal("want a checksum error")
	}
	for _, want := range []string{"linux/amd64", strings.Repeat("0", 64), sumHex(body)} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

func TestInstallReleaseRejectsAnUnsupportedArchitecture(t *testing.T) {
	fb := releaseFB("https://example.com/tool", "", strings.Repeat("0", 64))
	ctx := ctxIn(t)
	ctx.Arch = "riscv64"
	dl := &fakeDownloader{}
	_, err := pkgmgr.InstallRelease(fb, ctx, dl)
	if err == nil || !strings.Contains(err.Error(), "no release asset for linux/riscv64") {
		t.Fatalf("err = %v", err)
	}
	if len(dl.urls) != 0 {
		t.Fatalf("downloaded despite no matching asset: %v", dl.urls)
	}
}

func TestInstallReleaseNeedsADownloader(t *testing.T) {
	fb := releaseFB("https://example.com/tool", "", strings.Repeat("0", 64))
	if _, err := pkgmgr.InstallRelease(fb, ctxIn(t), nil); err == nil {
		t.Fatal("want an error without a downloader")
	}
}

func TestInstallReleaseRejectsAGitEntry(t *testing.T) {
	fb := module.Fallback{Type: "git"}
	if _, err := pkgmgr.InstallRelease(fb, ctxIn(t), &fakeDownloader{}); err == nil {
		t.Fatal("want an error for a non-release entry")
	}
}

func TestReleaseInstalledIsFalseWithoutABinary(t *testing.T) {
	fb := releaseFB("https://example.com/tool", "", strings.Repeat("0", 64))
	if pkgmgr.ReleaseInstalled(fb, ctxIn(t)) {
		t.Fatal("ReleaseInstalled = true with no binary")
	}
}

func TestExtractMemberEnforcesTheSizeLimit(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(archive, tarGzBytes(t, map[string]string{"big": strings.Repeat("x", 100)}), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := pkgmgr.ExtractMember(archive, "tar.gz", "big", &out, 10)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("err = %v, want a size-limit error", err)
	}
}

func TestHTTPDownloaderFetchesOverHTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()
	var buf bytes.Buffer
	if err := (pkgmgr.HTTPDownloader{Client: srv.Client()}).Download(srv.URL+"/a", &buf, 1024); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if buf.String() != "payload" {
		t.Fatalf("body = %q", buf.String())
	}
}

func TestHTTPDownloaderRejectsPlainHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	err := (pkgmgr.HTTPDownloader{Client: srv.Client()}).Download(srv.URL, io.Discard, 1024)
	if err == nil || !strings.Contains(err.Error(), "non-https") {
		t.Fatalf("err = %v, want a non-https refusal", err)
	}
}

func TestHTTPDownloaderRefusesARedirectToHTTP(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("downgraded"))
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer secure.Close()
	var buf bytes.Buffer
	err := (pkgmgr.HTTPDownloader{Client: secure.Client()}).Download(secure.URL, &buf, 1024)
	if err == nil || !strings.Contains(err.Error(), "non-https") {
		t.Fatalf("err = %v, want the redirect refused", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("wrote %q from the downgraded server", buf.String())
	}
}

func TestHTTPDownloaderEnforcesTheSizeLimit(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()
	err := (pkgmgr.HTTPDownloader{Client: srv.Client()}).Download(srv.URL, io.Discard, 10)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("err = %v, want a size-limit error", err)
	}
}

func TestHTTPDownloaderReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	err := (pkgmgr.HTTPDownloader{Client: srv.Client()}).Download(srv.URL, io.Discard, 1024)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want the 404 reported", err)
	}
}

func TestInstallReleasePicksTheAssetForTheHostVariant(t *testing.T) {
	body := []byte("#!/bin/sh\necho tool\n")
	sha := sumHex(body)
	fb := module.Fallback{
		Type: "release", Repo: "https://example.com/tool", Ref: "v1.2.3", Bin: "tool",
		Assets: []module.Asset{
			{OS: "linux", Arch: "arm", URL: "https://example.com/tool-any-arm", SHA256: sha},
			{OS: "linux", Arch: "arm", GoARM: "7", URL: "https://example.com/tool-armv7", SHA256: sha},
		},
	}
	cases := map[string]string{
		"7": "https://example.com/tool-armv7",
		"6": "https://example.com/tool-any-arm",
		"":  "https://example.com/tool-any-arm",
	}
	for goarm, wantURL := range cases {
		ctx := ctxIn(t)
		ctx.Arch, ctx.GoARM = "arm", goarm
		dl := &fakeDownloader{body: body}
		if _, err := pkgmgr.InstallRelease(fb, ctx, dl); err != nil {
			t.Fatalf("goarm %q: %v", goarm, err)
		}
		if len(dl.urls) != 1 || dl.urls[0] != wantURL {
			t.Errorf("goarm %q: downloaded %v, want %s", goarm, dl.urls, wantURL)
		}
	}
}
