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
	if err := module.ValidateManifest(m); err != nil {
		t.Fatalf("ValidateManifest: %v", err)
	}
	fb := m.Packages.Fallback[0]
	if fb.Type != "release" || fb.Bin != "tool" || fb.Ref != "v1.2.3" || len(fb.Assets) != 1 {
		t.Fatalf("fallback = %+v", fb)
	}
	asset, ok := fb.AssetFor("linux", "amd64", "")
	if !ok || asset.Member != "tool/bin/tool" || asset.Archive() != "tar.gz" {
		t.Fatalf("AssetFor(linux, amd64) = %+v, %v", asset, ok)
	}
	if _, ok := fb.AssetFor("linux", "arm64", ""); ok {
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
		{"unknown arch", func(s string) string { return strings.Replace(s, `arch   = "amd64"`, `arch   = "riscv64"`, 1) }, "arch must be amd64, arm64 or arm"},
		{"plain http url", func(s string) string { return strings.Replace(s, "https://example.com", "http://example.com", 1) }, "url must start with https://"},
		{"short sha", func(s string) string { return strings.Replace(s, sha, "xyz", 1) }, "64 lowercase hex"},
		{"uppercase sha", func(s string) string { return strings.Replace(s, sha, strings.Repeat("A", 64), 1) }, "64 lowercase hex"},
		{"broken url template", func(s string) string { return strings.Replace(s, "{{.Ref}}", "{{.Ref", 1) }, "invalid url template"},
		{"archive without member", func(s string) string { return strings.Replace(s, "member = \"tool/bin/tool\"\n", "", 1) }, "archive asset needs member"},
		{"member on a raw asset", func(s string) string { return strings.Replace(s, "-linux-amd64.tar.gz", "-linux-amd64", 1) }, "member is only valid"},
		{"templated host", func(s string) string {
			return strings.Replace(s, "https://example.com/{{.Ref}}/", "https://{{.Ref}}.example.com/", 1)
		}, "host must not be templated"},
		{"templated file type", func(s string) string {
			return strings.Replace(s, "tool-{{.Version}}-linux-amd64.tar.gz", "tool-{{.Ref}}", 1)
		}, "must not end with a template action"},
		{"duplicate asset", func(s string) string {
			return s + "\n[[packages.fallback.assets]]\nos = \"linux\"\narch = \"amd64\"\nurl = \"https://example.com/b.tar.gz\"\nsha256 = \"" + sha + "\"\nmember = \"b\"\n"
		}, "duplicate asset"},
		{"assets on a git entry", func(s string) string { return strings.Replace(s, `type = "release"`, `type = "git"`, 1) }, "only valid for type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := module.ParseManifest([]byte(releaseManifest(tc.mutate)))
			if err != nil {
				t.Fatalf("ParseManifest: %v", err)
			}
			err = module.ValidateManifest(m)
			if err == nil {
				t.Fatalf("want an error mentioning %q, got none", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestAssetForGoARM(t *testing.T) {
	fb := module.Fallback{Assets: []module.Asset{
		{OS: "linux", Arch: "amd64", URL: "amd64"},
		{OS: "linux", Arch: "arm", URL: "any-arm"},
		{OS: "linux", Arch: "arm", GoARM: "7", URL: "armv7"},
	}}
	cases := []struct {
		name, arch, goarm, wantURL string
	}{
		{"amd64 host", "amd64", "", "amd64"},
		{"armv7 prefers exact variant", "arm", "7", "armv7"},
		{"armv6 falls back to variant-less", "arm", "6", "any-arm"},
		{"unknown variant falls back to variant-less", "arm", "", "any-arm"},
		{"arm64 has none", "arm64", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, ok := fb.AssetFor("linux", c.arch, c.goarm)
			if c.wantURL == "" {
				if ok {
					t.Fatalf("AssetFor matched %q, want none", a.URL)
				}
				return
			}
			if !ok || a.URL != c.wantURL {
				t.Fatalf("AssetFor = %q, %v, want %q", a.URL, ok, c.wantURL)
			}
		})
	}
}

func TestAssetForGoARMNeverCrossesVariants(t *testing.T) {
	fb := module.Fallback{Assets: []module.Asset{{OS: "linux", Arch: "arm", GoARM: "7", URL: "armv7"}}}
	for _, goarm := range []string{"6", ""} {
		if a, ok := fb.AssetFor("linux", "arm", goarm); ok {
			t.Errorf("goarm %q matched the armv7-only asset %q", goarm, a.URL)
		}
	}
}

func TestValidateManifestGoARM(t *testing.T) {
	arm := func(s string) string { return strings.Replace(s, `arch   = "amd64"`, `arch   = "arm"`, 1) }
	withGoARM := func(v string) func(string) string {
		return func(s string) string {
			return strings.Replace(arm(s), `arch   = "arm"`, "arch   = \"arm\"\ngoarm  = \""+v+"\"", 1)
		}
	}
	validate := func(mutate func(string) string) error {
		m, err := module.ParseManifest([]byte(releaseManifest(mutate)))
		if err != nil {
			return err
		}
		return module.ValidateManifest(m)
	}
	ok := map[string]func(string) string{
		"arm without goarm": arm,
		"arm goarm 6":       withGoARM("6"),
		"arm goarm 7":       withGoARM("7"),
	}
	for name, mutate := range ok {
		if err := validate(mutate); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := []struct {
		name, want string
		mutate     func(string) string
	}{
		{"goarm 5", "goarm must be 6 or 7", withGoARM("5")},
		{"goarm on amd64", `goarm is only valid with arch = "arm"`, func(s string) string {
			return strings.Replace(s, `arch   = "amd64"`, "arch   = \"amd64\"\ngoarm  = \"7\"", 1)
		}},
		{"duplicate variant", "duplicate asset for linux/arm/7", func(s string) string {
			s = withGoARM("7")(s)
			return s + "\n[[packages.fallback.assets]]" + strings.SplitN(s, "[[packages.fallback.assets]]", 2)[1]
		}},
	}
	for _, c := range bad {
		if err := validate(c.mutate); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}
