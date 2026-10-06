package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesSummaryAndOutputs(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	r := &fakeRunner{tags: map[string]string{"https://example.com/alpha.git": lsRemote("v1.1.0")}}
	work := t.TempDir()
	summaryPath := filepath.Join(work, "summary.md")
	outputPath := filepath.Join(work, "output")

	var stdout bytes.Buffer
	args := []string{"--dir", dir, "--summary", summaryPath, "--github-output", outputPath}
	if err := run(args, &stdout, r); err != nil {
		t.Fatal(err)
	}
	if s, _ := os.ReadFile(summaryPath); !strings.Contains(string(s), "| `alpha` | `v1.0.0` | `v1.1.0` |") {
		t.Fatalf("summary file:\n%s", s)
	}
	out, _ := os.ReadFile(outputPath)
	if !strings.Contains(string(out), "changed=true\n") || !strings.Contains(string(out), "same_major=true\n") {
		t.Fatalf("github output:\n%s", out)
	}
}

func TestRunReportsNoChange(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	r := &fakeRunner{tags: map[string]string{"https://example.com/alpha.git": lsRemote("v1.0.0")}}
	outputPath := filepath.Join(t.TempDir(), "output")

	if err := run([]string{"--dir", dir, "--github-output", outputPath}, &bytes.Buffer{}, r); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(outputPath)
	if !strings.Contains(string(out), "changed=false\n") || !strings.Contains(string(out), "same_major=false\n") {
		t.Fatalf("github output:\n%s", out)
	}
}

func TestRunFailsWhenSomethingFailedAndNothingChanged(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	r := &fakeRunner{fail: map[string]error{"https://example.com/alpha.git": errors.New("exit status 128")}}
	summaryPath := filepath.Join(t.TempDir(), "summary.md")

	err := run([]string{"--dir", dir, "--summary", summaryPath}, &bytes.Buffer{}, r)
	if err == nil {
		t.Fatal("want an error so a run that could check nothing goes red")
	}
	if s, _ := os.ReadFile(summaryPath); !strings.Contains(string(s), "alpha") {
		t.Fatalf("the summary must still be written before the error:\n%s", s)
	}
}

func TestRunSucceedsWhenSomeModulesFailButOthersChange(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	writeModule(t, dir, "beta", "https://example.com/beta.git", "v2.0.0")
	r := &fakeRunner{
		fail: map[string]error{"https://example.com/alpha.git": errors.New("exit status 128")},
		tags: map[string]string{"https://example.com/beta.git": lsRemote("v2.1.0")},
	}
	if err := run([]string{"--dir", dir}, &bytes.Buffer{}, r); err != nil {
		t.Fatalf("a partial failure with a change must not fail the run: %v", err)
	}
}
