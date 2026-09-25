package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A repository selects from a relative in-repository directory and a
// personal-local root was written by v3.1.1 itself. Migrating the user root
// must leave that local root working, and migrating every layer must keep
// the same installation editable in both the repository and local scopes.
func TestPreV4LocalLayerUpgrade(t *testing.T) {
	if os.Getenv("AM_E2E_PRE_V4_BINARY") == "" {
		t.Skip("historical executable is supplied by the dedicated upgrade gate")
	}
	historical, arch := verifiedExecutable(t, "AM_E2E_PRE_V4_BINARY", preV4ReleaseArch)
	world := newHistoricalWorld(t, upgradeImage(t))
	candidate := world.buildCandidate(arch, "e2e")
	home, workspace := world.path("home"), world.path("workspace")
	registry := filepath.Join(workspace, "vendor", "registry")
	bodies := map[string]string{"review": "Review body", "second": "Second body"}
	for name, body := range bodies {
		path := filepath.Join(registry, "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(skillDocument(name, body)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{home, filepath.Join(workspace, ".agents")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	repoConfig := filepath.Join(workspace, ".agents", "config.yaml")
	localConfig := filepath.Join(workspace, ".agents", "config.local.yaml")
	if err := os.WriteFile(repoConfig, []byte("apiVersion: agent-marketplace/v2\nregistries:\n  - path: ./vendor/registry\n    skills:\n      include: [review]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	must := func(executable string, args ...string) {
		t.Helper()
		world.must(home, workspace, executable, args...)
	}
	installed := func(name string) bool {
		t.Helper()
		got, err := os.ReadFile(filepath.Join(workspace, ".factory", "skills", name, "SKILL.md"))
		if err == nil && string(got) != skillDocument(name, bodies[name]) {
			t.Fatalf("%s installed with unexpected content %q", name, got)
		}
		return err == nil
	}
	expect := func(stage string, review, second bool) {
		t.Helper()
		if got := installed("review"); got != review {
			t.Fatalf("%s: review installed=%v, want %v", stage, got, review)
		}
		if got := installed("second"); got != second {
			t.Fatalf("%s: second installed=%v, want %v", stage, got, second)
		}
	}
	readConfig := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	apiVersion := func(data []byte) string {
		for _, line := range strings.Split(string(data), "\n") {
			if value, ok := strings.CutPrefix(line, "apiVersion: "); ok {
				return value
			}
		}
		return "(none)"
	}

	must(historical, "init", "--harnesses", "factory")
	must(historical, "install", "second", "--scope", "local")
	must(historical, "sync")
	expect("v3.1.1", true, true)
	historicalLocal, err := os.ReadFile(localConfig)
	if err != nil {
		t.Fatalf("v3.1.1 did not write a personal-local root: %v", err)
	}

	must(candidate, "migrate", "--scope", "user")
	if got, err := os.ReadFile(localConfig); err != nil || string(got) != string(historicalLocal) {
		t.Fatalf("user migration changed the personal-local root: %q, %v", got, err)
	}
	must(candidate, "sync")
	expect("user migrated, local legacy", true, true)
	must(candidate, "remove", "second", "--scope", "local")
	expect("legacy local removal", true, false)
	must(candidate, "install", "second", "--scope", "local")
	expect("legacy local reinstall", true, true)
	must(candidate, "sync", "--check")

	must(candidate, "migrate", "--scope", "all")
	for _, path := range []string{repoConfig, localConfig} {
		data := readConfig(path)
		if apiVersion(data) != "agent-marketplace/v4" || !strings.Contains(string(data), "directory: ./vendor/registry") {
			t.Fatalf("migrate --scope all did not convert %s keeping its relative source:\n%s", path, data)
		}
	}
	must(candidate, "sync")
	expect("all migrated", true, true)
	must(candidate, "sync", "--check")
	must(candidate, "remove", "second", "--scope", "local")
	expect("migrated local removal", true, false)
	must(candidate, "install", "second", "--scope", "local")
	expect("migrated local reinstall", true, true)
	must(candidate, "remove", "review", "--scope", "repo")
	expect("migrated repository removal", false, true)
	must(candidate, "install", "review", "--scope", "repo")
	expect("migrated repository reinstall", true, true)
	must(candidate, "sync", "--check")

	settled := map[string][]byte{}
	for _, path := range []string{repoConfig, localConfig} {
		settled[path] = readConfig(path)
	}
	must(candidate, "migrate", "--scope", "all")
	for path, want := range settled {
		if got, err := os.ReadFile(path); err != nil || string(got) != string(want) {
			t.Fatalf("repeated migrate --scope all rewrote %s:\nbefore:\n%s\nafter:\n%s", path, want, got)
		}
	}
}
