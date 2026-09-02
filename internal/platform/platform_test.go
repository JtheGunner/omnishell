package platform_test

import (
	"errors"
	"testing"

	"github.com/JtheGunner/omnishell/internal/platform"
)

func env(goos string, present map[string]bool, vars map[string]string) platform.Env {
	return platform.Env{
		GOOS:   goos,
		GOARCH: "arm64",
		Getenv: func(k string) string { return vars[k] },
		LookPath: func(bin string) (string, error) {
			if present[bin] {
				return "/usr/bin/" + bin, nil
			}
			return "", errors.New("not found")
		},
	}
}

func TestDetectWithMacOSDefaultConfigDir(t *testing.T) {
	info, err := platform.DetectWith(env("darwin",
		map[string]bool{"zsh": true, "bash": false},
		map[string]string{"HOME": "/Users/j"}))
	if err != nil {
		t.Fatal(err)
	}
	if info.OS != platform.MacOS {
		t.Fatalf("OS = %q, want macos", info.OS)
	}
	if info.ConfigDir != "/Users/j/.config/omnishell" {
		t.Fatalf("ConfigDir = %q", info.ConfigDir)
	}
	if len(info.Shells) != 2 || info.Shells[0].Name != "zsh" || info.Shells[1].Name != "bash" {
		t.Fatalf("Shells = %+v, want [zsh bash]", info.Shells)
	}
	if !info.Shells[0].Present || info.Shells[1].Present {
		t.Fatalf("presence wrong: %+v", info.Shells)
	}
	if info.Shells[0].RCPath != "/Users/j/.zshrc" || info.Shells[1].RCPath != "/Users/j/.bashrc" {
		t.Fatalf("rc paths wrong: %+v", info.Shells)
	}
}

func TestDetectWithLinuxXDGConfigDir(t *testing.T) {
	info, err := platform.DetectWith(env("linux",
		map[string]bool{"bash": true},
		map[string]string{"HOME": "/home/j", "XDG_CONFIG_HOME": "/home/j/xdg"}))
	if err != nil {
		t.Fatal(err)
	}
	if info.OS != platform.Linux {
		t.Fatalf("OS = %q, want linux", info.OS)
	}
	if info.ConfigDir != "/home/j/xdg/omnishell" {
		t.Fatalf("ConfigDir = %q", info.ConfigDir)
	}
}

func TestDetectWithUnsupportedOS(t *testing.T) {
	_, err := platform.DetectWith(env("windows", nil, map[string]string{"HOME": "C:\\Users\\j"}))
	if err == nil {
		t.Fatal("want error for unsupported OS, got nil")
	}
}

func TestDetectWithMissingHome(t *testing.T) {
	_, err := platform.DetectWith(env("linux", nil, map[string]string{}))
	if err == nil {
		t.Fatal("want error when HOME is unset")
	}
}
