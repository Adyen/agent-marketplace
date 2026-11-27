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
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

// spec: target-profile-place/read-only-commands-refuse-a-pending-place-journal-without-replay
func TestReadOnlyCommandsRefusePendingPlaceJournal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture, binding, marker, targetPath, ownershipPath, generationKey := newInstalledTargetVerificationFixture(t)
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	plan := placeRecoveryFixturePlan(t, binding, targetPath, ownershipPath, generationKey)
	journalPath := interruptPlaceRecoveryFixture(t, stateDir, plan, dependencytxn.FaultPlaceAfterStage)

	targetBefore := snapshotTargetStateTree(t, fixture.targetRoot)
	stateBefore := snapshotTargetStateTree(t, fixture.stateRoot)
	configBefore := snapshotTargetStateTree(t, fixture.userConfig)
	lockBefore := snapshotTargetStateTree(t, fixture.userLock)
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "status", args: []string{"status"}},
		{name: "sync check", args: []string{"sync", "--check"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := executeCommand(test.args...)
			if err == nil || !strings.Contains(err.Error(), "dependency.recovery-required") ||
				!strings.Contains(err.Error(), journalPath) {
				t.Fatalf("%v = %v\n%s; want dependency.recovery-required naming %s", test.args, err, output, journalPath)
			}
			assertTargetStateTreeUnchanged(t, targetBefore, fixture.targetRoot)
			assertTargetStateTreeUnchanged(t, stateBefore, fixture.stateRoot)
			assertTargetStateTreeUnchanged(t, configBefore, fixture.userConfig)
			assertTargetStateTreeUnchanged(t, lockBefore, fixture.userLock)
			if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%v ran host verification: %v", test.args, err)
			}
		})
	}
}

// spec: target-profile-place/crash-recovery-leaves-verification-pending-distinct-from-edits
func TestPlaceRecoveryLeavesVerificationPendingDespiteDrift(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture, binding, marker, targetPath, ownershipPath, generationKey := newInstalledTargetVerificationFixture(t)
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	files, _ := resolvedTargetApprovalPackage(t, fixture)
	if len(files) != 1 {
		t.Fatalf("fixture package files = %d, want one", len(files))
	}
	plan := placeRecoveryFixturePlan(t, binding, targetPath, ownershipPath, generationKey)
	if err := os.Remove(targetPath); err != nil {
		t.Fatalf("remove fixture-owned target leaf: %v", err)
	}
	if err := os.Remove(ownershipPath); err != nil {
		t.Fatalf("remove fixture-owned ownership leaf: %v", err)
	}
	plan.Writes[0].Expected = dependencytxn.Generation{}
	plan.Writes[0].ExpectedMode = 0
	plan.Ownership.Expected = dependencytxn.Generation{}
	plan.Ownership.ExpectedMode = 0
	enabledConfig := bytes.Replace(
		fixture.userConfigBytes,
		[]byte("apiVersion: agent-marketplace/v4\n"),
		[]byte("apiVersion: agent-marketplace/v4\ntargets: [invented-app]\n"),
		1,
	)
	if bytes.Equal(enabledConfig, fixture.userConfigBytes) {
		t.Fatal("fixture user config did not contain its v4 root version")
	}
	if err := os.WriteFile(fixture.userConfig, enabledConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	journalPath := interruptPlaceRecoveryFixture(t, stateDir, plan, dependencytxn.FaultPlaceAfterPublish)
	if err := assertRecoveredTargetFile(targetPath, files[0].Bytes, files[0].Mode); err != nil {
		t.Fatalf("published target before recovery = %v", err)
	}
	if _, err := os.Lstat(ownershipPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ownership was published before its journal operation: %v", err)
	}
	pending := dependencytxn.CheckPending(stateDir)
	if !errors.Is(pending, dependencytxn.ErrRecoveryRequired) || !strings.Contains(pending.Error(), journalPath) {
		t.Fatalf("pending place recovery = %v, want journal %s", pending, journalPath)
	}

	if err := dependencytxn.Recover(context.Background(), stateDir); err != nil {
		t.Fatalf("recover published place write: %v", err)
	}
	if _, err := os.Lstat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery retained place journal: %v", err)
	}
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		t.Fatalf("journal remains after recovery: %v", err)
	}
	if err := assertRecoveredTargetFile(targetPath, files[0].Bytes, files[0].Mode); err != nil {
		t.Fatalf("recovered target = %v", err)
	}
	if data, err := os.ReadFile(ownershipPath); err != nil || len(data) == 0 {
		t.Fatalf("recovered ownership is unreadable: %v", err)
	}
	stateAfterRecovery := snapshotTargetStateTree(t, fixture.stateRoot)
	assertPendingStatus := func(wantDrift bool) {
		t.Helper()
		output, err := executeCommand("status")
		if err != nil {
			t.Fatalf("status after recovery = %v\n%s", err, output)
		}
		section := statusSection(output, "PLACE PACKAGES")
		rows := strings.Split(strings.TrimSpace(section), "\n")
		if len(rows) != 2 || !strings.Contains(rows[0], "PACKAGE") ||
			!strings.Contains(rows[1], "invented-app/mod/sample") ||
			!strings.Contains(rows[1], "verification-pending") || strings.Contains(rows[1], "failed") {
			t.Fatalf("place package status rows = %q, want one pending sample row", section)
		}
		if wantDrift {
			if !strings.Contains(rows[1], "mods/sample/guide.md") || strings.Contains(rows[1], "in-sync") {
				t.Fatalf("drifted pending package row = %q", rows[1])
			}
		} else if !strings.Contains(rows[1], "in-sync") || strings.Contains(rows[1], "mods/sample/guide.md") {
			t.Fatalf("clean pending package row = %q", rows[1])
		}
		assertTargetStateTreeUnchanged(t, stateAfterRecovery, fixture.stateRoot)
		if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("status ran host verification: %v", err)
		}
	}
	assertPendingStatus(false)

	if err := os.WriteFile(targetPath, []byte("edited after recovery\n"), files[0].Mode); err != nil {
		t.Fatal(err)
	}
	assertPendingStatus(true)
}

func assertRecoveredTargetFile(path string, expected []byte, mode os.FileMode) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, expected) || info.Mode().Perm() != mode {
		return errors.New("target bytes or mode differ from expected package file")
	}
	return nil
}

func placeRecoveryFixturePlan(
	t *testing.T,
	binding targetprofile.ApprovalBinding,
	targetPath, ownershipPath, generationKey string,
) dependencytxn.PlacePlan {
	t.Helper()
	targetBytes, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	targetGeneration, targetMode, err := dependencytxn.ReadGenerationAndMode(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	ownershipBytes, err := os.ReadFile(ownershipPath)
	if err != nil {
		t.Fatal(err)
	}
	ownershipGeneration, ownershipMode, err := dependencytxn.ReadGenerationAndMode(ownershipPath)
	if err != nil {
		t.Fatal(err)
	}
	root, err := fsboundary.OpenDir(binding.CanonicalRoot)
	if err != nil {
		t.Fatal(err)
	}
	rootIdentity := root.Identity()
	parent, err := root.OpenPath(filepath.Dir(filepath.FromSlash("mods/sample/guide.md")))
	if err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	parentIdentity := parent.Identity()
	if err := parent.Close(); err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	return dependencytxn.PlacePlan{
		Root:         binding.CanonicalRoot,
		RootIdentity: rootIdentity,
		Writes: []dependencytxn.PlaceWrite{{
			Path: "mods/sample/guide.md", GenerationKey: generationKey,
			SourcePath: "payload/guide.md", Expected: targetGeneration,
			ExpectedMode: targetMode, Digest: dependencytxn.Digest(targetBytes),
			Mode: targetMode, Size: int64(len(targetBytes)), Parent: &parentIdentity,
		}},
		Ownership: dependencytxn.DesiredFile{
			Path: ownershipPath, Expected: ownershipGeneration, ExpectedMode: ownershipMode,
			Bytes: ownershipBytes, Mode: ownershipMode,
		},
	}
}

func interruptPlaceRecoveryFixture(
	t *testing.T,
	stateDir string,
	plan dependencytxn.PlacePlan,
	stage dependencytxn.FaultStage,
) string {
	t.Helper()
	stopAtStage := errors.New("interrupt place transaction")
	_, err := dependencytxn.CommitPlace(
		context.Background(), stateDir, plan,
		dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
			if point.Stage == stage && point.Index == 0 {
				return stopAtStage
			}
			return nil
		}),
	)
	if !errors.Is(err, stopAtStage) {
		t.Fatalf("CommitPlace fault at %s = %v, want interruption sentinel", stage, err)
	}
	journals, err := filepath.Glob(filepath.Join(stateDir, ".dependencytxn-*.json"))
	if err != nil || len(journals) != 1 {
		t.Fatalf("pending place journals = %v, %v; want exactly one", journals, err)
	}
	return journals[0]
}
