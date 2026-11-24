package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestCommitPlaceUpsertsRequiresReceiptAndPublishes(t *testing.T) {
	t.Run("approval is required before writes", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		pkg := resolvedPlacePackageForPublish(t, fixture)
		if result, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg}); err == nil ||
			!strings.Contains(err.Error(), "target.approval.required") {
			t.Fatalf("CommitPlaceUpserts without receipt = %#v, %v", result, err)
		}
		if entries, err := os.ReadDir(fixture.root); err != nil || len(entries) != 0 {
			t.Fatalf("missing-receipt target root changed: %v, %v", entries, err)
		}
		if entries, err := os.ReadDir(fixture.state); err != nil || len(entries) != 0 {
			t.Fatalf("missing-receipt state root changed: %v, %v", entries, err)
		}
	})

	t.Run("unowned path is preserved", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		if err := targetprofile.RecordApproval(fixture.binding); err != nil {
			t.Fatal(err)
		}
		targetPath := filepath.Join(fixture.root, "mods", "sample", "guide.md")
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			t.Fatal(err)
		}
		userBytes := []byte("user-owned bytes\n")
		if err := os.WriteFile(targetPath, userBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		pkg := resolvedPlacePackageForPublish(t, fixture)
		if result, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg}); err == nil {
			t.Fatalf("CommitPlaceUpserts over user file = %#v, want refusal", result)
		}
		got, err := os.ReadFile(targetPath)
		info, statErr := os.Stat(targetPath)
		if err != nil || statErr != nil || !bytes.Equal(got, userBytes) || info.Mode().Perm() != 0o600 {
			t.Fatalf("user file changed: bytes=%q stat=%v read=%v statErr=%v", got, info, err, statErr)
		}
		assertPlacePublishPathAbsent(t, filepath.Join(fixture.state, "ownership"))
		assertPlacePublishPathAbsent(t, filepath.Join(fixture.state, "packages"))
	})

	t.Run("pre-existing parent proof survives a no-op upsert", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		if err := targetprofile.RecordApproval(fixture.binding); err != nil {
			t.Fatal(err)
		}
		parentPath := filepath.Join(fixture.root, "mods", "sample")
		if err := os.MkdirAll(parentPath, 0o755); err != nil {
			t.Fatal(err)
		}
		root, err := fsboundary.OpenDir(fixture.binding.CanonicalRoot)
		if err != nil {
			t.Fatal(err)
		}
		parent, err := root.OpenPath(filepath.Join("mods", "sample"))
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

		pkg := resolvedPlacePackageForPublish(t, fixture)
		if result, err := CommitPlaceUpserts(
			context.Background(), fixture.binding, fixture.raw, fixture.env,
			[]marketplace.ResolvedPlacePackage{pkg},
		); err != nil || len(result.Changed) == 0 {
			t.Fatalf("initial upsert = %#v, %v", result, err)
		}
		owner, snapshot := openPlacePublishOwnership(t, fixture)
		projection, err := owner.PlaceFromSnapshot(snapshot)
		if err != nil || len(projection.Packages) != 1 || len(projection.Packages[0].Files) != 1 ||
			projection.Packages[0].Files[0].Parent == nil ||
			!projection.Packages[0].Files[0].Parent.SameObject(parentIdentity) {
			t.Fatalf("installed file parent proof = %#v, %v", projection, err)
		}
		stateBefore, err := os.ReadFile(owner.StateFile())
		if err != nil {
			t.Fatal(err)
		}
		stateInfoBefore, err := os.Stat(owner.StateFile())
		if err != nil {
			t.Fatal(err)
		}
		result, err := CommitPlaceUpserts(
			context.Background(), fixture.binding, fixture.raw, fixture.env,
			[]marketplace.ResolvedPlacePackage{pkg},
		)
		snapshot, snapshotErr := owner.SnapshotState()
		after, projectionErr := owner.PlaceFromSnapshot(snapshot)
		stateAfter, readErr := os.ReadFile(owner.StateFile())
		stateInfoAfter, statErr := os.Stat(owner.StateFile())
		if err != nil || snapshotErr != nil || projectionErr != nil || readErr != nil || statErr != nil ||
			len(result.Changed) != 0 || !bytes.Equal(stateBefore, stateAfter) ||
			!stateInfoBefore.ModTime().Equal(stateInfoAfter.ModTime()) ||
			after.Packages[0].Files[0].Parent == nil ||
			!after.Packages[0].Files[0].Parent.SameObject(parentIdentity) {
			t.Fatalf("no-op upsert changed or lost parent proof: result=%#v err=%v snapshot=%v projection=%v read=%v stat=%v",
				result, err, snapshotErr, projectionErr, readErr, statErr)
		}
	})

	t.Run("missing parent is marked for transaction creation", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		if err := targetprofile.RecordApproval(fixture.binding); err != nil {
			t.Fatal(err)
		}
		pkg := resolvedPlacePackageForPublish(t, fixture)
		prepared, err := preparePlaceUpserts(
			context.Background(), fixture.binding, fixture.raw, fixture.env,
			[]marketplace.ResolvedPlacePackage{pkg},
		)
		if err != nil || !prepared.changed || len(prepared.plan.Writes) != 1 {
			t.Fatalf("prepared missing-parent upsert = %#v, %v", prepared, err)
		}
		write := prepared.plan.Writes[0]
		if write.Parent != nil || !write.ParentCreated {
			t.Fatalf("missing-parent write proof = parent %#v, parent-created %t", write.Parent, write.ParentCreated)
		}
	})

	t.Run("oversized package file is refused before generation", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		if err := targetprofile.RecordApproval(fixture.binding); err != nil {
			t.Fatal(err)
		}
		pkg := resolvedPlacePackageForPublishWithFiles(t, fixture, []targetprofile.PackageFile{{
			SourcePath: "payload/guide.md", DestinationPath: "mods/sample/guide.md",
			Mode: 0o644, Bytes: bytes.Repeat([]byte("x"), int(dependencytxn.PlaceFileLimit)+1),
		}})
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg}); err == nil ||
			!strings.Contains(err.Error(), "placement file limit") {
			t.Fatalf("oversized place package error = %v", err)
		}
		assertPlacePublishPathAbsent(t, filepath.Join(fixture.state, "packages"))
		assertPlacePublishPathAbsent(t, filepath.Join(fixture.state, "ownership"))
	})

	t.Run("approved upsert is stable and guards revalidation", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		if err := targetprofile.RecordApproval(fixture.binding); err != nil {
			t.Fatal(err)
		}
		pkg := resolvedPlacePackageForPublish(t, fixture)
		result, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg})
		if err != nil {
			t.Fatalf("CommitPlaceUpserts: %v", err)
		}
		targetPath := filepath.Join(fixture.root, "mods", "sample", "guide.md")
		assertPlacePublishFile(t, targetPath, pkg.Files[0].Bytes, pkg.Files[0].Mode)
		if len(result.Changed) == 0 {
			t.Fatalf("first commit changed no paths: %#v", result)
		}

		store, snapshot := openPlacePublishOwnership(t, fixture)
		projection, err := store.PlaceFromSnapshot(snapshot)
		if err != nil || len(projection.Packages) != 1 ||
			projection.Packages[0].Binding != pkg.Pin.LockHostPackageIdentity {
			t.Fatalf("stored place projection = %#v, %v", projection, err)
		}
		statePath := store.StateFile()
		stateBefore, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		targetBefore, err := os.Stat(targetPath)
		if err != nil {
			t.Fatal(err)
		}
		stateBeforeInfo, err := os.Stat(statePath)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg}); err != nil || len(result.Changed) != 0 {
			t.Fatalf("repeat CommitPlaceUpserts = %#v, %v", result, err)
		}
		targetAfter, err := os.Stat(targetPath)
		stateAfter, readErr := os.ReadFile(statePath)
		stateAfterInfo, statErr := os.Stat(statePath)
		if err != nil || readErr != nil || statErr != nil ||
			!targetBefore.ModTime().Equal(targetAfter.ModTime()) ||
			!stateBeforeInfo.ModTime().Equal(stateAfterInfo.ModTime()) ||
			!bytes.Equal(stateBefore, stateAfter) {
			t.Fatalf("repeat upsert rewrote state/output: target=%v state=%v read=%v stat=%v", targetAfter, stateAfterInfo, readErr, statErr)
		}

		transactionState := filepath.Join(fixture.state, "dependencies")
		if err := os.MkdirAll(transactionState, 0o700); err != nil {
			t.Fatal(err)
		}
		pendingJournal := filepath.Join(transactionState, ".dependencytxn-pending.json")
		if err := os.WriteFile(pendingJournal, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg}); !errors.Is(err, dependencytxn.ErrRecoveryRequired) {
			t.Fatalf("upsert with pending journal = %v, want ErrRecoveryRequired", err)
		}
		assertPlacePublishFile(t, targetPath, pkg.Files[0].Bytes, pkg.Files[0].Mode)
		if err := os.Remove(pendingJournal); err != nil {
			t.Fatal(err)
		}

		otherRoot := filepath.Join(fixture.env.Home, ".config", "other-invented-app")
		if err := os.MkdirAll(otherRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("INVENTED_APP_HOME", otherRoot)
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg}); err == nil ||
			!strings.Contains(err.Error(), "target.approval.root-mismatch") {
			t.Fatalf("changed override error = %v", err)
		}
		t.Setenv("INVENTED_APP_HOME", fixture.root)
		assertPlacePublishFile(t, targetPath, pkg.Files[0].Bytes, pkg.Files[0].Mode)
		if got, err := os.ReadFile(statePath); err != nil || !bytes.Equal(got, stateBefore) {
			t.Fatalf("override refusal changed ownership: %v", err)
		}

		changed := pkg
		changed.Files = append([]targetprofile.PackageFile(nil), pkg.Files...)
		changed.Files[0].Bytes = []byte("bytes no longer match the locked binding\n")
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{changed}); err == nil {
			t.Fatal("changed package bytes were accepted")
		}
		assertPlacePublishFile(t, targetPath, pkg.Files[0].Bytes, pkg.Files[0].Mode)
		if got, err := os.ReadFile(statePath); err != nil || !bytes.Equal(got, stateBefore) {
			t.Fatalf("package binding refusal changed ownership: %v", err)
		}
	})

	t.Run("same coordinate updates in place", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		if err := targetprofile.RecordApproval(fixture.binding); err != nil {
			t.Fatal(err)
		}
		first := resolvedPlacePackageForPublish(t, fixture)
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{first}); err != nil {
			t.Fatalf("commit initial package: %v", err)
		}
		store, snapshot := openPlacePublishOwnership(t, fixture)
		projection, err := store.PlaceFromSnapshot(snapshot)
		if err != nil || len(projection.Packages) != 1 {
			t.Fatalf("initial place projection = %#v, %v", projection, err)
		}
		packageID := projection.Packages[0].ID

		unrelatedPath := filepath.Join(fixture.root, "user", "keep.txt")
		if err := os.MkdirAll(filepath.Dir(unrelatedPath), 0o755); err != nil {
			t.Fatal(err)
		}
		unrelatedBytes := []byte("unrelated user data\n")
		if err := os.WriteFile(unrelatedPath, unrelatedBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		updated := resolvedPlacePackageForPublishWithFiles(t, fixture, []targetprofile.PackageFile{{
			SourcePath: "payload/guide.md", DestinationPath: "mods/sample/guide.md",
			Mode: 0o644, Bytes: []byte("updated fixture package\n"),
		}})
		if updated.Entry.InstanceID != first.Entry.InstanceID || updated.Pin.RegistryID != first.Pin.RegistryID ||
			updated.Pin.Kind != first.Pin.Kind || updated.Pin.Name != first.Pin.Name {
			t.Fatal("updated fixture changed its package coordinate")
		}
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{updated}); err != nil {
			t.Fatalf("update package: %v", err)
		}
		assertPlacePublishFile(t, filepath.Join(fixture.root, "mods", "sample", "guide.md"), updated.Files[0].Bytes, updated.Files[0].Mode)
		assertPlacePublishFile(t, unrelatedPath, unrelatedBytes, 0o600)
		store, snapshot = openPlacePublishOwnership(t, fixture)
		projection, err = store.PlaceFromSnapshot(snapshot)
		if err != nil || len(projection.Packages) != 1 || projection.Packages[0].ID != packageID ||
			projection.Packages[0].Binding != updated.Pin.LockHostPackageIdentity {
			t.Fatalf("updated place projection = %#v, %v; want stable package ID %s and new binding", projection, err, packageID)
		}
	})

	t.Run("verification survives identical upsert and clears on new generation", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		if err := targetprofile.RecordApproval(fixture.binding); err != nil {
			t.Fatal(err)
		}
		first := resolvedPlacePackageForPublish(t, fixture)
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{first}); err != nil {
			t.Fatalf("commit initial package: %v", err)
		}
		owner, snapshot := openPlacePublishOwnership(t, fixture)
		projection, err := owner.PlaceFromSnapshot(snapshot)
		if err != nil || len(projection.Packages) != 1 {
			t.Fatalf("initial projection = %#v, %v", projection, err)
		}
		verification := ownership.PlaceVerification{
			Binding: projection.Packages[0].Binding, GenerationKey: projection.Packages[0].GenerationKey,
			Check: fixture.identity.TargetID, Scope: "eager-package-load", HostVersionStatus: "unknown",
			Outcome: "pass", EvidenceSource: "initial-load",
		}
		desired, err := owner.PreparePlaceVerification(snapshot, projection.Packages[0].ID, verification)
		if err != nil {
			t.Fatal(err)
		}
		stateDir, err := dependencytxn.StateDir()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dependencytxn.Commit(context.Background(), stateDir, []dependencytxn.DesiredFile{desired}); err != nil {
			t.Fatalf("persist place verification: %v", err)
		}
		stateBefore, err := os.ReadFile(owner.StateFile())
		if err != nil {
			t.Fatal(err)
		}
		stateInfo, err := os.Stat(owner.StateFile())
		if err != nil {
			t.Fatal(err)
		}
		targetPath := filepath.Join(fixture.root, "mods", "sample", "guide.md")
		targetInfo, err := os.Stat(targetPath)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{first}); err != nil || len(result.Changed) != 0 {
			t.Fatalf("identical upsert after verification = %#v, %v", result, err)
		}
		currentSnapshot, err := owner.SnapshotState()
		if err != nil {
			t.Fatal(err)
		}
		verifiedProjection, err := owner.PlaceFromSnapshot(currentSnapshot)
		if err != nil || verifiedProjection.Packages[0].Verification == nil ||
			*verifiedProjection.Packages[0].Verification != verification {
			t.Fatalf("verified projection before no-op = %#v, %v", verifiedProjection, err)
		}
		stateAfter, err := os.ReadFile(owner.StateFile())
		stateAfterInfo, statErr := os.Stat(owner.StateFile())
		targetAfter, targetStatErr := os.Stat(targetPath)
		if err != nil || statErr != nil || targetStatErr != nil || !bytes.Equal(stateBefore, stateAfter) ||
			!stateInfo.ModTime().Equal(stateAfterInfo.ModTime()) || !targetInfo.ModTime().Equal(targetAfter.ModTime()) {
			t.Fatalf("identical upsert rewrote verification or package: read=%v stat=%v targetStat=%v", err, statErr, targetStatErr)
		}

		updated := resolvedPlacePackageForPublishWithFiles(t, fixture, []targetprofile.PackageFile{{
			SourcePath: "payload/guide.md", DestinationPath: "mods/sample/guide.md",
			Mode: 0o644, Bytes: []byte("new package generation\n"),
		}})
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{updated}); err != nil {
			t.Fatalf("commit updated package: %v", err)
		}
		updatedSnapshot, err := owner.SnapshotState()
		if err != nil {
			t.Fatal(err)
		}
		updatedProjection, err := owner.PlaceFromSnapshot(updatedSnapshot)
		if err != nil || len(updatedProjection.Packages) != 1 ||
			updatedProjection.Packages[0].Binding == projection.Packages[0].Binding ||
			updatedProjection.Packages[0].GenerationKey == projection.Packages[0].GenerationKey ||
			updatedProjection.Packages[0].Verification != nil {
			t.Fatalf("updated projection = %#v, %v; want changed binding and no verification", updatedProjection, err)
		}
		assertPlacePublishFile(t, targetPath, updated.Files[0].Bytes, updated.Files[0].Mode)
	})
}

func TestPlanPlaceRetirement(t *testing.T) {
	t.Run("prunes only unchanged owned files and cedes edits or absence", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		if err := targetprofile.RecordApproval(fixture.binding); err != nil {
			t.Fatal(err)
		}
		files := []targetprofile.PackageFile{
			{SourcePath: "payload/keep.md", DestinationPath: "mods/sample/keep.md", Mode: 0o644, Bytes: []byte("keep\n")},
			{SourcePath: "payload/edited.md", DestinationPath: "mods/sample/edited.md", Mode: 0o644, Bytes: []byte("owned\n")},
			{SourcePath: "payload/missing.md", DestinationPath: "mods/sample/missing.md", Mode: 0o644, Bytes: []byte("missing\n")},
		}
		pkg := resolvedPlacePackageForPublishWithFiles(t, fixture, files)
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg}); err != nil {
			t.Fatal(err)
		}
		userPath := filepath.Join(fixture.root, "mods", "sample", "user.txt")
		userBytes := []byte("user-owned\n")
		if err := os.WriteFile(userPath, userBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		editedPath := filepath.Join(fixture.root, "mods", "sample", "edited.md")
		if err := os.WriteFile(editedPath, []byte("operator edit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		missingPath := filepath.Join(fixture.root, "mods", "sample", "missing.md")
		if err := os.Remove(missingPath); err != nil {
			t.Fatal(err)
		}
		owner, snapshot := openPlacePublishOwnership(t, fixture)
		installed, err := owner.PlaceFromSnapshot(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		stateBefore, err := os.ReadFile(owner.StateFile())
		if err != nil {
			t.Fatal(err)
		}
		editedBefore, err := os.ReadFile(editedPath)
		if err != nil {
			t.Fatal(err)
		}
		keepPath := filepath.Join(fixture.root, "mods", "sample", "keep.md")
		keepBefore, err := os.ReadFile(keepPath)
		if err != nil {
			t.Fatal(err)
		}
		rootInfoBefore, err := os.Stat(fixture.root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := planPlaceRetirement(context.Background(), nil, installed, fixture.identity.TargetID); err == nil {
			t.Fatal("nil root was accepted")
		}

		root, err := fsboundary.OpenDir(fixture.binding.CanonicalRoot)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := planPlaceRetirement(context.Background(), root, installed, "not a target"); err == nil {
			t.Fatal("invalid target ID was accepted")
		}
		if _, err := planPlaceRetirement(context.Background(),
			root, ownership.PlaceProjection{Directories: installed.Directories}, fixture.identity.TargetID,
		); err == nil || !strings.Contains(err.Error(), "target.place.not-installed") {
			t.Fatalf("empty target projection error = %v", err)
		}
		survivor := installed.Packages[0]
		survivor.ID = strings.Repeat("0", 64)
		survivor.Kind = "other-app/mod"
		survivor.Name = "dependent"
		survivor.Binding.ProfileIdentity.TargetID = "other-app"
		survivor.Files = nil
		survivor.Requires = []string{installed.Packages[0].ID}
		withDependent := ownership.PlaceProjection{
			Packages:    append(append([]ownership.PlacePackage(nil), installed.Packages...), survivor),
			Directories: append([]dependencytxn.PlaceDirectory(nil), installed.Directories...),
		}
		if _, err := planPlaceRetirement(context.Background(), root, withDependent, fixture.identity.TargetID); err == nil ||
			!strings.Contains(err.Error(), "target.place.retirement-dependency") {
			t.Fatalf("surviving dependent error = %v", err)
		}
		planned, planErr := planPlaceRetirement(context.Background(), root, installed, fixture.identity.TargetID)
		closeErr := root.Close()
		if planErr != nil || closeErr != nil {
			t.Fatalf("planPlaceRetirement() = %#v, %v (close %v)", planned, planErr, closeErr)
		}
		var keepDigest string
		for _, file := range installed.Packages[0].Files {
			if file.Path == "mods/sample/keep.md" {
				keepDigest = strings.TrimPrefix(file.Digest, "sha256:")
			}
		}
		if len(planned.Prunes) != 1 || planned.Prunes[0].Path != "mods/sample/keep.md" ||
			!planned.Prunes[0].Expected.Exists || planned.Prunes[0].Expected.Digest != keepDigest ||
			planned.Prunes[0].Mode.Perm() != 0o644 {
			t.Fatalf("retirement prunes = %#v", planned.Prunes)
		}
		var recordedParent fsboundary.Identity
		for _, directory := range installed.Directories {
			if directory.Path == "mods/sample" {
				recordedParent = directory.Identity
			}
		}
		if !planned.Prunes[0].Parent.SameObject(recordedParent) || recordedParent.Inode == 0 {
			t.Fatalf("prune parent = %#v, recorded = %#v", planned.Prunes[0].Parent, recordedParent)
		}
		if !slices.Equal(planned.Ceded, []string{"mods/sample/edited.md", "mods/sample/missing.md"}) ||
			len(planned.Unproven) != 0 {
			t.Fatalf("ceded=%v unproven=%v", planned.Ceded, planned.Unproven)
		}
		if len(planned.Projection.Packages) != 0 ||
			!slices.Equal(planned.Projection.Directories, installed.Directories) {
			t.Fatalf("next projection = %#v; want packages removed and directories retained", planned.Projection)
		}
		for _, list := range [][]string{planned.Ceded, planned.Unproven} {
			if slices.Contains(list, "mods/sample/user.txt") {
				t.Fatalf("unowned file was classified: %v", list)
			}
		}
		stateAfter, stateErr := os.ReadFile(owner.StateFile())
		keepAfter, keepErr := os.ReadFile(keepPath)
		editedAfter, editedErr := os.ReadFile(editedPath)
		userAfter, userErr := os.ReadFile(userPath)
		if stateErr != nil || keepErr != nil || editedErr != nil || userErr != nil ||
			!bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(keepBefore, keepAfter) ||
			!bytes.Equal(editedBefore, editedAfter) || !bytes.Equal(userBytes, userAfter) {
			t.Fatalf("planning mutated state or files: state=%v keep=%v edited=%v user=%v",
				stateErr, keepErr, editedErr, userErr)
		}
		if _, err := os.Stat(missingPath); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("planning restored missing file: %v", err)
		}
		rootInfoAfter, err := os.Stat(fixture.root)
		if err != nil || !os.SameFile(rootInfoBefore, rootInfoAfter) {
			t.Fatalf("planning changed target root identity: before=%v after=%v err=%v", rootInfoBefore, rootInfoAfter, err)
		}
	})

	t.Run("same bytes under a replaced parent are unproven", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		if err := targetprofile.RecordApproval(fixture.binding); err != nil {
			t.Fatal(err)
		}
		pkg := resolvedPlacePackageForPublish(t, fixture)
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg}); err != nil {
			t.Fatal(err)
		}
		owner, snapshot := openPlacePublishOwnership(t, fixture)
		installed, err := owner.PlaceFromSnapshot(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		stateBefore, err := os.ReadFile(owner.StateFile())
		if err != nil {
			t.Fatal(err)
		}
		parent := filepath.Join(fixture.root, "mods", "sample")
		oldParent := parent + ".old"
		if err := os.Rename(parent, oldParent); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		file := pkg.Files[0]
		if err := os.WriteFile(filepath.Join(parent, "guide.md"), file.Bytes, file.Mode); err != nil {
			t.Fatal(err)
		}
		root, err := fsboundary.OpenDir(fixture.binding.CanonicalRoot)
		if err != nil {
			t.Fatal(err)
		}
		planned, planErr := planPlaceRetirement(context.Background(), root, installed, fixture.identity.TargetID)
		closeErr := root.Close()
		if planErr != nil || closeErr != nil {
			t.Fatalf("planPlaceRetirement() = %#v, %v (close %v)", planned, planErr, closeErr)
		}
		if len(planned.Prunes) != 0 ||
			!slices.Equal(planned.Unproven, []string{"mods/sample/guide.md"}) {
			t.Fatalf("replacement-parent plan = prunes %v, unproven %v", planned.Prunes, planned.Unproven)
		}
		got, readErr := os.ReadFile(filepath.Join(parent, "guide.md"))
		stateAfter, stateErr := os.ReadFile(owner.StateFile())
		if readErr != nil || stateErr != nil || !bytes.Equal(got, file.Bytes) || !bytes.Equal(stateBefore, stateAfter) {
			t.Fatalf("replacement-parent planning changed data: read=%v state=%v", readErr, stateErr)
		}
	})

	t.Run("same bytes under a replaced pre-existing parent are unproven", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		if err := targetprofile.RecordApproval(fixture.binding); err != nil {
			t.Fatal(err)
		}
		parent := filepath.Join(fixture.root, "mods", "sample")
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		pkg := resolvedPlacePackageForPublish(t, fixture)
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg}); err != nil {
			t.Fatal(err)
		}
		owner, snapshot := openPlacePublishOwnership(t, fixture)
		installed, err := owner.PlaceFromSnapshot(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if installed.Packages[0].Files[0].Parent == nil {
			t.Fatal("pre-existing directory install did not persist parent proof")
		}
		stateBefore, err := os.ReadFile(owner.StateFile())
		if err != nil {
			t.Fatal(err)
		}
		oldParent := parent + ".old"
		if err := os.Rename(parent, oldParent); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		file := pkg.Files[0]
		if err := os.WriteFile(filepath.Join(parent, "guide.md"), file.Bytes, file.Mode); err != nil {
			t.Fatal(err)
		}
		root, err := fsboundary.OpenDir(fixture.binding.CanonicalRoot)
		if err != nil {
			t.Fatal(err)
		}
		planned, planErr := planPlaceRetirement(context.Background(), root, installed, fixture.identity.TargetID)
		closeErr := root.Close()
		if planErr != nil || closeErr != nil {
			t.Fatalf("planPlaceRetirement() = %#v, %v (close %v)", planned, planErr, closeErr)
		}
		if len(planned.Prunes) != 0 ||
			!slices.Equal(planned.Unproven, []string{"mods/sample/guide.md"}) {
			t.Fatalf("replacement pre-existing parent plan = prunes %v, unproven %v", planned.Prunes, planned.Unproven)
		}
		got, readErr := os.ReadFile(filepath.Join(parent, "guide.md"))
		stateAfter, stateErr := os.ReadFile(owner.StateFile())
		if readErr != nil || stateErr != nil || !bytes.Equal(got, file.Bytes) || !bytes.Equal(stateBefore, stateAfter) {
			t.Fatalf("replacement pre-existing parent planning changed data: read=%v state=%v", readErr, stateErr)
		}
	})

	t.Run("pre-existing parent proof enables retirement and legacy files remain unproven", func(t *testing.T) {
		fixture := newPlaceAdmissionFixture(t)
		setPlacePublishEnvironment(t, fixture)
		if err := targetprofile.RecordApproval(fixture.binding); err != nil {
			t.Fatal(err)
		}
		parent := filepath.Join(fixture.root, "mods", "sample")
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		pkg := resolvedPlacePackageForPublish(t, fixture)
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg}); err != nil {
			t.Fatal(err)
		}
		owner, snapshot := openPlacePublishOwnership(t, fixture)
		installed, err := owner.PlaceFromSnapshot(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if len(installed.Packages) != 1 || len(installed.Packages[0].Files) != 1 ||
			installed.Packages[0].Files[0].Parent == nil {
			t.Fatalf("pre-existing parent proof was not persisted: %#v", installed)
		}
		root, err := fsboundary.OpenDir(fixture.binding.CanonicalRoot)
		if err != nil {
			t.Fatal(err)
		}
		planned, planErr := planPlaceRetirement(context.Background(), root, installed, fixture.identity.TargetID)
		legacy := installed
		legacy.Packages = append([]ownership.PlacePackage(nil), installed.Packages...)
		legacy.Packages[0].Files = append([]ownership.PlaceFile(nil), installed.Packages[0].Files...)
		legacy.Packages[0].Files[0].Parent = nil
		legacyPlanned, legacyErr := planPlaceRetirement(context.Background(), root, legacy, fixture.identity.TargetID)
		closeErr := root.Close()
		if planErr != nil || legacyErr != nil || closeErr != nil {
			t.Fatalf("planPlaceRetirement() = %#v, %v; legacy=%#v, %v (close %v)",
				planned, planErr, legacyPlanned, legacyErr, closeErr)
		}
		if len(planned.Prunes) != 1 || len(planned.Unproven) != 0 ||
			!slices.Equal(planned.Prunes, []dependencytxn.PlacePrune{{
				Path: "mods/sample/guide.md", Parent: *installed.Packages[0].Files[0].Parent,
				Expected: dependencytxn.Generation{
					Exists: true, Digest: strings.TrimPrefix(installed.Packages[0].Files[0].Digest, "sha256:"),
				},
				Mode: installed.Packages[0].Files[0].Mode,
			}}) {
			t.Fatalf("recorded pre-existing parent plan = prunes %v, unproven %v", planned.Prunes, planned.Unproven)
		}
		if len(legacyPlanned.Prunes) != 0 ||
			!slices.Equal(legacyPlanned.Unproven, []string{"mods/sample/guide.md"}) {
			t.Fatalf("legacy file plan = prunes %v, unproven %v", legacyPlanned.Prunes, legacyPlanned.Unproven)
		}
	})
}

func TestPlanPlaceDeselection(t *testing.T) {
	fixture := newPlaceAdmissionFixture(t)
	setPlacePublishEnvironment(t, fixture)
	if err := targetprofile.RecordApproval(fixture.binding); err != nil {
		t.Fatal(err)
	}
	retiringFiles := []targetprofile.PackageFile{
		{SourcePath: "payload/remove.md", DestinationPath: "mods/sample/remove.md", Mode: 0o644, Bytes: []byte("remove\n")},
		{SourcePath: "payload/edit.md", DestinationPath: "mods/sample/edit.md", Mode: 0o644, Bytes: []byte("owned\n")},
		{SourcePath: "payload/missing.md", DestinationPath: "mods/sample/missing.md", Mode: 0o644, Bytes: []byte("missing\n")},
	}
	retiring := resolvedPlacePackageForPublishWithFiles(t, fixture, retiringFiles)
	selected := resolvedPlacePackageNamedForPublish(t, fixture, "selected", []targetprofile.PackageFile{{
		SourcePath: "payload/selected.md", DestinationPath: "mods/selected/selected.md",
		Mode: 0o644, Bytes: []byte("selected\n"),
	}})
	if _, err := CommitPlaceUpserts(
		context.Background(), fixture.binding, fixture.raw, fixture.env,
		[]marketplace.ResolvedPlacePackage{retiring, selected},
	); err != nil {
		t.Fatal(err)
	}
	editedPath := filepath.Join(fixture.root, "mods", "sample", "edit.md")
	editedBytes := []byte("edited by user\n")
	if err := os.WriteFile(editedPath, editedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	missingPath := filepath.Join(fixture.root, "mods", "sample", "missing.md")
	if err := os.Remove(missingPath); err != nil {
		t.Fatal(err)
	}
	selectedPath := filepath.Join(fixture.root, "mods", "selected", "selected.md")
	selectedBytes := []byte("selected drift\n")
	if err := os.WriteFile(selectedPath, selectedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	owner, snapshot := openPlacePublishOwnership(t, fixture)
	installed, err := owner.PlaceFromSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var selectedID string
	for _, pkg := range installed.Packages {
		if pkg.Name == "selected" {
			selectedID = pkg.ID
		}
	}
	if selectedID == "" {
		t.Fatal("selected package was not installed")
	}
	sameTargetOtherRevision := installed.Packages[0]
	sameTargetOtherRevision.ID = strings.Repeat("f", 64)
	sameTargetOtherRevision.Name = "other-revision"
	sameTargetOtherRevision.Binding.ProfileIdentity.RevisionDigest = "sha256:" + strings.Repeat("1", 64)
	sameTargetOtherRevision.Files = nil
	sameTargetOtherRevision.Requires = nil
	sameTargetOtherRevision.Verification = nil
	otherTarget := sameTargetOtherRevision
	otherTarget.ID = strings.Repeat("e", 64)
	otherTarget.Name = "other-target"
	otherTarget.Kind = "other-app/mod"
	otherTarget.Binding.ProfileIdentity = targetprofile.Identity{
		TargetID: "other-app", SourceID: fixture.identity.SourceID,
		RevisionDigest: "sha256:" + strings.Repeat("2", 64),
	}
	installed.Packages = append(installed.Packages, sameTargetOtherRevision, otherTarget)

	root, err := fsboundary.OpenDir(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	planned, planErr := planPlaceDeselection(
		context.Background(), root, installed, fixture.identity, map[string]struct{}{selectedID: {}},
	)
	if closeErr := root.Close(); planErr != nil || closeErr != nil {
		t.Fatalf("planPlaceDeselection() = %#v, %v (close %v)", planned, planErr, closeErr)
	}
	if len(planned.Prunes) != 1 || planned.Prunes[0].Path != "mods/sample/remove.md" ||
		!slices.Equal(planned.Ceded, []string{"mods/sample/edit.md", "mods/sample/missing.md"}) ||
		len(planned.Unproven) != 0 {
		t.Fatalf("deselection plan prunes=%v ceded=%v unproven=%v", planned.Prunes, planned.Ceded, planned.Unproven)
	}
	for _, path := range append(append([]string(nil), planned.Ceded...), planned.Unproven...) {
		if path == "mods/selected/selected.md" {
			t.Fatalf("selected drift was classified for removal: %#v", planned)
		}
	}
	for _, pkg := range planned.Projection.Packages {
		if pkg.ID == selectedID || pkg.ID == sameTargetOtherRevision.ID || pkg.ID == otherTarget.ID {
			continue
		}
		t.Fatalf("unexpected package retained after deselection: %#v", pkg)
	}
	if len(planned.Projection.Packages) != 3 {
		t.Fatalf("deselection projection packages = %#v", planned.Projection.Packages)
	}
	allSelected := make(map[string]struct{})
	for _, pkg := range installed.Packages {
		if pkg.Binding.ProfileIdentity == fixture.identity {
			allSelected[pkg.ID] = struct{}{}
		}
	}
	noOpRoot, err := fsboundary.OpenDir(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	noOp, noOpErr := planPlaceDeselection(context.Background(), noOpRoot, installed, fixture.identity, allSelected)
	noOpCloseErr := noOpRoot.Close()
	if noOpErr != nil || noOpCloseErr != nil || len(noOp.Prunes) != 0 ||
		len(noOp.Ceded) != 0 || len(noOp.Unproven) != 0 ||
		len(noOp.Projection.Packages) != len(installed.Packages) {
		t.Fatalf("all-selected no-op plan = %#v, %v (close %v)", noOp, noOpErr, noOpCloseErr)
	}
	detached := false
	for i := range noOp.Projection.Packages {
		if len(noOp.Projection.Packages[i].Files) == 0 {
			continue
		}
		before := installed.Packages[i].Files[0].Path
		noOp.Projection.Packages[i].Files[0].Path = "detached-check.txt"
		detached = installed.Packages[i].Files[0].Path == before
		break
	}
	if !detached {
		t.Fatal("empty-removal projection was not detached from installed state")
	}
	if got, err := os.ReadFile(editedPath); err != nil || !bytes.Equal(got, editedBytes) {
		t.Fatalf("deselection planning changed edited file: %q, %v", got, err)
	}
	if got, err := os.ReadFile(selectedPath); err != nil || !bytes.Equal(got, selectedBytes) {
		t.Fatalf("deselection planning changed selected drift: %q, %v", got, err)
	}

	emptyRoot, err := fsboundary.OpenDir(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	allRemoved, allErr := planPlaceDeselection(context.Background(), emptyRoot, installed, fixture.identity, nil)
	closeErr := emptyRoot.Close()
	if allErr != nil || closeErr != nil {
		t.Fatalf("empty-selection plan = %#v, %v (close %v)", allRemoved, allErr, closeErr)
	}
	if len(allRemoved.Prunes) != 1 || len(allRemoved.Projection.Packages) != 2 {
		t.Fatalf("explicit empty selection did not remove only exact profile: %#v", allRemoved)
	}
	remaining := make(map[string]struct{})
	for _, pkg := range allRemoved.Projection.Packages {
		if pkg.Binding.ProfileIdentity == fixture.identity {
			t.Fatalf("empty selection retained a package for its explicit profile: %#v", pkg)
		}
		remaining[pkg.ID] = struct{}{}
	}
	if _, ok := remaining[sameTargetOtherRevision.ID]; !ok {
		t.Fatal("empty selection removed a different profile revision")
	}
	if _, ok := remaining[otherTarget.ID]; !ok {
		t.Fatal("empty selection removed another target's package")
	}
	emptyMapRoot, err := fsboundary.OpenDir(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	emptySelection, emptySelectionErr := planPlaceDeselection(
		context.Background(), emptyMapRoot, installed, fixture.identity, map[string]struct{}{},
	)
	emptyCloseErr := emptyMapRoot.Close()
	if emptySelectionErr != nil || emptyCloseErr != nil ||
		len(emptySelection.Prunes) != len(allRemoved.Prunes) ||
		len(emptySelection.Projection.Packages) != len(allRemoved.Projection.Packages) {
		t.Fatalf("empty-map selection = %#v, %v (close %v)", emptySelection, emptySelectionErr, emptyCloseErr)
	}
	invalidRoot, err := fsboundary.OpenDir(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	invalidProfile := targetprofile.Identity{}
	if _, err := planPlaceDeselection(context.Background(), invalidRoot, installed, invalidProfile, nil); err == nil {
		_ = invalidRoot.Close()
		t.Fatal("empty profile identity was accepted")
	}
	if err := invalidRoot.Close(); err != nil {
		t.Fatal(err)
	}

	dependent := sameTargetOtherRevision
	dependent.Requires = []string{installed.Packages[0].ID}
	withDependent := installed
	withDependent.Packages = append(append([]ownership.PlacePackage(nil), installed.Packages...), dependent)
	dependentRoot, err := fsboundary.OpenDir(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	_, dependencyErr := planPlaceDeselection(
		context.Background(), dependentRoot, withDependent, fixture.identity, map[string]struct{}{selectedID: {}},
	)
	_ = dependentRoot.Close()
	if dependencyErr == nil || !strings.Contains(dependencyErr.Error(), "target.place.retirement-dependency") {
		t.Fatalf("surviving dependant error = %v", dependencyErr)
	}

	unproven := installed
	unproven.Directories = nil
	unproven.Packages = append([]ownership.PlacePackage(nil), installed.Packages...)
	for i := range unproven.Packages {
		for j := range unproven.Packages[i].Files {
			unproven.Packages[i].Files[j].Parent = nil
		}
	}
	unprovenRoot, err := fsboundary.OpenDir(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	unprovenPlan, unprovenErr := planPlaceDeselection(
		context.Background(), unprovenRoot, unproven, fixture.identity, map[string]struct{}{selectedID: {}},
	)
	_ = unprovenRoot.Close()
	if unprovenErr != nil || len(unprovenPlan.Prunes) != 0 || len(unprovenPlan.Unproven) != len(retiringFiles) {
		t.Fatalf("unproven deselection plan = %#v, %v", unprovenPlan, unprovenErr)
	}
}

func TestCommitPlaceDeselection(t *testing.T) {
	t.Run("commits only deselected package files", func(t *testing.T) {
		fixture, _, selected := installPlaceDeselectionPackages(t)
		editedPath := filepath.Join(fixture.root, "mods", "sample", "edit.md")
		editedBytes := []byte("edited by user\n")
		if err := os.WriteFile(editedPath, editedBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		missingPath := filepath.Join(fixture.root, "mods", "sample", "missing.md")
		if err := os.Remove(missingPath); err != nil {
			t.Fatal(err)
		}
		userPath := filepath.Join(fixture.root, "mods", "sample", "user.txt")
		userBytes := []byte("unowned\n")
		if err := os.WriteFile(userPath, userBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		removePath := filepath.Join(fixture.root, "mods", "sample", "remove.md")
		_, ceded, err := commitPlaceChanges(
			context.Background(), fixture.binding, fixture.raw, fixture.env,
			[]marketplace.ResolvedPlacePackage{selected}, true,
		)
		if err != nil {
			t.Fatalf("commit deselection: %v", err)
		}
		if !slices.Equal(ceded, []string{"mods/sample/edit.md", "mods/sample/missing.md"}) {
			t.Fatalf("deselection ceded paths = %v", ceded)
		}
		if _, err := os.Stat(removePath); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("unchanged deselected file remains: %v", err)
		}
		if got, err := os.ReadFile(editedPath); err != nil || !bytes.Equal(got, editedBytes) {
			t.Fatalf("edited deselected file changed: %q, %v", got, err)
		}
		if got, err := os.ReadFile(userPath); err != nil || !bytes.Equal(got, userBytes) {
			t.Fatalf("unowned file changed: %q, %v", got, err)
		}
		owner, snapshot := openPlacePublishOwnership(t, fixture)
		projection, err := owner.PlaceFromSnapshot(snapshot)
		if err != nil || len(projection.Packages) != 1 || projection.Packages[0].Name != selected.Pin.Name {
			t.Fatalf("post-deselection ownership = %#v, %v", projection, err)
		}
	})

	t.Run("retiring paths remain reserved against selected upserts", func(t *testing.T) {
		fixture, _, selected := installPlaceDeselectionPackages(t)
		ownedPath := filepath.Join(fixture.root, "mods", "sample", "remove.md")
		before, err := os.ReadFile(ownedPath)
		if err != nil {
			t.Fatal(err)
		}
		statePath := filepath.Join(fixture.state, "ownership", ownership.RootKey(fixture.binding.CanonicalRoot), "state.json")
		stateBefore, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		collision := resolvedPlacePackageNamedForPublish(t, fixture, selected.Pin.Name, []targetprofile.PackageFile{
			{SourcePath: "payload/selected.md", DestinationPath: "mods/selected/selected.md", Mode: 0o644, Bytes: []byte("selected\n")},
			{SourcePath: "payload/collision.md", DestinationPath: "mods/sample/remove.md", Mode: 0o644, Bytes: []byte("collision\n")},
		})
		_, _, err = commitPlaceChanges(
			context.Background(), fixture.binding, fixture.raw, fixture.env,
			[]marketplace.ResolvedPlacePackage{collision}, true,
		)
		if err == nil {
			t.Fatal("selected upsert adopted a retiring package path")
		}
		if got, readErr := os.ReadFile(ownedPath); readErr != nil || !bytes.Equal(got, before) {
			t.Fatalf("reserved file changed on refusal: %q, %v", got, readErr)
		}
		if got, readErr := os.ReadFile(statePath); readErr != nil || !bytes.Equal(got, stateBefore) {
			t.Fatalf("ownership changed on collision refusal: %v", readErr)
		}
	})

	t.Run("missing receipt and changed profile never prune", func(t *testing.T) {
		for _, scenario := range []string{"missing receipt", "changed profile"} {
			t.Run(scenario, func(t *testing.T) {
				fixture, _, _ := installPlaceDeselectionPackages(t)
				path := filepath.Join(fixture.root, "mods", "sample", "remove.md")
				fileBefore, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				statePath := filepath.Join(fixture.state, "ownership", ownership.RootKey(fixture.binding.CanonicalRoot), "state.json")
				stateBefore, err := os.ReadFile(statePath)
				if err != nil {
					t.Fatal(err)
				}
				binding := fixture.binding
				if scenario == "missing receipt" {
					receipts, globErr := filepath.Glob(filepath.Join(fixture.state, "target-approval-*.json"))
					if globErr != nil || len(receipts) == 0 {
						t.Fatalf("approval receipts = %v, %v", receipts, globErr)
					}
					for _, receipt := range receipts {
						if err := os.Remove(receipt); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					binding.Profile.RevisionDigest = "sha256:" + strings.Repeat("9", 64)
				}
				_, _, err = commitPlaceChanges(
					context.Background(), binding, fixture.raw, fixture.env, nil, true,
				)
				if err == nil {
					t.Fatal("invalid retirement authority was accepted")
				}
				if got, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(got, fileBefore) {
					t.Fatalf("invalid authority pruned file: %q, %v", got, readErr)
				}
				if got, readErr := os.ReadFile(statePath); readErr != nil || !bytes.Equal(got, stateBefore) {
					t.Fatalf("invalid authority changed ownership: %v", readErr)
				}
			})
		}
	})

	t.Run("empty selection prunes only in explicit mode", func(t *testing.T) {
		fixture, _, _ := installPlaceDeselectionPackages(t)
		removePath := filepath.Join(fixture.root, "mods", "sample", "remove.md")
		if _, err := CommitPlaceUpserts(context.Background(), fixture.binding, fixture.raw, fixture.env, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(removePath); err != nil {
			t.Fatalf("upsert-only empty input changed installation: %v", err)
		}
		if _, ceded, err := commitPlaceChanges(
			context.Background(), fixture.binding, fixture.raw, fixture.env, nil, true,
		); err != nil || len(ceded) != 0 {
			t.Fatalf("explicit empty-selection commit ceded=%v err=%v", ceded, err)
		}
		if _, err := os.Stat(removePath); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("explicit empty selection did not retire package: %v", err)
		}
		owner, snapshot := openPlacePublishOwnership(t, fixture)
		projection, err := owner.PlaceFromSnapshot(snapshot)
		if err != nil || len(projection.Packages) != 0 {
			t.Fatalf("empty-selection ownership = %#v, %v", projection, err)
		}
	})
}

func installPlaceDeselectionPackages(
	t *testing.T,
) (placeAdmissionFixture, marketplace.ResolvedPlacePackage, marketplace.ResolvedPlacePackage) {
	t.Helper()
	fixture := newPlaceAdmissionFixture(t)
	setPlacePublishEnvironment(t, fixture)
	if err := targetprofile.RecordApproval(fixture.binding); err != nil {
		t.Fatal(err)
	}
	retiring := resolvedPlacePackageForPublishWithFiles(t, fixture, []targetprofile.PackageFile{
		{SourcePath: "payload/remove.md", DestinationPath: "mods/sample/remove.md", Mode: 0o644, Bytes: []byte("remove\n")},
		{SourcePath: "payload/edit.md", DestinationPath: "mods/sample/edit.md", Mode: 0o644, Bytes: []byte("owned\n")},
		{SourcePath: "payload/missing.md", DestinationPath: "mods/sample/missing.md", Mode: 0o644, Bytes: []byte("missing\n")},
	})
	selected := resolvedPlacePackageNamedForPublish(t, fixture, "selected", []targetprofile.PackageFile{{
		SourcePath: "payload/selected.md", DestinationPath: "mods/selected/selected.md",
		Mode: 0o644, Bytes: []byte("selected\n"),
	}})
	if _, err := CommitPlaceUpserts(
		context.Background(), fixture.binding, fixture.raw, fixture.env,
		[]marketplace.ResolvedPlacePackage{retiring, selected},
	); err != nil {
		t.Fatalf("install deselection packages: %v", err)
	}
	return fixture, retiring, selected
}

func resolvedPlacePackageNamedForPublish(
	t *testing.T,
	fixture placeAdmissionFixture,
	name string,
	files []targetprofile.PackageFile,
) marketplace.ResolvedPlacePackage {
	t.Helper()
	copyFixture := fixture
	cfg := *fixture.config
	cfg.Marketplaces = append([]config.MarketplaceEntry(nil), fixture.config.Marketplaces...)
	cfg.Marketplaces[0].Resources = append([]config.MarketplaceResourcePin(nil), fixture.config.Marketplaces[0].Resources...)
	cfg.Marketplaces[0].Resources[1].Name = name
	copyFixture.config = &cfg
	return resolvedPlacePackageForPublishWithFiles(t, copyFixture, files)
}

func resolvedPlacePackageForPublish(t *testing.T, fixture placeAdmissionFixture) marketplace.ResolvedPlacePackage {
	t.Helper()
	return resolvedPlacePackageForPublishWithFiles(t, fixture, []targetprofile.PackageFile{{
		SourcePath: "payload/guide.md", DestinationPath: "mods/sample/guide.md",
		Mode: 0o644, Bytes: []byte("fixture package\n"),
	}})
}

func resolvedPlacePackageForPublishWithFiles(t *testing.T, fixture placeAdmissionFixture, files []targetprofile.PackageFile) marketplace.ResolvedPlacePackage {
	t.Helper()
	entry := fixture.config.Marketplaces[0]
	var pin = entry.Resources[1]
	if pin.Kind != "invented-app/mod" {
		t.Fatalf("fixture package pin = %+v", pin)
	}
	hostBinding, err := targetprofile.BuildBinding(fixture.identity, files)
	if err != nil {
		t.Fatal(err)
	}
	pin.Digest = hostBinding.PackageContentDigest
	pin.Revision = hostBinding.PackageContentDigest
	pin.LockHostPackageIdentity = hostBinding
	coordinate := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID(entry.InstanceID), Registry: store.RegistryID(pin.RegistryID),
		Kind: pin.Kind, Name: pin.Name,
	}
	archiveDigest := store.ArchiveDigest("sha256:" + strings.Repeat("c", 64))
	pin.LockArchiveDigest = string(archiveDigest)
	assessment := store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: store.RevisionDigest(hostBinding.PackageContentDigest),
		AnalyzerVersion: "fixture-analyzer", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("d", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("e", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(),
		HostPackageIdentity:      &hostBinding,
	}
	assessment, err = store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatal(err)
	}
	pin.LockAssessmentDigest = string(assessment.AssessmentDigest)
	return marketplace.ResolvedPlacePackage{
		Entry: entry, Pin: pin, Files: files,
		Revision: store.AssessedResourceRevision{
			ResourceRevision: store.ResourceRevision{
				Coordinate: coordinate, ContentDigest: store.RevisionDigest(hostBinding.PackageContentDigest),
				ArchiveDigest: archiveDigest, Requires: "",
			},
			Assessment: assessment,
		},
	}
}

func setPlacePublishEnvironment(t *testing.T, fixture placeAdmissionFixture) {
	t.Helper()
	t.Setenv("HOME", fixture.env.Home)
	t.Setenv("XDG_STATE_HOME", filepath.Dir(fixture.state))
	t.Setenv("INVENTED_APP_HOME", fixture.root)
}

func assertPlacePublishPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unexpected place state path %s: %v", path, err)
	}
}

func assertPlacePublishFile(t *testing.T, path string, want []byte, mode fs.FileMode) {
	t.Helper()
	got, err := os.ReadFile(path)
	info, statErr := os.Stat(path)
	if err != nil || statErr != nil || !bytes.Equal(got, want) || info.Mode().Perm() != mode {
		t.Fatalf("place file = %q mode=%v read=%v stat=%v; want %q mode=%o", got, info, err, statErr, want, mode)
	}
}

func openPlacePublishOwnership(t *testing.T, fixture placeAdmissionFixture) (*ownership.Store, ownership.StateSnapshot) {
	t.Helper()
	store, err := ownership.Open(fixture.binding.CanonicalRoot, ownership.WithStateRoot(fixture.binding.CanonicalStateRoot))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	return store, snapshot
}
