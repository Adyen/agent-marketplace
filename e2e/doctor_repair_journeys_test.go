package e2e_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// spec: doctor-repair-planning/repair-exits-zero-when-the-setup-ends-clean
func TestDoctorRepairJourneyInterruptedWrite(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	createPendingDoctorRepairJournal(t, env)

	repair := runDoctorRepairJourney(t, env, nil, "y", "y")
	assertDoctorRepairJourney(t, env, repair)
	if !strings.Contains(repair.Combined, "replay interrupted dependency journal") {
		t.Fatalf("repair did not report journal recovery:\n%s", repair.Combined)
	}
	if remaining, err := filepath.Glob(filepath.Join(env.home, ".local", "state", "agent-marketplace",
		"dependencies", ".dependencytxn-*.json")); err != nil || len(remaining) != 0 {
		t.Fatalf("repair left dependency journals %v: %v", remaining, err)
	}
}

// spec: doctor-repair-planning/repair-exits-zero-when-the-setup-ends-clean
// spec: doctor-repair-planning/destructive-lock-regeneration-preserves-backup-and-agreed-pins
func TestDoctorRepairJourneyConflictedAcquisitionLock(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	registry := env.CreateBareRepo("doctor-lock-journey", RegistryFiles([]Skill{{Name: "repo-keeper"}}, nil, nil))
	env.WriteRepoConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [factory]
sources: {tools: {git: %q, ref: main}}
resources: {tools: {include: [skill/repo-keeper]}}
`, registry))
	env.MustRunAM("lock", "--scope", "repo")
	env.MustRunAM("sync")

	repoLock := filepath.Join(env.workspace, ".agents", "lock.yaml")
	original, err := os.ReadFile(repoLock)
	if err != nil {
		t.Fatal(err)
	}
	conflicted := []byte("<<<<<<< ours\n" + string(original) + "=======\n" +
		string(original) + ">>>>>>> theirs\n")
	if err := os.WriteFile(repoLock, conflicted, 0o644); err != nil {
		t.Fatal(err)
	}
	if broken := env.RunAM("sync", "--check"); broken.ExitCode == 0 || !strings.Contains(broken.Combined, "lock.conflicted") {
		t.Fatalf("sync --check did not diagnose the conflicted acquisition lock:\n%s", broken.Combined)
	}
	repair := runDoctorRepairJourney(t, env, nil, "y")
	assertDoctorRepairJourney(t, env, repair)
	if !strings.Contains(repair.Combined, "replace acquisition lock lock.yaml with the validated recovery? [y/N]") {
		t.Fatalf("repair did not ask to confirm destructive lock regeneration:\n%s", repair.Combined)
	}
	repairedLock, err := os.ReadFile(repoLock)
	if err != nil || bytes.Contains(repairedLock, []byte("<<<<<<<")) {
		t.Fatalf("conflicted acquisition lock remains: %v\n%s", err, repairedLock)
	}
	matches, err := filepath.Glob(repoLock + ".repair-*.bak")
	if err != nil || len(matches) != 1 {
		t.Fatalf("repair backups = %v, %v; want one", matches, err)
	}
	backup, err := os.ReadFile(matches[0])
	if err != nil || !bytes.Equal(backup, conflicted) {
		t.Fatalf("backup does not retain original conflict bytes: %v\nwant:\n%s\ngot:\n%s", err, conflicted, backup)
	}
	commitLine := ""
	for _, line := range strings.Split(string(original), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "commit:") {
			commitLine = strings.TrimSpace(line)
			break
		}
	}
	if commitLine == "" || !strings.Contains(string(repairedLock), commitLine) {
		t.Fatalf("regenerated lock does not retain the agreed pin %q:\n%s", commitLine, repairedLock)
	}
}

// spec: doctor-repair-planning/repair-exits-zero-when-the-setup-ends-clean
// spec: doctor-repair-planning/native-aggregate-repair-retains-action-safety
func TestDoctorRepairJourneyClaudeAggregateCollision(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	registry := env.CreateBareRepo("doctor-claude-aggregate-journey", map[string]string{
		".mcp.json": MergeMCPFile([]MCPServer{{Name: "replaceable", Command: "locked-bin"}}),
	})
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	env.WriteRepoConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [claude]
sources: {tools: {git: %q, ref: main, source_harness: claude}}
resources: {tools: {include: [mcp/replaceable]}}
`, registry))
	env.MustRunAM("lock", "--scope", "repo")
	originalAggregate := MergeMCPFile([]MCPServer{{Name: "replaceable", Command: "handwritten-bin"}})
	env.WriteFile("/workspace/.mcp.json", originalAggregate)
	if broken := env.RunAM("sync", "--check"); broken.ExitCode == 0 {
		t.Fatalf("sync --check accepted an unmanaged shared-aggregate collision:\n%s", broken.Combined)
	}
	repair := runDoctorRepairJourney(t, env, nil, "y")
	assertDoctorRepairJourney(t, env, repair)
	if !strings.Contains(repair.Combined, `replace mcp-server "replaceable" in .mcp.json with the entry generated`) ||
		!strings.Contains(repair.Combined, "? this deletes the existing value [y/N]") {
		t.Fatalf("repair did not ask to confirm the per-entry replacement:\n%s", repair.Combined)
	}
	aggregate := env.ReadFile("/workspace/.mcp.json")
	if !strings.Contains(aggregate, "locked-bin") || strings.Contains(aggregate, "handwritten-bin") {
		t.Fatalf("shared aggregate collision was not repaired:\n%s", aggregate)
	}
	backupRoot := filepath.Join(env.home, ".local", "state", "agent-marketplace", "safety-backups")
	if backups := filesWithContents(t, backupRoot, ".bak", originalAggregate); len(backups) == 0 {
		t.Fatalf("confirmed replacement created no fresh raw-byte safety backup under %s", backupRoot)
	}
}

// spec: doctor-repair-planning/repair-exits-zero-when-the-setup-ends-clean
// spec: doctor-repair-planning/an-editor-fix-is-diagnosed-again
func TestDoctorRepairJourneyGuidedConfigurationEdit(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	configPath := env.mustResolvePath("/workspace/.agents/config.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(config, []byte("\nunknown_field: true\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	editor := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nsed '/^unknown_field:/d' \"$1\" > \"$1.tmp\" && mv \"$1.tmp\" \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	editorCommand := "sh " + fmt.Sprintf("%q", editor)
	repair := runDoctorRepairJourney(t, env, map[string]string{"EDITOR": editorCommand}, "y")
	assertDoctorRepairJourney(t, env, repair)
	if !strings.Contains(repair.Combined, "nothing to repair") {
		t.Fatalf("repair did not converge after the editor exited:\n%s", repair.Combined)
	}
	if !strings.Contains(repair.Combined, "Edit ") ||
		!strings.Contains(repair.Combined, fmt.Sprintf(" with %s? [y/N]", editorCommand)) {
		t.Fatalf("repair did not offer the configured editor:\n%s", repair.Combined)
	}
	if strings.Contains(env.ReadFile("/workspace/.agents/config.yaml"), "unknown_field") {
		t.Fatal("scripted editor did not remove the unknown configuration field")
	}
}

func createPendingDoctorRepairJournal(t *testing.T, env *Env) {
	t.Helper()
	registry := env.CreateBareRepo("doctor-repair-journey", RegistryFiles([]Skill{{Name: "review"}}, nil, nil))
	config := fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [factory]
sources: {tools: {git: %q, ref: main}}
resources: {tools: {include: [skill/review]}}
`, registry)
	env.WriteFile("/workspace/.agents/config.yaml", config)
	crash := env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_E2E_CRASH_AT": "after-publish:0",
	}, "sync", "--no-backup")
	if crash.ExitCode != 137 {
		t.Fatalf("interrupted sync exited %d:\n%s", crash.ExitCode, crash.Combined)
	}
	journals, err := filepath.Glob(filepath.Join(env.home, ".local", "state", "agent-marketplace",
		"dependencies", ".dependencytxn-*.json"))
	if err != nil || len(journals) != 1 {
		t.Fatalf("pending dependency journals = %v, %v; want one", journals, err)
	}
}

type doctorRepairJourneyResult struct {
	ExecResult
	answered int
}

func runDoctorRepairJourney(t *testing.T, env *Env, overrides map[string]string, answers ...string) doctorRepairJourneyResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, env.binary, "doctor", "--repair")
	cmd.Dir = env.workspace
	var commandEnv []string
	for _, value := range env.subprocessEnv() {
		key, _, _ := strings.Cut(value, "=")
		if key != "VISUAL" && key != "EDITOR" {
			commandEnv = append(commandEnv, value)
		}
	}
	commandEnv = subprocessEnvWithOverrides(commandEnv, map[string]string{"TERM": "dumb"})
	commandEnv = subprocessEnvWithOverrides(commandEnv, overrides)
	cmd.Env = commandEnv
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 160})
	if err != nil {
		t.Fatalf("start doctor --repair: %v", err)
	}
	defer terminal.Close()

	var output synchronizedBuffer
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&output, terminal)
		close(readDone)
	}()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	answered := 0
	for {
		current := output.String()
		for answered < len(answers) && strings.Count(current, "[y/N]") > answered {
			if _, err := terminal.Write([]byte(answers[answered] + "\n")); err != nil {
				t.Fatalf("answer doctor repair prompt: %v", err)
			}
			answered++
		}
		select {
		case waitErr := <-wait:
			<-readDone
			result := ExecResult{Stdout: output.String(), Combined: output.String()}
			if waitErr != nil {
				var exitErr *exec.ExitError
				if !errors.As(waitErr, &exitErr) {
					t.Fatalf("doctor --repair process: %v\n%s", waitErr, result.Combined)
				}
				result.ExitCode = exitErr.ExitCode()
			}
			if answered != len(answers) || strings.Count(result.Combined, "[y/N]") != answered {
				t.Fatalf("answered %d of %d confirmations; output showed %d prompts:\n%s",
					answered, len(answers), strings.Count(result.Combined, "[y/N]"), result.Combined)
			}
			return doctorRepairJourneyResult{ExecResult: result, answered: answered}
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-wait
			t.Fatalf("doctor --repair did not finish; answered %d prompts:\n%s", answered, output.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func assertDoctorRepairJourney(t *testing.T, env *Env, repair doctorRepairJourneyResult) {
	t.Helper()
	if repair.answered == 0 {
		t.Fatalf("repair did not show and receive a confirmation (exit %d):\n%s", repair.ExitCode, repair.Combined)
	}
	if repair.ExitCode != 0 {
		t.Fatalf("doctor --repair exited %d:\n%s", repair.ExitCode, repair.Combined)
	}
	check := env.RunAM("sync", "--check")
	if check.ExitCode != 0 {
		t.Fatalf("sync --check after doctor repair exited %d:\n%s", check.ExitCode, check.Combined)
	}
}
