package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFilePath(t *testing.T) {
	t.Run("default_path", func(t *testing.T) {
		// Clear env vars that might interfere
		orig := os.Getenv("MU_CONFIG")
		origXDG := os.Getenv("XDG_CONFIG_HOME")
		os.Unsetenv("MU_CONFIG")
		os.Unsetenv("XDG_CONFIG_HOME")
		defer func() {
			if orig != "" {
				os.Setenv("MU_CONFIG", orig)
			}
			if origXDG != "" {
				os.Setenv("XDG_CONFIG_HOME", origXDG)
			}
		}()

		path := configFilePath()
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".config", "mu", "config.toml")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("mu_config_env", func(t *testing.T) {
		os.Setenv("MU_CONFIG", "/tmp/custom-mu.toml")
		defer os.Unsetenv("MU_CONFIG")

		path := configFilePath()
		if path != "/tmp/custom-mu.toml" {
			t.Errorf("expected /tmp/custom-mu.toml, got %q", path)
		}
	})

	t.Run("xdg_config_home", func(t *testing.T) {
		os.Unsetenv("MU_CONFIG")
		os.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
		defer os.Unsetenv("XDG_CONFIG_HOME")

		path := configFilePath()
		expected := filepath.Join("/tmp/xdg", "mu", "config.toml")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})
}

func TestValueOrDefault(t *testing.T) {
	tests := []struct {
		val, def, want string
	}{
		{"hello", "default", "hello"},
		{"", "default", "default"},
		{"value", "", "value"},
		{"", "", ""},
	}
	for _, tt := range tests {
		got := valueOrDefault(tt.val, tt.def)
		if got != tt.want {
			t.Errorf("valueOrDefault(%q, %q) = %q, want %q", tt.val, tt.def, got, tt.want)
		}
	}
}
