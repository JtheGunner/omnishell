package main

import "testing"

func TestSelectTag(t *testing.T) {
	cases := []struct {
		name    string
		current string
		tags    []string
		want    string
		changed bool
	}{
		{"picks the highest newer tag", "v1.25.0", []string{"v1.24.0", "v1.26.0", "v1.25.1"}, "v1.26.0", true},
		{"compares numerically", "v2.9.0", []string{"v2.10.0", "v2.9.5"}, "v2.10.0", true},
		{"keeps the prefix style", "2.68.0", []string{"v1.0.1", "2.69.0", "v3.0.0"}, "2.69.0", true},
		{"ignores weekly and release candidates", "v18.22.0", []string{"weekly", "v18.23.0-rc1", "v18.23.0"}, "v18.23.0", true},
		{"ignores vfox style tags", "v2026.10.2", []string{"weekly", "vfox-v2026.10.3"}, "", false},
		{"never downgrades", "v1.26.0", []string{"v1.25.0"}, "", false},
		{"an equal tag is no change", "v1.26.0", []string{"v1.26.0"}, "", false},
		{"no tags", "v1.26.0", nil, "", false},
		{"a branch ref is left alone", "main", []string{"v9.9.9"}, "", false},
	}
	for _, tc := range cases {
		got, changed := selectTag(tc.current, tc.tags)
		if got != tc.want || changed != tc.changed {
			t.Errorf("%s: selectTag(%q, %v) = (%q, %v), want (%q, %v)",
				tc.name, tc.current, tc.tags, got, changed, tc.want, tc.changed)
		}
	}
}
