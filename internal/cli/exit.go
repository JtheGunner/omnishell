package cli

// ClassifyError maps an error to a process exit code.
// Extended in later tasks for config (2) and drift (3) errors.
func ClassifyError(err error) int {
	if err == nil {
		return 0
	}
	return 1
}
