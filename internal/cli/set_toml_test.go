package cli_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/config"
)

// A control character in a value used to be written with a Go-only escape
// (BEL as \a, VT as \v), after which no command could load config.toml.
func TestSetWithControlCharactersKeepsTheConfigLoadable(t *testing.T) {
	cases := map[string]string{"BEL": "a\x07b", "VT": "a\x0bb", "ESC": "a\x1bb", "DEL": "a\x7fb", "NUL": "a\x00b"}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			cfgPath := setTestInit(t)

			var out, errb bytes.Buffer
			if code := cli.Execute([]string{"set", "direnv.log_format", value}, &out, &errb); code != 0 {
				t.Fatalf("set exit %d: %s", code, errb.String())
			}

			out.Reset()
			errb.Reset()
			if code := cli.Execute([]string{"list"}, &out, &errb); code != 0 {
				t.Fatalf("the next command cannot load the config: exit %d: %s", code, errb.String())
			}
			c, err := config.Load(cfgPath)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := c.Modules["direnv"].Options["log_format"]; got != value {
				t.Fatalf("log_format = %q, want %q", got, value)
			}
		})
	}
}

// A value that is not valid UTF-8 cannot be written to a TOML file; it must be
// refused up front instead of coming back as different characters.
func TestSetWithInvalidUTF8IsRejectedAndLeavesTheConfigUntouched(t *testing.T) {
	cfgPath := setTestInit(t)
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"set", "direnv.log_format", "a\xffb"}, &out, &errb)

	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "direnv.log_format") || !strings.Contains(errb.String(), "not valid UTF-8") {
		t.Fatalf("stderr should name the option and the problem, got: %s", errb.String())
	}
	if out.Len() != 0 {
		t.Fatalf("nothing to stdout on error, got: %s", out.String())
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("config.toml must be untouched (err=%v)", err)
	}
}
