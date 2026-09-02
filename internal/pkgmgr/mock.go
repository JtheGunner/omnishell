package pkgmgr

import (
	"errors"
	"strings"
)

// MockResponse is one canned command result.
type MockResponse struct {
	Out []byte
	Err error
}

// MockRunner is a Runner backed by a response table.
type MockRunner struct {
	Responses map[string]MockResponse
	Calls     []string
	LookOK    map[string]bool
}

// Run records the call and returns the canned response (nil, nil if none).
func (m *MockRunner) Run(name string, args ...string) ([]byte, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	m.Calls = append(m.Calls, key)
	if r, ok := m.Responses[key]; ok {
		return r.Out, r.Err
	}
	return nil, nil
}

// Look reports whether LookOK[name] is set.
func (m *MockRunner) Look(name string) (string, error) {
	if m.LookOK[name] {
		return "/usr/bin/" + name, nil
	}
	return "", errors.New("not found: " + name)
}

// MockManager is a Manager for tests.
type MockManager struct {
	NameV        string
	DetectV      bool
	Installed    map[string]bool
	InstallCalls [][]string
	SudoV        bool
	InstallErr   error
}

// Name returns the configured name.
func (m *MockManager) Name() string { return m.NameV }

// Detect returns the configured detection result.
func (m *MockManager) Detect() bool { return m.DetectV }

// NeedsSudo returns the configured sudo requirement.
func (m *MockManager) NeedsSudo() bool { return m.SudoV }

// IsInstalled reports whether pkg is marked installed.
func (m *MockManager) IsInstalled(pkg string) (bool, error) {
	return m.Installed[pkg], nil
}

// Install records the call and marks pkgs installed unless InstallErr is set.
func (m *MockManager) Install(pkgs []string) error {
	m.InstallCalls = append(m.InstallCalls, append([]string{}, pkgs...))
	if m.InstallErr != nil {
		return m.InstallErr
	}
	for _, p := range pkgs {
		if m.Installed == nil {
			m.Installed = map[string]bool{}
		}
		m.Installed[p] = true
	}
	return nil
}
