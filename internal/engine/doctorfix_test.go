package engine_test

import (
	"testing"

	"github.com/JtheGunner/omnishell/internal/engine"
)

func codesOf(fs []engine.Finding) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Code
	}
	return out
}

func TestDriftFixabilityPartitions(t *testing.T) {
	rep := engine.DoctorReport{Findings: []engine.Finding{
		{Severity: engine.SeverityDrift, Code: "initfile-stale:zsh"},
		{Severity: engine.SeverityDrift, Code: "rc-block-missing:zsh"},
		{Severity: engine.SeverityDrift, Code: "orphan-lock-entry:fzf"},
		{Severity: engine.SeverityDrift, Code: "initfile-edited:bash"},
		{Severity: engine.SeverityDrift, Code: "packages-missing:fzf"},
		{Severity: engine.SeverityNotice, Code: "unknown-module:foo"},
	}}
	fixable, blocked := rep.DriftFixability()

	wantFix := []string{"initfile-stale:zsh", "rc-block-missing:zsh", "orphan-lock-entry:fzf"}
	wantBlk := []string{"initfile-edited:bash", "packages-missing:fzf"}
	if got := codesOf(fixable); !equalStrs(got, wantFix) {
		t.Fatalf("fixable = %v, want %v", got, wantFix)
	}
	if got := codesOf(blocked); !equalStrs(got, wantBlk) {
		t.Fatalf("blocked = %v, want %v", got, wantBlk)
	}
}

func TestDriftFixabilityAllFixable(t *testing.T) {
	rep := engine.DoctorReport{Findings: []engine.Finding{
		{Severity: engine.SeverityDrift, Code: "never-applied"},
		{Severity: engine.SeverityDrift, Code: "initfile-missing:zsh"},
		{Severity: engine.SeverityDrift, Code: "pending-apply:completion"},
		{Severity: engine.SeverityDrift, Code: "stale-shell:bash"},
	}}
	fixable, blocked := rep.DriftFixability()
	if len(blocked) != 0 {
		t.Fatalf("blocked = %v, want none", codesOf(blocked))
	}
	if len(fixable) != 4 {
		t.Fatalf("fixable = %v, want 4", codesOf(fixable))
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
