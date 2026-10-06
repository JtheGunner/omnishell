// Command update-fallback-tags bumps the pinned ref of every built-in git
// fallback to the latest stable upstream release tag.
package main

import (
	"regexp"
	"strconv"
	"strings"
)

// tagRe matches a stable release tag: an optional "v" and at least two dotted
// numbers. Tags such as "weekly", "vfox-v1.2.3" or "v1.0.0-rc1" do not match.
var tagRe = regexp.MustCompile(`^(v?)(\d+(?:\.\d+)+)$`)

// version is a parsed release tag.
type version struct {
	prefix bool // the tag starts with "v"
	nums   []int
}

// parseTag parses a stable release tag; ok is false for anything else.
func parseTag(tag string) (version, bool) {
	m := tagRe.FindStringSubmatch(tag)
	if m == nil {
		return version{}, false
	}
	parts := strings.Split(m[2], ".")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return version{}, false
		}
		nums[i] = n
	}
	return version{prefix: m[1] == "v", nums: nums}, true
}

// compare returns -1, 0 or 1. A missing trailing component counts as zero.
func compare(a, b []int) int {
	n := max(len(a), len(b))
	for i := range n {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}
