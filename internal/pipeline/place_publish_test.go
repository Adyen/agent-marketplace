package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
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
