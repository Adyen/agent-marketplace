package e2e_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

const targetDryRunProfileYAML = `apiVersion: agent-marketplace.io/target-profile/v1
id: invented-app
label: Invented App
platforms: [macos, linux]
roots: {user: {override: INVENTED_APP_HOME, default: {macos: {base: home, path: .config/invented-app}, linux: {base: home, path: .config/invented-app}}, allowed_bases: [home]}}
kinds: {mod: {deliver: place, into: [mods/]}}
`

// spec: target-profile-import/dry-run-leaves-all-existing-state-unchanged
// spec: target-profile-import/local-dry-run-needs-no-configuration-approval-acquisition-or-host-execution
func TestTargetDryRunUsesOnlyLocalInputs(t *testing.T) {
	env := newEnv(t)
	destination := filepath.Join(env.home, ".config", "invented-app")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	destination, err := filepath.EvalSymlinks(destination)
	if err != nil {
		t.Fatal(err)
	}
	inputRoot := filepath.Join(env.workspace, "target-import")
	source := filepath.Join(inputRoot, "source")
	if err := os.MkdirAll(filepath.Join(source, "mods", "sample-links"), 0o755); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(inputRoot, "profile.yaml")
	importPath := filepath.Join(inputRoot, "import.yaml")
	refusedImportPath := filepath.Join(inputRoot, "refused.yaml")
	hostCounter := filepath.Join(inputRoot, "host-called")
	hostProbe := filepath.Join(inputRoot, "host-probe")
	if err := os.WriteFile(hostProbe, []byte("#!/bin/sh\nprintf called >> \"$TARGET_DRY_RUN_HOST_COUNTER\"\nprintf '1.2.3\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := strings.Replace(targetDryRunProfileYAML, "kinds:", fmt.Sprintf(
		"host: {executable: %s, version_probe: {argv: [--version], pattern: '(\\\\d+\\\\.\\\\d+\\\\.\\\\d+)'}, verified: [{range: '>=1.0.0 <2.0.0', date: 2026-11-12, evidence: fixture}]}\nkinds:",
		strconv.Quote(hostProbe),
	), 1)
	if err := os.WriteFile(profilePath, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer registry.Close()
	manifest := fmt.Sprintf(`apiVersion: agent-marketplace.io/target-import/v1
target_id: invented-app
kind: invented-app/mod
name: sample-links
mappings:
  - source_path: mods/sample-links/main.js
    destination_path: mods/sample-links/main.js
  - source_path: mods/sample-links/mod.json
    destination_path: mods/sample-links/mod.json
metadata:
  requires:
    - {kind: invented-app/mod, name: sample-tabs, registry: %s}
  manual_setup: [Bind the test key in settings.]
`, strconv.Quote(registry.URL))
	if err := os.WriteFile(importPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refusedImportPath, []byte(strings.Replace(manifest,
		"agent-marketplace.io/target-import/v1", "agent-marketplace.io/target-import/v2", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "mods", "sample-links", "main.js"),
		[]byte("#!/bin/sh\nprintf package >> \"$TARGET_DRY_RUN_HOST_COUNTER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "mods", "sample-links", "mod.json"),
		[]byte("{\"name\":\"sample-links\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	globalConfig := filepath.Join(env.home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(globalConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(env.home, "logs", "target-dry-run.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	const logSentinel = "existing log bytes\n"
	if err := os.WriteFile(logPath, []byte(logSentinel), 0o640); err != nil {
		t.Fatal(err)
	}
	logInfo, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  configured: {git: https://example.com/configured.git, ref: main}
resources:
  configured: {all: true}
logging: {level: debug, file: %q}
`, logPath)
	if err := os.WriteFile(globalConfig, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	acquisitionLockSentinel := filepath.Join(env.home, ".cache", "agent-marketplace", "exact-git", "locks", "namespace.lock")
	if err := os.MkdirAll(acquisitionLockSentinel, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSnapshot := func() map[string]string {
		t.Helper()
		out := make(map[string]string, 4)
		for name, path := range map[string]string{
			"home": env.home, "workspace": env.workspace, "registry": env.repos, "organization": env.etc,
		} {
			fingerprint, err := pathFingerprint(path)
			if err != nil {
				t.Fatal(err)
			}
			out[name] = fingerprint
		}
		return out
	}
	assertUnchanged := func(before map[string]string) {
		t.Helper()
		if after := writeSnapshot(); !maps.Equal(before, after) {
			t.Fatalf("dry-run changed local inputs or application state: before=%v after=%v", before, after)
		}
		logBytes, err := os.ReadFile(logPath)
		if err != nil || string(logBytes) != logSentinel {
			t.Fatalf("dry-run changed or removed sentinel log: bytes=%q err=%v", logBytes, err)
		}
		afterInfo, err := os.Stat(logPath)
		if err != nil {
			t.Fatalf("stat sentinel log after dry-run: %v", err)
		}
		if afterInfo.Mode() != logInfo.Mode() || !afterInfo.ModTime().Equal(logInfo.ModTime()) {
			t.Fatalf("dry-run touched sentinel log: mode=%v mtime=%v err=%v", afterInfo.Mode(), afterInfo.ModTime(), err)
		}
	}
	run := func(manifestPath string, withDestination bool) ExecResult {
		t.Helper()
		args := []string{
			"target", "dry-run", "--profile", profilePath,
			"--source", source, "--import", manifestPath,
		}
		if withDestination {
			args = append(args, "--destination", destination)
		}
		return env.RunAMWithEnv(map[string]string{
			"AGENT_MARKETPLACE_LOG_ENABLED": "true",
			"AGENT_MARKETPLACE_LOG_FILE":    logPath,
			"AGENT_MARKETPLACE_LOG_LEVEL":   "debug",
			"INVENTED_APP_HOME":             destination,
			"TARGET_DRY_RUN_HOST_COUNTER":   hostCounter,
		}, args...)
	}
	decodeStatus := func(stdout string) string {
		t.Helper()
		var report struct {
			SchemaVersion int    `json:"schema_version"`
			Status        string `json:"status"`
		}
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("decode dry-run report: %v\nstdout:\n%s", err, stdout)
		}
		if report.SchemaVersion != 1 {
			t.Fatalf("report schema_version = %d", report.SchemaVersion)
		}
		return report.Status
	}

	for _, withDestination := range []bool{false, true} {
		before := writeSnapshot()
		result := run(importPath, withDestination)
		if result.ExitCode != 0 || decodeStatus(result.Stdout) != "valid" {
			t.Fatalf("valid dry-run with destination=%t exited %d:\n%s", withDestination, result.ExitCode, result.Combined)
		}
		assertUnchanged(before)
	}
	before := writeSnapshot()
	result := run(refusedImportPath, true)
	if result.ExitCode == 0 || decodeStatus(result.Stdout) != "refused" {
		t.Fatalf("refused dry-run exited %d:\n%s", result.ExitCode, result.Combined)
	}
	assertUnchanged(before)
	if requests.Load() != 0 {
		t.Fatalf("dry-run made %d prerequisite registry requests", requests.Load())
	}
	if _, err := os.Stat(hostCounter); !os.IsNotExist(err) {
		t.Fatalf("dry-run ran host or package code; counter stat error = %v", err)
	}
	for _, path := range []string{
		filepath.Join(env.home, ".agents", "lock.yaml"),
		filepath.Join(env.home, ".local", "state", "agent-marketplace"),
		filepath.Join(env.etc, "config.yaml"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("dry-run created application state %q: %v", path, err)
		}
	}
}
