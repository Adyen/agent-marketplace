package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
)

func hookReviewRegistry(t *testing.T, dir, command string) string {
	t.Helper()
	defPath := filepath.Join(dir, ".factory", "hooks", "hooks.json")
	writeFile(t, defPath, fmt.Sprintf(`{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":%q}]}]}`+"\n", command))
	return defPath
}

func hookReviewRepo(t *testing.T, registry string) (repoRoot, nativeFile string) {
	t.Helper()
	repoRoot = filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hooks:
    directory: `+registry+`
    source_harness: factory
resources:
  hooks: {include: [hook/PreToolUse-command]}
`)
	withWorkingDir(t, repoRoot)
	return repoRoot, filepath.Join(repoRoot, ".factory", "hooks.json")
}

func hookReviewFileCommand(t *testing.T, nativeFile string) string {
	t.Helper()
	data, err := os.ReadFile(nativeFile)
	if err != nil {
		t.Fatalf("read native hooks: %v", err)
	}
	var document map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode native hooks: %v\n%s", err, data)
	}
	groups := document["PreToolUse"]
	if len(document) != 1 || len(groups) != 1 || len(groups[0].Hooks) != 1 {
		t.Fatalf("native hooks = %#v, want one PreToolUse group with one handler", document)
	}
	return groups[0].Hooks[0].Command
}

// spec: hook-resources/changed-hook-commands-are-listed-with-exact-values
// spec: hook-resources/check-reports-pending-hook-changes-without-writing
// spec: hook-resources/unchanged-hooks-produce-no-review
// spec: hook-sync-diagnostics/hook-review-labels-additions-replacements-and-removals
func TestSyncPrintsHookChangesForReview(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, "config"))

	registry := t.TempDir()
	hookReviewRegistry(t, registry, "echo hook-review-v1-7b2c")
	repoRoot, nativeFile := hookReviewRepo(t, registry)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Hook changes") || !strings.Contains(output, "echo hook-review-v1-7b2c") || !strings.Contains(output, "factory (repository) add ") {
		t.Fatalf("initial sync output = %q, want a Hook changes section naming the added command", output)
	}
	if !strings.Contains(output, "materialize.hook.review") {
		t.Fatalf("initial sync output = %q, want the materialize.hook.review warning", output)
	}

	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("repeat sync: %v\n%s", err, output)
	}
	if strings.Contains(output, "Hook changes") || strings.Contains(output, "materialize.hook.review") {
		t.Fatalf("repeat sync output = %q, want no Hook changes section and no review warning", output)
	}

	if err := os.Remove(nativeFile); err != nil {
		t.Fatalf("delete native hooks: %v", err)
	}
	output, checkErr := executeCommand("sync", "--check")
	if checkErr == nil {
		t.Fatalf("sync --check succeeded with a pending hook change: %s", output)
	}
	if !strings.Contains(output, "Hook changes") || !strings.Contains(output, "echo hook-review-v1-7b2c") || !strings.Contains(output, "factory (repository) add ") {
		t.Fatalf("sync --check output = %q, want the pending addition naming the command", output)
	}
	if !strings.Contains(output, "materialize.hook.review-pending") {
		t.Fatalf("sync --check output = %q, want the materialize.hook.review-pending warning", output)
	}
	if _, err := os.Stat(nativeFile); !os.IsNotExist(err) {
		t.Fatalf("sync --check recreated the hook file: %v", err)
	}

	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("re-add sync: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Hook changes") || !strings.Contains(output, "echo hook-review-v1-7b2c") {
		t.Fatalf("re-add sync output = %q, want the added command listed", output)
	}
	if got := hookReviewFileCommand(t, nativeFile); got != "echo hook-review-v1-7b2c" {
		t.Fatalf("native hook command = %q, want the restored command", got)
	}

	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hooks:
    directory: `+registry+`
    source_harness: factory
`)
	stateRoot := filepath.Join(homeDir, "state")
	repoSnap := cliFilesystemSnapshot(t, repoRoot)
	stateSnap := cliFilesystemSnapshot(t, stateRoot)
	output, checkErr = executeCommand("sync", "--check")
	if checkErr == nil {
		t.Fatalf("sync --check succeeded with a pending hook removal: %s", output)
	}
	if !strings.Contains(output, "factory (repository) remove ") || !strings.Contains(output, "Deselecting hooks removes their owned entries and owned support files.") {
		t.Fatalf("sync --check output = %q, want the pending removal and its cleanup explanation", output)
	}
	assertCliFilesystemSnapshot(t, "sync --check repository", repoRoot, repoSnap)
	assertCliFilesystemSnapshot(t, "sync --check state", stateRoot, stateSnap)

	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("deselecting sync: %v\n%s", err, output)
	}
	if strings.Contains(output, "Deselecting hooks") {
		t.Fatalf("applied removal printed the pending-removal explanation:\n%s", output)
	}
	data, readErr := os.ReadFile(nativeFile)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read native hooks after deselection: %v", readErr)
	}
	if readErr == nil && strings.Contains(string(data), "echo hook-review-v1-7b2c") {
		t.Fatalf("native hook file still carries the deselected command:\n%s", data)
	}
}

// spec: hook-resources/changed-hook-commands-are-listed-with-exact-values
// spec: hook-resources/check-reports-pending-hook-changes-without-writing
// spec: hook-resources/unchanged-hooks-produce-no-review
// spec: hook-resources/codex-keeps-its-trust-review-codes
// spec: hook-sync-diagnostics/hook-review-labels-additions-replacements-and-removals
func TestSyncHookReviewClaudeUpdateAndCodexCodes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, "config"))
	t.Setenv("CODEX_HOME", filepath.Join(homeDir, "codex-home"))
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(homeDir, "nonexistent-org.yaml"))

	registry := t.TempDir()
	defPath := filepath.Join(registry, "hooks", "hooks.json")
	writeFile(t, defPath, `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo claude-review-old-6f1d"}]}]}`+"\n")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [claude, codex]
sources:
  hooks:
    directory: `+registry+`
    source_harness: claude
resources:
  hooks: {include: [hook/PreToolUse-command]}
`)
	withWorkingDir(t, repoRoot)
	claudeFile := filepath.Join(repoRoot, ".claude", "settings.json")
	codexFile := filepath.Join(repoRoot, ".codex", "hooks.json")
	stateRoot := filepath.Join(homeDir, "state")

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	for _, want := range []string{"Hook changes", `"echo claude-review-old-6f1d"`, "materialize.hook.review", "materialize.codex.trust-review"} {
		if !strings.Contains(output, want) {
			t.Fatalf("initial sync output = %q, missing %q", output, want)
		}
	}
	for _, nativeFile := range []string{claudeFile, codexFile} {
		data, err := os.ReadFile(nativeFile)
		if err != nil || !bytes.Contains(data, []byte("echo claude-review-old-6f1d")) {
			t.Fatalf("native file %s = %q, err %v, want the old command", nativeFile, data, err)
		}
	}

	writeFile(t, defPath, `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo claude-review-new-8c2b"}]}]}`+"\n")

	repoSnap := cliFilesystemSnapshot(t, repoRoot)
	stateSnap := cliFilesystemSnapshot(t, stateRoot)
	output, checkErr := executeCommand("sync", "--check")
	if checkErr == nil {
		t.Fatalf("sync --check succeeded with a pending hook change: %s", output)
	}
	for _, want := range []string{"Hook changes", `"echo claude-review-old-6f1d"`, `"echo claude-review-new-8c2b"`, "claude (repository) replace ", "codex (repository) replace ", "materialize.hook.review-pending", "materialize.codex.trust-review-pending"} {
		if !strings.Contains(output, want) {
			t.Fatalf("sync --check output = %q, missing %q", output, want)
		}
	}
	assertCliFilesystemSnapshot(t, "sync --check repository", repoRoot, repoSnap)
	assertCliFilesystemSnapshot(t, "sync --check state", stateRoot, stateSnap)
	t.Logf("sync --check stdout:\n%s", output)

	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("update sync: %v\n%s", err, output)
	}
	t.Logf("sync stdout:\n%s", output)
	for _, want := range []string{`"echo claude-review-old-6f1d"`, `"echo claude-review-new-8c2b"`, "claude (repository) replace ", "codex (repository) replace ", "materialize.hook.review", "materialize.codex.trust-review"} {
		if !strings.Contains(output, want) {
			t.Fatalf("update sync output = %q, missing %q", output, want)
		}
	}
	for _, nativeFile := range []string{claudeFile, codexFile} {
		data, err := os.ReadFile(nativeFile)
		if err != nil || !bytes.Contains(data, []byte("echo claude-review-new-8c2b")) || bytes.Contains(data, []byte("claude-review-old")) {
			t.Fatalf("native file %s = %q, err %v, want only the new command", nativeFile, data, err)
		}
	}

	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("repeat sync: %v\n%s", err, output)
	}
	if strings.Contains(output, "Hook changes") || strings.Contains(output, "hook.review") || strings.Contains(output, "trust-review") {
		t.Fatalf("repeat sync output = %q, want silence", output)
	}
}

// spec: hook-resources/changed-hook-commands-are-listed-with-exact-values
// spec: hook-resources/check-reports-pending-hook-changes-without-writing
// spec: hook-resources/unchanged-hooks-produce-no-review
func TestSyncHookReviewReportsLegacyRemoval(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(homeDir, "nonexistent-org.yaml"))

	registryDir := t.TempDir()
	writeFile(t, filepath.Join(registryDir, ".factory", "hooks", "hooks.json"),
		`{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo zqx-native-4d9c"}]}]}`+"\n")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hooks:
    directory: `+registryDir+`
    source_harness: factory
resources:
  hooks: {include: [hook/PreToolUse-command]}
`)
	withWorkingDir(t, repoRoot)
	nativeFile := filepath.Join(repoRoot, ".factory", "hooks.json")
	legacyFile := filepath.Join(repoRoot, ".factory", "hooks", "hooks.json")

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	nativeBefore, err := os.ReadFile(nativeFile)
	if err != nil || !bytes.Contains(nativeBefore, []byte("echo zqx-native-4d9c")) {
		t.Fatalf("native hooks file = %q, err %v, want the owned entry seeded", nativeBefore, err)
	}
	writeFile(t, legacyFile,
		`{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo zqx-legacy-old-4d9c","_managed_by":"agent-marketplace"},{"type":"command","command":"echo zqx-legacy-keep-4d9c"}]}],"unrelated":{"keep":true}}`+"\n")
	if err := os.Chmod(legacyFile, 0o640); err != nil {
		t.Fatal(err)
	}
	repoSnap := cliFilesystemSnapshot(t, repoRoot)
	stateSnap := cliFilesystemSnapshot(t, filepath.Join(homeDir, "state"))

	output, err = executeCommand("sync", "--check")
	if err == nil || !strings.Contains(output, "out of sync") {
		t.Fatalf("sync --check = %v\n%s, want an out-of-sync refusal", err, output)
	}
	t.Logf("sync --check stdout:\n%s", output)
	if !strings.Contains(output, "materialize.hook.review-pending") || !strings.Contains(output, `"echo zqx-legacy-old-4d9c"`) {
		t.Fatalf("sync --check output = %q, want the pending code and the exact removed command", output)
	}
	if strings.Contains(output, "zqx-legacy-keep-4d9c") {
		t.Fatalf("sync --check output = %q, the preserved handwritten handler must not be listed as removed", output)
	}
	assertCliFilesystemSnapshot(t, "sync --check", repoRoot, repoSnap)
	assertCliFilesystemSnapshot(t, "sync --check", filepath.Join(homeDir, "state"), stateSnap)

	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, output)
	}
	t.Logf("sync stdout:\n%s", output)
	if !strings.Contains(output, "materialize.hook.review") || !strings.Contains(output, `"echo zqx-legacy-old-4d9c"`) {
		t.Fatalf("sync output = %q, want the review code and the exact removed command", output)
	}
	if after, err := os.ReadFile(nativeFile); err != nil || !bytes.Equal(nativeBefore, after) {
		t.Fatalf("native hooks file changed during a legacy-only removal: %v", err)
	}
	legacyAfter, err := os.ReadFile(legacyFile)
	if err != nil || bytes.Contains(legacyAfter, []byte("zqx-legacy-old-4d9c")) || bytes.Contains(legacyAfter, []byte("_managed_by")) ||
		!bytes.Contains(legacyAfter, []byte("zqx-legacy-keep-4d9c")) || !bytes.Contains(legacyAfter, []byte(`"unrelated"`)) {
		t.Fatalf("legacy hooks file = %q, err %v, want the marked entry removed and everything else kept", legacyAfter, err)
	}

	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("repeat sync: %v\n%s", err, output)
	}
	if strings.Contains(output, "Hook changes") || strings.Contains(output, "hook.review") || strings.Contains(output, "zqx-legacy") {
		t.Fatalf("repeat sync output = %q, want silence", output)
	}
}

// spec: hook-resources/review-commands-never-reach-logs
func TestSyncHookReviewCommandsNeverReachLogs(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, "config"))
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(homeDir, "nonexistent-org.yaml"))

	const (
		commandPayload = "echo zqx-tlog-3e5a reviewed"
		promptPayload  = "zqx-tprompt-5b2d reviewed prompt body"
		legacyPayload  = "echo zqx-tlegacy-77dd removed"
		keptHandler    = "echo zqx-tkeep-77dd preserved"
	)
	payloads := []string{commandPayload, promptPayload, legacyPayload}

	logPath := filepath.Join(homeDir, "agent-marketplace.log")
	t.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", "true")
	t.Setenv("AGENT_MARKETPLACE_LOG_FILE", logPath)
	t.Setenv("AGENT_MARKETPLACE_LOG_LEVEL", "debug")
	t.Setenv("AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING", "false")

	capture := startOTLPCapture(t)

	if out, err := executeCommand("init", "--harnesses", "claude"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if out, err := executeCommand("telemetry", "enable", "--level", "detailed", "--endpoint", capture.URL); err != nil {
		t.Fatalf("telemetry enable: %v\n%s", err, out)
	}
	capture.reset()

	registryCmd := t.TempDir()
	writeFile(t, filepath.Join(registryCmd, ".factory", "hooks", "hooks.json"),
		fmt.Sprintf(`{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":%q}]}]}`+"\n", commandPayload))
	registryPrompt := t.TempDir()
	writeFile(t, filepath.Join(registryPrompt, "hooks", "hooks.json"),
		fmt.Sprintf(`{"UserPromptSubmit":[{"hooks":[{"type":"prompt","prompt":%q}]}]}`+"\n", promptPayload))

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [claude]
sources:
  commands:
    directory: `+registryCmd+`
    source_harness: factory
  prompts:
    directory: `+registryPrompt+`
    source_harness: claude
resources:
  commands: {include: [hook/PreToolUse-command]}
  prompts: {include: [hook/UserPromptSubmit-prompt]}
`)
	withWorkingDir(t, repoRoot)
	nativeFile := filepath.Join(repoRoot, ".claude", "settings.json")
	legacyFile := filepath.Join(repoRoot, ".claude", "hooks", "hooks.json")
	writeFile(t, legacyFile,
		fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":%q,"_managed_by":"agent-marketplace"},{"type":"command","command":%q}]}]}}`+"\n", legacyPayload, keptHandler))

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, output)
	}
	for _, payload := range payloads {
		if !strings.Contains(output, payload) {
			t.Fatalf("sync output = %q, want the review listing containing %q", output, payload)
		}
	}
	nativeData, err := os.ReadFile(nativeFile)
	if err != nil || !bytes.Contains(nativeData, []byte(commandPayload)) || !bytes.Contains(nativeData, []byte(promptPayload)) {
		t.Fatalf("native claude settings = %q, err %v, want both hook payloads", nativeData, err)
	}
	legacyData, err := os.ReadFile(legacyFile)
	if err != nil || bytes.Contains(legacyData, []byte(legacyPayload)) || bytes.Contains(legacyData, []byte("_managed_by")) || !bytes.Contains(legacyData, []byte(keptHandler)) {
		t.Fatalf("legacy hooks file = %q, err %v, want the marked entry removed and the handwritten handler kept", legacyData, err)
	}

	assertLogRecordsWithout(t, logPath, "pipeline sync end", payloads)
	capture.assertExportedWithout(t, "agent-marketplace sync", filepath.Join(homeDir, "state", "agent-marketplace", "telemetry"), payloads)
}

// spec: hook-resources/changed-hook-commands-are-listed-with-exact-values
func TestWriteHookChangesRendering(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	var buf bytes.Buffer
	if err := writeHookChanges(&buf, &materialize.Report{}, false); err != nil || buf.Len() != 0 {
		t.Fatalf("empty report rendered %q err %v", buf.String(), err)
	}

	report := &materialize.Report{HookChanges: []materialize.HookChange{
		{
			Target:  "factory",
			File:    "/repo/.factory/hooks.json",
			Event:   "PreToolUse",
			Matcher: "Execute",
			Before:  json.RawMessage(`{"matcher":"Execute","hooks":[{"type":"command","command":"echo old value"}]}`),
			After:   json.RawMessage(`{"matcher":"Execute","hooks":[{"type":"command","command":"echo new\nline"}]}`),
		},
		{
			Target: "claude",
			File:   "/repo/.claude/settings.json",
			Event:  "UserPromptSubmit",
			After:  json.RawMessage(`{"hooks":[{"type":"prompt","prompt":"zqx prompt \"quoted\" body"}]}`),
		},
		{
			Target: "windsurf",
			File:   "/repo/.windsurf/hooks.json",
			Event:  "pre_run_command",
			After:  json.RawMessage(`{"hooks":[{"type":"agent","other":"x"}]}`),
		},
		{
			Target:  "factory",
			File:    "/repo/.factory/hooks.json",
			Event:   "PreToolUse",
			Matcher: "Execute",
			After:   json.RawMessage(`{"matcher":"Execute","hooks":[{"type":"command","command":"echo zzz last"}]}`),
		},
	}}
	if err := writeHookChanges(&buf, report, false); err != nil {
		t.Fatalf("writeHookChanges: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Hook changes:",
		"factory", "repository", "/repo/.factory/hooks.json", "PreToolUse",
		`command: "echo old value"`, `command: "echo new\nline"`,
		`matcher "Execute"`,
		"claude", "UserPromptSubmit", `prompt: "zqx prompt \"quoted\" body"`,
		"windsurf", `{"other":"x","type":"agent"}`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered review missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "echo new\nline\n") {
		t.Fatalf("rendered review leaked a literal newline inside the command:\n%s", out)
	}

	reversed := &materialize.Report{HookChanges: []materialize.HookChange{
		report.HookChanges[3], report.HookChanges[2], report.HookChanges[1], report.HookChanges[0],
	}}
	var bufRev bytes.Buffer
	if err := writeHookChanges(&bufRev, reversed, false); err != nil {
		t.Fatalf("writeHookChanges reversed: %v", err)
	}
	if bufRev.String() != out {
		t.Fatalf("reversed input rendered differently:\n--- first ---\n%s\n--- reversed ---\n%s", out, bufRev.String())
	}

	buf.Reset()
	if err := writeHookChanges(&buf, report, true); err != nil {
		t.Fatalf("writeHookChanges pending: %v", err)
	}
	if !strings.Contains(buf.String(), "pending") {
		t.Fatalf("pending review did not say so:\n%s", buf.String())
	}

	sentinel := errors.New("sink refused")
	failing := &failWriter{err: sentinel}
	if err := writeHookChanges(failing, report, false); !errors.Is(err, sentinel) {
		t.Fatalf("writeHookChanges writer error = %v, want errors.Is(..., sentinel)", err)
	}
}

type failWriter struct{ err error }

func (w *failWriter) Write([]byte) (int, error) { return 0, w.err }
