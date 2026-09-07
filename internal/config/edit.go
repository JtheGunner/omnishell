package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
)

// RenderDefault returns the commented default config file.
func RenderDefault() []byte {
	return []byte(`# omnishell configuration — https://github.com/JtheGunner/omnishell
# Edit this file by hand, or use: omnishell enable/disable/set
# Apply changes with: omnishell apply

[omnishell]
version = 1
# shells = ["zsh", "bash"]   # omit to auto-detect the login shell
# startup_budget_ms = 200    # 'omnishell bench' warns above this per-shell cost
`)
}

// SetEnabled sets modules.<id>.enabled in the file at path.
func SetEnabled(path, moduleID string, enabled bool) error {
	return editTable(path, moduleID, "", "enabled", strconv.FormatBool(enabled))
}

// SetOption sets modules.<id>.options.<key> in the file at path.
func SetOption(path, moduleID, key string, value any) error {
	lit, err := tomlLiteral(value)
	if err != nil {
		return err
	}
	return editTable(path, moduleID, "options", key, lit)
}

func tomlLiteral(value any) (string, error) {
	switch v := value.(type) {
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case string:
		return strconv.Quote(v), nil
	case []string:
		quoted := make([]string, len(v))
		for i, s := range v {
			quoted[i] = strconv.Quote(s)
		}
		return "[" + strings.Join(quoted, ", ") + "]", nil
	default:
		return "", fmt.Errorf("cannot render TOML literal for %T", value)
	}
}

// editTable ensures [modules.<id>] (and optionally a .<sub> child table) exists
// and that "<key> = <literal>" is present within it, replacing any existing
// assignment of <key>. All other lines and comments are preserved.
func editTable(path, moduleID, sub, key, literal string) error {
	src, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		src = RenderDefault()
	} else if err != nil {
		return Error{Path: path, Msg: err.Error()}
	}

	header := "[modules." + moduleID + "]"
	if sub != "" {
		header = "[modules." + moduleID + "." + sub + "]"
	}
	assignment := key + " = " + literal

	lines := strings.Split(strings.TrimRight(string(src), "\n"), "\n")
	hdrIdx := indexOfHeader(lines, header)

	if hdrIdx == -1 {
		// Append the table (and an empty parent table if the sub-table's parent
		// is absent). `set` must never change enablement — that is `enable`'s
		// job — so the parent table is created WITHOUT an `enabled` line; it
		// decodes fine to Enabled: false.
		if sub != "" && indexOfHeader(lines, "[modules."+moduleID+"]") == -1 {
			lines = append(lines, "", "[modules."+moduleID+"]")
		}
		lines = append(lines, "", header, assignment)
		return atomicfile.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}

	// Scan the table body until the next top-level "[" header or EOF.
	end := len(lines)
	for i := hdrIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	for i := hdrIdx + 1; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name := strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[0])
		if name == key {
			lines[i] = assignment
			return atomicfile.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
		}
	}
	// Not found: insert right after the header line.
	out := append([]string{}, lines[:hdrIdx+1]...)
	out = append(out, assignment)
	out = append(out, lines[hdrIdx+1:]...)
	return atomicfile.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}

func indexOfHeader(lines []string, header string) int {
	for i, l := range lines {
		if strings.TrimSpace(l) == header {
			return i
		}
	}
	return -1
}
