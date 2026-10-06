# Release-binary fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `type = "release"` fallback installs a pinned, SHA-256-verified static binary for `mise`, `starship` and `broot` in seconds, ahead of the Cargo build, with idempotent re-runs, visible cleanup of old Cargo builds, and a tag updater that keeps the pinned checksums current.

**Architecture:** `[[packages.fallback]]` gains a `release` type (ordered before `git`). `ComputePlan` walks the entries and takes the first usable one (an asset matches `(os, arch)`); `Apply` downloads through an injectable `Downloader`, verifies, extracts one named member and renames it atomically into `{{.VendorDir}}/bin`. The lockfile records `fallback_kind`/`fallback_sha256`; migration from a Cargo build is plan-visible cleanup with filesystem evidence.

**Tech Stack:** Go (stdlib `archive/tar`, `archive/zip`, `net/http`, `crypto/sha256`), `BurntSushi/toml`, existing `pkgmgr.Runner`/`MockRunner` test seams.

**Spec:** `docs/superpowers/specs/2026-10-06-release-binary-fallback-design.md`

## Global Constraints

- Work in the worktree `/Users/jeffry/Projects/omnishell/.worktrees/OMNIS-28-release-binary-fallback` on branch `fix/OMNIS-28-release-binary-fallback`. Never commit to `main`.
- Commit messages: `<type>: <description>`, English, imperative, ≤72 chars, no attribution or co-author trailers, never `--no-verify`.
- Code comments, identifiers, log messages: English. Match surrounding style (short doc comments, sentence-case, `_, _ = fmt.Fprintf(e.Stdout, …)` for output).
- `ComputePlan` must stay side-effect-free: no commands run, no downloads, no writes. Reads (stat, file reads) are allowed.
- Nothing touches the real system except `apply`, `remove`, `uninstall`. The hand-edit guard in `Apply` still runs before any install.
- All shelling out goes through `pkgmgr.Runner`; all network access goes through `pkgmgr.Downloader`. Tests use `MockRunner` and a fake `Downloader`, never the real network or `exec`.
- Release assets are `linux` only, `arch` is `amd64` or `arm64`, URLs are `https://`, `sha256` is 64 lowercase hex characters. No `libc` key, no libc detection.
- A failed download, checksum or extraction degrades the module and never falls through to the Cargo build. Only "no matching asset" selects the Cargo build (decided at plan time).
- Cleanup deletes nothing without filesystem evidence; `.crates.toml` and `.crates2.json` are never deleted, only this crate's entry is removed.
- Run `gofmt`, `go vet ./...` and `golangci-lint run` before the final hand-off. While iterating, run only the relevant tests (`go test ./internal/<pkg>/... -run <Name> -v`); run `make test` at the end of each phase.

## Review Focus

Inputs and conditions the spec implies that no happy-path test exercises, most likely first. Each has a test in the task named in brackets.

1. The archive does not contain the named `member` → a clear error, the previous binary is untouched, no temp files are left. [Task 2]
2. A download or an extracted file larger than the cap → error and temp files removed, never an unbounded write. [Task 2]
3. The server redirects an `https` URL to plain `http` → refused. [Task 2]
4. `VendorDir` contains a space → install and cleanup paths work. [Tasks 2, 6]
5. A lockfile written before this change (`fallback_ref` set, no `fallback_kind`) and an `apply --no-packages` run → the module is treated as `git` and the recorded kind/sha survive. [Tasks 3, 4]
6. Cleanup meets another tool's Cargo metadata, a foreign directory at the build path, or an unparsable metadata file → nothing foreign is touched, the module is not degraded. [Task 6]

---

# Phase 1 — release install

### Task 1: Manifest schema and validation

**Files:**
- Create: `internal/module/release.go`
- Modify: `internal/module/manifest.go` (the `Fallback` struct near line 36; the fallback validation near line 198)
- Test: `internal/module/release_test.go`

**Interfaces:**
- Produces:
  - `type Asset struct { OS, Arch, URL, SHA256, Member string }` (toml keys `os`, `arch`, `url`, `sha256`, `member`)
  - `Fallback` gains `Bin string` (`toml:"bin"`) and `Assets []Asset` (`toml:"assets"`)
  - `func (a Asset) Archive() string` → `"tar.gz"`, `"zip"` or `""` (raw)
  - `func (a Asset) RenderURL(ref string) (string, error)` — renders `{{.Ref}}` and `{{.Version}}` (ref without a leading `v`)
  - `func (f Fallback) AssetFor(osName, arch string) (Asset, bool)`

- [ ] **Step 1: Write the failing tests**

Create `internal/module/release_test.go`:

```go
package module_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
)

// releaseManifest returns a valid manifest with one release fallback; mutate,
// when set, edits the text to build an invalid variant.
func releaseManifest(mutate func(string) string) string {
	base := `
platforms = ["linux"]
shells    = ["bash"]

[module]
id          = "tool"
name        = "tool"
description = "A tool"
version     = "1.0.0"
schema      = 1

[[packages.fallback]]
type = "release"
repo = "https://github.com/o/tool"
ref  = "v1.2.3"
bin  = "tool"

[[packages.fallback.assets]]
os     = "linux"
arch   = "amd64"
url    = "https://example.com/{{.Ref}}/tool-{{.Version}}-linux-amd64.tar.gz"
sha256 = "SHA"
member = "tool/bin/tool"
`
	base = strings.ReplaceAll(base, "SHA", strings.Repeat("a", 64))
	if mutate != nil {
		base = mutate(base)
	}
	return base
}

func TestParseManifestReleaseFallback(t *testing.T) {
	m, err := module.ParseManifest([]byte(releaseManifest(nil)))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	fb := m.Packages.Fallback[0]
	if fb.Type != "release" || fb.Bin != "tool" || fb.Ref != "v1.2.3" || len(fb.Assets) != 1 {
		t.Fatalf("fallback = %+v", fb)
	}
	asset, ok := fb.AssetFor("linux", "amd64")
	if !ok || asset.Member != "tool/bin/tool" || asset.Archive() != "tar.gz" {
		t.Fatalf("AssetFor(linux, amd64) = %+v, %v", asset, ok)
	}
	if _, ok := fb.AssetFor("linux", "arm64"); ok {
		t.Fatal("AssetFor(linux, arm64) matched, want no asset")
	}
	url, err := asset.RenderURL("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://example.com/v1.2.3/tool-1.2.3-linux-amd64.tar.gz"; url != want {
		t.Fatalf("RenderURL = %q, want %q", url, want)
	}
}

func TestAssetArchive(t *testing.T) {
	cases := map[string]string{
		"https://x/a.tar.gz": "tar.gz",
		"https://x/a.zip":    "zip",
		"https://x/a":        "",
	}
	for url, want := range cases {
		if got := (module.Asset{URL: url}).Archive(); got != want {
			t.Errorf("Archive(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestParseManifestRejectsInvalidRelease(t *testing.T) {
	sha := strings.Repeat("a", 64)
	cases := []struct {
		name   string
		mutate func(string) string
		want   string
	}{
		{"unknown type", func(s string) string { return strings.Replace(s, `type = "release"`, `type = "zip"`, 1) }, "unknown type"},
		{"missing ref", func(s string) string { return strings.Replace(s, "ref  = \"v1.2.3\"\n", "", 1) }, "needs ref"},
		{"missing repo", func(s string) string { return strings.Replace(s, "repo = \"https://github.com/o/tool\"\n", "", 1) }, "needs repo"},
		{"missing bin", func(s string) string { return strings.Replace(s, "bin  = \"tool\"\n", "", 1) }, "bin must be a plain file name"},
		{"bin with a path", func(s string) string { return strings.Replace(s, `bin  = "tool"`, `bin  = "../tool"`, 1) }, "bin must be a plain file name"},
		{"requires on a release entry", func(s string) string {
			return strings.Replace(s, `bin  = "tool"`, "bin  = \"tool\"\nrequires = [\"cargo>=1.85\"]", 1)
		}, "requires applies only to git"},
		{"no assets", func(s string) string { return s[:strings.Index(s, "[[packages.fallback.assets]]")] }, "at least one asset"},
		{"unsupported os", func(s string) string { return strings.Replace(s, `os     = "linux"`, `os     = "macos"`, 1) }, "os must be linux"},
		{"unknown arch", func(s string) string { return strings.Replace(s, `arch   = "amd64"`, `arch   = "riscv64"`, 1) }, "arch must be amd64 or arm64"},
		{"plain http url", func(s string) string { return strings.Replace(s, "https://example.com", "http://example.com", 1) }, "url must start with https://"},
		{"short sha", func(s string) string { return strings.Replace(s, sha, "xyz", 1) }, "64 lowercase hex"},
		{"uppercase sha", func(s string) string { return strings.Replace(s, sha, strings.Repeat("A", 64), 1) }, "64 lowercase hex"},
		{"broken url template", func(s string) string { return strings.Replace(s, "{{.Ref}}", "{{.Ref", 1) }, "invalid url template"},
		{"archive without member", func(s string) string { return strings.Replace(s, "member = \"tool/bin/tool\"\n", "", 1) }, "archive asset needs member"},
		{"member on a raw asset", func(s string) string { return strings.Replace(s, "-linux-amd64.tar.gz", "-linux-amd64", 1) }, "member is only valid"},
		{"duplicate asset", func(s string) string {
			return s + "\n[[packages.fallback.assets]]\nos = \"linux\"\narch = \"amd64\"\nurl = \"https://example.com/b.tar.gz\"\nsha256 = \"" + sha + "\"\nmember = \"b\"\n"
		}, "duplicate asset"},
		{"assets on a git entry", func(s string) string { return strings.Replace(s, `type = "release"`, `type = "git"`, 1) }, "only valid for type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := module.ParseManifest([]byte(releaseManifest(tc.mutate)))
			if err == nil {
				t.Fatalf("want an error mentioning %q, got none", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/module/... -run 'Release|AssetArchive' -v`
Expected: FAIL to compile (`module.Asset` undefined, `fb.Bin` undefined).

- [ ] **Step 3: Implement the schema**

Create `internal/module/release.go`:

```go
package module

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// Asset is one [[packages.fallback.assets]] entry of a release fallback: the
// download for one OS and architecture. URL is a template over {{.Ref}} (the
// pinned tag) and {{.Version}} (the tag without a leading "v"). SHA256 pins the
// downloaded file. Member names the file to extract from a .tar.gz or .zip
// asset; it is empty for a raw binary download.
type Asset struct {
	OS     string `toml:"os"`
	Arch   string `toml:"arch"`
	URL    string `toml:"url"`
	SHA256 string `toml:"sha256"`
	Member string `toml:"member"`
}

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Archive reports the asset's archive format from its URL suffix: "tar.gz",
// "zip", or "" for a raw binary.
func (a Asset) Archive() string {
	switch {
	case strings.HasSuffix(a.URL, ".tar.gz"):
		return "tar.gz"
	case strings.HasSuffix(a.URL, ".zip"):
		return "zip"
	}
	return ""
}

// RenderURL renders the asset URL for the given pinned tag.
func (a Asset) RenderURL(ref string) (string, error) {
	t, err := template.New("url").Parse(a.URL)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	data := struct{ Ref, Version string }{Ref: ref, Version: strings.TrimPrefix(ref, "v")}
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// AssetFor returns the asset for an OS and architecture, if the fallback has one.
func (f Fallback) AssetFor(osName, arch string) (Asset, bool) {
	for _, a := range f.Assets {
		if a.OS == osName && a.Arch == arch {
			return a, true
		}
	}
	return Asset{}, false
}

// fallbackProblem returns why a fallback entry is invalid, or "" when it is
// valid. It checks the shape that belongs to the entry's type.
func fallbackProblem(fb Fallback) string {
	switch fb.Type {
	case "git":
		if fb.Bin != "" || len(fb.Assets) > 0 {
			return `bin and assets are only valid for type = "release"`
		}
		return ""
	case "release":
		return releaseProblem(fb)
	}
	return fmt.Sprintf("unknown type %q (want git or release)", fb.Type)
}

func releaseProblem(fb Fallback) string {
	switch {
	case fb.Ref == "":
		return "release fallback needs ref"
	case fb.Repo == "":
		return "release fallback needs repo"
	case fb.Bin == "" || fb.Bin == "." || fb.Bin == ".." || fb.Bin != filepath.Base(fb.Bin):
		return "bin must be a plain file name"
	case len(fb.Requires) > 0:
		return "requires applies only to git fallbacks"
	case len(fb.Assets) == 0:
		return "release fallback needs at least one asset"
	}
	seen := map[string]bool{}
	for i, a := range fb.Assets {
		if msg := assetProblem(a); msg != "" {
			return fmt.Sprintf("assets[%d]: %s", i, msg)
		}
		key := a.OS + "/" + a.Arch
		if seen[key] {
			return "duplicate asset for " + key
		}
		seen[key] = true
	}
	return ""
}

func assetProblem(a Asset) string {
	switch {
	case a.OS != "linux":
		return "os must be linux"
	case a.Arch != "amd64" && a.Arch != "arm64":
		return "arch must be amd64 or arm64"
	case !strings.HasPrefix(a.URL, "https://"):
		return "url must start with https://"
	case !sha256Re.MatchString(a.SHA256):
		return "sha256 must be 64 lowercase hex characters"
	}
	if _, err := a.RenderURL("v1.2.3"); err != nil {
		return "invalid url template: " + err.Error()
	}
	switch {
	case a.Archive() == "" && a.Member != "":
		return "member is only valid for .tar.gz and .zip assets"
	case a.Archive() != "" && a.Member == "":
		return "archive asset needs member"
	}
	return ""
}
```

In `internal/module/manifest.go`, add the two fields to `Fallback` (update its doc comment to mention `release`):

```go
type Fallback struct {
	Type     string   `toml:"type"`
	Repo     string   `toml:"repo"`
	Dest     string   `toml:"dest"`
	Ref      string   `toml:"ref"`
	Run      []string `toml:"run"`
	Requires []string `toml:"requires"`
	Bin      string   `toml:"bin"`
	Assets   []Asset  `toml:"assets"`
}
```

and call the new check at the start of the existing fallback validation (the loop that checks `fb.Requires`, near line 198):

```go
	for i, fb := range m.Packages.Fallback {
		if msg := fallbackProblem(fb); msg != "" {
			return e(fmt.Sprintf("packages.fallback[%d]", i), msg)
		}
	}
```

- [ ] **Step 4: Run the module tests**

Run: `go test ./internal/module/... -v 2>&1 | tail -30`
Expected: the new tests PASS. If an existing fixture fails with `unknown type ""`, that fixture's `[[packages.fallback]]` lacks `type`: add `type = "git"` to it (every git fallback needs it already, since `InstallGitFallback` rejects other types), then re-run until green.

- [ ] **Step 5: Commit**

```bash
git add internal/module
git commit -m "feat: add the release fallback type to the manifest schema"
```

---

### Task 2: Download, verify, extract and install

**Files:**
- Create: `internal/pkgmgr/download.go`, `internal/pkgmgr/extract.go`, `internal/pkgmgr/release.go`
- Modify: `internal/pkgmgr/export_test.go` (append one line)
- Test: `internal/pkgmgr/release_test.go`

**Interfaces:**
- Consumes: `module.Fallback`, `module.Asset` (Task 1).
- Produces:
  - `type Downloader interface { Download(url string, dst io.Writer, limit int64) error }`
  - `type HTTPDownloader struct{ Client *http.Client }`, `func NewHTTPDownloader() HTTPDownloader`
  - `const MaxReleaseBytes int64 = 512 << 20`
  - `type ReleaseContext struct{ VendorDir, OS, Arch string }`, `func (c ReleaseContext) BinPath(fb module.Fallback) string`
  - `func ReleaseInstalled(fb module.Fallback, c ReleaseContext) bool`
  - `type ReleaseInstall struct{ BinPath, SHA256 string }`
  - `func InstallRelease(fb module.Fallback, ctx ReleaseContext, d Downloader) (ReleaseInstall, error)`
  - test export: `pkgmgr.ExtractMember(archivePath, format, member string, w io.Writer, limit int64) error`

- [ ] **Step 1: Write the failing tests**

Create `internal/pkgmgr/release_test.go`:

```go
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
```

Append to `internal/pkgmgr/export_test.go`:

```go

// ExtractMember exposes extractMember to the external test package.
var ExtractMember = extractMember
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkgmgr/... -run 'Release|HTTPDownloader|ExtractMember' -v 2>&1 | head -20`
Expected: FAIL to compile (`pkgmgr.ReleaseContext`, `pkgmgr.InstallRelease`, `pkgmgr.HTTPDownloader` undefined).

- [ ] **Step 3: Implement the downloader**

Create `internal/pkgmgr/download.go`:

```go
package pkgmgr

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Downloader fetches a URL into dst, writing at most limit bytes. Injectable so
// tests never touch the network.
type Downloader interface {
	Download(url string, dst io.Writer, limit int64) error
}

// HTTPDownloader downloads over HTTPS with net/http. It refuses plain-http URLs
// and redirects to them.
type HTTPDownloader struct {
	Client *http.Client
}

// NewHTTPDownloader returns a downloader with a generous overall timeout.
func NewHTTPDownloader() HTTPDownloader {
	return HTTPDownloader{Client: &http.Client{Timeout: 15 * time.Minute}}
}

// Download implements Downloader.
func (d HTTPDownloader) Download(rawURL string, dst io.Writer, limit int64) error {
	if !strings.HasPrefix(rawURL, "https://") {
		return fmt.Errorf("refusing non-https url %s", rawURL)
	}
	client := *d.Client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing redirect to non-https url %s", req.URL)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("download exceeds the %d byte limit", limit)
	}
	return nil
}
```

- [ ] **Step 4: Implement the extractor**

Create `internal/pkgmgr/extract.go`:

```go
package pkgmgr

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
)

// extractMember copies the single file named member out of a .tar.gz or .zip
// archive into w, writing at most limit bytes. It only matches the named entry
// and never derives an output path from the archive, so archive entry names
// cannot escape anywhere.
func extractMember(archivePath, format, member string, w io.Writer, limit int64) error {
	switch format {
	case "tar.gz":
		return extractTarGz(archivePath, member, w, limit)
	case "zip":
		return extractZip(archivePath, member, w, limit)
	}
	return fmt.Errorf("unsupported archive format %q", format)
}

func extractTarGz(archivePath, member string, w io.Writer, limit int64) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Clean(hdr.Name) != member {
			continue
		}
		return copyLimited(w, tr, limit)
	}
	return fmt.Errorf("member %q not found in archive", member)
}

func extractZip(archivePath, member string, w io.Writer, limit int64) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("read zip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || path.Clean(f.Name) != member {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open %s: %w", f.Name, err)
		}
		defer func() { _ = rc.Close() }()
		return copyLimited(w, rc, limit)
	}
	return fmt.Errorf("member %q not found in archive", member)
}

// copyLimited copies r to w and fails once more than limit bytes arrive, so a
// compressed bomb cannot fill the disk.
func copyLimited(w io.Writer, r io.Reader, limit int64) error {
	n, err := io.Copy(w, io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("extracted file exceeds the %d byte limit", limit)
	}
	return nil
}
```

- [ ] **Step 5: Implement the install**

Create `internal/pkgmgr/release.go`:

```go
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
```

- [ ] **Step 6: Run the package tests**

Run: `go test ./internal/pkgmgr/... -race -count=1 -v 2>&1 | tail -40`
Expected: all PASS, including the existing git-fallback tests.

- [ ] **Step 7: Commit**

```bash
git add internal/pkgmgr
git commit -m "feat: download, verify and install release binaries"
```

---

### Task 3: Lockfile fields and fallback selection in the plan

**Files:**
- Modify: `internal/lockfile/lockfile.go` (`ModuleState`, near line 33)
- Modify: `internal/engine/engine.go` (add the `Downloader` field)
- Modify: `internal/engine/plan.go` (`ModulePlan`, `PackagePlan`, `planFallback`, `describePackage`)
- Test: `internal/lockfile/lockfile_test.go`, `internal/engine/plan_internal_test.go`

**Interfaces:**
- Consumes: `module.Fallback.AssetFor`, `pkgmgr.ReleaseInstalled`, `pkgmgr.ReleaseContext` (Tasks 1–2).
- Produces:
  - `lockfile.ModuleState.FallbackKind string` (`json:"fallback_kind,omitempty"`, values `"git"`/`"release"`), `FallbackSHA256 string` (`json:"fallback_sha256,omitempty"`)
  - `engine.Engine.Downloader pkgmgr.Downloader`
  - `ModulePlan.Fallback module.Fallback` — the selected entry (zero value when none)
  - `PackagePlan{Manager: "release"}` entries (`Name` = the binary name, `To` = the pinned ref, `Update`/`From` as for git)
  - `func selectFallback(fbs []module.Fallback, info platform.Info) (module.Fallback, bool)`
  - `func (e Engine) releaseContext() pkgmgr.ReleaseContext`
  - constants `fallbackKindGit = "git"`, `fallbackKindRelease = "release"`

- [ ] **Step 1: Write the failing tests**

Append to `internal/lockfile/lockfile_test.go`:

```go
func TestFallbackKindAndSHA256RoundTrip(t *testing.T) {
	l := sample()
	st := l.Modules["fzf"]
	st.FallbackRef, st.FallbackKind, st.FallbackSHA256 = "v1.2.3", "release", "abc123"
	l.Modules["fzf"] = st
	p := filepath.Join(t.TempDir(), "state.lock.json")
	if err := l.Write(p); err != nil {
		t.Fatal(err)
	}
	got, _, err := lockfile.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	m := got.Modules["fzf"]
	if m.FallbackKind != "release" || m.FallbackSHA256 != "abc123" || m.FallbackRef != "v1.2.3" {
		t.Fatalf("round-trip mismatch: %+v", m)
	}
}

func TestLockfileWithoutFallbackKindStillLoads(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.lock.json")
	legacy := `{"schema":1,"modules":{"fzf":{"module_version":"1.0.0","enabled":true,"fallback_ref":"v1","status":"ok"}}}`
	if err := os.WriteFile(p, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok, err := lockfile.Load(p)
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if m := got.Modules["fzf"]; m.FallbackRef != "v1" || m.FallbackKind != "" || m.FallbackSHA256 != "" {
		t.Fatalf("legacy entry = %+v", m)
	}
}
```

(Add `"os"` to the imports of `lockfile_test.go` if it is not there yet.)

Append to `internal/engine/plan_internal_test.go` (add `"io"` to its imports):

```go
func releaseFallbackEntry(ref string) module.Fallback {
	return module.Fallback{
		Type: "release", Repo: "https://example.com/x", Ref: ref, Bin: "x",
		Assets: []module.Asset{{OS: "linux", Arch: "amd64", URL: "https://example.com/{{.Ref}}/x", SHA256: strings.Repeat("a", 64)}},
	}
}

func gitFallbackEntry(ref string) module.Fallback {
	return module.Fallback{Type: "git", Repo: "https://example.com/x.git", Dest: "{{.VendorDir}}/x", Ref: ref}
}

func modulePlanWith(fbs ...module.Fallback) ModulePlan {
	return ModulePlan{Manifest: module.Manifest{Packages: module.Packages{Fallback: fbs}}}
}

// engineWithBinary builds an engine whose vendor/bin/x exists when present.
func engineWithBinary(t *testing.T, present bool, arch string) Engine {
	t.Helper()
	configDir := t.TempDir()
	if present {
		bin := filepath.Join(configDir, "vendor", "bin", "x")
		if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Engine{Platform: platform.Info{OS: platform.Linux, Arch: arch, ConfigDir: configDir}}
}

func TestSelectFallback(t *testing.T) {
	amd64 := platform.Info{OS: platform.Linux, Arch: "amd64"}
	riscv := platform.Info{OS: platform.Linux, Arch: "riscv64"}
	cases := []struct {
		name     string
		fbs      []module.Fallback
		info     platform.Info
		wantType string
		wantOK   bool
	}{
		{"release comes first and matches", []module.Fallback{releaseFallbackEntry("v1"), gitFallbackEntry("v1")}, amd64, "release", true},
		{"unsupported architecture falls through to git", []module.Fallback{releaseFallbackEntry("v1"), gitFallbackEntry("v1")}, riscv, "git", true},
		{"a git entry listed first wins", []module.Fallback{gitFallbackEntry("v1"), releaseFallbackEntry("v1")}, amd64, "git", true},
		{"release only and no matching asset", []module.Fallback{releaseFallbackEntry("v1")}, riscv, "", false},
		{"no fallbacks", nil, amd64, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fb, ok := selectFallback(tc.fbs, tc.info)
			if ok != tc.wantOK || fb.Type != tc.wantType {
				t.Fatalf("selectFallback = %q, %v; want %q, %v", fb.Type, ok, tc.wantType, tc.wantOK)
			}
		})
	}
}

func TestPlanRelease(t *testing.T) {
	cases := []struct {
		name       string
		present    bool
		prev       lockfile.ModuleState
		wantMissed int
		wantUpdate bool
		wantFrom   string
	}{
		{"fresh install", false, lockfile.ModuleState{}, 1, false, ""},
		{"settled", true, lockfile.ModuleState{FallbackKind: "release", FallbackRef: "v2"}, 0, false, ""},
		{"recorded but the binary is gone", false, lockfile.ModuleState{FallbackKind: "release", FallbackRef: "v2"}, 1, false, ""},
		{"older release is replaced", true, lockfile.ModuleState{FallbackKind: "release", FallbackRef: "v1"}, 1, true, "v1"},
		{"a git build is replaced", true, lockfile.ModuleState{FallbackKind: "git", FallbackRef: "v1"}, 1, true, "v1"},
		{"a legacy lockfile entry counts as git", true, lockfile.ModuleState{FallbackRef: "v1"}, 1, true, "v1"},
		{"a binary nobody recorded is replaced", true, lockfile.ModuleState{}, 1, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mp := modulePlanWith(releaseFallbackEntry("v2"), gitFallbackEntry("v2"))
			planFallback(&mp, engineWithBinary(t, tc.present, "amd64"), tc.prev)
			if !mp.UsesFallback || mp.Fallback.Type != "release" {
				t.Fatalf("UsesFallback=%v Fallback=%+v, want the release entry", mp.UsesFallback, mp.Fallback)
			}
			if len(mp.MissingPackages) != tc.wantMissed {
				t.Fatalf("MissingPackages = %+v, want %d", mp.MissingPackages, tc.wantMissed)
			}
			if tc.wantMissed == 0 {
				return
			}
			pp := mp.MissingPackages[0]
			if pp.Manager != "release" || pp.Name != "x" || pp.To != "v2" || pp.Update != tc.wantUpdate || pp.From != tc.wantFrom {
				t.Fatalf("entry = %+v", pp)
			}
		})
	}
}

func TestPlanFallbackFallsThroughToGitOnAnUnsupportedArchitecture(t *testing.T) {
	mp := modulePlanWith(releaseFallbackEntry("v2"), gitFallbackEntry("v2"))
	planFallback(&mp, engineWithBinary(t, false, "riscv64"), lockfile.ModuleState{})
	if mp.Fallback.Type != "git" || len(mp.MissingPackages) != 1 || mp.MissingPackages[0].Manager != "git" {
		t.Fatalf("Fallback=%+v Missing=%+v, want the git entry queued", mp.Fallback, mp.MissingPackages)
	}
}

func TestPlanFallbackDegradesWhenNothingIsUsable(t *testing.T) {
	mp := modulePlanWith(releaseFallbackEntry("v2"))
	planFallback(&mp, engineWithBinary(t, false, "riscv64"), lockfile.ModuleState{})
	if mp.UsesFallback || mp.DegradedReason == "" || len(mp.MissingPackages) != 0 {
		t.Fatalf("UsesFallback=%v Reason=%q Missing=%+v", mp.UsesFallback, mp.DegradedReason, mp.MissingPackages)
	}
	if !strings.Contains(mp.DegradedReason, "linux/riscv64") {
		t.Fatalf("reason %q does not name the platform", mp.DegradedReason)
	}
}

type neverDownloader struct{ calls int }

func (n *neverDownloader) Download(string, io.Writer, int64) error {
	n.calls++
	return nil
}

func TestPlanReleaseNeverRunsCommandsOrDownloads(t *testing.T) {
	runner := &pkgmgr.MockRunner{}
	dl := &neverDownloader{}
	e := engineWithBinary(t, true, "amd64")
	e.Runner, e.Downloader = runner, dl
	mp := modulePlanWith(releaseFallbackEntry("v2"), gitFallbackEntry("v2"))
	planFallback(&mp, e, lockfile.ModuleState{FallbackKind: "release", FallbackRef: "v1"})
	if len(runner.Calls) != 0 || dl.calls != 0 {
		t.Fatalf("planning must be side-effect-free; commands=%v downloads=%d", runner.Calls, dl.calls)
	}
}

func TestDescribeReleasePackages(t *testing.T) {
	if got := describePackage(PackagePlan{Name: "x", Manager: "release", To: "v2"}); got != "x (release binary v2)" {
		t.Fatalf("fresh = %q", got)
	}
	if got := describePackage(PackagePlan{Name: "x", Manager: "release", Update: true, From: "v1", To: "v2"}); got != "x (release update v1 → v2)" {
		t.Fatalf("update = %q", got)
	}
	if got := describePackage(PackagePlan{Name: "x", Manager: "release", Update: true, To: "v2"}); got != "x (release update unrecorded → v2)" {
		t.Fatalf("unrecorded = %q", got)
	}
}
```

Existing `TestPlanFallback` tests keep working: `planFallback` still queues the git entry for git-only manifests.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/lockfile/... ./internal/engine/... -run 'FallbackKind|SelectFallback|PlanRelease|DescribeRelease|NothingIsUsable|UnsupportedArchitecture' -v 2>&1 | head -20`
Expected: FAIL to compile (`FallbackKind`, `selectFallback`, `Fallback` field undefined).

- [ ] **Step 3: Implement**

`internal/lockfile/lockfile.go` — in `ModuleState`, directly after `FallbackRef`:

```go
	// FallbackKind is the kind of fallback FallbackRef was installed by: "git"
	// or "release". Empty with a non-empty FallbackRef means a lockfile from
	// before the field existed, which always was a git clone.
	FallbackKind string `json:"fallback_kind,omitempty"`
	// FallbackSHA256 is the checksum of the release asset that was installed;
	// empty for a git build or for a Cargo build adopted as the release binary.
	FallbackSHA256 string `json:"fallback_sha256,omitempty"`
```

`internal/engine/engine.go` — add to the `Engine` struct after `Runner`:

```go
	Downloader pkgmgr.Downloader
```

`internal/engine/plan.go`:

1. In `ModulePlan`, after `UsesFallback bool`, add:

```go
	// Fallback is the fallback entry the plan selected; the zero value when the
	// module uses none.
	Fallback module.Fallback
```

2. Replace `planFallback` entirely with the following, and add the constants, `selectFallback`, `planRelease` and `releaseContext`:

```go
const (
	fallbackKindGit     = "git"
	fallbackKindRelease = "release"
)

// selectFallback returns the first fallback entry usable on this host: a
// release entry needs an asset for the host's OS and architecture, a git entry
// is always usable.
func selectFallback(fbs []module.Fallback, info platform.Info) (module.Fallback, bool) {
	for _, fb := range fbs {
		if fb.Type == "release" {
			if _, ok := fb.AssetFor(string(info.OS), info.Arch); !ok {
				continue
			}
		}
		return fb, true
	}
	return module.Fallback{}, false
}

func (e Engine) releaseContext() pkgmgr.ReleaseContext {
	return pkgmgr.ReleaseContext{VendorDir: e.vendorDir(), OS: string(e.Platform.OS), Arch: e.Platform.Arch}
}

// planFallback selects the module's fallback. A git fallback with a missing
// clone is queued as a fresh install; an existing clone is queued as an update
// when the manifest pins a ref and the clone was not recorded as built from it.
// A release fallback is planned by planRelease. The lockfile supplies the
// recorded ref and kind, so planning never probes anything but the filesystem.
func planFallback(mp *ModulePlan, e Engine, prev lockfile.ModuleState) {
	fb, ok := selectFallback(mp.Manifest.Packages.Fallback, e.Platform)
	if !ok {
		mp.DegradedReason = fmt.Sprintf("no fallback is available for %s/%s", e.Platform.OS, e.Platform.Arch)
		return
	}
	mp.UsesFallback = true
	mp.Fallback = fb
	if fb.Type == "release" {
		planRelease(mp, e, fb, prev)
		return
	}
	satisfied, _ := pkgmgr.FallbackSatisfied(fb, pkgmgr.FallbackContext{
		VendorDir: e.Platform.ConfigDir + "/vendor",
		Platform:  string(e.Platform.OS),
	})
	switch {
	case !satisfied:
		mp.MissingPackages = append(mp.MissingPackages, PackagePlan{Name: fb.Repo, Manager: "git"})
	case fb.Ref != "" && prev.FallbackRef != fb.Ref && prev.FallbackSkippedRef != fb.Ref:
		mp.MissingPackages = append(mp.MissingPackages, PackagePlan{
			Name: fb.Repo, Manager: "git", Update: true, From: prev.FallbackRef, To: fb.Ref,
		})
	}
}

// planRelease queues the release binary when it is missing or was installed
// from another ref or another kind of fallback. The recorded kind of a
// lockfile entry that has a ref but no kind is git.
func planRelease(mp *ModulePlan, e Engine, fb module.Fallback, prev lockfile.ModuleState) {
	recordedKind := prev.FallbackKind
	if recordedKind == "" && prev.FallbackRef != "" {
		recordedKind = fallbackKindGit
	}
	pp := PackagePlan{Name: fb.Bin, Manager: "release", To: fb.Ref}
	switch {
	case !pkgmgr.ReleaseInstalled(fb, e.releaseContext()):
		mp.MissingPackages = append(mp.MissingPackages, pp)
	case recordedKind == fallbackKindRelease && prev.FallbackRef == fb.Ref:
		// Settled: this ref is installed.
	default:
		pp.Update, pp.From = true, prev.FallbackRef
		mp.MissingPackages = append(mp.MissingPackages, pp)
	}
}
```

Add `"github.com/JtheGunner/omnishell/internal/platform"` to the imports of `plan.go` if it is not imported yet.

3. Extend `describePackage` — insert at its top:

```go
	if pp.Manager == "release" {
		if !pp.Update {
			return fmt.Sprintf("%s (release binary %s)", pp.Name, pp.To)
		}
		from := pp.From
		if from == "" {
			from = "unrecorded"
		}
		return fmt.Sprintf("%s (release update %s → %s)", pp.Name, from, pp.To)
	}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/lockfile/... ./internal/engine/... -race -count=1 2>&1 | tail -30`
Expected: PASS. Existing engine tests still pass because git-only manifests select their git entry.

- [ ] **Step 5: Commit**

```bash
git add internal/lockfile internal/engine
git commit -m "feat: plan release fallbacks and record their kind in the lockfile"
```

---

### Task 4: Apply wiring, lock recording and doctor

**Files:**
- Modify: `internal/engine/apply_helpers.go` (`installPackages`, `fallbackOutcome`, `newFallbackOutcome`, `rebuildLock`, `fallbackNotice`)
- Modify: `internal/engine/doctor.go` (outdated notice and the skipped-ref check)
- Modify: `internal/cli/context.go` (inject the downloader)
- Modify: `internal/engine/apply_helpers_internal_test.go` (`TestFallbackNotice`)
- Test: `internal/engine/release_apply_test.go`

**Interfaces:**
- Consumes: `ModulePlan.Fallback`, `PackagePlan{Manager:"release"}`, `Engine.Downloader`, `Engine.releaseContext()`, `pkgmgr.InstallRelease` (Tasks 2–3).
- Produces: `(e Engine) installRelease(id string, mp ModulePlan, fb module.Fallback, degraded map[string]string, vendorPaths map[string][]string, outcome *fallbackOutcome)`; `fallbackOutcome` gains `kind` and `sha` maps; a doctor notice `fallback-outdated:<id>` for release binaries.

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/release_apply_test.go`:

```go
package engine_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

const relPayload = "#!/bin/sh\necho reltool\n"

type relDownloader struct {
	body  []byte
	calls []string
}

func (d *relDownloader) Download(url string, dst io.Writer, _ int64) error {
	d.calls = append(d.calls, url)
	_, err := dst.Write(d.body)
	return err
}

func relSHA(body string) string {
	s := sha256.Sum256([]byte(body))
	return hex.EncodeToString(s[:])
}

// writeRelModule writes a user module "reltool" with a release fallback ahead
// of a git fallback, the release asset pinned to sha.
func writeRelModule(t *testing.T, dir, sha string) {
	t.Helper()
	modDir := filepath.Join(dir, "reltool")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `platforms = ["macos", "linux"]
shells    = ["bash"]
requires  = []
after     = []

[module]
id          = "reltool"
name        = "reltool"
description = "Fixture: a module with a release fallback"
version     = "1.0.0"
schema      = 1

[[packages.fallback]]
type = "release"
repo = "https://example.com/reltool"
ref  = "v1.0.0"
bin  = "reltool"

[[packages.fallback.assets]]
os     = "linux"
arch   = "amd64"
url    = "https://example.com/reltool/{{.Ref}}/reltool-linux-amd64"
sha256 = "` + sha + `"

[[packages.fallback]]
type = "git"
repo = "https://example.com/reltool.git"
dest = "{{.VendorDir}}/reltool"
ref  = "v1.0.0"
run  = ["cargo", "install", "--path", "{{.VendorDir}}/reltool"]

[options.flag]
type    = "bool"
default = false
help    = "Changing it forces a re-apply in tests"
`
	if err := os.WriteFile(filepath.Join(modDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "bash.tmpl"), []byte("echo reltool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

type relSandbox struct {
	e                 engine.Engine
	home              string
	modDir            string
	cfgPath, lockPath string
	out               *bytes.Buffer
	dl                *relDownloader
	runner            *pkgmgr.MockRunner
}

func newRelSandbox(t *testing.T) relSandbox {
	t.Helper()
	home := t.TempDir()
	out := &bytes.Buffer{}
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, out)
	modDir := t.TempDir()
	writeRelModule(t, modDir, relSHA(relPayload))
	reg, err := module.LoadRegistry(nil, modDir)
	if err != nil {
		t.Fatal(err)
	}
	runner := &pkgmgr.MockRunner{}
	dl := &relDownloader{body: []byte(relPayload)}
	e.Registry, e.Runner, e.Downloader = reg, runner, dl
	e.Platform.Arch = "amd64"
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.reltool]\nenabled=true\n")
	return relSandbox{e: e, home: home, modDir: modDir, cfgPath: cfgPath,
		lockPath: filepath.Join(home, ".config", "omnishell", "state.lock.json"), out: out, dl: dl, runner: runner}
}

func (s relSandbox) binPath() string {
	return filepath.Join(s.home, ".config", "omnishell", "vendor", "bin", "reltool")
}

func (s relSandbox) applyWith(t *testing.T, opts engine.ApplyOptions) (engine.Result, error) {
	t.Helper()
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return s.e.Apply(cfg, s.cfgPath, s.lockPath, opts)
}

func (s relSandbox) apply(t *testing.T) (engine.Result, error) {
	t.Helper()
	return s.applyWith(t, engine.ApplyOptions{Yes: true})
}

func (s relSandbox) lockState(t *testing.T) lockfile.ModuleState {
	t.Helper()
	lock, ok, err := lockfile.Load(s.lockPath)
	if err != nil || !ok {
		t.Fatalf("lock: ok=%v err=%v", ok, err)
	}
	return lock.Modules["reltool"]
}

func (s relSandbox) findings(t *testing.T) map[string]string {
	t.Helper()
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := s.e.Doctor(cfg, s.cfgPath, s.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]string{}
	for _, f := range rep.Findings {
		codes[f.Code] = f.Severity
	}
	return codes
}

func TestApplyInstallsTheReleaseBinary(t *testing.T) {
	s := newRelSandbox(t)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := os.ReadFile(s.binPath())
	if err != nil || string(got) != relPayload {
		t.Fatalf("binary = %q (err %v)", got, err)
	}
	if len(s.dl.calls) != 1 || s.dl.calls[0] != "https://example.com/reltool/v1.0.0/reltool-linux-amd64" {
		t.Fatalf("downloads = %v", s.dl.calls)
	}
	if len(s.runner.Calls) != 0 {
		t.Fatalf("a release install must not run commands: %v", s.runner.Calls)
	}
	if !strings.Contains(s.out.String(), "installing reltool v1.0.0 from its release binary") {
		t.Fatalf("output lacks the install line:\n%s", s.out.String())
	}
	st := s.lockState(t)
	if st.FallbackKind != "release" || st.FallbackRef != "v1.0.0" || st.FallbackSHA256 != relSHA(relPayload) {
		t.Fatalf("lock state = %+v", st)
	}
	found := false
	for _, vp := range st.VendorPaths {
		found = found || vp == s.binPath()
	}
	if !found {
		t.Fatalf("VendorPaths %v lacks the binary %s, so remove would leave it behind", st.VendorPaths, s.binPath())
	}
}

func TestApplyReleaseIsIdempotent(t *testing.T) {
	s := newRelSandbox(t)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := s.apply(t); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if len(s.dl.calls) != 1 {
		t.Fatalf("the second apply downloaded again: %v", s.dl.calls)
	}
}

func TestApplyReleaseChecksumMismatchDegradesTheModule(t *testing.T) {
	s := newRelSandbox(t)
	s.dl.body = []byte("tampered")
	res, err := s.apply(t)
	if !errors.Is(err, engine.ErrDegraded) {
		t.Fatalf("err = %v, want ErrDegraded", err)
	}
	mr := moduleResult(t, res, "reltool")
	if mr.Status != "degraded" || !strings.Contains(mr.Note, "checksum mismatch") {
		t.Fatalf("status=%q note=%q", mr.Status, mr.Note)
	}
	if _, statErr := os.Stat(s.binPath()); statErr == nil {
		t.Fatal("a binary with a bad checksum was installed")
	}
	if anyCallContains(s.runner.Calls, "cargo") || anyCallContains(s.runner.Calls, "git ") {
		t.Fatalf("a failed download must not fall through to the Cargo build: %v", s.runner.Calls)
	}
}

func TestApplyUsesTheGitFallbackOnAnUnsupportedArchitecture(t *testing.T) {
	s := newRelSandbox(t)
	s.e.Platform.Arch = "riscv64"
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(s.dl.calls) != 0 {
		t.Fatalf("downloaded on an unsupported architecture: %v", s.dl.calls)
	}
	if !anyCallContains(s.runner.Calls, "clone") {
		t.Fatalf("the git fallback did not run: %v", s.runner.Calls)
	}
	if got := s.lockState(t).FallbackKind; got != "git" {
		t.Fatalf("recorded kind = %q, want git", got)
	}
}

func TestApplyReplacesAGitBuildWithTheReleaseBinary(t *testing.T) {
	s := newRelSandbox(t)
	s.e.Platform.Arch = "riscv64"
	if _, err := s.apply(t); err != nil { // records a git build
		t.Fatalf("Apply: %v", err)
	}
	// The same machine now has a release asset (the manifest gained one); a
	// leftover binary from the Cargo build sits where the release binary goes.
	s.e.Platform.Arch = "amd64"
	if err := os.MkdirAll(filepath.Dir(s.binPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.binPath(), []byte("cargo-built"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.apply(t); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if got, _ := os.ReadFile(s.binPath()); string(got) != relPayload {
		t.Fatalf("binary = %q, want the release binary", got)
	}
	if got := s.lockState(t).FallbackKind; got != "release" {
		t.Fatalf("recorded kind = %q, want release", got)
	}
}

func TestApplyWithoutPackagesKeepsTheRecordedReleaseState(t *testing.T) {
	s := newRelSandbox(t)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Changing an option forces a re-apply; doctor --fix runs apply without
	// packages, and that must not forget how the binary was installed.
	writeConfig(t, s.cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.reltool]\nenabled=true\n[modules.reltool.options]\nflag=true\n")
	if _, err := s.applyWith(t, engine.ApplyOptions{Yes: true, NoPackages: true}); err != nil {
		t.Fatalf("Apply without packages: %v", err)
	}
	st := s.lockState(t)
	if st.FallbackKind != "release" || st.FallbackSHA256 != relSHA(relPayload) || st.FallbackRef != "v1.0.0" {
		t.Fatalf("a no-packages apply forgot the release state: %+v", st)
	}
}

func TestDoctorReportsAnOutdatedReleaseBinary(t *testing.T) {
	s := newRelSandbox(t)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	lock, _, err := lockfile.Load(s.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	st := lock.Modules["reltool"]
	st.FallbackRef = "v0.9.0"
	lock.Modules["reltool"] = st
	if err := lock.Write(s.lockPath); err != nil {
		t.Fatal(err)
	}
	codes := s.findings(t)
	if codes["fallback-outdated:reltool"] != engine.SeverityNotice {
		t.Fatalf("doctor lacks the fallback-outdated notice: %v", codes)
	}
	if _, bad := codes["packages-missing:reltool"]; bad {
		t.Fatalf("an outdated binary must not be reported as a missing package: %v", codes)
	}
}
```

In `internal/engine/apply_helpers_internal_test.go`, update `TestFallbackNotice` so the helper sets the selected entry (the notice now reads `mp.Fallback`):

```go
	mk := func(ref string, unavailable ...string) ModulePlan {
		fb := module.Fallback{Type: "git", Ref: ref}
		return ModulePlan{
			Manifest:            module.Manifest{Packages: module.Packages{Fallback: []module.Fallback{fb}}},
			Fallback:            fb,
			UnavailablePackages: unavailable,
		}
	}
```

and add a release case to its table plus a check below the table:

```go
	rel := mk("v1", "mise")
	rel.Fallback = module.Fallback{Type: "release", Ref: "v1"}
	if got, want := fallbackNotice("apt", rel), "mise: not available via apt, installing the release binary (ref v1)"; got != want {
		t.Errorf("release: fallbackNotice = %q, want %q", got, want)
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/engine/... -run 'Release|FallbackNotice' -v 2>&1 | head -30`
Expected: FAIL — `installPackages` ignores `Manager == "release"`, so nothing installs; `fallbackNotice` does not know releases.

- [ ] **Step 3: Implement the apply path**

In `internal/engine/apply_helpers.go`:

1. Extend `fallbackOutcome` and its constructor:

```go
type fallbackOutcome struct {
	built   map[string]string // module id -> ref the clone or binary was (re)installed from
	skipped map[string]string // module id -> pinned ref an update was declined for
	kind    map[string]string // module id -> "git" or "release", for the entries in built
	sha     map[string]string // module id -> checksum of the installed release asset
}

func newFallbackOutcome() *fallbackOutcome {
	return &fallbackOutcome{
		built: map[string]string{}, skipped: map[string]string{},
		kind: map[string]string{}, sha: map[string]string{},
	}
}
```

2. In `installPackages`, make the fallback detection cover releases and dispatch on the entry's type. Replace the `if pp.Manager == "git" {` check:

```go
			if pp.Manager == "git" || pp.Manager == "release" {
				fallback = true
				continue
			}
```

Replace the head of the fallback block (`if fallback && mp.UsesFallback && len(mp.Manifest.Packages.Fallback) > 0 { fb := mp.Manifest.Packages.Fallback[0]`) with:

```go
		if fallback && mp.UsesFallback && mp.Fallback.Type != "" {
			fb := mp.Fallback
			if fb.Type == "release" {
				e.installRelease(id, mp, fb, degraded, vendorPaths, outcome)
				continue
			}
```

keeping the rest of the git block unchanged, and record the kind where git builds are recorded — after each `outcome.built[id] = fb.Ref` (the update `default:` case and the fresh-install path) add:

```go
					outcome.kind[id] = fallbackKindGit
```

(use one more tab of indentation in the fresh-install path as appropriate).

3. Add `installRelease` after `installPackages`:

```go
// installRelease downloads and installs a module's release binary, recording
// the result for the lockfile. A failure degrades the module; it never falls
// through to the Cargo build.
func (e Engine) installRelease(id string, mp ModulePlan, fb module.Fallback, degraded map[string]string,
	vendorPaths map[string][]string, outcome *fallbackOutcome) {
	if len(mp.UnavailablePackages) > 0 {
		_, _ = fmt.Fprintln(e.Stdout, fallbackNotice(e.Manager.Name(), mp))
	}
	_, _ = fmt.Fprintf(e.Stdout, "installing %s %s from its release binary\n", fb.Bin, fb.Ref)
	res, err := pkgmgr.InstallRelease(fb, e.releaseContext(), e.Downloader)
	if err != nil {
		degraded[id] = fallbackFailure(e.Manager, mp, err)
		return
	}
	vendorPaths[id] = append(vendorPaths[id], res.BinPath)
	outcome.built[id] = fb.Ref
	outcome.kind[id] = fallbackKindRelease
	outcome.sha[id] = res.SHA256
}
```

4. In `rebuildLock`, replace the block that computes `fallbackRef, skippedRef` with:

```go
		fallbackRef, skippedRef := prevMod.FallbackRef, prevMod.FallbackSkippedRef
		fallbackKind, fallbackSHA := prevMod.FallbackKind, prevMod.FallbackSHA256
		if ref, ok := outcome.built[id]; ok {
			fallbackRef, skippedRef = ref, ""
			fallbackKind, fallbackSHA = outcome.kind[id], outcome.sha[id]
		}
		if ref, ok := outcome.skipped[id]; ok {
			skippedRef = ref
		}
		// Forget the recorded fallback state only when packages were actually
		// planned and the module no longer uses a fallback. `apply --no-packages`
		// (which is what doctor --fix runs) and a planner-degraded module plan no
		// packages and must keep what an earlier apply recorded.
		if mp.PackagesPlanned && !mp.UsesFallback {
			fallbackRef, skippedRef, fallbackKind, fallbackSHA = "", "", "", ""
		}
```

and add the two fields to the `lockfile.ModuleState{…}` literal below it:

```go
			FallbackKind:       fallbackKind,
			FallbackSHA256:     fallbackSHA,
```

5. Update `fallbackNotice` to read the selected entry and word releases:

```go
func fallbackNotice(manager string, mp ModulePlan) string {
	if len(mp.UnavailablePackages) == 0 || mp.Fallback.Type == "" {
		return ""
	}
	source := "unpinned"
	if ref := mp.Fallback.Ref; ref != "" {
		source = "ref " + ref
	}
	action := "building from git"
	if mp.Fallback.Type == "release" {
		action = "installing the release binary"
	}
	return fmt.Sprintf("%s: not available via %s, %s (%s)",
		strings.Join(mp.UnavailablePackages, ", "), manager, action, source)
}
```

Also update its doc comment to say "tells the user a fallback replaces a distro package".

In `internal/engine/doctor.go`, change the `if pp.Update {` branch of the missing-packages loop so release entries get their own wording, and use the selected entry for the skipped-ref check:

```go
			if pp.Update {
				from := pp.From
				if from == "" {
					from = "unrecorded"
				}
				what := "fallback clone was built from"
				if pp.Manager == "release" {
					what = "installed release binary is"
				}
				add(SeverityNotice, "fallback-outdated:"+id,
					fmt.Sprintf("module %q: %s %s, the manifest pins %s (run apply to update)", id, what, from, pp.To))
				continue
			}
```

```go
		if st := lock.Modules[id]; mp.UsesFallback && mp.Fallback.Type != "" &&
			st.FallbackSkippedRef != "" && st.FallbackSkippedRef == mp.Fallback.Ref {
```

(the existing `fallback-outdated` test asserts only the code, so the message change is safe).

In `internal/cli/context.go`, add to the `engine.Engine{…}` literal after `Runner: runner,`:

```go
		Downloader: pkgmgr.NewHTTPDownloader(),
```

(run `gofmt -w` afterwards so the literal stays aligned).

- [ ] **Step 4: Run the engine and CLI tests**

Run: `go test ./internal/engine/... ./internal/cli/... -race -count=1 2>&1 | tail -30`
Expected: PASS, including every existing fallback test.
If an existing internal test builds a `ModulePlan` by hand and passes it to `installPackages` or `fallbackNotice`, set its `Fallback` field to the entry from its manifest: the apply path now reads the selected entry from `mp.Fallback`.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal && git add internal && git commit -m "feat: install release fallbacks during apply and record them in the lockfile"
```

---

### Task 5: Built-in manifests, builtin tests and Phase 1 docs

**Files:**
- Modify: `modules/builtin/mise/manifest.toml`, `modules/builtin/starship/manifest.toml`, `modules/builtin/broot/manifest.toml`
- Modify: `modules/builtin_test.go`
- Modify: `README.md`, `docs/writing-a-module.md`, `CHANGELOG.md`

**Interfaces:**
- Consumes: the `release` schema (Task 1).
- Produces: every built-in `release` entry covers `linux/amd64` and `linux/arm64` and precedes the module's `git` entry.

- [ ] **Step 1: Write the failing builtin tests**

Append to `modules/builtin_test.go`:

```go
// The three Rust modules ship a release fallback ahead of the Cargo build; each
// covers both supported architectures, so an arm64 server needs no toolchain.
func TestBuiltinReleaseFallbacks(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"mise", "starship", "broot"} {
		m, ok := reg.Get(id)
		if !ok {
			t.Fatalf("module %s missing", id)
		}
		fbs := m.Manifest.Packages.Fallback
		if len(fbs) != 2 || fbs[0].Type != "release" || fbs[1].Type != "git" {
			t.Errorf("module %s: fallbacks = %+v, want release then git", id, fbs)
			continue
		}
		for _, arch := range []string{"amd64", "arm64"} {
			if _, ok := fbs[0].AssetFor("linux", arch); !ok {
				t.Errorf("module %s: release fallback has no linux/%s asset", id, arch)
			}
		}
		if fbs[0].Ref != fbs[1].Ref {
			t.Errorf("module %s: release ref %s and git ref %s must pin the same tag", id, fbs[0].Ref, fbs[1].Ref)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./modules/... -run BuiltinReleaseFallbacks -v`
Expected: FAIL — `fallbacks = [...], want release then git`.

- [ ] **Step 3: Add the release entries**

In each manifest, insert the `release` entry **before** the existing `[[packages.fallback]]` (type `git`) and update the comment above `[packages]` to say that release binaries come before the source build.

`modules/builtin/mise/manifest.toml` — replace the comment and add the entry:

```toml
# mise is not in the base apt / dnf / zypper / apk repos (it ships its own),
# so only brew and pacman get a system package. Everything else installs the
# upstream release binary, and only builds from source where no release
# binary exists for the host.
[packages]
brew   = ["mise"]
pacman = ["mise"]

[[packages.fallback]]
type = "release"
repo = "https://github.com/jdx/mise"
ref  = "v2026.10.3"
bin  = "mise"

[[packages.fallback.assets]]
os     = "linux"
arch   = "amd64"
url    = "https://github.com/jdx/mise/releases/download/{{.Ref}}/mise-{{.Ref}}-linux-x64-musl.tar.gz"
sha256 = "23c45c76567f8f1e4cfc40efff3f19e3352d3745e6c25e74891102765d1cdc3c"
member = "mise/bin/mise"

[[packages.fallback.assets]]
os     = "linux"
arch   = "arm64"
url    = "https://github.com/jdx/mise/releases/download/{{.Ref}}/mise-{{.Ref}}-linux-arm64-musl.tar.gz"
sha256 = "0af3379b7a8060b047416576a746078daee3c9d2bf57fa8f6ba07d3cdf5f47aa"
member = "mise/bin/mise"
```

`modules/builtin/starship/manifest.toml`:

```toml
# starship is in brew / pacman / apk and recent apt; dnf / zypper and older
# releases install the upstream release binary, and only build from source
# (starship is Rust) where no release binary exists for the host.
[packages]
brew   = ["starship"]
apt    = ["starship"]
pacman = ["starship"]
apk    = ["starship"]

[[packages.fallback]]
type = "release"
repo = "https://github.com/starship/starship"
ref  = "v1.26.0"
bin  = "starship"

[[packages.fallback.assets]]
os     = "linux"
arch   = "amd64"
url    = "https://github.com/starship/starship/releases/download/{{.Ref}}/starship-x86_64-unknown-linux-musl.tar.gz"
sha256 = "b7c232b0e8249d8e55a40beb79c5c43a7d370f3f9408bd215deb0170daeaadf3"
member = "starship"

[[packages.fallback.assets]]
os     = "linux"
arch   = "arm64"
url    = "https://github.com/starship/starship/releases/download/{{.Ref}}/starship-aarch64-unknown-linux-musl.tar.gz"
sha256 = "dc30189378d2f2e287384e8a692d3f95ad1df64cf0e8c36aa9201516028aed6b"
member = "starship"
```

`modules/builtin/broot/manifest.toml`:

```toml
# zypper / apk don't ship broot in the base repos, and older apt releases lack
# it too → the upstream release binary, with a source build only where no
# release binary exists for the host.
[packages]
brew   = ["broot"]
apt    = ["broot"]
dnf    = ["broot"]
pacman = ["broot"]

[[packages.fallback]]
type = "release"
repo = "https://github.com/Canop/broot"
ref  = "v1.61.0"
bin  = "broot"

[[packages.fallback.assets]]
os     = "linux"
arch   = "amd64"
url    = "https://github.com/Canop/broot/releases/download/{{.Ref}}/broot_{{.Version}}.zip"
sha256 = "6cafe8e993fb8e5684eb25549703a4fb9dec61ac6a8f1947cbed6566cb4e58e5"
member = "x86_64-unknown-linux-musl/broot"

[[packages.fallback.assets]]
os     = "linux"
arch   = "arm64"
url    = "https://github.com/Canop/broot/releases/download/{{.Ref}}/broot_{{.Version}}.zip"
sha256 = "6cafe8e993fb8e5684eb25549703a4fb9dec61ac6a8f1947cbed6566cb4e58e5"
member = "aarch64-unknown-linux-musl/broot"
```

Keep each manifest's existing `[[packages.fallback]]` (git) table and `[options.*]` tables unchanged after the new entry.

- [ ] **Step 4: Verify the pinned checksums against upstream**

Run (read-only; compares each pinned `sha256` with the digest GitHub reports):

```bash
cd modules/builtin
for pair in "mise:jdx/mise" "starship:starship/starship" "broot:Canop/broot"; do
  id=${pair%%:*}; repo=${pair#*:}
  tag=$(grep -m1 '^ref' $id/manifest.toml | sed 's/.*"\(.*\)"/\1/')
  echo "== $id $tag"
  gh api repos/$repo/releases/tags/$tag --jq '.assets[] | "\(.digest | sub("sha256:";"")) \(.name)"' > /tmp/digests-$id.txt
  grep -o 'sha256 = "[0-9a-f]*"' $id/manifest.toml | sed 's/sha256 = "\(.*\)"/\1/' | while read -r sha; do
    grep -q "^$sha " /tmp/digests-$id.txt && echo "ok   $sha" || echo "MISS $sha"
  done
done
```

Expected: every line prints `ok`. Any `MISS` means a checksum was mistyped: correct it from `/tmp/digests-<id>.txt` before continuing.

- [ ] **Step 5: Run the tests**

Run: `go test ./modules/... ./internal/module/... -count=1 2>&1 | tail -20`
Expected: PASS (`TestBuiltinFallbacksArePinned` also passes: the release entries carry a `ref`).

- [ ] **Step 6: Update the docs**

`docs/writing-a-module.md` — directly after the section that documents the `git` fallback (the `[[packages.fallback]]` example near line 77), add:

````markdown
### Release fallback

A module can install a ready-made binary instead of building from source. List a `type = "release"` entry **before** the `git` entry; omnishell uses the first entry that fits the host, so the build stays the last resort:

```toml
[[packages.fallback]]
type = "release"
repo = "https://github.com/jdx/mise"   # used by the tag updater
ref  = "v2026.10.3"                     # the pinned upstream tag
bin  = "mise"                           # installed as <vendor>/bin/mise

[[packages.fallback.assets]]
os     = "linux"
arch   = "amd64"                        # amd64 | arm64
url    = "https://github.com/jdx/mise/releases/download/{{.Ref}}/mise-{{.Ref}}-linux-x64-musl.tar.gz"
sha256 = "…64 lowercase hex characters…"
member = "mise/bin/mise"                # file inside the .tar.gz / .zip; omit for a raw binary
```

- `url` must be `https://`. `{{.Ref}}` is the pinned tag and `{{.Version}}` the tag without a leading `v`.
- `.tar.gz` and `.zip` downloads are extracted (`member` is required); any other URL is taken as the binary itself (`member` must be omitted).
- `sha256` pins the downloaded file; a mismatch aborts the install and degrades the module.
- Prefer static (musl) builds: they run on glibc and musl systems alike, so no libc detection is needed. A host without a matching `(os, arch)` asset moves on to the next fallback entry.
- `requires` is not allowed on a release entry; it belongs to the `git` entry.
````

`README.md` — in the module table, change the fallback column text for the three modules: for `mise` use ```mise` (`brew`, `pacman`); else release binary, then `git` + `cargo` fallback``, for `starship` and `broot` replace ``; else `git` + `cargo` fallback (needs Rust)`` with ``; else release binary, then `git` + `cargo` fallback``. In the section `### 🔨 Build prerequisites for the \`git\` fallback` (near line 184), add this note directly under its heading line:

```markdown
> [!NOTE]
> `mise`, `starship` and `broot` install a verified release binary first (Linux x86_64 and arm64). The prerequisites below only apply to them on other architectures.
```

Also adjust the `vendor/` row (near line 134) to read: ``Clones made by the `git` package fallback (`apply` moves them to the tag the manifest pins and rebuilds) and `bin/` with release binaries.``

`CHANGELOG.md` — under `## [Unreleased]` → `### Added`, add:

```markdown
- `[[packages.fallback]]` accepts `type = "release"`: a pinned, SHA-256-verified
  binary per OS and architecture (`[[packages.fallback.assets]]`), installed to
  `<vendor>/bin`. It is listed before the `git` entry, so the order is system
  package → release binary → source build. A host with no matching asset moves
  on to the source build; a failed download or checksum degrades the module.
- `mise`, `starship` and `broot` install their upstream release binary on
  Linux x86_64 and arm64 instead of building with Cargo (about 22 minutes for
  `mise`). Their `requires` now apply only to the source build.
```

- [ ] **Step 7: Phase 1 verification and commit**

Run: `go vet ./... && make test 2>&1 | tail -15`
Expected: vet clean, every package `ok`.

```bash
git add modules README.md docs CHANGELOG.md
git commit -m "feat: ship release binaries for mise, starship and broot"
```

---

# Phase 2 — migration, PATH check and the tag updater

### Task 6: Cleanup of old Cargo builds and adopting a matching build

**Files:**
- Create: `internal/pkgmgr/leftovers.go`
- Modify: `internal/engine/plan.go` (`PackagePlan`, `ModulePlan`, `planRelease`, `RenderPlan`)
- Modify: `internal/engine/apply_helpers.go` (`installRelease`)
- Test: `internal/pkgmgr/leftovers_test.go`, `internal/engine/plan_internal_test.go`, `internal/engine/release_apply_test.go`

**Interfaces:**
- Consumes: `module.Fallback` (git entry: `Dest`, `Repo`), `pkgmgr.FallbackContext`, `atomicfile.WriteFile`.
- Produces:
  - `type LeftoverKind string` with `LeftoverTree = "source-tree"`, `LeftoverCrateEntry = "crates-entry"`
  - `type Leftover struct{ Kind LeftoverKind; Path, Key string }`, `func (l Leftover) Describe() string`
  - `func FindLeftovers(gitFB module.Fallback, ctx FallbackContext) (found []Leftover, skipped []string)` — read-only; `skipped` holds human-readable reasons for things it left alone
  - `func RemoveLeftover(l Leftover) error`
  - `PackagePlan.Adopt bool`; `ModulePlan.Cleanup []pkgmgr.Leftover`, `ModulePlan.CleanupNotes []string`
  - `func gitFallbackOf(fbs []module.Fallback) (module.Fallback, bool)` (engine)

- [ ] **Step 1: Write the failing leftover tests**

Create `internal/pkgmgr/leftovers_test.go`:

```go
package pkgmgr_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

const toolRepo = "https://github.com/o/tool.git"

func gitFB() module.Fallback {
	return module.Fallback{Type: "git", Repo: toolRepo, Dest: "{{.VendorDir}}/tool", Ref: "v1"}
}

// buildSandbox lays out an old Cargo build under a vendor dir with a space in
// its name: a clone with a target/ dir, plus cargo metadata that also lists
// another tool.
func buildSandbox(t *testing.T, remote string) pkgmgr.FallbackContext {
	t.Helper()
	vendor := filepath.Join(t.TempDir(), "my vendor")
	tree := filepath.Join(vendor, "tool")
	for _, dir := range []string{filepath.Join(tree, ".git"), filepath.Join(tree, "target")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitConfig := "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = " + remote + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	writeFile(t, filepath.Join(tree, ".git", "config"), gitConfig)
	writeFile(t, filepath.Join(tree, "target", "artifact"), "x")
	ours := "tool 1.0.0 (path+file://" + tree + ")"
	other := "other 2.0.0 (path+file://" + filepath.Join(vendor, "other") + ")"
	writeFile(t, filepath.Join(vendor, ".crates.toml"),
		"[v1]\n\""+ours+"\" = [\"tool\"]\n\""+other+"\" = [\"other\"]\n")
	writeFile(t, filepath.Join(vendor, ".crates2.json"),
		`{"installs":{"`+ours+`":{"version_req":null},"`+other+`":{"version_req":null}},"extra":1}`)
	return pkgmgr.FallbackContext{VendorDir: vendor, Platform: "linux"}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func kinds(ls []pkgmgr.Leftover) map[pkgmgr.LeftoverKind]int {
	m := map[pkgmgr.LeftoverKind]int{}
	for _, l := range ls {
		m[l.Kind]++
	}
	return m
}

func TestFindLeftoversListsOnlyThisBuild(t *testing.T) {
	ctx := buildSandbox(t, toolRepo)
	found, skipped := pkgmgr.FindLeftovers(gitFB(), ctx)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v", skipped)
	}
	got := kinds(found)
	if got[pkgmgr.LeftoverTree] != 1 || got[pkgmgr.LeftoverCrateEntry] != 2 || len(found) != 3 {
		t.Fatalf("found = %+v, want the tree and one entry in each metadata file", found)
	}
}

func TestRemoveLeftoversKeepsOtherToolsMetadata(t *testing.T) {
	ctx := buildSandbox(t, toolRepo)
	found, _ := pkgmgr.FindLeftovers(gitFB(), ctx)
	for _, l := range found {
		if err := pkgmgr.RemoveLeftover(l); err != nil {
			t.Fatalf("RemoveLeftover(%+v): %v", l, err)
		}
	}
	if _, err := os.Stat(filepath.Join(ctx.VendorDir, "tool")); !os.IsNotExist(err) {
		t.Fatalf("source tree still exists (err %v)", err)
	}
	toml, err := os.ReadFile(filepath.Join(ctx.VendorDir, ".crates.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(toml), "tool 1.0.0") || !strings.Contains(string(toml), "other 2.0.0") || !strings.Contains(string(toml), "[v1]") {
		t.Fatalf(".crates.toml after cleanup:\n%s", toml)
	}
	raw, err := os.ReadFile(filepath.Join(ctx.VendorDir, ".crates2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Installs map[string]json.RawMessage `json:"installs"`
		Extra    int                        `json:"extra"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf(".crates2.json is no longer valid JSON: %v\n%s", err, raw)
	}
	if len(doc.Installs) != 1 || doc.Extra != 1 {
		t.Fatalf(".crates2.json after cleanup = %s", raw)
	}
	for key := range doc.Installs {
		if !strings.HasPrefix(key, "other 2.0.0") {
			t.Fatalf("remaining install = %q, want the other tool's", key)
		}
	}
}

func TestFindLeftoversLeavesAForeignCloneAlone(t *testing.T) {
	ctx := buildSandbox(t, "https://github.com/someone-else/tool.git")
	found, _ := pkgmgr.FindLeftovers(gitFB(), ctx)
	if kinds(found)[pkgmgr.LeftoverTree] != 0 {
		t.Fatalf("a clone of another repository was listed for deletion: %+v", found)
	}
}

func TestFindLeftoversLeavesANonCloneAlone(t *testing.T) {
	ctx := buildSandbox(t, toolRepo)
	if err := os.RemoveAll(filepath.Join(ctx.VendorDir, "tool", ".git")); err != nil {
		t.Fatal(err)
	}
	found, _ := pkgmgr.FindLeftovers(gitFB(), ctx)
	if kinds(found)[pkgmgr.LeftoverTree] != 0 {
		t.Fatalf("a directory that is not a git clone was listed for deletion: %+v", found)
	}
}

func TestFindLeftoversIgnoresAPathOutsideTheVendorDir(t *testing.T) {
	ctx := buildSandbox(t, toolRepo)
	fb := gitFB()
	fb.Dest = filepath.Join(t.TempDir(), "elsewhere")
	found, _ := pkgmgr.FindLeftovers(fb, ctx)
	if len(found) != 0 {
		t.Fatalf("found = %+v for a dest outside the vendor dir", found)
	}
}

func TestFindLeftoversReportsUnparsableMetadata(t *testing.T) {
	ctx := buildSandbox(t, toolRepo)
	writeFile(t, filepath.Join(ctx.VendorDir, ".crates2.json"), "{not json")
	found, skipped := pkgmgr.FindLeftovers(gitFB(), ctx)
	if kinds(found)[pkgmgr.LeftoverCrateEntry] != 1 {
		t.Fatalf("found = %+v, want only the .crates.toml entry", found)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], ".crates2.json") {
		t.Fatalf("skipped = %v, want a note naming .crates2.json", skipped)
	}
}

func TestFindLeftoversWithNothingToClean(t *testing.T) {
	ctx := pkgmgr.FallbackContext{VendorDir: filepath.Join(t.TempDir(), "my vendor"), Platform: "linux"}
	found, skipped := pkgmgr.FindLeftovers(gitFB(), ctx)
	if len(found) != 0 || len(skipped) != 0 {
		t.Fatalf("found=%+v skipped=%v on an empty vendor dir", found, skipped)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/pkgmgr/... -run 'Leftover' -v 2>&1 | head -15`
Expected: FAIL to compile (`pkgmgr.FindLeftovers` undefined).

- [ ] **Step 3: Implement leftovers**

Create `internal/pkgmgr/leftovers.go`:

```go
package pkgmgr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
	"github.com/JtheGunner/omnishell/internal/module"
)

// LeftoverKind says what a Leftover is.
type LeftoverKind string

const (
	// LeftoverTree is the cloned source tree (with its target/ directory).
	LeftoverTree LeftoverKind = "source-tree"
	// LeftoverCrateEntry is this build's entry in cargo's install metadata.
	LeftoverCrateEntry LeftoverKind = "crates-entry"
)

// Leftover is one piece of an old Cargo build that a release install makes
// obsolete. Path is the tree or the metadata file; Key names the metadata entry.
type Leftover struct {
	Kind LeftoverKind
	Path string
	Key  string
}

// Describe words the leftover for plan and log output.
func (l Leftover) Describe() string {
	if l.Kind == LeftoverCrateEntry {
		return fmt.Sprintf("%s (cargo install entry %q)", l.Path, l.Key)
	}
	return l.Path
}

// FindLeftovers lists what an earlier `cargo install --path <dest> --root
// <vendor>` build of gitFB left behind, without changing anything. A tree is
// listed only when it lives inside the vendor dir and is a git clone of
// gitFB.Repo. Cargo metadata entries are matched by their source path, so
// another tool's entries are never touched. Things it cannot judge (an
// unparsable metadata file) are left alone and described in skipped.
func FindLeftovers(gitFB module.Fallback, ctx FallbackContext) (found []Leftover, skipped []string) {
	dest, err := renderPath(gitFB.Dest, ctx)
	if err != nil || dest == "" || filepath.Dir(dest) != filepath.Clean(ctx.VendorDir) {
		return nil, nil
	}
	if populatedDir(dest) && isCloneOf(dest, gitFB.Repo) {
		found = append(found, Leftover{Kind: LeftoverTree, Path: dest})
	}
	suffix := "(path+file://" + dest + ")"
	tomlPath := filepath.Join(ctx.VendorDir, ".crates.toml")
	if keys, err := crateKeysTOML(tomlPath); err != nil {
		skipped = append(skipped, fmt.Sprintf("cannot read %s: %v", tomlPath, err))
	} else {
		found = append(found, entriesWithSuffix(tomlPath, keys, suffix)...)
	}
	jsonPath := filepath.Join(ctx.VendorDir, ".crates2.json")
	if keys, err := crateKeysJSON(jsonPath); err != nil {
		skipped = append(skipped, fmt.Sprintf("cannot parse %s: %v", jsonPath, err))
	} else {
		found = append(found, entriesWithSuffix(jsonPath, keys, suffix)...)
	}
	return found, skipped
}

// RemoveLeftover deletes one leftover. Metadata files are rewritten atomically
// without the entry; they are never deleted.
func RemoveLeftover(l Leftover) error {
	switch l.Kind {
	case LeftoverTree:
		return os.RemoveAll(l.Path)
	case LeftoverCrateEntry:
		if strings.HasSuffix(l.Path, ".json") {
			return removeCrateEntryJSON(l.Path, l.Key)
		}
		return removeCrateEntryTOML(l.Path, l.Key)
	}
	return fmt.Errorf("unknown leftover kind %q", l.Kind)
}

func entriesWithSuffix(path string, keys []string, suffix string) []Leftover {
	var out []Leftover
	for _, key := range keys {
		if strings.HasSuffix(key, suffix) {
			out = append(out, Leftover{Kind: LeftoverCrateEntry, Path: path, Key: key})
		}
	}
	return out
}

// isCloneOf reports whether dir is a git clone whose origin is repo. It reads
// .git/config directly, so no command runs.
func isCloneOf(dir, repo string) bool {
	f, err := os.Open(filepath.Join(dir, ".git", "config"))
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	inOrigin := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if !inOrigin {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok && strings.TrimSpace(key) == "url" {
			return normalizeRepoURL(value) == normalizeRepoURL(repo)
		}
	}
	return false
}

func normalizeRepoURL(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	return strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
}

// crateKeysTOML returns the install keys of a .crates.toml: the quoted keys at
// the start of a line. A missing file has no keys.
func crateKeysTOML(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, line := range strings.Split(string(data), "\n") {
		if key, ok := tomlLineKey(line); ok {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func tomlLineKey(line string) (string, bool) {
	if !strings.HasPrefix(line, `"`) {
		return "", false
	}
	end := strings.Index(line[1:], `"`)
	if end < 0 {
		return "", false
	}
	return line[1 : 1+end], true
}

func removeCrateEntryTOML(path, key string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	kept := lines[:0]
	for _, line := range lines {
		if k, ok := tomlLineKey(line); ok && k == key {
			continue
		}
		kept = append(kept, line)
	}
	return atomicfile.WriteFile(path, []byte(strings.Join(kept, "\n")), info.Mode().Perm())
}

type cratesDoc map[string]json.RawMessage

func readCratesJSON(path string) (cratesDoc, map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var doc cratesDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	var installs map[string]json.RawMessage
	if raw, ok := doc["installs"]; ok {
		if err := json.Unmarshal(raw, &installs); err != nil {
			return nil, nil, err
		}
	}
	return doc, installs, nil
}

func crateKeysJSON(path string) ([]string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	_, installs, err := readCratesJSON(path)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(installs))
	for k := range installs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func removeCrateEntryJSON(path, key string) error {
	doc, installs, err := readCratesJSON(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	delete(installs, key)
	rawInstalls, err := json.Marshal(installs)
	if err != nil {
		return err
	}
	doc["installs"] = rawInstalls
	out, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, out, info.Mode().Perm())
}
```

- [ ] **Step 4: Run the leftover tests**

Run: `go test ./internal/pkgmgr/... -run 'Leftover' -race -count=1 -v 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Write the failing engine tests (plan and apply)**

In `internal/engine/plan_internal_test.go`, **change** the `TestPlanRelease` row `"a git build is replaced"`: a git record at the pinned ref now adopts. Replace that row with the two rows below and add `wantAdopt bool` to the case struct and the assertion; add `TestPlanReleaseCleanup` after it:

```go
		{"a git build at the pinned ref is adopted", true, lockfile.ModuleState{FallbackKind: "git", FallbackRef: "v2"}, 1, false, "", true},
		{"a git build at another ref is replaced", true, lockfile.ModuleState{FallbackKind: "git", FallbackRef: "v1"}, 1, true, "v1", false},
```

(Every other row gets a trailing `false` for `wantAdopt`; in the loop assert `pp.Adopt == tc.wantAdopt`. For the adopt row `pp.Update` is false and `pp.From` is empty, which the row already encodes.)

```go
func TestPlanReleaseCleanup(t *testing.T) {
	e := engineWithBinary(t, true, "amd64")
	vendor := filepath.Join(e.Platform.ConfigDir, "vendor")
	tree := filepath.Join(vendor, "x")
	if err := os.MkdirAll(filepath.Join(tree, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitConfig := "[remote \"origin\"]\n\turl = https://example.com/x.git\n"
	if err := os.WriteFile(filepath.Join(tree, ".git", "config"), []byte(gitConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "Cargo.toml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name        string
		prev        lockfile.ModuleState
		wantCleanup bool
	}{
		{"adopting a matching Cargo build cleans its source", lockfile.ModuleState{FallbackKind: "git", FallbackRef: "v2"}, true},
		{"replacing an older Cargo build cleans its source", lockfile.ModuleState{FallbackKind: "git", FallbackRef: "v1"}, true},
		{"a settled release install has nothing to clean", lockfile.ModuleState{FallbackKind: "release", FallbackRef: "v2"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mp := modulePlanWith(releaseFallbackEntry("v2"), gitFallbackEntry("v2"))
			planFallback(&mp, e, tc.prev)
			if got := len(mp.Cleanup) > 0; got != tc.wantCleanup {
				t.Fatalf("Cleanup = %+v, want cleanup=%v", mp.Cleanup, tc.wantCleanup)
			}
		})
	}

	// Planning lists the cleanup but removes nothing.
	if _, err := os.Stat(filepath.Join(tree, "Cargo.toml")); err != nil {
		t.Fatalf("ComputePlan-side planning deleted the source tree: %v", err)
	}
}

func TestRenderPlanListsTheCleanup(t *testing.T) {
	mp := modulePlanWith(releaseFallbackEntry("v2"))
	mp.ID, mp.Action = "x", ActionUpdate
	mp.MissingPackages = []PackagePlan{{Name: "x", Manager: "release", Adopt: true, To: "v2"}}
	mp.Cleanup = []pkgmgr.Leftover{{Kind: pkgmgr.LeftoverTree, Path: "/v/x"}}
	got := RenderPlan(Plan{Order: []string{"x"}, Modules: map[string]ModulePlan{"x": mp}, ManagedShells: []string{"bash"}})
	for _, want := range []string{"adopt the Cargo build v2", "remove build leftover /v/x"} {
		if !strings.Contains(got, want) {
			t.Fatalf("plan lacks %q:\n%s", want, got)
		}
	}
}
```

Update `TestDescribeReleasePackages` with one more assertion:

```go
	if got := describePackage(PackagePlan{Name: "x", Manager: "release", Adopt: true, To: "v2"}); got != "x (adopt the Cargo build v2)" {
		t.Fatalf("adopt = %q", got)
	}
```

Append to `internal/engine/release_apply_test.go`:

```go
// seedCargoBuild lays out an old Cargo build for reltool under the sandbox's
// vendor dir: a clone of the git fallback's repo, a binary, and cargo metadata
// that also lists another tool.
func (s relSandbox) seedCargoBuild(t *testing.T) (tree, crates string) {
	t.Helper()
	vendor := filepath.Join(s.home, ".config", "omnishell", "vendor")
	tree = filepath.Join(vendor, "reltool")
	if err := os.MkdirAll(filepath.Join(tree, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitConfig := "[remote \"origin\"]\n\turl = https://example.com/reltool.git\n"
	if err := os.WriteFile(filepath.Join(tree, ".git", "config"), []byte(gitConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "Cargo.toml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vendor, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.binPath(), []byte("cargo-built"), 0o755); err != nil {
		t.Fatal(err)
	}
	crates = filepath.Join(vendor, ".crates.toml")
	body := "[v1]\n\"reltool 1.0.0 (path+file://" + tree + ")\" = [\"reltool\"]\n\"other 2.0.0 (path+file://" + filepath.Join(vendor, "other") + ")\" = [\"other\"]\n"
	if err := os.WriteFile(crates, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return tree, crates
}

func (s relSandbox) recordGitBuild(t *testing.T, ref string) {
	t.Helper()
	lock := lockfile.Lock{Schema: lockfile.SchemaVersion, Modules: map[string]lockfile.ModuleState{
		"reltool": {ModuleVersion: "1.0.0", Enabled: true, FallbackKind: "git", FallbackRef: ref, Status: "ok"},
	}}
	if err := lock.Write(s.lockPath); err != nil {
		t.Fatal(err)
	}
}

func TestApplyAdoptsAMatchingCargoBuild(t *testing.T) {
	s := newRelSandbox(t)
	tree, crates := s.seedCargoBuild(t)
	s.recordGitBuild(t, "v1.0.0") // built from the pinned tag

	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(s.dl.calls) != 0 {
		t.Fatalf("downloaded although the Cargo build already is the pinned version: %v", s.dl.calls)
	}
	if got, _ := os.ReadFile(s.binPath()); string(got) != "cargo-built" {
		t.Fatalf("the Cargo binary was replaced by %q", got)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatalf("source tree not cleaned up (err %v)", err)
	}
	meta, _ := os.ReadFile(crates)
	if strings.Contains(string(meta), "reltool 1.0.0") || !strings.Contains(string(meta), "other 2.0.0") {
		t.Fatalf(".crates.toml after cleanup:\n%s", meta)
	}
	st := s.lockState(t)
	if st.FallbackKind != "release" || st.FallbackSHA256 != "" || st.FallbackRef != "v1.0.0" {
		t.Fatalf("lock state = %+v, want an adopted release record with no checksum", st)
	}
	if !strings.Contains(s.out.String(), "removing build leftover") {
		t.Fatalf("output does not log the cleanup:\n%s", s.out.String())
	}

	before := len(s.dl.calls)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if len(s.dl.calls) != before {
		t.Fatalf("the run after adoption downloaded: %v", s.dl.calls)
	}
}

func TestApplyReplacesAnOlderCargoBuildAndCleansUp(t *testing.T) {
	s := newRelSandbox(t)
	tree, _ := s.seedCargoBuild(t)
	s.recordGitBuild(t, "v0.9.0")

	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, _ := os.ReadFile(s.binPath()); string(got) != relPayload {
		t.Fatalf("binary = %q, want the release binary", got)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatalf("source tree not cleaned up (err %v)", err)
	}
}

func TestApplyLeavesAForeignDirectoryAndStillSucceeds(t *testing.T) {
	s := newRelSandbox(t)
	tree, _ := s.seedCargoBuild(t)
	other := "[remote \"origin\"]\n\turl = https://example.com/someone-elses.git\n"
	if err := os.WriteFile(filepath.Join(tree, ".git", "config"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	s.recordGitBuild(t, "v0.9.0")

	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tree, "Cargo.toml")); err != nil {
		t.Fatalf("a directory that is not our clone was deleted: %v", err)
	}
}

func TestDryRunPlansTheCleanupWithoutTouchingDisk(t *testing.T) {
	s := newRelSandbox(t)
	tree, _ := s.seedCargoBuild(t)
	s.recordGitBuild(t, "v1.0.0")

	res, err := s.applyWith(t, engine.ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run Apply: %v", err)
	}
	if !strings.Contains(res.PlanText, "remove build leftover") {
		t.Fatalf("the plan does not show the cleanup:\n%s", res.PlanText)
	}
	if _, err := os.Stat(filepath.Join(tree, "Cargo.toml")); err != nil {
		t.Fatalf("a dry run deleted the source tree: %v", err)
	}
	if len(s.dl.calls) != 0 {
		t.Fatalf("a dry run downloaded: %v", s.dl.calls)
	}
}
```

The existing `TestApplyReplacesAGitBuildWithTheReleaseBinary` (Task 4) records a git build at the **pinned** ref `v1.0.0` via a real riscv64 apply, with a leftover binary. Under the new rule that run adopts instead of downloading. Update its final assertion block so it keeps testing replacement: make the first apply happen with the manifest's ref unchanged but record an older ref before the second apply, i.e. insert after the first `s.apply(t)`:

```go
	lock, _, err := lockfile.Load(s.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	st := lock.Modules["reltool"]
	st.FallbackRef = "v0.9.0"
	lock.Modules["reltool"] = st
	if err := lock.Write(s.lockPath); err != nil {
		t.Fatal(err)
	}
```

- [ ] **Step 6: Run the engine tests to verify they fail**

Run: `go test ./internal/engine/... -run 'Adopt|Cleanup|DryRun|Foreign|OlderCargo|PlanRelease|RenderPlanLists|DescribeRelease' -v 2>&1 | head -30`
Expected: FAIL to compile (`PackagePlan.Adopt`, `ModulePlan.Cleanup` undefined).

- [ ] **Step 7: Implement plan and apply for adoption and cleanup**

`internal/engine/plan.go`:

1. In `PackagePlan`, add after `From, To string`:

```go
	// Adopt marks a release fallback whose binary is a Cargo build of the
	// pinned ref: it is kept as the release binary and only its build
	// leftovers are cleaned up.
	Adopt bool
```

2. In `ModulePlan`, after `Fallback module.Fallback`:

```go
	// Cleanup lists the leftovers of an earlier Cargo build that Apply removes
	// once the release binary is in place; CleanupNotes explain what was left
	// alone. Both come from read-only checks, so planning stays side-effect-free.
	Cleanup      []pkgmgr.Leftover
	CleanupNotes []string
```

3. Replace `planRelease` and add `gitFallbackOf`:

```go
// gitFallbackOf returns the module's git fallback entry, if it has one.
func gitFallbackOf(fbs []module.Fallback) (module.Fallback, bool) {
	for _, fb := range fbs {
		if fb.Type == "git" {
			return fb, true
		}
	}
	return module.Fallback{}, false
}

// planRelease queues the release binary when it is missing or was installed
// from another ref or another kind of fallback. A Cargo build recorded at the
// pinned ref is adopted: its binary stays and only the build leftovers are
// cleaned up. A lockfile entry that has a ref but no kind counts as git.
func planRelease(mp *ModulePlan, e Engine, fb module.Fallback, prev lockfile.ModuleState) {
	recordedKind := prev.FallbackKind
	if recordedKind == "" && prev.FallbackRef != "" {
		recordedKind = fallbackKindGit
	}
	pp := PackagePlan{Name: fb.Bin, Manager: "release", To: fb.Ref}
	switch {
	case !pkgmgr.ReleaseInstalled(fb, e.releaseContext()):
		mp.MissingPackages = append(mp.MissingPackages, pp)
	case recordedKind == fallbackKindRelease && prev.FallbackRef == fb.Ref:
		// Settled: this ref is installed.
	case recordedKind == fallbackKindRelease:
		pp.Update, pp.From = true, prev.FallbackRef
		mp.MissingPackages = append(mp.MissingPackages, pp)
	case recordedKind == fallbackKindGit && prev.FallbackRef == fb.Ref:
		pp.Adopt = true
		mp.MissingPackages = append(mp.MissingPackages, pp)
		planCleanup(mp, e)
	default:
		pp.Update, pp.From = true, prev.FallbackRef
		mp.MissingPackages = append(mp.MissingPackages, pp)
		planCleanup(mp, e)
	}
}

// planCleanup records what is left of an earlier Cargo build of the module's
// git fallback.
func planCleanup(mp *ModulePlan, e Engine) {
	gitFB, ok := gitFallbackOf(mp.Manifest.Packages.Fallback)
	if !ok {
		return
	}
	mp.Cleanup, mp.CleanupNotes = pkgmgr.FindLeftovers(gitFB, pkgmgr.FallbackContext{
		VendorDir: e.vendorDir(),
		Platform:  string(e.Platform.OS),
	})
}
```

4. In `describePackage`, put the adopt wording first inside the `release` branch:

```go
	if pp.Manager == "release" {
		if pp.Adopt {
			return fmt.Sprintf("%s (adopt the Cargo build %s)", pp.Name, pp.To)
		}
```

(the rest of that branch is unchanged).

5. In `RenderPlan`'s `line` closure, directly after the `_, _ = fmt.Fprintf(&b, "  %-8s %-20s %s\n", string(mp.Action), mp.ID, strings.TrimSpace(extra))` line of the `ActionInstall, ActionUpdate` case, add:

```go
			for _, l := range mp.Cleanup {
				_, _ = fmt.Fprintf(&b, "  remove   %s: remove build leftover %s\n", mp.ID, l.Describe())
			}
```

`internal/engine/apply_helpers.go` — replace `installRelease` with the version that handles adoption and cleanup:

```go
// installRelease downloads and installs a module's release binary, or adopts
// a Cargo build that already is the pinned version, then removes the leftovers
// of the old build. A failed install degrades the module and never falls
// through to the Cargo build; a failed cleanup only warns.
func (e Engine) installRelease(id string, mp ModulePlan, fb module.Fallback, degraded map[string]string,
	vendorPaths map[string][]string, outcome *fallbackOutcome) {
	if len(mp.UnavailablePackages) > 0 {
		_, _ = fmt.Fprintln(e.Stdout, fallbackNotice(e.Manager.Name(), mp))
	}
	binPath := e.releaseContext().BinPath(fb)
	if isReleaseAdopt(mp) {
		_, _ = fmt.Fprintf(e.Stdout, "adopting the existing build of %s %s as its release binary\n", fb.Bin, fb.Ref)
		outcome.sha[id] = ""
	} else {
		_, _ = fmt.Fprintf(e.Stdout, "installing %s %s from its release binary\n", fb.Bin, fb.Ref)
		res, err := pkgmgr.InstallRelease(fb, e.releaseContext(), e.Downloader)
		if err != nil {
			degraded[id] = fallbackFailure(e.Manager, mp, err)
			return
		}
		binPath = res.BinPath
		outcome.sha[id] = res.SHA256
	}
	vendorPaths[id] = append(vendorPaths[id], binPath)
	outcome.built[id] = fb.Ref
	outcome.kind[id] = fallbackKindRelease
	e.removeLeftovers(id, mp)
}

// removeLeftovers deletes the planned leftovers of an old Cargo build, logging
// each one. A failure is reported and does not stop the others.
func (e Engine) removeLeftovers(id string, mp ModulePlan) {
	for _, note := range mp.CleanupNotes {
		_, _ = fmt.Fprintf(e.Stdout, "%s: %s; leaving it as is\n", id, note)
	}
	for _, l := range mp.Cleanup {
		_, _ = fmt.Fprintf(e.Stdout, "removing build leftover %s\n", l.Describe())
		if err := pkgmgr.RemoveLeftover(l); err != nil {
			_, _ = fmt.Fprintf(e.Stdout, "%s: could not remove %s: %v\n", id, l.Describe(), err)
		}
	}
}

// isReleaseAdopt reports whether the module's queued release fallback keeps an
// existing Cargo build instead of downloading.
func isReleaseAdopt(mp ModulePlan) bool {
	for _, pp := range mp.MissingPackages {
		if pp.Manager == "release" && pp.Adopt {
			return true
		}
	}
	return false
}
```

- [ ] **Step 8: Run all engine and pkgmgr tests**

Run: `go test ./internal/engine/... ./internal/pkgmgr/... -race -count=1 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
gofmt -l internal && git add internal && git commit -m "feat: adopt a matching Cargo build and clean up its leftovers"
```

---

### Task 7: PATH conflict notice

**Files:**
- Modify: `internal/engine/apply_helpers.go` (`installRelease`, new helper)
- Modify: `internal/engine/doctor.go`
- Test: `internal/engine/release_apply_test.go`

**Interfaces:**
- Consumes: `pkgmgr.Runner.Look`, `Engine.releaseContext()`, `lockfile.ModuleState.FallbackKind`.
- Produces: `func (e Engine) shadowingBinary(fb module.Fallback) string` — the path of another copy of the binary found on `PATH` ("" when there is none); apply output line `... also found at <path>`; doctor notice code `path-shadow:<id>` (severity notice).

- [ ] **Step 1: Write the failing tests**

Append to `internal/engine/release_apply_test.go`:

```go
func TestApplyWarnsWhenAnotherCopyIsOnThePath(t *testing.T) {
	s := newRelSandbox(t)
	s.runner.LookOK = map[string]bool{"reltool": true} // MockRunner.Look resolves /usr/bin/reltool
	res, err := s.apply(t)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := moduleResult(t, res, "reltool").Status; got != "applied" {
		t.Fatalf("a PATH notice must not degrade the module; status = %q", got)
	}
	for _, want := range []string{"/usr/bin/reltool", s.binPath()} {
		if !strings.Contains(s.out.String(), want) {
			t.Fatalf("output does not name %q:\n%s", want, s.out.String())
		}
	}
}

func TestApplyStaysQuietWhenNoOtherCopyExists(t *testing.T) {
	s := newRelSandbox(t)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if strings.Contains(s.out.String(), "also found") {
		t.Fatalf("unexpected PATH notice:\n%s", s.out.String())
	}
}

func TestDoctorReportsACompetingCopyOnThePath(t *testing.T) {
	s := newRelSandbox(t)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	s.runner.LookOK = map[string]bool{"reltool": true}
	codes := s.findings(t)
	if codes["path-shadow:reltool"] != engine.SeverityNotice {
		t.Fatalf("doctor lacks the path-shadow notice: %v", codes)
	}
	s.runner.LookOK = nil
	if _, present := s.findings(t)["path-shadow:reltool"]; present {
		t.Fatal("path-shadow reported although no other copy exists")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/engine/... -run 'PathShadow|AnotherCopy|CompetingCopy|NoOtherCopy' -v 2>&1 | head -20`
Expected: FAIL (no notice is produced).

- [ ] **Step 3: Implement**

In `internal/engine/apply_helpers.go`, add the helper and a message builder:

```go
// shadowingBinary returns the path of another copy of the release binary found
// on PATH, or "" when the only copy is the one omnishell installed.
func (e Engine) shadowingBinary(fb module.Fallback) string {
	if e.Runner == nil {
		return ""
	}
	found, err := e.Runner.Look(fb.Bin)
	if err != nil || filepath.Clean(found) == filepath.Clean(e.releaseContext().BinPath(fb)) {
		return ""
	}
	return found
}

func shadowMessage(id string, fb module.Fallback, other, ours string) string {
	return fmt.Sprintf("%s: %s is also found at %s besides the omnishell copy at %s; whichever comes first in PATH wins",
		id, fb.Bin, other, ours)
}
```

At the end of `installRelease` (after `e.removeLeftovers(id, mp)`), add:

```go
	if other := e.shadowingBinary(fb); other != "" {
		_, _ = fmt.Fprintln(e.Stdout, shadowMessage(id, fb, other, binPath))
	}
```

In `internal/engine/doctor.go`, inside the per-module loop of section 5 (after the `module-degraded` check, still inside the `for _, id := range enabledIDs` loop), add:

```go
		if st := lock.Modules[id]; mp.Fallback.Type == "release" && st.FallbackKind == "release" {
			if other := e.shadowingBinary(mp.Fallback); other != "" {
				add(SeverityNotice, "path-shadow:"+id,
					shadowMessage(id, mp.Fallback, other, e.releaseContext().BinPath(mp.Fallback)))
			}
		}
```

- [ ] **Step 4: Run the engine tests**

Run: `go test ./internal/engine/... -race -count=1 2>&1 | tail -15`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine && git commit -m "feat: warn when another copy of a release binary is on PATH"
```

---

### Task 8: Tag updater keeps ref and checksums in step

**Files:**
- Create: `tools/update-fallback-tags/github.go`
- Modify: `tools/update-fallback-tags/rewrite.go`, `update.go`, `main.go`
- Modify: `.github/workflows/fallback-tags.yml`
- Test: `tools/update-fallback-tags/github_test.go`, `release_update_test.go`, `rewrite_test.go`, plus the call sites of `Update` and `run` in existing tests

**Interfaces:**
- Consumes: `module.Asset.RenderURL`, `module.Fallback.Assets`.
- Produces:
  - `type Releases interface { AssetDigests(repo, tag string) (map[string]string, error) }` — asset file name → lowercase hex sha256
  - `func Update(dir string, r Runner, rel Releases) (Result, error)` (new third parameter; `nil` is allowed when no module has a release entry)
  - `func run(args []string, stdout io.Writer, r Runner, rel Releases) error`
  - `Change.Checksums int`
  - `func rewriteRefs(manifest, oldRef, newRef string) (string, error)` — replaces `rewriteRef`
  - `func rewriteAssetSHAs(manifest string, shas []string) (string, error)`
  - `func newGitHubReleases(token string) githubReleases`

- [ ] **Step 1: Write the failing tests**

Create `tools/update-fallback-tags/release_update_test.go`:

```go
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tagRunner struct{ out string }

func (r tagRunner) Run(string, ...string) ([]byte, error) { return []byte(r.out), nil }

type fakeReleases struct {
	digests map[string]string
	err     error
	calls   []string
}

func (f *fakeReleases) AssetDigests(repo, tag string) (map[string]string, error) {
	f.calls = append(f.calls, repo+"@"+tag)
	return f.digests, f.err
}

const releaseUpdateManifest = `platforms = ["linux"]
shells    = ["bash"]

[module]
id          = "tool"
name        = "tool"
description = "A tool"
version     = "1.0.0"
schema      = 1

# release first, build last
[[packages.fallback]]
type = "release"
repo = "https://github.com/o/tool"
ref  = "v1.2.3"
bin  = "tool"

[[packages.fallback.assets]]
os     = "linux"
arch   = "amd64"
url    = "https://example.com/{{.Ref}}/tool-{{.Version}}-amd64.tar.gz"
sha256 = "OLD1"
member = "tool"

[[packages.fallback.assets]]
os     = "linux"
arch   = "arm64"
url    = "https://example.com/{{.Ref}}/tool-{{.Version}}-arm64.tar.gz"
sha256 = "OLD2"
member = "tool"

[[packages.fallback]]
type = "git"
repo = "https://github.com/o/tool.git"
dest = "{{.VendorDir}}/tool"
ref  = "v1.2.3"
run  = ["cargo", "install", "--path", "{{.VendorDir}}/tool"]
`

func writeReleaseModule(t *testing.T) (dir, manifest string) {
	t.Helper()
	dir = t.TempDir()
	manifest = filepath.Join(dir, "tool", "manifest.toml")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.NewReplacer("OLD1", strings.Repeat("1", 64), "OLD2", strings.Repeat("2", 64)).Replace(releaseUpdateManifest)
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, manifest
}

const lsRemoteTags = "aaa\trefs/tags/v1.2.3\nbbb\trefs/tags/v1.3.0\n"

func TestUpdateBumpsRefAndChecksumsTogether(t *testing.T) {
	dir, manifest := writeReleaseModule(t)
	rel := &fakeReleases{digests: map[string]string{
		"tool-1.3.0-amd64.tar.gz": strings.Repeat("a", 64),
		"tool-1.3.0-arm64.tar.gz": strings.Repeat("b", 64),
	}}
	res, err := Update(dir, tagRunner{lsRemoteTags}, rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failures) != 0 || len(res.Changes) != 1 || res.Changes[0].New != "v1.3.0" || res.Changes[0].Checksums != 2 {
		t.Fatalf("result = %+v", res)
	}
	if len(rel.calls) != 1 || rel.calls[0] != "https://github.com/o/tool@v1.3.0" {
		t.Fatalf("release lookups = %v", rel.calls)
	}
	got, _ := os.ReadFile(manifest)
	text := string(got)
	if strings.Count(text, `ref  = "v1.3.0"`) != 2 || strings.Contains(text, "v1.2.3") {
		t.Fatalf("both the release and the git ref must move to v1.3.0:\n%s", text)
	}
	for _, want := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64), "# release first, build last"} {
		if !strings.Contains(text, want) {
			t.Fatalf("manifest lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, strings.Repeat("1", 64)) || strings.Contains(text, strings.Repeat("2", 64)) {
		t.Fatalf("an old checksum survived:\n%s", text)
	}
}

func TestUpdateLeavesAModuleUnchangedWhenADigestIsMissing(t *testing.T) {
	dir, manifest := writeReleaseModule(t)
	before, _ := os.ReadFile(manifest)
	rel := &fakeReleases{digests: map[string]string{"tool-1.3.0-amd64.tar.gz": strings.Repeat("a", 64)}} // arm64 missing
	res, err := Update(dir, tagRunner{lsRemoteTags}, rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 0 || len(res.Failures) != 1 || !strings.Contains(res.Failures[0].Err.Error(), "tool-1.3.0-arm64.tar.gz") {
		t.Fatalf("result = %+v, want one failure naming the missing asset", res)
	}
	if after, _ := os.ReadFile(manifest); string(after) != string(before) {
		t.Fatalf("a half-bumped manifest was written:\n%s", after)
	}
}

func TestUpdateReportsAReleaseLookupError(t *testing.T) {
	dir, manifest := writeReleaseModule(t)
	before, _ := os.ReadFile(manifest)
	res, err := Update(dir, tagRunner{lsRemoteTags}, &fakeReleases{err: errors.New("rate limited")})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failures) != 1 || !strings.Contains(res.Failures[0].Err.Error(), "rate limited") {
		t.Fatalf("failures = %+v", res.Failures)
	}
	if after, _ := os.ReadFile(manifest); string(after) != string(before) {
		t.Fatal("manifest changed despite the lookup error")
	}
}

func TestUpdateWithoutAReleaseClientFailsForAReleaseModule(t *testing.T) {
	dir, _ := writeReleaseModule(t)
	res, err := Update(dir, tagRunner{lsRemoteTags}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failures) != 1 || !strings.Contains(res.Failures[0].Err.Error(), "release client") {
		t.Fatalf("failures = %+v", res.Failures)
	}
}

func TestSummaryMentionsRepinnedChecksums(t *testing.T) {
	got := summary(Result{Changes: []Change{{Module: "tool", Repo: "r", Old: "v1", New: "v2", Checksums: 2}}})
	if !strings.Contains(got, "re-pinned") || !strings.Contains(got, "`tool`") {
		t.Fatalf("summary does not mention the checksums:\n%s", got)
	}
}
```

Create `tools/update-fallback-tags/github_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubReleasesReadsAssetDigests(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"assets":[
			{"name":"a.tar.gz","digest":"sha256:` + strings.Repeat("c", 64) + `"},
			{"name":"b.zip","digest":null},
			{"name":"c","digest":"md5:zzz"}]}`))
	}))
	defer srv.Close()
	g := githubReleases{client: srv.Client(), apiBase: srv.URL, token: "tok"}

	got, err := g.AssetDigests("https://github.com/o/tool.git", "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/o/tool/releases/tags/v1.2.3" || gotAuth != "Bearer tok" {
		t.Fatalf("request path=%q auth=%q", gotPath, gotAuth)
	}
	if len(got) != 1 || got["a.tar.gz"] != strings.Repeat("c", 64) {
		t.Fatalf("digests = %v, want only the sha256 one", got)
	}
}

func TestGitHubReleasesRejectsNonGitHubRepos(t *testing.T) {
	g := githubReleases{client: http.DefaultClient, apiBase: "https://unused"}
	if _, err := g.AssetDigests("https://gitlab.com/o/tool", "v1"); err == nil {
		t.Fatal("want an error for a non-GitHub repository")
	}
}

func TestGitHubReleasesReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	g := githubReleases{client: srv.Client(), apiBase: srv.URL}
	if _, err := g.AssetDigests("https://github.com/o/tool", "v9"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want the 404 reported", err)
	}
}
```

Replace the tests of `rewriteRef` in `tools/update-fallback-tags/rewrite_test.go` with tests of `rewriteRefs` and add the checksum rewriter tests. Open the file, port each existing `rewriteRef(manifest, newRef)` case to `rewriteRefs(manifest, oldRef, newRef)` (pass the manifest's current ref as `oldRef`), keep every expectation about preserved comments and formatting, and append:

```go
func TestRewriteRefsOnlyMovesRefsEqualToTheOldOne(t *testing.T) {
	in := "[[packages.fallback]]\ntype = \"release\"\nref  = \"v1\"\n\n[[packages.fallback]]\ntype = \"git\"\nref  = \"v0.9\"\n"
	got, err := rewriteRefs(in, "v1", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `ref  = "v2"`) || !strings.Contains(got, `ref  = "v0.9"`) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRewriteRefsFailsWithoutAMatchingRef(t *testing.T) {
	if _, err := rewriteRefs("[[packages.fallback]]\nref = \"v1\"\n", "v9", "v10"); err == nil {
		t.Fatal("want an error when no fallback pins the old ref")
	}
}

func TestRewriteAssetSHAsReplacesThemInOrder(t *testing.T) {
	in := "[[packages.fallback.assets]]\nsha256 = \"" + strings.Repeat("1", 64) + "\" # amd64\n\n[[packages.fallback.assets]]\nsha256 = \"" + strings.Repeat("2", 64) + "\"\n"
	got, err := rewriteAssetSHAs(in, []string{strings.Repeat("a", 64), strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, strings.Repeat("a", 64)+"\" # amd64") || !strings.Contains(got, strings.Repeat("b", 64)) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRewriteAssetSHAsFailsOnACountMismatch(t *testing.T) {
	in := "[[packages.fallback.assets]]\nsha256 = \"" + strings.Repeat("1", 64) + "\"\n"
	if _, err := rewriteAssetSHAs(in, []string{"a", "b"}); err == nil {
		t.Fatal("want an error when the number of checksums differs from the number of assets")
	}
}
```

(Add `"strings"` to `rewrite_test.go` imports if needed.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./tools/update-fallback-tags/... 2>&1 | head -20`
Expected: FAIL to compile (`Releases`, `Update` arity, `rewriteRefs`, `githubReleases` undefined).

- [ ] **Step 3: Implement the rewriters**

Replace `rewriteRef` in `tools/update-fallback-tags/rewrite.go` with:

```go
package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	fallbackHeader = "[[packages.fallback]]"
	assetsHeader   = "[[packages.fallback.assets]]"
)

// refLineRe matches a ref assignment. Groups: 1 up to and including the opening
// quote, 2 the value, 3 the closing quote and any trailing text.
var refLineRe = regexp.MustCompile(`^(ref\s*=\s*")([^"\n]*)(".*)$`)

// shaLineRe matches a sha256 assignment, with the same group layout.
var shaLineRe = regexp.MustCompile(`^(sha256\s*=\s*")([^"\n]*)(".*)$`)

// rewriteRefs sets the ref of every [[packages.fallback]] table whose ref is
// oldRef to newRef, replacing only the value between the quotes so comments and
// formatting survive. It fails when no table pins oldRef.
func rewriteRefs(manifest, oldRef, newRef string) (string, error) {
	lines := strings.Split(manifest, "\n")
	inFallback, replaced := false, 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inFallback = trimmed == fallbackHeader
			continue
		}
		if !inFallback {
			continue
		}
		if m := refLineRe.FindStringSubmatch(line); m != nil && m[2] == oldRef {
			lines[i] = m[1] + newRef + m[3]
			replaced++
		}
	}
	if replaced == 0 {
		return "", errors.New("no fallback pins ref " + oldRef)
	}
	return strings.Join(lines, "\n"), nil
}

// rewriteAssetSHAs replaces the sha256 of each [[packages.fallback.assets]]
// table, in order, with shas. The number of checksums must equal the number of
// asset tables.
func rewriteAssetSHAs(manifest string, shas []string) (string, error) {
	lines := strings.Split(manifest, "\n")
	inAssets, n := false, 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inAssets = trimmed == assetsHeader
			continue
		}
		if !inAssets {
			continue
		}
		if m := shaLineRe.FindStringSubmatch(line); m != nil {
			if n >= len(shas) {
				return "", fmt.Errorf("manifest has more assets than the %d checksums given", len(shas))
			}
			lines[i] = m[1] + shas[n] + m[3]
			n++
		}
	}
	if n != len(shas) {
		return "", fmt.Errorf("manifest has %d asset checksums, %d were given", n, len(shas))
	}
	return strings.Join(lines, "\n"), nil
}
```

- [ ] **Step 4: Implement the GitHub client**

Create `tools/update-fallback-tags/github.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Releases looks up the SHA-256 digests GitHub reports for a release's assets.
// Injectable so tests need no network.
type Releases interface {
	// AssetDigests returns asset file name -> lowercase hex sha256 for the
	// release of tag in repo. Assets without a sha256 digest are omitted.
	AssetDigests(repo, tag string) (map[string]string, error)
}

type githubReleases struct {
	client  *http.Client
	apiBase string
	token   string
}

func newGitHubReleases(token string) githubReleases {
	return githubReleases{client: &http.Client{Timeout: time.Minute}, apiBase: "https://api.github.com", token: token}
}

func (g githubReleases) AssetDigests(repo, tag string) (map[string]string, error) {
	slug, ok := strings.CutPrefix(repo, "https://github.com/")
	slug = strings.TrimSuffix(strings.TrimSuffix(slug, "/"), ".git")
	if !ok || strings.Count(slug, "/") != 1 {
		return nil, fmt.Errorf("%s is not a GitHub repository", repo)
	}
	endpoint := fmt.Sprintf("%s/repos/%s/releases/tags/%s", g.apiBase, slug, url.PathEscape(tag))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", endpoint, resp.Status)
	}
	var body struct {
		Assets []struct {
			Name   string  `json:"name"`
			Digest *string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode %s: %w", endpoint, err)
	}
	digests := map[string]string{}
	for _, a := range body.Assets {
		if a.Digest == nil {
			continue
		}
		if hexSum, ok := strings.CutPrefix(*a.Digest, "sha256:"); ok {
			digests[a.Name] = strings.ToLower(hexSum)
		}
	}
	return digests, nil
}
```

- [ ] **Step 5: Wire the updater**

`tools/update-fallback-tags/update.go`:

1. Add `Checksums int` to `Change` (doc comment: "Checksums is how many release asset checksums were re-pinned").
2. Change the signatures and the call: `func Update(dir string, r Runner, rel Releases) (Result, error)` and `change, err := updateModule(filepath.Join(dir, id, "manifest.toml"), id, r, rel)`; update the doc comment of `Update` to mention that a release entry's checksums move with its ref.
3. Replace the tail of `updateModule` (from `updated, err := rewriteRef(...)` down to the `return &Change{…}`) with:

```go
	updated := string(raw)
	checksums := 0
	if release, ok := releaseEntry(m); ok {
		shas, err := releaseChecksums(release, newRef, rel)
		if err != nil {
			return nil, err
		}
		if updated, err = rewriteAssetSHAs(updated, shas); err != nil {
			return nil, err
		}
		checksums = len(shas)
	}
	updated, err = rewriteRefs(updated, fb.Ref, newRef)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(updated), info.Mode().Perm()); err != nil {
		return nil, err
	}
	return &Change{Module: id, Repo: fb.Repo, Old: fb.Ref, New: newRef, Checksums: checksums}, nil
}

// releaseEntry returns the module's release fallback, if it has one.
func releaseEntry(m module.Manifest) (module.Fallback, bool) {
	for _, fb := range m.Packages.Fallback {
		if fb.Type == "release" {
			return fb, true
		}
	}
	return module.Fallback{}, false
}

// releaseChecksums returns the digest of every asset of the release entry at
// newRef, in manifest order. It fails, so nothing is written, when any asset
// has no digest.
func releaseChecksums(fb module.Fallback, newRef string, rel Releases) ([]string, error) {
	if rel == nil {
		return nil, errors.New("module has a release fallback but no release client was given")
	}
	digests, err := rel.AssetDigests(fb.Repo, newRef)
	if err != nil {
		return nil, fmt.Errorf("release digests for %s %s: %w", fb.Repo, newRef, err)
	}
	shas := make([]string, len(fb.Assets))
	for i, asset := range fb.Assets {
		assetURL, err := asset.RenderURL(newRef)
		if err != nil {
			return nil, fmt.Errorf("render asset url: %w", err)
		}
		name := path.Base(assetURL)
		sum, ok := digests[name]
		if !ok {
			return nil, fmt.Errorf("no sha256 digest for %s in %s %s", name, fb.Repo, newRef)
		}
		shas[i] = sum
	}
	return shas, nil
}
```

Add `"path"` to the imports of `update.go` (it already imports `path/filepath`, `errors`, `fmt`, `os`, `strings`, `module`).

4. In `summary`, after the changes table and before the "Before merging" paragraph, add:

```go
		var repinned []string
		for _, c := range res.Changes {
			if c.Checksums > 0 {
				repinned = append(repinned, "`"+c.Module+"`")
			}
		}
		if len(repinned) > 0 {
			_, _ = fmt.Fprintf(&b, "\nRelease checksums were re-pinned from the per-asset digests GitHub reports for: %s. Review them like any other pinned value.\n", strings.Join(repinned, ", "))
		}
```

and change the intro sentence to "Bumps the pinned fallback tags of the built-in modules to the latest stable upstream release."

`tools/update-fallback-tags/main.go`: `main` passes the GitHub client and `run` forwards it:

```go
func main() {
	rel := newGitHubReleases(os.Getenv("GITHUB_TOKEN"))
	if err := run(os.Args[1:], os.Stdout, execRunner{}, rel); err != nil {
```

```go
func run(args []string, stdout io.Writer, r Runner, rel Releases) error {
```

and `res, err := Update(*dir, r, rel)`.

Fix the call sites in the existing tests (`update_test.go`, `main_test.go`, `guard_test.go` if it calls them): the compiler lists each `Update(`/`run(` call; pass `nil` as the new last argument.

`.github/workflows/fallback-tags.yml` — give the tool step a token so the GitHub API is not rate-limited anonymously:

```yaml
      - name: Look up newer tags
        id: tags
        env:
          GITHUB_TOKEN: ${{ github.token }}
        run: |
```

- [ ] **Step 6: Run the updater tests**

Run: `go test ./tools/... -race -count=1 -v 2>&1 | tail -30`
Expected: PASS. Then `go run ./tools/update-fallback-tags --dir modules/builtin --summary /tmp/summary.md` (needs network and `GITHUB_TOKEN=$(gh auth token)`): if it prints `0 change(s)`, the manifests are current; if it bumps something, inspect `git diff modules/` (a bump must change `ref` twice and every `sha256` of that module) and `git checkout modules/` to discard it — this plan's PR must not carry a tag bump.

- [ ] **Step 7: Commit**

```bash
gofmt -l tools && git add tools .github && git commit -m "feat: keep release checksums in step with the fallback tag updater"
```

---

### Task 9: Final docs and verification

**Files:**
- Modify: `CLAUDE.md`, `CONTRIBUTING.md`, `README.md`, `CHANGELOG.md`

- [ ] **Step 1: Update the docs**

`CLAUDE.md` — in the `internal/pkgmgr` paragraph (architecture item 8), append directly after the sentence ending "`ComputePlan` probes only when the module has a fallback; a failed probe means "unknown" and keeps the package path)." the following, and keep the rest of the paragraph:

```markdown
   A fallback can instead be `type = "release"` (`release.go`): a pinned,
   SHA-256-verified binary per `(os, arch)` asset, downloaded through the
   injectable `Downloader` and renamed into `{{.VendorDir}}/bin`. `ComputePlan`
   walks a module's fallbacks in order (`selectFallback`) and takes the first
   usable one, so the order is system package → release → git; an unsupported
   architecture reaches the git entry. A failed download or checksum degrades
   the module and never falls through to the build. The lockfile records
   `fallback_kind` (`git`/`release`; a ref without a kind counts as git) and
   `fallback_sha256`. A git build recorded at the pinned ref is adopted (its
   binary stays) and its leftovers (`leftovers.go`: source tree, this crate's
   `.crates.toml`/`.crates2.json` entries) are removed only with filesystem
   evidence that they are ours; the plan lists them.
```

`CONTRIBUTING.md` — under `## Pinned fallback tags`, after the paragraph that ends "CI does not build the fallbacks.", add:

```markdown
Modules with a `release` fallback also pin one `sha256` per asset. The updater re-pins them together with `ref`, taking the digests GitHub reports for the new release's assets; if any asset has no digest it leaves that module unchanged and says so in the pull request. Review the new checksums like any other pinned value. When adding a release fallback by hand, copy the digest from `gh api repos/<owner>/<repo>/releases/tags/<tag> --jq '.assets[] | "\(.digest) \(.name)"'`.
```

`README.md` — in the `.github`-independent "safety" or `omnishell doctor` description, if a list of doctor notices exists, add `path-shadow` ("another copy of a release binary is earlier or later on PATH"); otherwise skip. (Run `grep -n "fallback-outdated\|doctor" README.md` to find it; if there is no list of notice codes, make no README change in this step.)

`CHANGELOG.md` — under `### Added` append:

```markdown
- When a release binary replaces an earlier Cargo build, `apply` removes the
  build's leftovers (the cloned source tree and this tool's entries in cargo's
  install metadata) after verifying they are its own, and lists them in the
  plan. A Cargo build already at the pinned tag is kept as the release binary
  and only cleaned up. `doctor` notes another copy of the binary on `PATH`.
- The `fallback-tags` workflow re-pins the SHA-256 of release assets together
  with the tag.
```

- [ ] **Step 2: Whole-branch verification**

Run each and confirm clean output:

```bash
gofmt -l . ; go vet ./... ; make test 2>&1 | tail -20 ; make lint 2>&1 | tail -20
```

Expected: `gofmt` prints nothing, vet and lint report no issues, every package `ok`. Fix lint findings in the new code (`gosec` may flag the `http.Client.Get` with a variable URL in `download.go`; the URL is validated to start with `https://` just above it, so add `//nolint:gosec // url is checked to be https above` only if the linter is enabled for that rule).

- [ ] **Step 3: Manual smoke test (disposable sandbox, no root needed)**

```bash
make build && HOME=$(mktemp -d) ./omnishell version
```

Expected: prints the version. (The real install paths are covered by the fake-downloader tests; `test/e2e/run.sh` installs real packages and is only for a disposable environment, so it is not part of this task.)

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "docs: document the release fallback, cleanup and checksum updates"
```
