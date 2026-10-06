package main

import (
	"reflect"
	"testing"
)

func TestParseTag(t *testing.T) {
	cases := []struct {
		tag    string
		ok     bool
		prefix bool
		nums   []int
	}{
		{"v1.26.0", true, true, []int{1, 26, 0}},
		{"0.8.0", true, false, []int{0, 8, 0}},
		{"2.69.0", true, false, []int{2, 69, 0}},
		{"v2026.10.3", true, true, []int{2026, 10, 3}},
		{"weekly", false, false, nil},
		{"vfox-v2026.10.3", false, false, nil},
		{"v1.0.0-rc1", false, false, nil},
		{"v1", false, false, nil},
		{"", false, false, nil},
	}
	for _, tc := range cases {
		got, ok := parseTag(tc.tag)
		if ok != tc.ok {
			t.Errorf("parseTag(%q) ok = %v, want %v", tc.tag, ok, tc.ok)
			continue
		}
		if ok && (got.prefix != tc.prefix || !reflect.DeepEqual(got.nums, tc.nums)) {
			t.Errorf("parseTag(%q) = %+v, want prefix %v nums %v", tc.tag, got, tc.prefix, tc.nums)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b []int
		want int
	}{
		{[]int{1, 2, 3}, []int{1, 2, 3}, 0},
		{[]int{2, 10, 0}, []int{2, 9, 0}, 1},
		{[]int{1, 2}, []int{1, 2, 0}, 0},
		{[]int{1, 2}, []int{1, 2, 1}, -1},
		{[]int{0, 8, 0}, []int{0, 7, 1}, 1},
	}
	for _, tc := range cases {
		if got := compare(tc.a, tc.b); got != tc.want {
			t.Errorf("compare(%v, %v) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
