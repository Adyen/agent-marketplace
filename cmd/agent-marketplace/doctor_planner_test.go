package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = runDoctor(doctorOptions{
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
	for _, want := range []string{"plan", "1. [safe]", "adopt", "ownership.unmanaged-key", "[y/N]"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("repair plan missing %q:\n%s", want, out.String())
		}
	}
	if strings.Count(out.String(), "[y/N]") != 1 {
		t.Fatalf("plan asked %d confirmations, want 1:\n%s", strings.Count(out.String(), "[y/N]"), out.String())
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("declined plan changed aggregate bytes: %v\n%s", err, after)
	}
	if !devinRepairPreview(t, repoRoot, selection).Actionable() {
		t.Fatal("declined plan recorded ownership")
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
	_, _ = devinDoctorEnv(t)
	want := errors.New("test resolution unavailable")
	var out bytes.Buffer
	err := runDoctor(doctorOptions{
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
}

// spec: doctor-repair-planning/policy-change-before-a-confirmed-step-stops-repair
func TestDoctorRepairRevalidatesPolicyAfterPlanConfirmation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {}\n")
	mcpPath, selection := prepareDevinRepairFixture(t, repoRoot)
	before, err := os.ReadFile(mcpPath)
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
}

type doctorPlannerReadFunc func([]byte) (int, error)

func (read doctorPlannerReadFunc) Read(p []byte) (int, error) { return read(p) }
