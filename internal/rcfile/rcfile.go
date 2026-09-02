// Package rcfile inserts, updates, and removes the single omnishell marker
// block in a shell rc file. The rest of the file is never touched.
package rcfile

import "strings"

const (
	BlockStart = "# >>> omnishell >>>"
	BlockEnd   = "# <<< omnishell <<<"
)

// SourceLine returns the one line that lives inside the marker block.
func SourceLine(shell, initPath string) string {
	if shell == "bash" {
		return `[ -f "` + initPath + `" ] && source "` + initPath + `"`
	}
	return `[[ -f "` + initPath + `" ]] && source "` + initPath + `"`
}

// BlockPresent reports whether the marker block exists.
func BlockPresent(content string) bool {
	return strings.Contains(content, BlockStart) && strings.Contains(content, BlockEnd)
}

func block(shell, initPath string) string {
	return BlockStart + "\n" + SourceLine(shell, initPath) + "\n" + BlockEnd + "\n"
}

// EnsureBlock guarantees the block exists with the current source line.
func EnsureBlock(content, shell, initPath string) (string, bool) {
	want := block(shell, initPath)
	start := strings.Index(content, BlockStart)
	if start >= 0 {
		endIdx := strings.Index(content[start:], BlockEnd)
		if endIdx >= 0 {
			end := start + endIdx + len(BlockEnd)
			if end < len(content) && content[end] == '\n' {
				end++
			}
			existing := content[start:end]
			if existing == want {
				return content, false
			}
			return content[:start] + want + content[end:], true
		}
	}
	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return want, true
	}
	return trimmed + "\n\n" + want, true
}

// RemoveBlock deletes the block and one preceding blank line if present.
func RemoveBlock(content string) (string, bool) {
	start := strings.Index(content, BlockStart)
	if start < 0 {
		return content, false
	}
	endIdx := strings.Index(content[start:], BlockEnd)
	if endIdx < 0 {
		return content, false
	}
	end := start + endIdx + len(BlockEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	pre := content[:start]
	pre = strings.TrimRight(pre, "\n")
	if pre != "" {
		pre += "\n"
	}
	return pre + content[end:], true
}
