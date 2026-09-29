package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec: code-mode-mcp/retired-enablement-fails-with-migration-guidance
func TestCodeModeRootsFailWithMigrationGuidance(t *testing.T) {
	assertGuidance := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("expected codeMode migration error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "codeMode") || !strings.Contains(msg, "install") || !strings.Contains(msg, "code-mode") {
			t.Fatalf("error is not actionable code-mode guidance: %v", err)
		}
	}

	t.Run("v4 root", func(t *testing.T) {
		_, err := ParseV4Config([]byte("apiVersion: agent-marketplace/v4\ncodeMode:\n  enabled: true\n"), "config.yaml", true)
		assertGuidance(t, err)
		var v4err *V4ValidationError
		if !errors.As(err, &v4err) || v4err.Code != "v4.code-mode-removed" {
			t.Fatalf("expected coded v4.code-mode-removed error, got %v", err)
		}
	})

	t.Run("v4 included fragment", func(t *testing.T) {
		dir := t.TempDir()
		writeIncludeConfig(t, filepath.Join(dir, "fragment.yaml"), "codeMode:\n  enabled: true\n")
		root := filepath.Join(dir, "config.yaml")
		writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n")
		_, err := LoadV4ConfigFromPath(root)
		assertGuidance(t, err)
	})

	t.Run("v2 root", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		body := "apiVersion: agent-marketplace/v2\nharnesses: [claude]\ncodeMode:\n  enabled: true\n"
		writeIncludeConfig(t, path, body)
		_, err := LoadConfigFromPath(path)
		assertGuidance(t, err)
		after, rerr := os.ReadFile(path)
		if rerr != nil || string(after) != body {
			t.Fatalf("rejected root was modified:\n%s", after)
		}
	})

	t.Run("v3 root", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		writeIncludeConfig(t, path, "apiVersion: agent-marketplace/v3\nharnesses: [claude]\ncodeMode:\n  enabled: true\n")
		_, err := LoadConfigFromPath(path)
		assertGuidance(t, err)
	})
}
