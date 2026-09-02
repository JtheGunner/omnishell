package platform

import "path/filepath"

// SupportedShells is the fixed, ordered list of shells omnishell can manage.
var SupportedShells = []string{"zsh", "bash"}

func rcPath(shell, home string) string {
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "bash":
		return filepath.Join(home, ".bashrc")
	default:
		return ""
	}
}
