package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
)

func TestVersionCommandPrintsVersion(t *testing.T) {
	var out, errb bytes.Buffer
	code := cli.Execute([]string{"version"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, errb.String())
	}
	if !strings.Contains(out.String(), "dev") {
		t.Fatalf("version output = %q, want it to contain %q", out.String(), "dev")
	}
}
