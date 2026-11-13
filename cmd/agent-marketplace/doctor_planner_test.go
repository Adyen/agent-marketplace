package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// spec: doctor-repair-planning/repair-plan-lists-every-step-before-confirmation
// spec: doctor-repair-planning/declining-the-repair-plan-changes-nothing
func TestDoctorRepairPlanDeclineLeavesCandidatesUnchanged(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareDevinRepairFixture(t, repoRoot)
	assertUnchanged := snapshotDoctorRepairFiles(t, repoRoot, mcpPath)

	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("n\n"),
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if err != nil {
		t.Fatalf("declining repair plan: %v\n%s", err, out.String())
	}
	wantPlanLine := "  1. [safe] adopt exact shared-aggregate matches (ownership.unmanaged-key; files: devin:" +
		mcpPath + "; needs: selection)"
	if !strings.Contains(out.String(), wantPlanLine) {
		t.Fatalf("repair plan missing full adoption line %q:\n%s", wantPlanLine, out.String())
	}
	if strings.Count(out.String(), "[y/N]") != 1 {
		t.Fatalf("plan asked %d confirmations, want 1:\n%s", strings.Count(out.String(), "[y/N]"), out.String())
	}
	assertUnchanged()
	if !devinRepairPreview(t, repoRoot, selection).Actionable() {
		t.Fatal("declined plan recorded ownership")
	}
}

// spec: doctor-repair-planning/declining-the-repair-plan-changes-nothing
func TestDoctorRepairPlanEmptyAndInvalidAnswersLeaveCandidatesUnchanged(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name, answer string
	}{
		{name: "empty line", answer: "\n"},
		{name: "EOF"},
		{name: "unexpected answer", answer: "maybe\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			mcpPath, selection := prepareDevinRepairFixture(t, repoRoot)
			assertUnchanged := snapshotDoctorRepairFiles(t, repoRoot, mcpPath)
			var out bytes.Buffer
			err := runDoctor(doctorOptions{
				out:     &out,
				in:      strings.NewReader(tc.answer),
				isTTY:   true,
				repair:  true,
				probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
				resolve: staticResolve(selection),
			})
			if err != nil {
				t.Fatalf("declining repair plan: %v\n%s", err, out.String())
			}
			assertUnchanged()
			if !devinRepairPreview(t, repoRoot, selection).Actionable() {
				t.Fatal("declined plan recorded ownership")
			}
		})
	}
}

func snapshotDoctorRepairFiles(t *testing.T, repoRoot, aggregatePath string) func() {
	t.Helper()
	store, err := ownership.Open(filepath.Join(repoRoot, ".devin"))
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{aggregatePath, store.StateFile(), filepath.Join(repoRoot, ".agents", "lock.yaml")}
	before := make([][]byte, len(paths))
	present := make([]bool, len(paths))
	for i, path := range paths {
		before[i], err = os.ReadFile(path)
		if err == nil {
			present[i] = true
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("snapshot %s: %v", path, err)
		}
	}
	return func() {
		t.Helper()
		for i, path := range paths {
			after, err := os.ReadFile(path)
			if !present[i] {
				if !errors.Is(err, os.ErrNotExist) {
					t.Errorf("declined plan created %s: %v", path, err)
				}
			} else if err != nil || !bytes.Equal(before[i], after) {
				t.Errorf("declined plan changed %s: %v\n%s", path, err, after)
			}
		}
	}
}

// spec: doctor-repair-planning/off-terminal-repair-without-yes-is-preview-only
func TestDoctorRepairOffTerminalPlanIsPreviewOnly(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareDevinRepairFixture(t, repoRoot)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("y\n"),
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if err != nil {
		t.Fatalf("off-terminal plan preview: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "preview only: not a terminal") {
		t.Fatalf("off-terminal plan omitted preview-only status:\n%s", out.String())
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("off-terminal plan changed aggregate bytes: %v\n%s", err, after)
	}
}

// spec: doctor-repair-planning/repair-with-yes-runs-only-safe-steps
func TestDoctorRepairYesRunsSafeStepAndDefersDestructiveEntry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	outDir := filepath.Join(repoRoot, ".devin")
	adoptable := devinMcpArtifact("adoptable", "adoptable-binary")
	selection := []resolver.ResolvedArtifact{adoptable}
	if _, err := materialize.Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}
	mcpPath := filepath.Join(outDir, harness.DevinMcpConfigFile)
	generated, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	withConflict := strings.Replace(string(generated), `"mcpServers": {`,
		`"mcpServers": {"conflicting": {"command": "handwritten-binary"},`, 1)
	if withConflict == string(generated) {
		t.Fatalf("fixture did not add a conflicting entry:\n%s", generated)
	}
	if err := os.WriteFile(mcpPath, []byte(withConflict), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatal(err)
	}
	selection = append(selection, devinMcpArtifact("conflicting", "generated-binary"))
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader(""),
		repair:  true,
		yes:     true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if err != nil {
		t.Fatalf("--repair --yes: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "adopted 1 entr(y/ies)") ||
		!strings.Contains(out.String(), "conflicting mcp-server \"conflicting\" in mcp_config.json left unchanged") {
		t.Fatalf("--yes did not separate safe and destructive work:\n%s", out.String())
	}
	if strings.Contains(out.String(), "[y/N]") {
		t.Fatalf("--yes prompted for safe work:\n%s", out.String())
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("--yes changed aggregate bytes: %v\n%s", err, after)
	}
	preview := devinRepairPreview(t, repoRoot, selection)
	if len(preview.Files[0].Adoptions) != 0 || len(preview.Files[0].Conflicts) != 1 {
		t.Fatalf("safe adoption and destructive conflict state = %#v", preview.Files[0])
	}
}

// spec: doctor-repair-planning/drift-repair-leaves-acquisition-locks-byte-identical
func TestDoctorRepairDriftKeepsAcquisitionLockUnchanged(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _, _, lockPath := setupReadOnlyExactRegistry(t)
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	initialOutput, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("initial sync: %v\n%s", err, initialOutput)
	}
	var driftPath string
	for _, path := range []string{
		filepath.Join(repoRoot, ".factory", "skills", "locked-skill", "SKILL.md"),
		filepath.Join(repoRoot, "skills", "locked-skill", "SKILL.md"),
	} {
		if _, err := os.Stat(path); err == nil {
			driftPath = path
			break
		}
	}
	if driftPath == "" {
		t.Fatalf("initial sync did not materialize locked-skill:\n%s", initialOutput)
	}
	content, err := os.ReadFile(driftPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(driftPath, append(content, []byte("\nlocal drift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	repairOutput, err := executeCommand("doctor", "--repair", "--yes")
	if err != nil {
		t.Fatalf("doctor --repair --yes: %v\n%s", err, repairOutput)
	}
	if !strings.Contains(repairOutput, "ownership.entry-drift") {
		t.Fatalf("repair plan omitted drift stage:\n%s", repairOutput)
	}
	if output, err := executeCommand("sync", "--check"); err != nil {
		t.Fatalf("sync check after drift repair: %v\n%s", err, output)
	}
	if lockAfter, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(lockBefore, lockAfter) {
		t.Fatalf("drift repair changed acquisition lock: %v\n%s", err, lockAfter)
	}
}

// spec: doctor-repair-planning/drift-repair-leaves-acquisition-locks-byte-identical
// spec: doctor-repair-planning/shared-aggregate-repair-runs-after-drift-repair
func TestDoctorRepairRunsDriftBesideCodexCollision(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, origin, _, lockPath := setupReadOnlyExactRegistry(t)
	source := filepath.Join(repoRoot, "extra")
	writeFile(t, filepath.Join(source, "mcp.json"), `{"mcpServers":{"collision":{"command":"collision-binary"}}}`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [factory, codex]\nsources:\n"+
			"  locked: {git: file://"+origin+", ref: main}\n"+
			"  extra: {directory: "+filepath.ToSlash(source)+"}\nresources:\n"+
			"  locked: [skill/locked-skill]\n  extra: [mcp/collision]\n")
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	driftPath := filepath.Join(repoRoot, ".factory", "skills", "locked-skill", "SKILL.md")
	content, err := os.ReadFile(driftPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(driftPath, append(content, []byte("\nlocal drift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(filepath.Join(repoRoot, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("doctor", "--repair", "--yes")
	if err != nil {
		t.Fatalf("doctor --repair --yes: %v\n%s", err, output)
	}
	if driftStep := strings.Index(output, "sync --locked harness drift"); driftStep < 0 ||
		strings.Index(output, `adopt mcp-server "collision"`) < driftStep {
		t.Fatalf("drift and Codex adoption were not planned in order:\n%s", output)
	}
	if !strings.Contains(output, "adopted 1 entr(y/ies) in config.toml") {
		t.Fatalf("Codex collision was not offered to stage 6:\n%s", output)
	}
	if after, err := os.ReadFile(driftPath); err != nil || strings.Contains(string(after), "local drift") {
		t.Fatalf("drift step did not restore the locked skill: %v\n%s", err, after)
	}
	if after, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(lockBefore, after) {
		t.Fatalf("drift repair changed the acquisition lock: %v\n%s", err, after)
	}
}

// spec: doctor-repair-planning/drift-repair-leaves-acquisition-locks-byte-identical
func TestDoctorRepairRunsDriftBesideSameBasenameDevinCollision(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, origin, _, lockPath := setupReadOnlyExactRegistry(t)
	source := filepath.Join(repoRoot, "extra")
	writeFile(t, filepath.Join(source, "mcp.json"), `{"mcpServers":{"collision":{"command":"echo collision"}}}`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [devin, windsurf]\nsources:\n"+
			"  locked: {git: file://"+origin+", ref: main}\n"+
			"  extra: {directory: "+filepath.ToSlash(source)+"}\nresources:\n"+
			"  locked: [skill/locked-skill]\n  extra: [mcp/collision]\n")
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	windsurfMcp := filepath.Join(repoRoot, ".windsurf", "mcp_config.json")
	windsurfBefore, err := os.ReadFile(windsurfMcp)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, windsurfMcp, strings.Replace(string(windsurfBefore), "echo collision", "echo local drift", 1))
	devinRoot := filepath.Join(repoRoot, ".devin")
	store, err := ownership.Open(devinRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatal(err)
	}
	devinMcp := filepath.Join(devinRoot, "mcp_config.json")
	writeFile(t, devinMcp, `{"mcpServers":{"collision":{"command":"echo handwritten"}}}`)
	checkOutput, checkErr := executeCommand("sync", "--check")
	if checkErr == nil || !strings.Contains(checkOutput, "out of sync") || !strings.Contains(checkOutput, "unmanaged-key") {
		t.Fatalf("fixture did not produce a Devin aggregate collision and Windsurf drift: %v\n%s", checkErr, checkOutput)
	}

	output, err := executeCommand("doctor", "--repair", "--yes")
	if err != nil {
		t.Fatalf("--repair --yes must exit 0 when only a deferred destructive entry remains: %v\n%s", err, output)
	}
	if !strings.Contains(output, "[safe] sync --locked harness drift") {
		t.Fatalf("repair plan omitted Windsurf drift beside the Devin collision:\n%s", output)
	}
	if after, err := os.ReadFile(windsurfMcp); err != nil || !bytes.Equal(windsurfBefore, after) {
		t.Fatalf("drift step did not restore Windsurf MCP config: %v\n%s\n%s", err, after, output)
	}
	if after, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(lockBefore, after) {
		t.Fatalf("drift repair changed acquisition lock: %v\n%s", err, after)
	}
}

func TestSyncCheckCollisions(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot := t.TempDir()
	opencodeFile := filepath.Join(repoRoot, "opencode.jsonc")
	collision := "warning: ownership.unmanaged-key: collision: mcp-server entry \"conflict\" in \"" +
		opencodeFile + "\""
	aggregateError := "error: materialize.opencode.aggregate-collision: A handwritten MCP entry conflicts with the selected server; configuration preserved"
	tests := []struct {
		name            string
		output          string
		wantFiles       []string
		wantOtherErrors bool
	}{
		{
			name:      "OpenCode aggregate collision",
			output:    collision + "\n" + aggregateError,
			wantFiles: []string{opencodeFile},
		},
		{
			name:            "unrelated errors remain",
			output:          "error: materialize.opencode.aggregate-write: write failed\n" + aggregateError + "\n" + collision,
			wantFiles:       []string{opencodeFile},
			wantOtherErrors: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFiles, gotOtherErrors := syncCheckCollisions(tt.output, repoRoot)
			if len(gotFiles) != len(tt.wantFiles) {
				t.Fatalf("files = %v, want %v", gotFiles, tt.wantFiles)
			}
			for i := range tt.wantFiles {
				if gotFiles[i] != tt.wantFiles[i] {
					t.Fatalf("files = %v, want %v", gotFiles, tt.wantFiles)
				}
			}
			if gotOtherErrors != tt.wantOtherErrors {
				t.Fatalf("otherErrors = %t, want %t", gotOtherErrors, tt.wantOtherErrors)
			}
		})
	}
}

func TestSyncCheckFiles(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	outputDir := t.TempDir()
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "target header",
			header: "→ mcp (" + outputDir + ")",
			want:   filepath.Join(outputDir, "opencode.jsonc"),
		},
		{
			name:   "bare path header",
			header: "→ " + outputDir,
			want:   filepath.Join(outputDir, "opencode.jsonc"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := syncCheckFiles(tt.header + "\n  + opencode.jsonc")
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("files = %v, want [%s]", got, tt.want)
			}
		})
	}
}

// spec: doctor-repair-planning/repair-exits-one-when-only-blocked-or-human-only-items-remain
func TestDoctorRepairKeepsOwnershipAdoptionAvailableAfterSyncCheckError(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "mcp.json"), `{"mcpServers":{"adoptable":{"command":"adoptable-binary"}}}`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [devin, codex]\nsources:\n"+
			"  local: {directory: "+filepath.ToSlash(source)+"}\nresources:\n  local: {include: [mcp/adoptable]}\n")
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	codexPath := filepath.Join(repoRoot, ".codex", "config.toml")
	const corruptAggregate = "mcp_servers = [\n"
	if err := os.WriteFile(codexPath, []byte(corruptAggregate), 0o600); err != nil {
		t.Fatal(err)
	}
	devinStore, err := ownership.Open(filepath.Join(repoRoot, ".devin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(devinStore.StateFile()); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("doctor", "--repair", "--yes")
	if err == nil {
		t.Fatalf("corrupt aggregate must remain an error:\n%s", output)
	}
	firstPlan, _, _ := strings.Cut(output, "\nrepair:")
	if strings.Count(firstPlan, "sync drift diagnosis") != 1 ||
		!strings.Contains(firstPlan, "adopt exact shared-aggregate matches") || strings.Contains(firstPlan, "blocked by") {
		t.Fatalf("sync-check failure blocked the runnable ownership step:\n%s", firstPlan)
	}
	if !strings.Contains(output, "human-only") || !strings.Contains(output, "materialize.codex.aggregate-malformed") ||
		!strings.Contains(output, `adopt mcp-server "adoptable"`) || !strings.Contains(output, "adopted 1 entr(y/ies) in mcp_config.json") {
		t.Fatalf("repair output is incomplete:\n%s", output)
	}
	if got, err := os.ReadFile(codexPath); err != nil || string(got) != corruptAggregate {
		t.Fatalf("repair changed corrupt aggregate: %v\n%s", err, got)
	}
}

// spec: owned-entry-reconciliation/organization-acquisition-failure-refuses-repair
func TestDoctorRepairListsOrganizationAcquisitionBlockers(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, home := devinDoctorEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+server.URL+"}\nresources:\n  hosted: {include: [tools/skill/review]}\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	mcpPath, before := filepath.Join(repoRoot, ".devin", harness.DevinMcpConfigFile), []byte(`{"mcpServers":{"preserve":{"command":"manual"}}}`)
	writeFile(t, mcpPath, string(before))
	var out bytes.Buffer
	if err := runDoctor(doctorOptions{out: &out, repair: true}); err == nil ||
		!strings.Contains(out.String(), "acquire transient organization Marketplace lock") {
		t.Fatalf("repair did not report organization acquisition failure: %v\n%s", err, out.String())
	}
	if strings.Count(out.String(), "blocked by") != 2 || !strings.Contains(out.String(), "sync harness drift") ||
		!strings.Contains(out.String(), "reconcile shared-aggregate ownership") {
		t.Fatalf("repair did not list both blocked selection steps:\n%s", out.String())
	}
	if after, err := os.ReadFile(mcpPath); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed acquisition changed the aggregate: %v\n%s", err, after)
	}
	if _, err := os.Stat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("failed acquisition created an organization lock: %v", err)
	}
}

// spec: doctor-repair-planning/invalid-configuration-blocks-only-the-steps-that-need-it
// spec: doctor-repair-planning/repair-exits-one-when-only-blocked-or-human-only-items-remain
func TestDoctorRepairListsInvalidConfigurationAsHumanOnly(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [\n")
	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out:    &out,
		in:     strings.NewReader(""),
		repair: true,
		probe:  staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve([]resolver.ResolvedArtifact{
			devinMcpArtifact("managed", "managed-binary"),
		}),
	})
	if err == nil {
		t.Fatal("invalid configuration repair exited successfully")
	}
	if !strings.Contains(out.String(), "human-only") || !strings.Contains(out.String(), "blocked by") {
		t.Fatalf("invalid configuration was not represented in the plan:\n%s", out.String())
	}
}

// spec: doctor-repair-planning/failed-resolution-blocks-selection-dependent-steps
func TestDoctorRepairListsResolutionFailureAndBlockedSteps(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, _ := prepareDevinRepairFixture(t, repoRoot)
	aggregateBefore, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("fixture unexpectedly has an acquisition lock: %v", err)
	}
	want := errors.New("test resolution unavailable")
	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out:    &out,
		in:     strings.NewReader(""),
		repair: true,
		probe:  staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: func(context.Context, string) ([]resolver.ResolvedArtifact, error) {
			return nil, want
		},
	})
	if err == nil {
		t.Fatal("runDoctor exited successfully with unresolved selection")
	}
	if !strings.Contains(out.String(), "human-only") || !strings.Contains(out.String(), "blocked by") || !strings.Contains(out.String(), want.Error()) {
		t.Fatalf("resolution failure was not represented in the plan:\n%s", out.String())
	}
	wantBlockedLine := "  2. [safe] sync harness drift (ownership.entry-drift; files: selected harness outputs; blocked by 1 (selection unresolved))"
	if !strings.Contains(out.String(), wantBlockedLine) {
		t.Fatalf("resolution failure plan missing blocked step %q:\n%s", wantBlockedLine, out.String())
	}
	if after, err := os.ReadFile(mcpPath); err != nil || !bytes.Equal(aggregateBefore, after) {
		t.Fatalf("failed resolution changed aggregate bytes: %v\n%s", err, after)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("failed resolution created an acquisition lock: %v", err)
	}
}

// spec: doctor-repair-planning/policy-change-before-a-confirmed-step-stops-repair
func TestDoctorRepairRevalidatesPolicyAfterPlanConfirmation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {}\n")
	selection := prepareDevinRepairFixtureOwned(t, repoRoot)
	mcpPath := filepath.Join(repoRoot, ".devin", harness.DevinMcpConfigFile)
	generated, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	withUnownedMatch := strings.Replace(string(generated), `"mcpServers": {`,
		`"mcpServers": {"adoptable": {"command": "adoptable-binary"},`, 1)
	if withUnownedMatch == string(generated) {
		t.Fatalf("fixture did not add an unowned matching entry:\n%s", generated)
	}
	if err := os.WriteFile(mcpPath, []byte(withUnownedMatch), 0o644); err != nil {
		t.Fatal(err)
	}
	selection = append(selection, devinMcpArtifact("adoptable", "adoptable-binary"))
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(filepath.Join(repoRoot, ".devin"))
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(store.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	input := doctorPlannerReadFunc(func(p []byte) (int, error) {
		if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [codex]\nsources: {}\n"), 0o644); err != nil {
			return 0, err
		}
		return strings.NewReader("y\n").Read(p)
	})
	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out:     &out,
		in:      input,
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("repair error = %v, want policy generation conflict\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "policy changed") {
		t.Fatalf("repair output omitted policy change:\n%s", out.String())
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("policy change mutated aggregate bytes: %v\n%s", err, after)
	}
	stateAfter, err := os.ReadFile(store.StateFile())
	if err != nil || !bytes.Equal(stateBefore, stateAfter) {
		t.Fatalf("policy change mutated ownership-state bytes: %v\n%s", err, stateAfter)
	}
}

type doctorPlannerReadFunc func([]byte) (int, error)

func (read doctorPlannerReadFunc) Read(p []byte) (int, error) { return read(p) }
