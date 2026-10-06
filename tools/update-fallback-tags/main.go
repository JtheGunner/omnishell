package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, execRunner{}); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// execRunner runs real commands.
type execRunner struct{}

func (execRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// run is main without the process exit, so tests can drive it.
func run(args []string, stdout io.Writer, r Runner) error {
	fs := flag.NewFlagSet("update-fallback-tags", flag.ContinueOnError)
	dir := fs.String("dir", "modules/builtin", "directory holding the built-in module folders")
	summaryPath := fs.String("summary", "", "write the pull request body to this file")
	outputPath := fs.String("github-output", "", "append changed/same_major lines to this file (GITHUB_OUTPUT format)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	res, err := Update(*dir, r)
	if err != nil {
		return err
	}
	if *summaryPath != "" {
		if err := os.WriteFile(*summaryPath, []byte(summary(res)), 0o644); err != nil {
			return fmt.Errorf("write summary: %w", err)
		}
	}
	if *outputPath != "" {
		if err := appendOutputs(*outputPath, len(res.Changes) > 0, sameMajor(res.Changes)); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(stdout, "%d change(s), %d failure(s)\n", len(res.Changes), len(res.Failures))
	for _, f := range res.Failures {
		_, _ = fmt.Fprintf(stdout, "  %s: %v\n", f.Module, f.Err)
	}
	if len(res.Failures) > 0 && len(res.Changes) == 0 {
		return fmt.Errorf("%d module(s) could not be checked and nothing changed", len(res.Failures))
	}
	return nil
}

func appendOutputs(path string, changed, sameMajor bool) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "changed=%t\nsame_major=%t\n", changed, sameMajor); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
