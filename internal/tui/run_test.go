package tui

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestRunReturnsTheBackendErrorWithoutTouchingTheTerminal(t *testing.T) {
	boom := errors.New("boom")
	var out bytes.Buffer

	_, err := Run(&fakeBackend{modulesErr: boom}, strings.NewReader(""), &out)

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if out.Len() != 0 {
		t.Fatalf("nothing may be written when loading fails, got %q", out.String())
	}
}

// A real program loop, fed a q on its input, must start and quit cleanly.
func TestRunQuitsWhenTheUserPressesQ(t *testing.T) {
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	var out bytes.Buffer
	go func() {
		result, err := Run(&fakeBackend{views: sampleViews()}, strings.NewReader("q"), &out)
		done <- outcome{result, err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("Run: %v", got.err)
		}
		if got.result.ApplyRequested {
			t.Fatal("quitting with q must not request an apply")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after q")
	}
}

func TestResultOfReadsTheUsersDecisionOffTheFinalModel(t *testing.T) {
	if (resultOf(Model{applyRequested: true})) != (Result{ApplyRequested: true}) {
		t.Fatal("a confirmed plan screen must request an apply")
	}
	if resultOf(Model{}) != (Result{}) {
		t.Fatal("a model that never confirmed must not request an apply")
	}
	if resultOf(nil) != (Result{}) {
		t.Fatal("a missing model must not request an apply")
	}
}

// SIGINT from outside (kill -INT) ends the program with tea.ErrInterrupted;
// that is the user leaving, not a failure to report.
func TestAnInterruptEndsTheUIWithoutAnError(t *testing.T) {
	for _, err := range []error{tea.ErrInterrupted, fmt.Errorf("wrapped: %w", tea.ErrInterrupted)} {
		result, got := finish(Model{applyRequested: true}, err)
		if got != nil {
			t.Fatalf("finish(%v) = %v, want no error", err, got)
		}
		if result.ApplyRequested {
			t.Fatal("an interrupt must never request an apply, even if the plan was confirmed")
		}
	}
}

func TestOtherRunErrorsStillSurface(t *testing.T) {
	boom := errors.New("boom")
	if _, got := finish(nil, boom); !errors.Is(got, boom) {
		t.Fatalf("finish = %v, want the error to be wrapped and returned", got)
	}
}
