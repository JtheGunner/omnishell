package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

// semver is a parsed semantic version. Build metadata is dropped because it
// never takes part in precedence.
type semver struct {
	major, minor, patch uint64
	pre                 []string
}

// parseSemver parses "1.2.3", "v1.2.3" and forms with a "-pre" or "+build" part.
func parseSemver(s string) (semver, error) {
	raw := strings.TrimPrefix(s, "v")
	raw, _, _ = strings.Cut(raw, "+")
	core, pre, hasPre := strings.Cut(raw, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("%q is not a semantic version", s)
	}
	var nums [3]uint64
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil || (len(p) > 1 && p[0] == '0') {
			return semver{}, fmt.Errorf("%q is not a semantic version", s)
		}
		nums[i] = n
	}
	v := semver{major: nums[0], minor: nums[1], patch: nums[2]}
	if hasPre {
		if pre == "" {
			return semver{}, fmt.Errorf("%q is not a semantic version", s)
		}
		v.pre = strings.Split(pre, ".")
	}
	return v, nil
}

// compare returns -1, 0 or 1 following semver precedence rules.
func (a semver) compare(b semver) int {
	for _, d := range [][2]uint64{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if c := cmpUint(d[0], d[1]); c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := comparePreIdent(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return cmpUint(uint64(len(a.pre)), uint64(len(b.pre)))
}

func comparePreIdent(a, b string) int {
	an, aErr := strconv.ParseUint(a, 10, 64)
	bn, bErr := strconv.ParseUint(b, 10, 64)
	switch {
	case aErr == nil && bErr == nil:
		return cmpUint(an, bn)
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
