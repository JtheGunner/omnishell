package configsync_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/configsync"
	"github.com/JtheGunner/omnishell/internal/module"
)

const cfgPath = "config.toml"

func schemas() map[string]map[string]module.OptionSchema {
	return map[string]map[string]module.OptionSchema{
		"fzf": {
			"ctrl_r": {Type: "bool", Default: true, Help: "Bind Ctrl+R"},
			"height": {Type: "string", Default: "40%", Help: "Popup height"},
		},
		"history": {
			"size": {Type: "int", Default: int64(50000), Help: "Entries to keep"},
		},
		"plain": {},
	}
}

func lookup(id string) (map[string]module.OptionSchema, bool) {
	s, ok := schemas()[id]
	return s, ok
}

func neverSkip(string) bool { return false }

func mustPlan(t *testing.T, src string) configsync.Report {
	t.Helper()
	rep, err := configsync.Plan(cfgPath, []byte(src), lookup, neverSkip)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return rep
}

func mustApply(t *testing.T, src string) (string, []configsync.Finding) {
	t.Helper()
	out, rest, err := configsync.Apply(cfgPath, []byte(src), lookup, neverSkip)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, perr := config.Parse(cfgPath, out); perr != nil {
		t.Fatalf("result does not parse: %v\n%s", perr, out)
	}
	return string(out), rest
}

func TestPlanReportsMissingOption(t *testing.T) {
	rep := mustPlan(t, "[modules.history]\nenabled = true\n")
	if len(rep.Findings) != 1 {
		t.Fatalf("findings = %+v, want one", rep.Findings)
	}
	f := rep.Findings[0]
	if f.Kind != configsync.MissingOption || f.Module != "history" || f.Option != "size" {
		t.Fatalf("finding = %+v", f)
	}
	if f.Default != "50000" || f.Help != "Entries to keep" {
		t.Fatalf("default/help = %q / %q", f.Default, f.Help)
	}
}

func TestPlanReportsOrphanOptionAndModule(t *testing.T) {
	src := "[modules.fzf]\nenabled = true\n[modules.fzf.options]\nctrl_r = true\nheight = \"40%\"\nlegacy = 1\n\n[modules.gone]\nenabled = false\n"
	rep := mustPlan(t, src)
	var kinds []string
	for _, f := range rep.Findings {
		kinds = append(kinds, string(f.Kind)+":"+f.Module+":"+f.Option)
	}
	got := strings.Join(kinds, ",")
	want := "orphan-option:fzf:legacy,orphan-module:gone:"
	if got != want {
		t.Fatalf("findings = %s, want %s", got, want)
	}
}

func TestPlanUpToDateIsEmpty(t *testing.T) {
	src := "[modules.fzf]\nenabled = true\n[modules.fzf.options]\nctrl_r = true\nheight = \"40%\"\n\n[modules.plain]\nenabled = true\n"
	if rep := mustPlan(t, src); len(rep.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", rep.Findings)
	}
}

func TestPlanSkipsModulesTheCallerKeeps(t *testing.T) {
	skip := func(id string) bool { return id == "gone" }
	rep, err := configsync.Plan(cfgPath, []byte("[modules.gone]\nenabled = true\n"), lookup, skip)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 0 {
		t.Fatalf("findings = %+v, want none for a skipped module", rep.Findings)
	}
}

func TestPlanIgnoresOptionAlreadyListedAsComment(t *testing.T) {
	src := "[modules.history]\nenabled = true\n[modules.history.options]\n# size = 50000   # Entries to keep\n"
	if rep := mustPlan(t, src); len(rep.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", rep.Findings)
	}
}

func TestPlanRejectsBrokenFile(t *testing.T) {
	_, err := configsync.Plan(cfgPath, []byte("[modules.fzf\nenabled = true\n"), lookup, neverSkip)
	var cerr config.Error
	if err == nil || !asConfigError(err, &cerr) {
		t.Fatalf("err = %v, want config.Error", err)
	}
}

func asConfigError(err error, target *config.Error) bool {
	e, ok := err.(config.Error)
	if ok {
		*target = e
	}
	return ok
}

func TestApplyAddsCommentedDefaultToExistingOptionsTable(t *testing.T) {
	src := "# my setup\n[modules.fzf]\nenabled = true\n\n[modules.fzf.options]\n# keep me\nctrl_r = false\n\n[modules.plain]\nenabled = true\n"
	out, rest := mustApply(t, src)
	want := "# my setup\n[modules.fzf]\nenabled = true\n\n[modules.fzf.options]\n# keep me\nctrl_r = false\n# height = \"40%\"   # Popup height\n\n[modules.plain]\nenabled = true\n"
	if out != want {
		t.Fatalf("out =\n%s\nwant =\n%s", out, want)
	}
	if len(rest) != 0 {
		t.Fatalf("rest = %+v", rest)
	}
}

func TestApplyCreatesOptionsTableWhenAbsent(t *testing.T) {
	src := "[modules.history]\nenabled = true\n\n[modules.plain]\nenabled = true\n"
	out, _ := mustApply(t, src)
	want := "[modules.history]\nenabled = true\n\n[modules.history.options]\n# size = 50000   # Entries to keep\n\n[modules.plain]\nenabled = true\n"
	if out != want {
		t.Fatalf("out =\n%s\nwant =\n%s", out, want)
	}
}

func TestApplyCreatesOptionsTableAtEndOfFile(t *testing.T) {
	out, _ := mustApply(t, "[modules.history]\nenabled = true\n")
	want := "[modules.history]\nenabled = true\n\n[modules.history.options]\n# size = 50000   # Entries to keep\n"
	if out != want {
		t.Fatalf("out =\n%s\nwant =\n%s", out, want)
	}
}

func TestApplyRemovesOrphanOptionAndKeepsTheRest(t *testing.T) {
	src := "[modules.fzf]\nenabled = true\n[modules.fzf.options]\nctrl_r = true\n# note\nlegacy = [\n  \"a\",\n  \"b\",\n]\nheight = \"30%\"\n"
	out, rest := mustApply(t, src)
	want := "[modules.fzf]\nenabled = true\n[modules.fzf.options]\nctrl_r = true\n# note\nheight = \"30%\"\n"
	if out != want {
		t.Fatalf("out =\n%s\nwant =\n%s", out, want)
	}
	if len(rest) != 0 {
		t.Fatalf("rest = %+v", rest)
	}
}

func TestApplyRemovesOrphanModuleTables(t *testing.T) {
	src := "[modules.fzf]\nenabled = true\n[modules.fzf.options]\nctrl_r = true\nheight = \"40%\"\n\n[modules.gone]\nenabled = true\n\n[modules.gone.options]\nx = 1\n\n[modules.plain]\nenabled = true\n"
	out, rest := mustApply(t, src)
	want := "[modules.fzf]\nenabled = true\n[modules.fzf.options]\nctrl_r = true\nheight = \"40%\"\n\n[modules.plain]\nenabled = true\n"
	if out != want {
		t.Fatalf("out =\n%s\nwant =\n%s", out, want)
	}
	if len(rest) != 0 {
		t.Fatalf("rest = %+v", rest)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	src := "[modules.fzf]\nenabled = true\n[modules.fzf.options]\nlegacy = 1\n\n[modules.gone]\nenabled = true\n\n[modules.history]\nenabled = true\n"
	first, _ := mustApply(t, src)
	second, _ := mustApply(t, first)
	if first != second {
		t.Fatalf("second run changed the file:\n%s\n---\n%s", first, second)
	}
	if rep := mustPlan(t, first); len(rep.Findings) != 0 {
		t.Fatalf("findings after apply = %+v", rep.Findings)
	}
}

func TestApplyLeavesUpToDateFileByteIdentical(t *testing.T) {
	src := "# header\n[omnishell]\nversion = 1\n\n[modules.plain]\nenabled = true\n"
	out, _ := mustApply(t, src)
	if out != src {
		t.Fatalf("file changed:\n%s", out)
	}
}

func TestApplyReportsWhatItCannotEdit(t *testing.T) {
	src := "[modules.fzf]\nenabled = true\noptions = { ctrl_r = true, legacy = 1 }\n"
	out, rest := mustApply(t, src)
	if !strings.Contains(out, "options = { ctrl_r = true, legacy = 1 }") {
		t.Fatalf("inline table was edited:\n%s", out)
	}
	kinds := map[configsync.Kind]bool{}
	for _, f := range rest {
		kinds[f.Kind] = true
	}
	if !kinds[configsync.OrphanOption] || !kinds[configsync.MissingOption] {
		t.Fatalf("rest = %+v, want the orphan and the missing option left for the user", rest)
	}
}

func TestApplyRejectsBrokenFile(t *testing.T) {
	if _, _, err := configsync.Apply(cfgPath, []byte("[modules.fzf\n"), lookup, neverSkip); err == nil {
		t.Fatal("want an error for a file that does not parse")
	}
}
