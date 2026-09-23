//go:build !windows

package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	codeCodexAggregateMalformed = "materialize.codex.aggregate-malformed"
	codeOwnershipLockContended  = "ownership.lock-contended"
	codeOwnershipStateCorrupt   = "ownership.state-corrupt"
	codeOwnershipStateMissing   = "ownership.state-missing"
)

func newCodexEnv(t *testing.T) *Env {
	t.Helper()
	env := newEnv(t)
	env.InitGitRepo()
	t.Setenv("CODEX_HOME", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(env.home, ".local", "state"))
	return env
}

func writeCodexRepoMCPConfig(env *Env, registryURL, name string, harnesses ...string) {
	env.t.Helper()
	var selected strings.Builder
	for _, harness := range harnesses {
		selected.WriteString("  - ")
		selected.WriteString(harness)
		selected.WriteByte('\n')
	}
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v3\nharnesses:\n" + selected.String() + "registries: []")
	env.WriteRepoConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v3
registries:
  - url: %s
    ref: main
    source_harness: claude
    mcp:
      - %s
`, registryURL, name))
}

func mcpRegistry(env *Env, repo, name, command string) string {
	env.t.Helper()
	return env.CreateBareRepo(repo, RegistryFilesEx(nil, nil, nil, []MCPServer{{Name: name, Command: command}}))
}

func TestE2ECodexCustomHomeAndNativeDetectionDoNotRequireExecutable(t *testing.T) {
	t.Run("resolved CODEX_HOME is installation evidence", func(t *testing.T) {
		env := newCodexEnv(t)
		customHome := filepath.Join(filepath.Dir(env.home), "detected-codex-home")
		t.Setenv("CODEX_HOME", customHome)
		if err := os.MkdirAll(customHome, 0o755); err != nil {
			t.Fatal(err)
		}

		result := runAMInTTY(t, env, "init", "", "Use these?", "y\nn\n")
		if result.ExitCode != 0 || !strings.Contains(result.Combined, "Detected harnesses: codex") {
			t.Fatalf("init did not detect resolved CODEX_HOME (exit=%d):\n%s", result.ExitCode, result.Combined)
		}
		if got := readFile(t, filepath.Join(env.home, ".agents", "config.yaml")); !strings.Contains(got, "- codex") {
			t.Fatalf("detected Codex harness was not selected:\n%s", got)
		}
	})

	t.Run("shared agents root alone is not installation evidence", func(t *testing.T) {
		env := newCodexEnv(t)
		if err := os.MkdirAll(filepath.Join(env.home, ".agents", "skills"), 0o755); err != nil {
			t.Fatal(err)
		}

		result := runAMInTTY(t, env, "init", "", "Enter comma-separated harnesses:", "codex\nn\n")
		if result.ExitCode != 0 || !strings.Contains(result.Combined, "No harnesses detected.") {
			t.Fatalf("init treated shared .agents as installation evidence (exit=%d):\n%s", result.ExitCode, result.Combined)
		}
		if strings.Contains(result.Combined, "Detected harnesses: codex") {
			t.Fatalf("init falsely detected Codex from .agents alone:\n%s", result.Combined)
		}
	})

	env := newCodexEnv(t)
	customHome := filepath.Join(filepath.Dir(env.home), "codex-outside-home")
	if rel, err := filepath.Rel(env.home, customHome); err != nil || !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("CODEX_HOME = %q is not outside HOME %q (rel=%q, err=%v)", customHome, env.home, rel, err)
	}
	t.Setenv("CODEX_HOME", customHome)

	// A native .codex marker selects the Codex ingester, which also reads the
	// sibling shared .agents skill directory. Shared .agents alone is not Codex
	// installation evidence; the machine detector is exercised above.
	registryURL := env.CreateBareRepo("codex-native-source", map[string]string{
		".agents/skills/native-skill/SKILL.md": Skill{Name: "native-skill"}.SkillFile(),
		".codex/agents/native-reviewer.toml":   "name = 'native-reviewer'\ndescription = 'Review changes'\ndeveloper_instructions = 'Review carefully.'\n",
		".codex/config.toml":                   "[mcp_servers.native]\ncommand = 'native-server'\n",
	})
	env.WriteGlobalConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v3
harnesses:
  - codex
registries:
  - url: %s
    ref: main
    skills:
      - native-skill
    subagents:
      - native-reviewer
    mcp:
      - native
`, registryURL))

	withoutCodex := pathWithoutExecutable(t, env.path, "codex")
	if found, ok := lookInPath(withoutCodex, "codex"); ok {
		t.Fatalf("test PATH still resolves codex at %s", found)
	}
	result := env.RunAMWithEnv(map[string]string{"PATH": withoutCodex, "CODEX_HOME": customHome}, "sync")
	if result.ExitCode != 0 {
		t.Fatalf("sync with no Codex executable exited %d:\n%s", result.ExitCode, result.Combined)
	}

	for _, path := range []string{
		filepath.Join(env.home, ".agents", "skills", "native-skill", "SKILL.md"),
		filepath.Join(customHome, "agents", "native-reviewer.toml"),
		filepath.Join(customHome, "config.toml"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected native Codex output %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(env.home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("default Codex home was touched despite CODEX_HOME override: %v", err)
	}
	if got := readFile(t, filepath.Join(customHome, "config.toml")); !strings.Contains(got, "native-server") {
		t.Fatalf("custom Codex config lacks native MCP server:\n%s", got)
	}
}

func TestE2ECodexConcurrentAggregateSync(t *testing.T) {
	env := newCodexEnv(t)
	alphaURL := mcpRegistry(env, "codex-concurrent-alpha", "alpha", "alpha-bin")
	betaURL := mcpRegistry(env, "codex-concurrent-beta", "beta", "beta-bin")
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v3\nharnesses:\n  - codex\nregistries: []")

	baseOrg := `apiVersion: agent-marketplace/v3
harnesses:
  - codex
registries:
  - url: %s
    ref: main
    source_harness: claude
    mcp:
      - %s
`
	env.WriteFile("/etc/agent-marketplace/codex-alpha/config.yaml", fmt.Sprintf(baseOrg, alphaURL, "alpha"))
	env.WriteFile("/etc/agent-marketplace/codex-beta/config.yaml", fmt.Sprintf(baseOrg, betaURL, "beta"))
	alphaOrg := env.mustResolvePath("/etc/agent-marketplace/codex-alpha/config.yaml")
	betaOrg := env.mustResolvePath("/etc/agent-marketplace/codex-beta/config.yaml")
	env.MustLockOrganizationConfig(alphaOrg)
	env.MustLockOrganizationConfig(betaOrg)

	const seed = "# handwritten\n[mcp_servers.handwritten]\ncommand = 'user-bin'\n"
	configPath := filepath.Join(env.home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	proofPath, proofBytes := prepareOwnershipProofFIFO(t, env, alphaOrg)

	var alpha, beta ExecResult
	alphaDone := make(chan struct{})
	go func() {
		defer close(alphaDone)
		alpha = env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_V3_ORG_CONFIG": alphaOrg}, "sync", "--no-backup")
	}()

	fifoWriter := openFIFOWriter(t, proofPath, alphaDone, &alpha)
	if err := os.Rename(proofPath+".seed", proofPath); err != nil {
		_ = fifoWriter.Close()
		t.Fatal(err)
	}

	betaLog := filepath.Join(t.TempDir(), "beta.log")
	betaDone := make(chan struct{})
	go func() {
		defer close(betaDone)
		beta = env.RunAMWithEnv(map[string]string{
			"AGENT_MARKETPLACE_V3_ORG_CONFIG": betaOrg,
			"AGENT_MARKETPLACE_LOG_FILE":      betaLog,
			"AGENT_MARKETPLACE_LOG_LEVEL":     "debug",
		}, "sync", "--no-backup")
	}()
	waitForFileContains(t, betaLog, "Codex MCP reconciliation start")
	// Hold the first writer long enough for the default-wait path to report
	// contention after the second writer reaches the same root.
	time.Sleep(1100 * time.Millisecond)
	if _, err := fifoWriter.Write(proofBytes); err != nil {
		_ = fifoWriter.Close()
		t.Fatal(err)
	}
	if err := fifoWriter.Close(); err != nil {
		t.Fatal(err)
	}
	waitForProcess(t, "alpha", alphaDone)
	waitForProcess(t, "beta", betaDone)
	for _, process := range []struct {
		name   string
		result ExecResult
	}{
		{name: "alpha", result: alpha},
		{name: "beta", result: beta},
	} {
		t.Logf("%s sync exit=%d output:\n%s", process.name, process.result.ExitCode, process.result.Combined)
		if process.result.ExitCode != 0 {
			t.Fatalf("%s serialized sync exited %d:\n%s", process.name, process.result.ExitCode, process.result.Combined)
		}
	}
	if log := readFile(t, betaLog); !strings.Contains(log, codeOwnershipLockContended) {
		t.Fatalf("second default-wait writer did not report %s:\n%s", codeOwnershipLockContended, log)
	}
	got := readFile(t, configPath)
	for _, want := range []string{"user-bin", "beta-bin"} {
		if !strings.Contains(got, want) {
			t.Errorf("serialized config missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "alpha-bin") {
		t.Fatalf("stale managed entry survived serialized sync:\n%s", got)
	}
	check := env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_V3_ORG_CONFIG": betaOrg}, "sync", "--check")
	if check.ExitCode != 0 {
		t.Fatalf("winning state is inconsistent (exit=%d):\n%s", check.ExitCode, check.Combined)
	}
	if found := findAnyFile(t, filepath.Join(env.home, ".local", "state", "agent-marketplace"), "journal.json"); found != "" {
		t.Fatalf("completed concurrent sync left a journal: %s", found)
	}
}

func TestE2ECodexMissingAndCorruptOwnershipFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name       string
		breakState func(*testing.T, string)
		wantCode   string
	}{
		{name: "missing", breakState: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}, wantCode: codeOwnershipStateMissing},
		{name: "corrupt", breakState: func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, wantCode: codeOwnershipStateCorrupt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCodexEnv(t)
			firstURL := env.CreateBareRepo("ownership-first", RegistryFilesEx(nil, nil, nil, []MCPServer{
				{Name: "managed", Command: "first-bin"},
				{Name: "stale", Command: "stale-bin"},
			}))
			env.WriteGlobalConfig("apiVersion: agent-marketplace/v3\nharnesses:\n  - codex\nregistries: []")
			env.WriteRepoConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v3
registries:
  - url: %s
    ref: main
    source_harness: claude
    mcp:
      - managed
      - stale
`, firstURL))
			if result := env.RunAM("sync"); result.ExitCode != 0 {
				t.Fatalf("seed sync exited %d:\n%s", result.ExitCode, result.Combined)
			}
			configPath := filepath.Join(env.workspace, ".codex", "config.toml")
			before := readFile(t, configPath)
			statePath := findAnyFile(t, filepath.Join(env.home, ".local", "state", "agent-marketplace"), "state.json")
			if statePath == "" {
				t.Fatal("seed sync created no ownership state")
			}
			tc.breakState(t, statePath)

			secondURL := mcpRegistry(env, "ownership-second", "managed", "second-bin")
			writeCodexRepoMCPConfig(env, secondURL, "managed", "codex")
			result := env.RunAM("sync")
			if result.ExitCode == 0 || !strings.Contains(result.Combined, tc.wantCode) || !strings.Contains(result.Combined, "ownership.unmanaged-key") {
				t.Fatalf("fail-closed sync output lacks %s/unmanaged collision (exit=%d):\n%s", tc.wantCode, result.ExitCode, result.Combined)
			}
			if after := readFile(t, configPath); after != before {
				t.Fatalf("sync changed aggregate without valid ownership:\n--- before ---\n%s\n--- after ---\n%s", before, after)
			}
			if strings.Contains(before, "second-bin") || !strings.Contains(before, "first-bin") || !strings.Contains(before, "stale-bin") {
				t.Fatalf("invalid ownership fixture: %s", before)
			}
			if after := readFile(t, configPath); !strings.Contains(after, "stale-bin") {
				t.Fatalf("fail-closed sync pruned the distinct stale entry:\n%s", after)
			}
		})
	}
}

func TestE2ECodexMalformedAggregatesAreIsolated(t *testing.T) {
	for _, tc := range []struct {
		name          string
		malformedPath string
		malformed     string
		wantCode      string
		continuedPath string
		continuedText string
	}{
		{
			name:          "MCP file does not block hooks",
			malformedPath: ".codex/config.toml",
			malformed:     "[mcp_servers.broken\nsecret = 'must-stay'\n",
			wantCode:      codeCodexAggregateMalformed,
			continuedPath: ".codex/hooks.json",
			continuedText: "generated-hook",
		},
		{
			name:          "hook file does not block MCP",
			malformedPath: ".codex/hooks.json",
			malformed:     `{"hooks":{"PreToolUse":[`,
			wantCode:      "json.malformed",
			continuedPath: ".codex/config.toml",
			continuedText: "generated-server",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCodexEnv(t)
			files := RegistryFilesEx([]Skill{{Name: "survivor"}}, nil, nil, []MCPServer{{Name: "generated", Command: "generated-server"}})
			files["hooks/hooks.json"] = `{"hooks":{"PreToolUse":[{"type":"command","command":"generated-hook","matcher":"Bash"}]}}`
			registryURL := env.CreateBareRepo("malformed-isolation", files)
			env.WriteGlobalConfig("apiVersion: agent-marketplace/v3\nharnesses:\n  - codex\nregistries: []")
			env.WriteRepoConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v3
registries:
  - url: %s
    ref: main
    source_harness: claude
    skills:
      - survivor
    hooks:
      - PreToolUse-command
    mcp:
      - generated
`, registryURL))
			malformedPath := filepath.Join(env.workspace, filepath.FromSlash(tc.malformedPath))
			if err := os.MkdirAll(filepath.Dir(malformedPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(malformedPath, []byte(tc.malformed), 0o644); err != nil {
				t.Fatal(err)
			}

			result := env.RunAM("sync")
			if result.ExitCode == 0 || !strings.Contains(result.Combined, tc.wantCode) {
				t.Fatalf("malformed aggregate did not fail independently (exit=%d):\n%s", result.ExitCode, result.Combined)
			}
			if got := readFile(t, malformedPath); got != tc.malformed {
				t.Fatalf("malformed aggregate changed:\nwant %q\n got %q", tc.malformed, got)
			}
			continued := readFile(t, filepath.Join(env.workspace, filepath.FromSlash(tc.continuedPath)))
			if !strings.Contains(continued, tc.continuedText) {
				t.Fatalf("independent aggregate did not continue; missing %q:\n%s", tc.continuedText, continued)
			}
			if _, err := os.Stat(filepath.Join(env.workspace, ".agents", "skills", "survivor", "SKILL.md")); err != nil {
				t.Fatalf("independent skill did not continue: %v", err)
			}
		})
	}
}

func TestE2ECodexRepairPreviewAndExactMatchAdoption(t *testing.T) {
	env := newCodexEnv(t)
	registryURL := mcpRegistry(env, "repair-adoption", "managed", "managed-bin")
	writeCodexRepoMCPConfig(env, registryURL, "managed", "codex")
	configPath := filepath.Join(env.workspace, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const original = "# handwritten formatting stays\n[mcp_servers.managed]\ncommand = 'managed-bin'\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	env.MustRunAM("lock")

	preview := env.RunAM("doctor", "--repair")
	if preview.ExitCode != 0 || !strings.Contains(preview.Combined, `adopt mcp-server "managed"`) || !strings.Contains(preview.Combined, "preview only") {
		t.Fatalf("repair preview mismatch (exit=%d):\n%s", preview.ExitCode, preview.Combined)
	}
	if got := readFile(t, configPath); got != original {
		t.Fatalf("preview changed config.toml:\n%s", got)
	}

	adopt := env.RunAM("doctor", "--repair", "--yes")
	if adopt.ExitCode != 0 || !strings.Contains(adopt.Combined, "adopted 1 entr(y/ies) in config.toml") {
		t.Fatalf("exact adoption failed (exit=%d):\n%s", adopt.ExitCode, adopt.Combined)
	}
	if got := readFile(t, configPath); got != original {
		t.Fatalf("adoption changed config.toml bytes:\n%s", got)
	}
	if check := env.RunAM("sync", "--check"); check.ExitCode != 0 {
		t.Fatalf("adopted state does not describe the live file (exit=%d):\n%s", check.ExitCode, check.Combined)
	}
}

// The same exact-match adoption works when the user and repository
// configs are v4.
func TestE2ECodexRepairExactMatchAdoptionWithV4Config(t *testing.T) {
	env := newCodexEnv(t)
	registryURL := mcpRegistry(env, "repair-adoption-v4", "managed", "managed-bin")
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [codex]\n")
	env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  tools: {git: '%s', ref: main, source_harness: claude}\nresources:\n  tools: {include: [mcp/managed]}\n", registryURL))
	configPath := filepath.Join(env.workspace, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const original = "# handwritten formatting stays\n[mcp_servers.managed]\ncommand = 'managed-bin'\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	env.MustRunAM("lock")

	preview := env.RunAM("doctor", "--repair")
	if preview.ExitCode != 0 || !strings.Contains(preview.Combined, `adopt mcp-server "managed"`) || !strings.Contains(preview.Combined, "preview only") {
		t.Fatalf("repair preview mismatch (exit=%d):\n%s", preview.ExitCode, preview.Combined)
	}
	adopt := env.RunAM("doctor", "--repair", "--yes")
	if adopt.ExitCode != 0 || !strings.Contains(adopt.Combined, "adopted 1 entr(y/ies) in config.toml") {
		t.Fatalf("exact adoption failed (exit=%d):\n%s", adopt.ExitCode, adopt.Combined)
	}
	if got := readFile(t, configPath); got != original {
		t.Fatalf("adoption changed config.toml bytes:\n%s", got)
	}
	if check := env.RunAM("sync", "--check"); check.ExitCode != 0 {
		t.Fatalf("adopted state does not describe the live file (exit=%d):\n%s", check.ExitCode, check.Combined)
	}
}

func TestE2ECodexDestructiveRepairRequiresConfirmationAndRedactsSecrets(t *testing.T) {
	env := newCodexEnv(t)
	const (
		oldSecret = "old-command-secret-must-not-print"
		newSecret = "new-command-secret-must-not-print"
	)
	registryURL := mcpRegistry(env, "repair-conflict", "managed", newSecret)
	writeCodexRepoMCPConfig(env, registryURL, "managed", "codex")
	configPath := filepath.Join(env.workspace, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "[mcp_servers.managed]\ncommand = '" + oldSecret + "'\nenv = { TOKEN = 'second-secret-must-not-print' }\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	env.MustRunAM("lock", "--scope", "repo")

	preview := env.RunAM("doctor", "--repair", "--yes")
	assertRepairResult(t, preview, 0, "replace mcp-server", "left unchanged")
	assertNoSecrets(t, preview.Combined, oldSecret, newSecret, "second-secret-must-not-print")
	if got := readFile(t, configPath); got != original {
		t.Fatalf("--yes replaced destructive conflict:\n%s", got)
	}

	decline := runDoctorRepairInTTY(t, env, "n\n")
	assertRepairResult(t, decline, 0, "declined:", "left unchanged")
	assertNoSecrets(t, decline.Combined, oldSecret, newSecret, "second-secret-must-not-print")
	if got := readFile(t, configPath); got != original {
		t.Fatalf("declined repair changed config.toml:\n%s", got)
	}
	backupRoot := filepath.Join(env.home, ".local", "state", "agent-marketplace", "safety-backups")
	if backups := filesWithContents(t, backupRoot, ".bak", original); len(backups) != 0 {
		t.Fatalf("unconfirmed repair created safety backups: %v", backups)
	}

	repair := runDoctorRepairInTTY(t, env, "y\n")
	assertRepairResult(t, repair, 0, `replaced mcp-server "managed"`, "generated entry")
	assertNoSecrets(t, repair.Combined, oldSecret, newSecret, "second-secret-must-not-print")
	after := readFile(t, configPath)
	if strings.Contains(after, oldSecret) || !strings.Contains(after, newSecret) {
		t.Fatalf("confirmed replacement wrote the wrong value:\n%s", after)
	}
	if backups := filesWithContents(t, backupRoot, ".bak", original); len(backups) == 0 {
		t.Fatalf("confirmed replacement created no fresh raw-byte safety backup under %s", backupRoot)
	}
	logPath := filepath.Join(env.home, ".agent-marketplace", "logs", "agent-marketplace.log")
	assertNoSecrets(t, readFile(t, logPath), oldSecret, newSecret, "second-secret-must-not-print")
}

func TestE2ECodexRepairCorruptionAfterIndependentAdoptionIsPartialFailure(t *testing.T) {
	env := newCodexEnv(t)
	registryURL := mcpRegistry(env, "repair-partial", "managed", "managed-bin")
	writeCodexRepoMCPConfig(env, registryURL, "managed", "devin", "codex")
	devinPath := filepath.Join(env.workspace, ".devin", "mcp_config.json")
	codexPath := filepath.Join(env.workspace, ".codex", "config.toml")
	for _, path := range []string{devinPath, codexPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const devinOriginal = "{\n  \"mcpServers\": {\n    \"managed\": {\"command\": \"managed-bin\"}\n  }\n}\n"
	const corruptSecret = "corrupt-payload-must-not-print"
	const codexOriginal = "[mcp_servers.managed\ncommand = '" + corruptSecret + "'\n"
	if err := os.WriteFile(devinPath, []byte(devinOriginal), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(codexOriginal), 0o644); err != nil {
		t.Fatal(err)
	}
	env.MustRunAM("lock", "--scope", "repo")

	result := env.RunAM("doctor", "--repair", "--yes")
	if result.ExitCode == 0 {
		t.Fatalf("partial repair with corrupt Codex aggregate exited 0:\n%s", result.Combined)
	}
	for _, want := range []string{"adopted 1 entr(y/ies) in mcp_config.json", codeCodexAggregateMalformed} {
		if !strings.Contains(result.Combined, want) {
			t.Fatalf("partial repair output missing %q:\n%s", want, result.Combined)
		}
	}
	assertNoSecrets(t, result.Combined, corruptSecret)
	if got := readFile(t, devinPath); got != devinOriginal {
		t.Fatalf("independent Devin adoption changed bytes:\n%s", got)
	}
	if got := readFile(t, codexPath); got != codexOriginal {
		t.Fatalf("corrupt Codex aggregate changed:\n%s", got)
	}
}

func pathWithoutExecutable(t *testing.T, pathList, executable string) string {
	t.Helper()
	shadowRoot := t.TempDir()
	parts := filepath.SplitList(pathList)
	for i, dir := range parts {
		if !dirHasAny(dir, []string{executable}) {
			continue
		}
		shadow := filepath.Join(shadowRoot, fmt.Sprint(i))
		if err := shadowDir(dir, shadow, []string{executable}); err != nil {
			t.Fatalf("hide %s from PATH entry %s: %v", executable, dir, err)
		}
		parts[i] = shadow
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func waitForFileContains(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatalf("%s did not contain %q before timeout", path, want)
		case <-ticker.C:
			data, err := os.ReadFile(path)
			if err == nil && strings.Contains(string(data), want) {
				return
			}
			if err != nil && !os.IsNotExist(err) {
				t.Fatalf("read %s while waiting for %q: %v", path, want, err)
			}
		}
	}
}

func filesWithContents(t *testing.T, root, suffix, want string) []string {
	t.Helper()
	var matches []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, suffix) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(data) == want {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", root, err)
	}
	return matches
}

func assertRepairResult(t *testing.T, result ExecResult, wantExit int, contains ...string) {
	t.Helper()
	if result.ExitCode != wantExit {
		t.Fatalf("repair exit=%d, want %d:\n%s", result.ExitCode, wantExit, result.Combined)
	}
	for _, want := range contains {
		if !strings.Contains(result.Combined, want) {
			t.Fatalf("repair output missing %q:\n%s", want, result.Combined)
		}
	}
}

func assertNoSecrets(t *testing.T, output string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(output, secret) {
			t.Fatalf("repair output leaked %q:\n%s", secret, output)
		}
	}
}

// runDoctorRepairInTTY uses the system script utility to give the shipped CLI
// a real terminal. The wrapper takes no interpolated shell input: the binary
// path and fixed test arguments are passed through quoted environment values.
func runDoctorRepairInTTY(t *testing.T, env *Env, answer string) ExecResult {
	t.Helper()
	return runAMInTTY(t, env, "doctor", "--repair", "[y/N]", answer)
}

func runAMInTTY(t *testing.T, env *Env, command, option, prompt, answer string) ExecResult {
	t.Helper()
	scriptPath, err := exec.LookPath("script")
	if err != nil {
		t.Fatalf("interactive E2E requires the system 'script' utility: %v", err)
	}
	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "agent-marketplace-tty")
	stdoutPath := filepath.Join(tmp, "stdout")
	stderrPath := filepath.Join(tmp, "stderr")
	const wrapperBody = `#!/bin/sh
if [ -n "$AM_E2E_OPTION" ]; then
  exec "$AM_E2E_BINARY" "$AM_E2E_COMMAND" "$AM_E2E_OPTION" >"$AM_E2E_STDOUT" 2>"$AM_E2E_STDERR"
fi
exec "$AM_E2E_BINARY" "$AM_E2E_COMMAND" >"$AM_E2E_STDOUT" 2>"$AM_E2E_STDERR"
`
	if err := os.WriteFile(wrapper, []byte(wrapperBody), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"-q", "/dev/null", wrapper}
	commandEnv := env.subprocessEnv()
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		commandEnv = subprocessEnvWithOverrides(commandEnv, map[string]string{"CODEX_HOME": codexHome})
	}
	commandEnv = append(commandEnv,
		"AM_E2E_BINARY="+env.binary,
		"AM_E2E_COMMAND="+command,
		"AM_E2E_OPTION="+option,
		"AM_E2E_STDOUT="+stdoutPath,
		"AM_E2E_STDERR="+stderrPath,
		"AGENT_MARKETPLACE_NO_TUI=1",
	)
	if runtime.GOOS != "darwin" {
		help, _ := exec.Command(scriptPath, "--help").CombinedOutput()
		if strings.Contains(string(help), "-c") {
			// util-linux accepts a command path with -c; the wrapper path comes
			// from t.TempDir and contains no user-controlled text.
			args = []string{"-q", "-e", "-c", wrapper, "/dev/null"}
		} else {
			// BusyBox script has no -c flag and launches $SHELL.
			args = []string{"-q", "/dev/null"}
			commandEnv = append(commandEnv, "SHELL="+wrapper)
			answer += "exit\n"
		}
	}
	cmd := exec.CommandContext(ctx, scriptPath, args...)
	cmd.Dir = env.workspace
	cmd.Env = commandEnv
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s in TTY: %v", command, err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err = <-wait:
			t.Fatalf("%s exited before prompting: %v (script stdout: %q; script stderr: %q)", command, err, stdout.String(), stderr.String())
		case <-ctx.Done():
			t.Fatalf("%s did not prompt before timeout: %v (script stdout: %q; script stderr: %q)", command, ctx.Err(), stdout.String(), stderr.String())
		case <-ticker.C:
			output, readErr := os.ReadFile(stdoutPath)
			if readErr == nil && strings.Contains(string(output), prompt) {
				goto prompted
			}
		}
	}

prompted:
	if _, err := stdin.Write([]byte(answer)); err != nil {
		t.Fatalf("answer %s prompt: %v", command, err)
	}
	err = <-wait
	cliStdout, readStdoutErr := os.ReadFile(stdoutPath)
	cliStderr, readStderrErr := os.ReadFile(stderrPath)
	if readStdoutErr != nil || readStderrErr != nil {
		t.Fatalf("PTY wrapper did not capture CLI output (stdout: %v; stderr: %v; script stdout: %q; script stderr: %q)",
			readStdoutErr, readStderrErr, stdout.String(), stderr.String())
	}
	result := ExecResult{Stdout: string(cliStdout), Stderr: string(cliStderr), Combined: string(cliStdout) + string(cliStderr)}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run %s in TTY: %v", command, err)
		}
	}
	return result
}
