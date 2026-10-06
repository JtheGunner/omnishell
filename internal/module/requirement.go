package module

import (
	"fmt"
	"strconv"
	"strings"
)

// Requirement is one entry of a fallback's requires list: a tool that must be
// runnable, optionally at a minimum version.
type Requirement struct {
	Tool string
	// Min is the minimum version as numeric components (e.g. [1 85]); nil means
	// any version is acceptable.
	Min []int
}

// ParseRequirement parses "tool" or "tool>=X.Y[.Z]".
func ParseRequirement(s string) (Requirement, error) {
	tool, minStr, hasMin := strings.Cut(s, ">=")
	if tool == "" || strings.ContainsAny(tool, " \t<>=/") {
		return Requirement{}, fmt.Errorf("invalid requirement %q: want \"tool\" or \"tool>=X.Y\"", s)
	}
	if !hasMin {
		return Requirement{Tool: tool}, nil
	}
	min, err := parseVersion(minStr)
	if err != nil {
		return Requirement{}, fmt.Errorf("invalid requirement %q: %w", s, err)
	}
	return Requirement{Tool: tool, Min: min}, nil
}

func parseVersion(s string) ([]int, error) {
	if s == "" {
		return nil, fmt.Errorf("empty version")
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("bad version %q", s)
		}
		out = append(out, n)
	}
	return out, nil
}
