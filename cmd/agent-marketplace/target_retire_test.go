package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

func TestTargetRetireRequiresTerminal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newTargetApprovalFixture(t)
	id := strings.Repeat("a", 64)
	for _, test := range []struct {
		name string
		run  func() (string, error)
	}{
		{name: "non-terminal", run: func() (string, error) {
			return executeCommand("target", "retire", id)
		}},
		{name: "yes", run: func() (string, error) {
			_, stderr, err := executeTargetApprovalAtTerminal(t, "yes\n", nil, "target", "retire", id, "--yes")
			return stderr, err
		}},
		{name: "explicit false yes", run: func() (string, error) {
			_, stderr, err := executeTargetApprovalAtTerminal(t, "yes\n", nil, "target", "retire", id, "--yes=false")
			return stderr, err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := test.run()
			if err == nil || !strings.Contains(err.Error(), "target.confirmation-required") {
				t.Fatalf("retire error = %v, want target.confirmation-required\n%s", err, output)
			}
			assertTargetApprovalFixtureUnchanged(t, fixture)
			if _, err := os.Stat(filepath.Join(fixture.targetRoot, "mods")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("terminal refusal changed target files: %v", err)
			}
		})
	}
}

func TestTargetRetirePreservesEditedAndUnownedFiles(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newTargetApprovalFixtureWithProfile(t, []byte(targetVerifyProfileYAML))
	hostDir := filepath.Join(fixture.home, "bin")
	if err := os.Mkdir(hostDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(fixture.home, "retire-host-invoked")
	hostPath := filepath.Join(hostDir, "target-verify-fixture")
	hostScript := "#!/bin/sh\nprintf invoked >> " + targetVerifyShellQuote(marker) + "\nexit 0\n"
	if err := os.WriteFile(hostPath, []byte(hostScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", hostDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	warmTargetApprovalProfileCache(t, fixture)
	binding, err := pipeline.PrepareDeployedPlaceApproval(
		context.Background(), mustTargetWorkspace(t, fixture), fixture.identity.TargetID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := targetprofile.RecordApproval(binding); err != nil {
		t.Fatal(err)
	}
	files := []targetprofile.PackageFile{
		{SourcePath: "payload/keep.md", DestinationPath: "mods/sample/keep.md", Mode: 0o644, Bytes: []byte("keep\n")},
		{SourcePath: "payload/edit.md", DestinationPath: "mods/sample/edit.md", Mode: 0o644, Bytes: []byte("owned\n")},
	}
	_, pkg := resolvedTargetApprovalPackageWithFiles(t, fixture, files)
	if _, err := pipeline.CommitPlaceUpserts(
		context.Background(), binding, fixture.profileBytes, targetApprovalEnvironment(t, fixture),
		[]marketplace.ResolvedPlacePackage{pkg},
	); err != nil {
		t.Fatal(err)
	}
	rootKeep := filepath.Join(fixture.targetRoot, "keep.txt")
	rootKeepBefore, err := os.ReadFile(rootKeep)
	if err != nil {
		t.Fatal(err)
	}
	editPath := filepath.Join(fixture.targetRoot, "mods", "sample", "edit.md")
	editBytes := []byte("operator edit\n")
	if err := os.WriteFile(editPath, editBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(fixture.targetRoot, "mods", "sample", "user.md")
	userBytes := []byte("user-owned\n")
	if err := os.WriteFile(userPath, userBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	receiptPaths, err := filepath.Glob(filepath.Join(fixture.stateRoot, "target-approval-*.json"))
	if err != nil || len(receiptPaths) != 1 {
		t.Fatalf("approval receipts = %v, %v", receiptPaths, err)
	}
	receiptBefore, err := os.ReadFile(receiptPaths[0])
	if err != nil {
		t.Fatal(err)
	}

	newRoot := filepath.Join(fixture.home, ".config", "changed-invented-app")
	if err := os.MkdirAll(newRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	newRootKeep := filepath.Join(newRoot, "keep.txt")
	if err := os.WriteFile(newRootKeep, []byte("new root remains untouched\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, retireErr := executeTargetApprovalAtTerminal(t, "yes\n", func() {
		t.Setenv("INVENTED_APP_HOME", newRoot)
	}, "target", "retire", pipeline.PlaceInstallationID(binding))
	if retireErr != nil {
		t.Fatalf("retire recorded installation: %v\nstdout=%s\nstderr=%s", retireErr, stdout, stderr)
	}
	if !strings.Contains(stderr, fixture.targetRoot) || !strings.Contains(stderr, fixture.stateRoot) ||
		!strings.Contains(stderr, "mods/sample/keep.md") || !strings.Contains(stderr, "mods/sample/edit.md") {
		t.Fatalf("retirement preview omitted recorded context: %s", stderr)
	}
	if !strings.Contains(stdout, "mods/sample/keep.md") || !strings.Contains(stdout, "mods/sample/edit.md") {
		t.Fatalf("retirement result omitted removed or ceded paths: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(fixture.targetRoot, "mods", "sample", "keep.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unchanged owned file remains after retirement: %v", err)
	}
	for path, expected := range map[string][]byte{editPath: editBytes, userPath: userBytes, rootKeep: rootKeepBefore, newRootKeep: []byte("new root remains untouched\n")} {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("preserved file %s = %q, %v; want %q", path, got, err, expected)
		}
	}
	if got, err := os.ReadFile(fixture.userConfig); err != nil || !bytes.Equal(got, fixture.userConfigBytes) {
		t.Fatalf("retirement changed user config: %q, %v", got, err)
	}
	if got, err := os.ReadFile(fixture.userLock); err != nil || !bytes.Equal(got, fixture.userLockBytes) {
		t.Fatalf("retirement changed deployed lock: %v", err)
	}
	if got, err := os.ReadFile(receiptPaths[0]); err != nil || !bytes.Equal(got, receiptBefore) {
		t.Fatalf("retirement changed approval receipt: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retirement ran host command: %v", err)
	}
	store, err := ownership.Open(binding.CanonicalRoot, ownership.WithStateRoot(binding.CanonicalStateRoot))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	projection, err := store.PlaceFromSnapshot(snapshot)
	if err != nil || len(projection.Packages) != 0 {
		t.Fatalf("retirement ownership projection = %#v, %v", projection, err)
	}
}

func TestTargetRetireRefusesStateRootReplacementAfterPrompt(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture, binding, marker, targetPath, statePath, _ := newInstalledTargetVerificationFixture(t)
	originalRecord, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	recordRel, err := filepath.Rel(fixture.stateRoot, statePath)
	if err != nil {
		t.Fatal(err)
	}
	movedRoot := fixture.stateRoot + ".moved"
	stdout, stderr, retireErr := executeTargetApprovalAtTerminal(t, "yes\n", func() {
		if err := os.Rename(fixture.stateRoot, movedRoot); err != nil {
			t.Fatal(err)
		}
		newRecordPath := filepath.Join(fixture.stateRoot, recordRel)
		if err := os.MkdirAll(filepath.Dir(newRecordPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(newRecordPath, originalRecord, 0o600); err != nil {
			t.Fatal(err)
		}
	}, "target", "retire", pipeline.PlaceInstallationID(binding))
	if retireErr == nil || !strings.Contains(retireErr.Error(), "target.place.retirement-context-changed") || stdout != "" {
		t.Fatalf("replacement state root retirement = %v, stdout=%q, stderr=%q", retireErr, stdout, stderr)
	}
	if got, err := os.ReadFile(targetPath); err != nil || len(got) == 0 {
		t.Fatalf("state-root replacement removed installed file: %q, %v", got, err)
	}
	for _, path := range []string{filepath.Join(movedRoot, recordRel), filepath.Join(fixture.stateRoot, recordRel)} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, originalRecord) {
			t.Fatalf("ownership record %s changed: %v", path, err)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retirement ran host command: %v", err)
	}
}
