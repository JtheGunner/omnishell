package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

type fakeBackend struct {
	views []modedit.ModuleView
	err   error
}

func (f fakeBackend) Modules() ([]modedit.ModuleView, error) { return f.views, f.err }

func TestRunReturnsTheBackendErrorWithoutTouchingTheTerminal(t *testing.T) {
	boom := errors.New("boom")
	var out bytes.Buffer

	err := Run(fakeBackend{err: boom}, strings.NewReader(""), &out)

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if out.Len() != 0 {
		t.Fatalf("nothing may be written when loading fails, got %q", out.String())
	}
}

// A real program loop, fed a q on its input, must start and quit cleanly.
func TestRunQuitsWhenTheUserPressesQ(t *testing.T) {
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() { done <- Run(fakeBackend{views: sampleViews()}, strings.NewReader("q"), &out) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after q")
	}
}
