package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubprocessEnvPreventsHostPathWrites(t *testing.T) {
	host := t.TempDir()
	hostPaths := map[string]string{
		"XDG_STATE_HOME":             filepath.Join(host, "state"),
		"XDG_CONFIG_HOME":            filepath.Join(host, "config"),
		"XDG_CACHE_HOME":             filepath.Join(host, "cache"),
		"CODEX_HOME":                 filepath.Join(host, "codex"),
		"AGENT_MARKETPLACE_LOG_FILE": filepath.Join(host, "logs", "host.log"),
	}
	for key, path := range hostPaths {
		t.Setenv(key, path)
	}
	t.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", "true")
	t.Setenv("AGENT_MARKETPLACE_LOG_LEVEL", "debug")
	t.Setenv("AGENT_MARKETPLACE_TELEMETRY_ENDPOINT", "file://"+filepath.Join(host, "telemetry"))
	t.Setenv("AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT", "not-a-duration")

	env := newEnv(t)
	result := env.RunAM("status")
	if result.ExitCode != 0 {
		t.Fatalf("isolated status failed (exit=%d):\n%s", result.ExitCode, result.Combined)
	}
	entries, err := os.ReadDir(host)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("subprocess wrote outside sandbox under sentinel host root %s: %v", host, entries)
	}

	overrides := map[string]string{
		"CODEX_HOME":                      filepath.Join(env.home, "fixture-codex"),
		"AGENT_MARKETPLACE_LOG_ENABLED":   "false",
		"AGENT_MARKETPLACE_V3_ORG_CONFIG": filepath.Join(env.etc, "fixture.yaml"),
		"AGENT_MARKETPLACE_DEVIN_PROBES":  "1",
	}
	got := subprocessEnvWithOverrides(env.subprocessEnv(), overrides)
	for key, want := range overrides {
		found := false
		for _, assignment := range got {
			if assignment == key+"="+want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("explicit fixture environment %s=%q was not preserved", key, want)
		}
	}
	if len(overrides) != 4 {
		t.Fatalf("fixture override map was mutated: %#v", overrides)
	}
}

func TestResolvePathRejectsTraversal(t *testing.T) {
	env := &Env{
		home:      "/sandbox/home",
		workspace: "/sandbox/workspace",
		repos:     "/sandbox/repos",
		etc:       "/sandbox/etc/agent-marketplace",
	}

	for _, tc := range []struct {
		path   string
		prefix string
	}{
		{path: "/root/../outside", prefix: "/root"},
		{path: "/workspace/subdir/../../outside", prefix: "/workspace"},
		{path: "/repos/../outside", prefix: "/repos"},
		{path: "/etc/agent-marketplace/../../outside", prefix: "/etc/agent-marketplace"},
	} {
		_, err := env.resolvePath(tc.path)
		if err == nil {
			t.Errorf("resolvePath(%q) returned nil error", tc.path)
			continue
		}
		if !strings.Contains(err.Error(), tc.prefix) {
			t.Errorf("resolvePath(%q) error = %v, want mapping context %q", tc.path, err, tc.prefix)
		}
	}
}
