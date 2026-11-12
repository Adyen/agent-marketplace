package main

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// spec: hook-sync-diagnostics/hook-errors-retain-target-and-source
// spec: hook-sync-diagnostics/matcher-refusal-identifies-its-translation-stage
func TestSyncHookMatcherRefusalNamesTargetAndStage(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name        string
		matcher     string
		wantMessage string
		wantHint    string
	}{
		{
			name:        "undecodable source matcher",
			matcher:     `^(mcp_call_tool|mcp__blackboard__.*)$`,
			wantMessage: "The source matcher cannot be decoded for cross-harness translation; hook suppressed",
			wantHint:    "Use exact source tool names or a supported MCP pattern, or author a target-specific hook",
		},
		{
			name:        "decoded matcher the target cannot cover",
			matcher:     `^mcp_call_tool$`,
			wantMessage: "The target tool catalog cannot exactly cover the source matcher; hook suppressed",
			wantHint:    "Author a target-specific hook or explicitly narrow the selected harnesses; preview cleanup with agent-marketplace sync --check",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, "config"))
			config.SetTestV3OrganizationConfigPath(t, filepath.Join(homeDir, "nonexistent-org.yaml"))
			logPath := filepath.Join(homeDir, "agent-marketplace.log")
			t.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", "true")
			t.Setenv("AGENT_MARKETPLACE_LOG_FILE", logPath)
			t.Setenv("AGENT_MARKETPLACE_LOG_LEVEL", "debug")
			t.Setenv("AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING", "false")
			capture := startOTLPCapture(t)
			writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")
			if out, err := executeCommand("telemetry", "enable", "--level", "detailed", "--endpoint", capture.URL); err != nil {
				t.Fatalf("telemetry enable: %v\n%s", err, out)
			}

			// Two handlers in the refused group ingest as two hook resources,
			// so each refused target has two distinct refusals to render.
			registry := t.TempDir()
			writeFile(t, filepath.Join(registry, ".devin", "hooks.v1.json"), fmt.Sprintf(`{"PreToolUse":[{"matcher":%q,"hooks":[{"type":"command","command":"echo guard-check-5d1a"},{"type":"command","command":"echo guard-check-second-7c2e"}]}]}`+"\n", tt.matcher))
			payloads := []string{tt.matcher, "mcp_call_tool", "blackboard", "guard-check-5d1a", "guard-check-second-7c2e"}

			repoRoot := filepath.Join(t.TempDir(), "repo")
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [devin, factory, cursor]
sources:
  hooks:
    directory: `+registry+`
    source_harness: devin
resources:
  hooks: {include: [hook/PreToolUse-command, hook/PreToolUse-command-2]}
`)
			withWorkingDir(t, repoRoot)

			capture.reset()
			output, err := executeCommand("sync")
			if err == nil {
				t.Fatalf("sync succeeded with a suppressed factory hook:\n%s", output)
			}
			assertMatcherRefusalDiagnostics(t, "sync", err, output, payloads, tt.wantMessage, tt.wantHint)
			devinHooks, readErr := os.ReadFile(filepath.Join(repoRoot, ".devin", "hooks.v1.json"))
			if readErr != nil || !strings.Contains(string(devinHooks), "echo guard-check-5d1a") || !strings.Contains(string(devinHooks), "echo guard-check-second-7c2e") {
				t.Fatalf("same-harness devin hooks = %q, err %v; want both handlers installed despite the cross-harness refusal", devinHooks, readErr)
			}
			assertLogRecordsWithout(t, logPath, "hook.matcher-untranslatable", payloads)
			capture.assertExportedWithout(t, "agent-marketplace sync", filepath.Join(homeDir, "state", "agent-marketplace", "telemetry"), payloads)

			stateRoot := filepath.Join(homeDir, "state")
			repoSnap := cliFilesystemSnapshot(t, repoRoot)
			registrySnap := cliFilesystemSnapshot(t, registry)
			stateSnap := cliFilesystemSnapshot(t, stateRoot)
			checkOutput, checkErr := executeCommand("sync", "--check")
			if checkErr == nil {
				t.Fatalf("sync --check succeeded with a suppressed factory hook:\n%s", checkOutput)
			}
			assertMatcherRefusalDiagnostics(t, "sync --check", checkErr, checkOutput, payloads, tt.wantMessage, tt.wantHint)
			assertCliFilesystemSnapshot(t, "sync --check repository", repoRoot, repoSnap)
			assertCliFilesystemSnapshot(t, "sync --check registry", registry, registrySnap)
			assertCliFilesystemSnapshot(t, "sync --check state", stateRoot, stateSnap)
		})
	}
}

// spec: command-error-output/owned-aggregate-diagnostics-print-their-code-guidance
func TestSyncOwnedAggregateDiagnosticsPrintCodeGuidance(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	registry := t.TempDir()
	writeFile(t, filepath.Join(registry, ".devin", "hooks.v1.json"),
		`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo source"}]}]}`)
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [devin]
sources:
  local:
    directory: `+registry+`
    source_harness: devin
resources:
  local:
    include: [hook/PreToolUse-command]
`)
	aggregateFile := filepath.Join(repo, ".devin", "hooks.v1.json")
	writeFile(t, aggregateFile,
		`{"PostToolUse":[{"command":"echo mine","type":"command"}]}`)
	withWorkingDir(t, repo)
	output, err := executeCommand("sync")
	if err == nil {
		t.Fatalf("sync succeeded despite the unpatchable Devin aggregate:\n%s", output)
	}
	ownershipDoc, ok := diag.DocForCode(diag.CodeOwnershipStateMissing)
	if !ok || ownershipDoc.Hint == "" {
		t.Fatalf("DocForCode(%q) has no hint", diag.CodeOwnershipStateMissing)
	}
	aggregateDoc, ok := diag.DocForCode(diag.CodeMaterializeDevinAggregateShape)
	if !ok || aggregateDoc.Hint == "" {
		t.Fatalf("DocForCode(%q) has no hint", diag.CodeMaterializeDevinAggregateShape)
	}
	for _, code := range []string{diag.CodeOwnershipStateMissing, diag.CodeMaterializeDevinAggregateShape} {
		if !strings.Contains(output, code) {
			t.Errorf("sync output lacks %s:\n%s", code, output)
		}
	}
	if !strings.Contains(output, "\n  → "+ownershipDoc.Hint+"\n") {
		t.Errorf("sync warning lacks ownership guidance on its remediation line %q:\n%s", ownershipDoc.Hint, output)
	}
	if strings.Contains(output, "\n  → "+aggregateDoc.Hint+"\n") {
		t.Errorf("sync error duplicated aggregate guidance on a remediation line:\n%s", output)
	}
	aggregateHint := "hint: " + expectedDevinEntryHint(aggregateFile, "PostToolUse", 0)
	if count := strings.Count(output, aggregateHint); count != 1 {
		t.Errorf("sync error hint count = %d, want one %q:\n%s", count, aggregateHint, output)
	}
	if hasEmptyRemediationLine(output) || strings.Contains(output, "Run `agent-marketplace sync --check`") {
		t.Fatalf("sync used generic guidance or printed an empty remediation line:\n%s", output)
	}
}

// spec: command-error-output/a-repository-devin-hook-entry-refusal-names-the-entry-to-rewrite
// spec: command-error-output/a-user-devin-hook-entry-refusal-names-the-entry-to-rewrite
// spec: command-error-output/a-devin-aggregate-root-or-member-refusal-keeps-root-guidance
func TestSyncDevinAggregateShapeRefusalHints(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	catalogDoc, ok := diag.DocForCode(diag.CodeMaterializeDevinAggregateShape)
	if !ok || catalogDoc.Hint == "" {
		t.Fatalf("DocForCode(%q) has no hint", diag.CodeMaterializeDevinAggregateShape)
	}
	assertRefusal := func(t *testing.T, output, path string, before []byte, beforeInfo os.FileInfo, wantHint, forbidden string) {
		t.Helper()
		if !strings.Contains(output, diag.CodeMaterializeDevinAggregateShape) {
			t.Fatalf("sync output lacks %s:\n%s", diag.CodeMaterializeDevinAggregateShape, output)
		}
		var hints []string
		for _, line := range strings.Split(output, "\n") {
			if strings.HasPrefix(line, "hint: ") {
				hints = append(hints, line)
			}
		}
		if len(hints) != 1 || hints[0] != "hint: "+wantHint {
			t.Errorf("sync hint lines = %q, want exactly %q", hints, "hint: "+wantHint)
		}
		if forbidden != "" && strings.Contains(output, forbidden) {
			t.Errorf("sync output contains forbidden guidance %q:\n%s", forbidden, output)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, before) {
			t.Errorf("%s bytes changed: err=%v before=%q after=%q", path, err, before, after)
		}
		afterInfo, err := os.Stat(path)
		if err != nil {
			t.Errorf("stat %s after sync: %v", path, err)
			return
		}
		if afterInfo.Mode().Perm() != beforeInfo.Mode().Perm() || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
			t.Errorf("%s metadata changed: mode=%#o mtime=%s, want mode=%#o mtime=%s",
				path, afterInfo.Mode().Perm(), afterInfo.ModTime(), beforeInfo.Mode().Perm(), beforeInfo.ModTime())
		}
	}
	for _, tc := range []struct {
		name      string
		userScope bool
		rootShape bool
		contents  string
	}{
		{
			name:     "repository entry",
			contents: `{"PostToolUse":[{"command":"echo mine","type":"command"}]}`,
		},
		{
			name:      "user entry",
			userScope: true,
			contents:  `{"hooks":{"PostToolUse":[{"command":"echo mine","type":"command"}]}}`,
		},
		{
			name:      "repository root",
			rootShape: true,
			contents:  `[]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			config.SetTestV3OrganizationConfigPath(t, filepath.Join(home, "nonexistent-org.yaml"))
			registry := t.TempDir()
			writeFile(t, filepath.Join(registry, ".devin", "hooks.v1.json"),
				`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo source"}]}]}`)
			repo := filepath.Join(t.TempDir(), "repo")
			if err := os.MkdirAll(repo, 0o755); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(repo, ".devin", "hooks.v1.json")
			configPath := filepath.Join(repo, ".agents", "config.yaml")
			source := "local"
			if tc.userScope {
				configPath = filepath.Join(home, ".agents", "config.yaml")
				source = "user"
				file = filepath.Join(home, ".config", "devin", "config.json")
			}
			writeConfig(t, configPath, fmt.Sprintf(`
apiVersion: agent-marketplace/v4
harnesses: [devin]
sources:
  %s:
    directory: %s
    source_harness: devin
resources:
  %s:
    include: [hook/PreToolUse-command]
`, source, registry, source))
			writeFile(t, file, tc.contents)
			if err := os.Chmod(file, 0o640); err != nil {
				t.Fatal(err)
			}
			fixedTime := time.Date(2020, time.January, 2, 3, 4, 5, 0, time.UTC)
			if err := os.Chtimes(file, fixedTime, fixedTime); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			beforeInfo, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			withWorkingDir(t, repo)
			output, err := executeCommand("sync")
			if err == nil {
				t.Fatalf("sync succeeded despite the invalid Devin aggregate:\n%s", output)
			}
			wantHint := catalogDoc.Hint
			forbidden := "Rewrite entry"
			if !tc.rootShape {
				wantHint = expectedDevinEntryHint(file, "PostToolUse", 0)
				forbidden = catalogDoc.Hint
			}
			assertRefusal(t, output, file, before, beforeInfo, wantHint, forbidden)
		})
	}
}

func expectedDevinEntryHint(file, event string, index int) string {
	return fmt.Sprintf(
		`Rewrite entry %d of event %q in %s in the matcher-groups shape {"matcher": "<optional tool pattern>", "hooks": [{"type": "command", "command": "<command>"}]}, or remove that entry if you no longer want it, then run `+"`agent-marketplace sync`"+` again`,
		index, event, file,
	)
}

// assertMatcherRefusalDiagnostics inspects the whole rendered result: the
// returned error and the command output, including hints and summaries,
// not only lines that carry the diagnostic code. Executable payloads may
// appear only in the hook review, and only in rows for the same-harness
// devin target that installed them. Across the error value and the output,
// each refused target names each refused resource exactly once.
func assertMatcherRefusalDiagnostics(t *testing.T, command string, err error, output string, payloads []string, message, hint string) {
	t.Helper()
	echo := "Error: " + err.Error()
	cut := strings.LastIndex(output, echo)
	if cut < 0 || strings.TrimSpace(output[cut+len(echo):]) != "" {
		t.Fatalf("%s output does not end with the echoed error %q:\n%s", command, echo, output)
	}
	report, review := splitHookReview(output[:cut])
	reviewed := 0
	for _, line := range review {
		if strings.HasPrefix(line, "    ") {
			continue
		}
		if !strings.HasPrefix(line, "  devin (repository) ") {
			t.Fatalf("%s hook review lists a target other than devin: %q", command, line)
		}
		reviewed++
	}
	if command == "sync" && (reviewed == 0 || !strings.Contains(strings.Join(review, "\n"), "guard-check-5d1a")) {
		t.Fatalf("%s hook review = %q; want the installed devin payload as a non-vacuity control", command, review)
	}
	for _, rendered := range []struct{ name, text string }{{"error", err.Error()}, {"output", report}} {
		for _, payload := range payloads {
			if strings.Contains(rendered.text, payload) {
				t.Fatalf("%s %s outside the hook review quotes %q:\n%s", command, rendered.name, payload, rendered.text)
			}
		}
	}
	rendered := err.Error() + "\n" + report
	for _, target := range []string{"factory", "cursor", "devin"} {
		want := map[string]int{`resource "PreToolUse-command"`: 1, `resource "PreToolUse-command-2"`: 1}
		if target == "devin" {
			want = map[string]int{}
		}
		got := map[string]int{}
		for _, line := range strings.Split(rendered, "\n") {
			if !strings.Contains(line, "hook.matcher-untranslatable:") || !strings.Contains(line, "(target "+target+",") {
				continue
			}
			resource := refusalResource.FindString(line)
			if resource == "" || !strings.Contains(line, ".devin/hooks.v1.json") || !strings.Contains(line, message) {
				t.Fatalf("%s refusal for %s lacks its resource, source, or %q:\n%s", command, target, message, line)
			}
			got[resource]++
		}
		if !maps.Equal(got, want) {
			t.Fatalf("%s refusal lines for target %s = %v, want %v:\n%s", command, target, got, want, rendered)
		}
	}
	if !strings.Contains(rendered, hint) {
		t.Fatalf("%s missing remediation %q:\n%s", command, hint, rendered)
	}
}

var refusalResource = regexp.MustCompile(`resource "[^"]+"`)

// splitHookReview separates the "Hook changes" listing, whose rows are
// indented beneath its header, from the rest of the rendered output.
func splitHookReview(output string) (rest string, review []string) {
	var kept []string
	inReview := false
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "Hook changes"):
			inReview = true
		case inReview && strings.HasPrefix(line, "  "):
			review = append(review, line)
		default:
			inReview = false
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n"), review
}
