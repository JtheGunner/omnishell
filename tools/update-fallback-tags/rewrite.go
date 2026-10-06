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
