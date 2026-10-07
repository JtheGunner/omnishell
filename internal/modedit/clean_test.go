package modedit

import "testing"

func TestCleanReplacesControlAndBidiCharactersWithSpaces(t *testing.T) {
	cases := map[string]string{
		"plain text":              "plain text",
		"ä日 ok":                   "ä日 ok",
		"a\x1b]0;x\x07b":          "a ]0;x b",
		"a\x1b[2Jb":               "a [2Jb",
		"one\ntwo\r\tthree":       "one two  three",
		"a\u202etxt\u2066b\u2069": "a txt b ",
		"c1\u009bcontrol":         "c1 control",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}
