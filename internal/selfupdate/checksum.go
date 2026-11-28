package selfupdate

import (
	"fmt"
	"strings"
)

// expectedChecksum finds the SHA-256 of name in checksums.txt content
// ("<64 hex>  <name>" per line, as goreleaser writes it).
func expectedChecksum(checksums, name string) (string, error) {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if !isSHA256Hex(fields[0]) {
			return "", fmt.Errorf("malformed checksum line for %s in checksums.txt", name)
		}
		return strings.ToLower(fields[0]), nil
	}
	return "", fmt.Errorf("no checksum for %s in checksums.txt", name)
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// archiveName maps the host to the release archive name, with the same os/arch
// mapping as install.sh and .goreleaser.yaml.
func archiveName(goos, goarch, goarm string) (string, error) {
	if goos != "linux" && goos != "darwin" {
		return "", fmt.Errorf("unsupported OS: %s", goos)
	}
	arch := goarch
	switch goarch {
	case "amd64", "arm64":
	case "arm":
		if goos != "linux" {
			return "", fmt.Errorf("unsupported OS for %s: %s", goarch, goos)
		}
		variant, _, _ := strings.Cut(goarm, ",")
		if variant != "6" && variant != "7" {
			return "", fmt.Errorf("unsupported 32-bit ARM variant %q", goarm)
		}
		arch = "armv" + variant
	default:
		return "", fmt.Errorf("unsupported arch: %s", goarch)
	}
	return fmt.Sprintf("omnishell_%s_%s.tar.gz", goos, arch), nil
}
