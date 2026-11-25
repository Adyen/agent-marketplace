package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
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
	plan := dependencytxn.PlacePlan{
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
	stopAfterStage := errors.New("stop after staging first write")
	_, err = dependencytxn.CommitPlace(
		context.Background(), stateDir, plan,
		dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
			if point.Stage == dependencytxn.FaultPlaceAfterStage && point.Index == 0 {
				return stopAfterStage
			}
			return nil
		}),
	)
	if !errors.Is(err, stopAfterStage) {
		t.Fatalf("CommitPlace fault = %v, want staging sentinel", err)
	}
	journals, err := filepath.Glob(filepath.Join(stateDir, ".dependencytxn-*.json"))
	if err != nil || len(journals) != 1 {
		t.Fatalf("pending place journals = %v, %v; want exactly one", journals, err)
	}
	journalPath := journals[0]

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
