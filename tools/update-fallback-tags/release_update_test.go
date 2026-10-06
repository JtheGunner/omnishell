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
