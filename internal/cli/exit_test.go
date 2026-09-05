package cli_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
)

func TestClassifyError(t *testing.T) {
	if got := cli.ClassifyError(nil); got != 0 {
		t.Fatalf("nil -> %d, want 0", got)
	}
	if got := cli.ClassifyError(config.Error{Path: "x", Msg: "y"}); got != 2 {
		t.Fatalf("config.Error -> %d, want 2", got)
	}
	if got := cli.ClassifyError(engine.ConfigError{Err: errors.New("bad")}); got != 2 {
		t.Fatalf("engine.ConfigError -> %d, want 2", got)
	}
	if got := cli.ClassifyError(fmt.Errorf("wrapped: %w", config.ErrNotFound)); got != 2 {
		t.Fatalf("config.ErrNotFound -> %d, want 2", got)
	}
	if got := cli.ClassifyError(engine.ErrDegraded); got != 1 {
		t.Fatalf("ErrDegraded -> %d, want 1", got)
	}
	if got := cli.ClassifyError(engine.ErrAborted); got != 1 {
		t.Fatalf("ErrAborted -> %d, want 1", got)
	}
	if got := cli.ClassifyError(errors.New("misc")); got != 1 {
		t.Fatalf("misc -> %d, want 1", got)
	}
	if got := cli.ClassifyError(engine.ErrNoSuchSnapshot); got != 2 {
		t.Fatalf("ClassifyError(ErrNoSuchSnapshot) = %d, want 2", got)
	}
}
