package pkgmgr

// SetRunningAsRootForTest overrides the package-level runningAsRoot seam and
// returns a function that restores the previous value.
func SetRunningAsRootForTest(v bool) func() {
	old := runningAsRoot
	runningAsRoot = v
	return func() { runningAsRoot = old }
}

// ExtractMember exposes extractMember to the external test package.
var ExtractMember = extractMember
