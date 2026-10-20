package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
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
