// Package buildinfo exposes version metadata injected at build time via -ldflags.
package buildinfo

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a single human-readable version line.
func String() string {
	return Version + " (commit " + Commit + ", built " + Date + ")"
}
