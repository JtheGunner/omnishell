package cli_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/selfupdate"
)

type stubResolver struct {
	tag string
	err error
}

func (s stubResolver) Latest() (string, error) { return s.tag, s.err }

func stubUpdater(version string, r stubResolver) func(io.Writer) selfupdate.Updater {
	return func(stdout io.Writer) selfupdate.Updater {
		return selfupdate.Updater{
			Version:    version,
			Resolver:   r,
			Executable: func() (string, error) { return "/opt/homebrew/bin/omnishell", nil },
			Stdout:     stdout,
		}
	}
}

func TestSelfUpdateCommand(t *testing.T) {
	t.Cleanup(func() { cli.SetUpdaterFactoryForTest(nil) })
	tests := []struct {
		name     string
		args     []string
		version  string
		resolver stubResolver
		wantCode int
		wantOut  string
	}{
		{"check reports newer", []string{"self-update", "--check"}, "1.0.0", stubResolver{tag: "v1.1.0"}, 0, "update available: current 1.0.0, latest v1.1.0"},
		{"check up to date", []string{"self-update", "--check"}, "1.1.0", stubResolver{tag: "v1.1.0"}, 0, "already up to date"},
		{"check lookup failure", []string{"self-update", "--check"}, "1.0.0", stubResolver{err: errors.New("offline")}, 1, ""},
		{"homebrew install points to brew", []string{"self-update"}, "1.0.0", stubResolver{tag: "v1.1.0"}, 0, "brew upgrade"},
		{"extra argument is rejected", []string{"self-update", "v1.2.0"}, "1.0.0", stubResolver{tag: "v1.1.0"}, 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli.SetUpdaterFactoryForTest(stubUpdater(tt.version, tt.resolver))
			var out, errb bytes.Buffer
			code := cli.Execute(tt.args, &out, &errb)
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d; stderr=%q", code, tt.wantCode, errb.String())
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Fatalf("stdout = %q, want it to contain %q", out.String(), tt.wantOut)
			}
		})
	}
}

func TestSelfUpdateDevBuildExitsNonZero(t *testing.T) {
	t.Cleanup(func() { cli.SetUpdaterFactoryForTest(nil) })
	cli.SetUpdaterFactoryForTest(func(stdout io.Writer) selfupdate.Updater {
		return selfupdate.Updater{
			Version:    "dev",
			Resolver:   stubResolver{tag: "v1.1.0"},
			Executable: func() (string, error) { return "/usr/local/bin/omnishell", nil },
			Stdout:     stdout,
		}
	})
	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"self-update"}, &out, &errb); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "unknown") {
		t.Fatalf("stderr = %q, want an 'unknown version' message", errb.String())
	}
}
