package config

import (
	"os/user"
	"path/filepath"
	"testing"
)

// TestFromEnvironment 校验安装目录的三种取值来源。
func TestFromEnvironment(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil {
		t.Fatalf("find current user: %v", err)
	}

	tests := []struct {
		name     string
		home     string
		dataHome string
		expected string
	}{
		{
			name:     "explicit home wins",
			home:     "/srv/clashctl",
			dataHome: "/srv/data",
			expected: "/srv/clashctl",
		},
		{
			name:     "xdg data home",
			dataHome: "/srv/data",
			expected: "/srv/data/clashctl",
		},
		{
			name:     "user share fallback",
			expected: filepath.Join(currentUser.HomeDir, ".local", "share", "clashctl"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLASHCTL_HOME", tt.home)
			t.Setenv("XDG_DATA_HOME", tt.dataHome)

			paths, err := FromEnvironment("mihomo")
			if err != nil {
				t.Fatalf("FromEnvironment() error = %v", err)
			}
			if paths.Home != tt.expected {
				t.Fatalf("Home = %q, want %q", paths.Home, tt.expected)
			}
			if got := filepath.Base(paths.KernelPath); got != "mihomo" {
				t.Fatalf("kernel file = %q, want %q", got, "mihomo")
			}
		})
	}
}

// TestNewPaths 校验资源文件都位于安装目录之下。
func TestNewPaths(t *testing.T) {
	paths := NewPaths("/srv/clashctl", "clash")

	if paths.RuntimeConfig != "/srv/clashctl/resources/runtime.yaml" {
		t.Fatalf("RuntimeConfig = %q", paths.RuntimeConfig)
	}
	if paths.KernelPath != "/srv/clashctl/bin/clash" {
		t.Fatalf("KernelPath = %q", paths.KernelPath)
	}
	if paths.ProfilesMeta != "/srv/clashctl/resources/profiles.yaml" {
		t.Fatalf("ProfilesMeta = %q", paths.ProfilesMeta)
	}
}
