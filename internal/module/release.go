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
	// The host and the file-type suffix are what the checks above and Archive()
	// read from the raw template, so they must be literal: a templated host or
	// ending could render to something else than what was validated.
	host, _, _ := strings.Cut(strings.TrimPrefix(a.URL, "https://"), "/")
	switch {
	case strings.Contains(host, "{{"):
		return "url host must not be templated"
	case strings.HasSuffix(a.URL, "}}"):
		return "url must not end with a template action"
	}
	switch {
	case a.Archive() == "" && a.Member != "":
		return "member is only valid for .tar.gz and .zip assets"
	case a.Archive() != "" && a.Member == "":
		return "archive asset needs member"
	}
	return ""
}
