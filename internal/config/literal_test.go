package config_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
)

// newConfigFile writes the default config into a temp dir and returns its path.
func newConfigFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, config.RenderDefault(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// writtenLine returns the line `key = ...` that SetOption wrote into the file.
func writtenLine(t *testing.T, path, key string) string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(line, key+" = ") {
			return strings.TrimPrefix(line, key+" = ")
		}
	}
	t.Fatalf("no line for %q in:\n%s", key, src)
	return ""
}

// escapesIn returns the character after every backslash in a TOML basic string
// literal, skipping the character that a backslash itself escapes.
func escapesIn(literal string) []byte {
	var out []byte
	for i := 0; i < len(literal); i++ {
		if literal[i] == '\\' && i+1 < len(literal) {
			out = append(out, literal[i+1])
			i++
		}
	}
	return out
}

// toml10Escapes are the only escape sequences TOML 1.0 defines for basic
// strings: \b \t \n \f \r \" \\ and the two Unicode forms.
const toml10Escapes = `btnfr"\uU`

func assertOnlyTOMLEscapes(t *testing.T, input, literal string) {
	t.Helper()
	for _, c := range escapesIn(literal) {
		if !strings.ContainsRune(toml10Escapes, rune(c)) {
			t.Fatalf("input %q was written as %s, which uses the escape \\%c that TOML 1.0 does not have", input, literal, c)
		}
	}
}

// Every ASCII character, in the middle of a value: the file must load again,
// the value must come back unchanged, and only TOML escapes may be written.
func TestSetOptionRoundTripsEveryAsciiCharacter(t *testing.T) {
	for r := rune(0); r <= 0x7f; r++ {
		input := "a" + string(r) + "b"
		p := newConfigFile(t)

		if err := config.SetOption(p, "m", "k", input); err != nil {
			t.Fatalf("%q: SetOption: %v", input, err)
		}
		c, err := config.Load(p)
		if err != nil {
			t.Fatalf("%q: the written file does not load: %v\nwritten: %s", input, err, writtenLine(t, p, "k"))
		}
		if got := c.Modules["m"].Options["k"]; got != input {
			t.Fatalf("%q: came back as %q", input, got)
		}
		assertOnlyTOMLEscapes(t, input, writtenLine(t, p, "k"))
	}
}

func TestSetOptionRoundTripsOtherAwkwardCharacters(t *testing.T) {
	for _, r := range []rune{0x80, 0x85, 0x9f, 0xa0, 0xad, 0x200b, 0x2028, 0x2029, 0x202e, 0xfeff, 0xfffd, 0x1f600, 0x10ffff} {
		input := "a" + string(r) + "b"
		p := newConfigFile(t)

		if err := config.SetOption(p, "m", "k", input); err != nil {
			t.Fatalf("U+%04X: SetOption: %v", r, err)
		}
		c, err := config.Load(p)
		if err != nil {
			t.Fatalf("U+%04X: the written file does not load: %v", r, err)
		}
		if got := c.Modules["m"].Options["k"]; got != input {
			t.Fatalf("U+%04X: came back as %q", r, got)
		}
		assertOnlyTOMLEscapes(t, input, writtenLine(t, p, "k"))
	}
}

func TestSetOptionRoundTripsListElementsWithControlCharacters(t *testing.T) {
	p := newConfigFile(t)
	in := []string{"a\x07b", "c\x0bd", "e\x01f", "plain", `q"uo\te`}

	if err := config.SetOption(p, "m", "k", in); err != nil {
		t.Fatalf("SetOption: %v", err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("the written file does not load: %v\nwritten: %s", err, writtenLine(t, p, "k"))
	}

	got, ok := c.Modules["m"].Options["k"].([]any)
	if !ok || len(got) != len(in) {
		t.Fatalf("list = %#v, want %d elements", c.Modules["m"].Options["k"], len(in))
	}
	for i, want := range in {
		if got[i] != want {
			t.Fatalf("element %d = %q, want %q", i, got[i], want)
		}
	}
	assertOnlyTOMLEscapes(t, fmt.Sprint(in), writtenLine(t, p, "k"))
}

// What worked before must be written exactly as before, so existing files and
// diffs do not change; only the Go-only escapes are replaced.
func TestSetOptionWritesTheSameLiteralsAsBefore(t *testing.T) {
	cases := []struct{ input, literal string }{
		{"--height 40%", `"--height 40%"`},
		{"a\"b\\c\nd\te", `"a\"b\\c\nd\te"`},
		{"\b\f\r", `"\b\f\r"`},
		{"é", `"é"`},
		{"日本語", `"日本語"`},
		{"😀", `"😀"`},
		{"\u200b", `"\u200b"`},
		{"\u2028", `"\u2028"`},
		{"\ufeff", `"\ufeff"`},
		{"\u0085", `"\u0085"`},
		{"\U0010ffff", `"\U0010ffff"`},
		{"", `""`},
	}
	for _, c := range cases {
		p := newConfigFile(t)
		if err := config.SetOption(p, "m", "k", c.input); err != nil {
			t.Fatalf("%q: SetOption: %v", c.input, err)
		}
		if got := writtenLine(t, p, "k"); got != c.literal {
			t.Fatalf("%q written as %s, want %s", c.input, got, c.literal)
		}
	}
}

// The characters Go writes with escapes TOML lacks become \u00XX.
func TestSetOptionWritesGoOnlyEscapesAsUnicodeEscapes(t *testing.T) {
	cases := []struct{ input, literal string }{
		{"\x07", `"\u0007"`}, // BEL, Go: \a
		{"\x0b", `"\u000b"`}, // VT, Go: \v
		{"\x00", `"\u0000"`},
		{"\x01", `"\u0001"`}, // Go: \x01
		{"\x1b", `"\u001b"`}, // ESC
		{"\x1f", `"\u001f"`},
		{"\x7f", `"\u007f"`}, // DEL, Go: \x7f
	}
	for _, c := range cases {
		p := newConfigFile(t)
		if err := config.SetOption(p, "m", "k", c.input); err != nil {
			t.Fatalf("%q: SetOption: %v", c.input, err)
		}
		if got := writtenLine(t, p, "k"); got != c.literal {
			t.Fatalf("%q written as %s, want %s", c.input, got, c.literal)
		}
	}
}

// TOML text must be valid UTF-8; a value that is not cannot be written, and
// silently turning each stray byte into another character would change it.
func TestSetOptionRejectsInvalidUTF8AndLeavesTheFileUntouched(t *testing.T) {
	for _, bad := range []string{"a\xffb", "a\xc3b", "\xe2\x82"} {
		p := newConfigFile(t)
		before, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}

		err = config.SetOption(p, "m", "k", bad)

		var cfgErr config.Error
		if !errors.As(err, &cfgErr) || !strings.Contains(cfgErr.Msg, "m.k") || !strings.Contains(cfgErr.Msg, "not valid UTF-8") {
			t.Fatalf("%q: err = %v, want a config.Error naming m.k and the UTF-8 problem", bad, err)
		}
		after, err := os.ReadFile(p)
		if err != nil || string(before) != string(after) {
			t.Fatalf("%q: the file must be untouched (err=%v)", bad, err)
		}
	}
}

func TestSetOptionRejectsInvalidUTF8InAListElementAndWritesNothing(t *testing.T) {
	p := newConfigFile(t)
	before, _ := os.ReadFile(p)

	err := config.SetOption(p, "m", "k", []string{"fine", "bad\xff"})

	var cfgErr config.Error
	if !errors.As(err, &cfgErr) || !strings.Contains(cfgErr.Msg, "not valid UTF-8") {
		t.Fatalf("err = %v, want a config.Error about UTF-8", err)
	}
	if after, _ := os.ReadFile(p); string(before) != string(after) {
		t.Fatal("the file must be untouched")
	}
}

// Writing a value with a control character must not disturb the rest of the
// file: comments, other tables and other keys stay as they were.
func TestSetOptionWithAControlCharacterLeavesTheRestOfTheFileAlone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	original := `# my omnishell config
[omnishell]
version = 1

[modules.direnv]
# keep this comment
enabled = true

[modules.direnv.options]
whitelist = ["a", "b"]
`
	if err := os.WriteFile(p, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := config.SetOption(p, "direnv", "log_format", "x\x07y"); err != nil {
		t.Fatalf("SetOption: %v", err)
	}

	got, _ := os.ReadFile(p)
	for _, line := range strings.Split(strings.TrimRight(original, "\n"), "\n") {
		if !strings.Contains(string(got), line) {
			t.Fatalf("line %q is gone:\n%s", line, got)
		}
	}
	if !strings.Contains(string(got), `log_format = "x\u0007y"`) {
		t.Fatalf("the new option is missing:\n%s", got)
	}
	if _, err := config.Load(p); err != nil {
		t.Fatalf("the file must still load: %v", err)
	}
}
