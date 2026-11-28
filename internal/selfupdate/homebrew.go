package selfupdate

import (
	"path/filepath"
	"strings"
)

var homebrewPrefixes = []string{
	"/opt/homebrew/",
	"/usr/local/Cellar/",
	"/home/linuxbrew/.linuxbrew/",
}

// IsHomebrewPath reports whether a symlink-resolved executable path belongs to
// a Homebrew installation: it has a Cellar segment or lies under a known
// Homebrew prefix.
func IsHomebrewPath(resolved string) bool {
	clean := filepath.ToSlash(filepath.Clean(resolved))
	for _, prefix := range homebrewPrefixes {
		if strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "Cellar" {
			return true
		}
	}
	return false
}
