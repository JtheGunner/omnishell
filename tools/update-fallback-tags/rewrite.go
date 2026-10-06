package main

import (
	"errors"
	"regexp"
	"strings"
)

const fallbackHeader = "[[packages.fallback]]"

// refLineRe matches a ref assignment. Group 1 is everything up to and including
// the opening quote, group 2 the closing quote and any trailing text.
var refLineRe = regexp.MustCompile(`(?m)^(ref\s*=\s*")[^"\n]*(".*)$`)

// rewriteRef sets the ref of the first [[packages.fallback]] table to newRef by
// replacing only the value between the quotes, so comments and formatting are
// preserved.
func rewriteRef(manifest, newRef string) (string, error) {
	start := strings.Index(manifest, fallbackHeader)
	if start < 0 {
		return "", errors.New("manifest has no [[packages.fallback]] table")
	}
	tail := manifest[start:]
	loc := refLineRe.FindStringSubmatchIndex(tail)
	if loc == nil {
		return "", errors.New("fallback has no ref line")
	}
	// The ref must belong to the fallback table, not to a table after it.
	if strings.Contains(tail[len(fallbackHeader):loc[0]], "\n[") {
		return "", errors.New("fallback has no ref line")
	}
	return manifest[:start] + tail[:loc[3]] + newRef + tail[loc[4]:], nil
}
