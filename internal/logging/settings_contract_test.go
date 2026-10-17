package logging

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: logging/default-and-overridden-logging-settings
func TestLoggingSettingsContract(t *testing.T) {
	clearLoggingEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if !resolveEnabled(Options{}) || !resolveCompress(Options{}) || resolveLevel(Options{}) != slog.LevelInfo {
		t.Fatal("default logging must be enabled, compressed, and info level")
	}
	if got := resolvePath(""); got != filepath.Join(home, ".agent-marketplace", "logs", "agent-marketplace.log") {
		t.Fatalf("default log path = %q", got)
	}
	for _, tc := range []struct {
		env       string
		fallback  int
		want      int
		allowZero bool
	}{
		{"AGENT_MARKETPLACE_LOG_MAX_SIZE_MB", config.DefaultLogMaxSizeMB, 10, false},
		{"AGENT_MARKETPLACE_LOG_MAX_BACKUPS", config.DefaultLogMaxBackups, 5, true},
		{"AGENT_MARKETPLACE_LOG_MAX_AGE_DAYS", config.DefaultLogMaxAgeDays, 30, true},
	} {
		t.Run(tc.env, func(t *testing.T) {
			if got := resolveInt(tc.env, nil, tc.fallback, tc.allowZero); got != tc.want {
				t.Fatalf("default = %d, want %d", got, tc.want)
			}
			configured := 7
			for _, raw := range []string{"bad", "-1"} {
				t.Setenv(tc.env, raw)
				if got := resolveInt(tc.env, &configured, tc.fallback, tc.allowZero); got != 7 {
					t.Fatalf("invalid override %q resolved to %d, want 7", raw, got)
				}
			}
			t.Setenv(tc.env, "9")
			if got := resolveInt(tc.env, &configured, tc.fallback, tc.allowZero); got != 9 {
				t.Fatalf("environment override = %d, want 9", got)
			}
			t.Setenv(tc.env, "0")
			want := 7
			if tc.allowZero {
				want = 0
			}
			if got := resolveInt(tc.env, &configured, tc.fallback, tc.allowZero); got != want {
				t.Fatalf("zero override = %d, want %d", got, want)
			}
		})
	}
	for _, tc := range []struct {
		raw        string
		configured bool
		want       bool
	}{
		{"1", false, true},
		{"TRUE", false, true},
		{"Yes", false, true},
		{"oN", false, true},
		{"0", true, false},
		{"FALSE", true, false},
		{"No", true, false},
		{"oFf", true, false},
		{"invalid", true, true},
	} {
		t.Run("boolean="+tc.raw, func(t *testing.T) {
			t.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", tc.raw)
			t.Setenv("AGENT_MARKETPLACE_LOG_COMPRESS", tc.raw)
			if resolveEnabled(Options{Enabled: &tc.configured}) != tc.want || resolveCompress(Options{Compress: &tc.configured}) != tc.want {
				t.Fatalf("boolean %q did not resolve to %t", tc.raw, tc.want)
			}
		})
	}
	t.Setenv("AGENT_MARKETPLACE_LOG_LEVEL", "unknown")
	if resolveLevel(Options{Level: "error"}) != slog.LevelInfo {
		t.Fatal("unknown environment level must fall back to info")
	}
	t.Setenv("AGENT_MARKETPLACE_LOG_LEVEL", "warning")
	if resolveLevel(Options{Level: "error"}) != slog.LevelWarn {
		t.Fatal("environment warning level must override configured error")
	}
	t.Setenv("AM_TEST_LOG_NAME", "expanded.log")
	if got := resolvePath("~/$AM_TEST_LOG_NAME"); got != filepath.Join(home, "expanded.log") {
		t.Fatalf("expanded log path = %q", got)
	}
}
