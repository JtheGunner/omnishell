package pkgmgr

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/JtheGunner/omnishell/internal/module"
)

var versionRe = regexp.MustCompile(`\d+(?:\.\d+)+`)

// CheckRequirements verifies every entry of a fallback's requires list by
// running "<tool> --version" through r. A tool whose probe fails counts as not
// found; a tool with a minimum version must report a parsable version at or
// above it. All problems are returned in a single error so the user can fix
// them in one go.
func CheckRequirements(requires []string, r Runner) error {
	var problems []string
	for _, raw := range requires {
		req, err := module.ParseRequirement(raw)
		if err != nil {
			return err
		}
		if problem := checkRequirement(req, r); problem != "" {
			problems = append(problems, problem)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("fallback prerequisites missing: %s", strings.Join(problems, "; "))
}

// checkRequirement returns a description of what is wrong with req, or "" if
// it is satisfied.
func checkRequirement(req module.Requirement, r Runner) string {
	out, err := r.Run(req.Tool, "--version")
	if err != nil {
		return req.Tool + " (not found)"
	}
	if req.Min == nil {
		return ""
	}
	want := req.Tool + " >= " + joinVersion(req.Min)
	found, ok := parseVersionOutput(string(out))
	if !ok {
		return want + " (version not recognised)"
	}
	if compareVersions(found, req.Min) < 0 {
		return want + " (found " + joinVersion(found) + ")"
	}
	return ""
}

// parseVersionOutput extracts the first dotted version number from a tool's
// --version output, e.g. "cargo 1.75.0 (1d8b05cdd 2023-11-20)" -> [1 75 0].
func parseVersionOutput(out string) ([]int, bool) {
	m := versionRe.FindString(out)
	if m == "" {
		return nil, false
	}
	parts := strings.Split(m, ".")
	v := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		v = append(v, n)
	}
	return v, true
}

// compareVersions compares numerically component by component, treating
// missing components as zero.
func compareVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func joinVersion(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}
