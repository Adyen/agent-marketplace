//go:build unix

package ownership

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

func TestPlaceOwnershipProjection(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, stateRoot := filepath.Join(base, "repo", ".devin"), filepath.Join(base, "state", "agent-marketplace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	absFile := filepath.Join(root, "mcp_config.json")
	f := &fixture{t: t, targetRoot: root, stateRoot: stateRoot, rel: "mcp_config.json", absFile: absFile}
	st := f.store()
	stateDir := filepath.Join(stateRoot, "dependencies")

	snapshot, err := st.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	if empty, err := st.PlaceFromSnapshot(snapshot); err != nil || !reflect.DeepEqual(empty, PlaceProjection{}) {
		t.Fatalf("absent place projection = %#v, %v", empty, err)
	}
	foreign := snapshot
	foreign.rootKey = "foreign-root"
	if _, err := st.PlaceFromSnapshot(foreign); !errors.Is(err, ErrStateRootMismatch) {
		t.Fatalf("foreign-root PlaceFromSnapshot error = %v", err)
	}
	f.writeAggregate("{}\n")
	mustApply(t, st, Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"mcpServers":{"original":{}}}` + "\n"),
		Entries: []Entry{mcpEntry("original", "fixture", `{"command":"original"}`)},
		Reason:  ReasonSync,
	})

	profile := targetprofile.Identity{
		TargetID: "herdr-app", SourceID: "builtin:agent-marketplace",
		RevisionDigest: "sha256:" + strings.Repeat("a", 64),
	}
	binding := targetprofile.Binding{
		PackageContentDigest:  "sha256:" + strings.Repeat("b", 64),
		DeliveryMappingDigest: "sha256:" + strings.Repeat("c", 64),
		ProfileDigest:         profile.RevisionDigest, ProfileIdentity: profile,
	}
	if err := os.Mkdir(filepath.Join(f.targetRoot, "mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	directoryIdentity := placeStateDirectoryIdentity(t, filepath.Join(f.targetRoot, "mods"))
	parentIdentity := directoryIdentity
	want := PlaceProjection{
		Packages: []PlacePackage{{
			ID: strings.Repeat("b", 64), Kind: "herdr-app/mod", Name: "sample",
			Binding: binding, GenerationKey: strings.Repeat("c", 64),
			Files: []PlaceFile{{
				Path: "mods/a.txt", Digest: Digest([]byte("payload")), Mode: 0o644, Parent: &parentIdentity,
			}},
			Requires: []string{strings.Repeat("d", 64)},
		}},
		Directories: []dependencytxn.PlaceDirectory{{Path: "mods", Identity: directoryIdentity}},
	}
	snapshot, err = st.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	ownershipFile, err := st.PreparePlace(snapshot, want)
	if err != nil {
		t.Fatal(err)
	}
	rootIdentity := placeStateDirectoryIdentity(t, st.CanonicalRoot())
	if _, err := dependencytxn.CommitPlace(context.Background(), stateDir, dependencytxn.PlacePlan{
		Root: st.CanonicalRoot(), RootIdentity: rootIdentity, Ownership: ownershipFile,
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err = st.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.record.Generation != 2 || !reflect.DeepEqual(snapshot.record.Files[f.rel].Entries, []Entry{
		mcpEntry("original", "fixture", `{"command":"original"}`),
	}) {
		t.Fatalf("aggregate entries after place commit = %#v at generation %d", snapshot.record.Files, snapshot.record.Generation)
	}
	got, err := st.PlaceFromSnapshot(snapshot)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("PlaceFromSnapshot = %#v, %v; want %#v", got, err, want)
	}
	got.Packages[0].Files[0].Path = "changed.txt"
	got.Packages[0].Files[0].Parent.Inode++
	got.Packages[0].Requires[0] = strings.Repeat("e", 64)
	got.Directories[0].Path = "changed"
	got.Directories[0].Identity.Inode++
	again, err := st.PlaceFromSnapshot(snapshot)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("mutating projection view changed the next view: %#v, %v", again, err)
	}
	for _, invalidParent := range []fsboundary.Identity{
		{Mode: os.ModeDir},
		{Inode: parentIdentity.Inode, Mode: 0o755},
	} {
		invalid := want
		invalid.Packages = append([]PlacePackage(nil), want.Packages...)
		invalid.Packages[0].Files = append([]PlaceFile(nil), want.Packages[0].Files...)
		invalid.Packages[0].Files[0].Parent = &invalidParent
		if _, err := st.PreparePlace(snapshot, invalid); !errors.Is(err, ErrUnsafeStateValue) {
			t.Fatalf("invalid file parent %#v error = %v", invalidParent, err)
		}
	}
	verification := PlaceVerification{
		Binding: binding, GenerationKey: want.Packages[0].GenerationKey, Check: "herdr-app",
		Scope: "eager-package-load", Outcome: "pass", EvidenceSource: "initial-load",
		HostVersionStatus: "unknown",
	}
	legacyBytes, err := os.ReadFile(st.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacyBytes), `"verification"`) {
		t.Fatal("unverified legacy place package encoded a verification field")
	}
	desired, err := st.PreparePlaceVerification(snapshot, want.Packages[0].ID, verification)
	if err != nil || desired.Path == "" {
		t.Fatalf("PreparePlaceVerification = %#v, %v", desired, err)
	}
	if _, err := dependencytxn.Commit(context.Background(), stateDir, []dependencytxn.DesiredFile{desired}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = st.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.record.Version != stateVersion {
		t.Fatalf("verification persistence changed schema version: %d", snapshot.record.Version)
	}
	verificationBytes, err := os.ReadFile(st.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"verification"`, `"generation_key"`, `"host_version_status"`, `"evidence_source"`} {
		if !strings.Contains(string(verificationBytes), field) {
			t.Fatalf("persisted verification is missing JSON field %s", field)
		}
	}
	if strings.Contains(string(verificationBytes), `"host_version":`) {
		t.Fatal("empty host version was not omitted")
	}
	got, err = st.PlaceFromSnapshot(snapshot)
	if err != nil || got.Packages[0].Verification == nil || *got.Packages[0].Verification != verification {
		t.Fatalf("recorded place verification = %#v, %v", got, err)
	}
	got.Packages[0].Verification.Check = "changed"
	again, err = st.PlaceFromSnapshot(snapshot)
	if err != nil || again.Packages[0].Verification == nil || *again.Packages[0].Verification != verification {
		t.Fatalf("mutating verification view changed the next view: %#v, %v", again, err)
	}
	want.Packages[0].Verification = &verification
	if noop, err := st.PreparePlaceVerification(snapshot, want.Packages[0].ID, verification); err != nil ||
		!reflect.DeepEqual(noop, dependencytxn.DesiredFile{}) {
		t.Fatalf("identical place verification is not a no-op: %#v, %v", noop, err)
	}
	stale := verification
	stale.GenerationKey = strings.Repeat("e", 64)
	stateBeforeVerificationRefusal, err := os.ReadFile(st.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PreparePlaceVerification(snapshot, want.Packages[0].ID, stale); !errors.Is(err, ErrUnsafeStateValue) {
		t.Fatalf("stale place verification error = %v, want ErrUnsafeStateValue", err)
	}
	stateAfterVerificationRefusal, err := os.ReadFile(st.StateFile())
	if err != nil || !reflect.DeepEqual(stateBeforeVerificationRefusal, stateAfterVerificationRefusal) {
		t.Fatalf("refused place verification changed state: %v", err)
	}
	if _, err := st.PreparePlaceVerification(foreign, want.Packages[0].ID, verification); !errors.Is(err, ErrStateRootMismatch) {
		t.Fatalf("foreign snapshot verification error = %v, want ErrStateRootMismatch", err)
	}
	if _, err := st.PreparePlaceVerification(snapshot, "missing-package", verification); !errors.Is(err, ErrUnsafeStateValue) {
		t.Fatalf("missing place package verification error = %v, want ErrUnsafeStateValue", err)
	}
	if _, err := st.PreparePlaceVerification(StateSnapshot{rootKey: st.rootKey}, want.Packages[0].ID, verification); !errors.Is(err, ErrStateMissing) {
		t.Fatalf("missing ownership record verification error = %v, want ErrStateMissing", err)
	}
	validVersion := verification
	validVersion.HostVersion = "1.2.3"
	validVersion.HostVersionStatus = "verified"
	validVersion.EvidenceSource = "target-verify"
	if _, err := st.PreparePlaceVerification(snapshot, want.Packages[0].ID, validVersion); err != nil {
		t.Fatalf("valid host version verification = %v", err)
	}
	validVersion.HostVersionStatus = "outside-verified-range"
	if _, err := st.PreparePlaceVerification(snapshot, want.Packages[0].ID, validVersion); err != nil {
		t.Fatalf("valid outside-range host version verification = %v", err)
	}
	invalidVerification := []struct {
		name   string
		change func(*PlaceVerification)
	}{
		{"invalid check", func(v *PlaceVerification) { v.Check = "not a target" }},
		{"unsupported scope", func(v *PlaceVerification) { v.Scope = "lazy" }},
		{"unsupported outcome", func(v *PlaceVerification) { v.Outcome = "pending" }},
		{"unsupported evidence", func(v *PlaceVerification) { v.EvidenceSource = "manual" }},
		{"unsupported host version status", func(v *PlaceVerification) { v.HostVersionStatus = "maybe" }},
		{"unknown version is not empty", func(v *PlaceVerification) { v.HostVersion = "1.2.3" }},
		{"verified version required", func(v *PlaceVerification) { v.HostVersionStatus = "verified" }},
		{"version suffix refused", func(v *PlaceVerification) { v.HostVersion = "1.2.3-beta"; v.HostVersionStatus = "verified" }},
		{"version component bounded", func(v *PlaceVerification) { v.HostVersion = "1234567890.2.3"; v.HostVersionStatus = "verified" }},
		{"version normalized", func(v *PlaceVerification) { v.HostVersion = "01.2.3"; v.HostVersionStatus = "verified" }},
		{"binding mismatch", func(v *PlaceVerification) { v.Binding.DeliveryMappingDigest = strings.Repeat("f", 64) }},
		{"generation mismatch", func(v *PlaceVerification) { v.GenerationKey = strings.Repeat("e", 64) }},
	}
	for _, tc := range invalidVerification {
		t.Run("invalid-verification/"+tc.name, func(t *testing.T) {
			invalid := verification
			tc.change(&invalid)
			if _, err := st.PreparePlaceVerification(snapshot, want.Packages[0].ID, invalid); !errors.Is(err, ErrUnsafeStateValue) {
				t.Fatalf("invalid verification error = %v, want ErrUnsafeStateValue", err)
			}
		})
	}

	nextAggregate := []byte(`{"mcpServers":{"original":{},"later":{}}}` + "\n")
	mustApply(t, st, Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: nextAggregate,
		Entries: []Entry{
			mcpEntry("original", "fixture", `{"command":"original"}`),
			mcpEntry("later", "fixture", `{"command":"later"}`),
		},
		Reason: ReasonSync,
	})
	snapshot, err = st.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(snapshot.record.Files[f.rel].Entries); got != 2 {
		t.Fatalf("later aggregate update kept %d entries, want 2", got)
	}
	got, err = st.PlaceFromSnapshot(snapshot)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("later aggregate update changed place projection: %#v, %v", got, err)
	}

	beforeBytes, err := os.ReadFile(st.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	file := want.Packages[0].Files[0]
	malformed := file
	malformed.Path = "../escape"
	invalid := []struct {
		name  string
		files []PlaceFile
	}{
		{name: "duplicate", files: []PlaceFile{file, file}},
		{name: "malformed", files: []PlaceFile{malformed}},
	}
	for _, tc := range invalid {
		t.Run("invalid-files/"+tc.name, func(t *testing.T) {
			next := want
			next.Packages = append([]PlacePackage(nil), want.Packages...)
			next.Packages[0].Files = append([]PlaceFile(nil), tc.files...)
			if _, err := st.PreparePlace(snapshot, next); err == nil {
				t.Fatal("PreparePlace accepted invalid file paths")
			}
			afterBytes, err := os.ReadFile(st.StateFile())
			if err != nil || !reflect.DeepEqual(afterBytes, beforeBytes) {
				t.Fatalf("refused PreparePlace changed ownership bytes: equal=%t err=%v", reflect.DeepEqual(afterBytes, beforeBytes), err)
			}
		})
	}
	t.Run("decode rejects malformed projection", func(t *testing.T) {
		raw := []byte(fmt.Sprintf(
			`{"version":1,"canonical_root":%q,"root_key":%q,"generation":1,"files":{},"place_packages":[{"id":"not-a-canonical-hash"}]}`,
			st.CanonicalRoot(), st.RootKey(),
		))
		if err := os.WriteFile(st.StateFile(), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := st.SnapshotState(); !errors.Is(err, ErrStateCorrupt) {
			t.Fatalf("SnapshotState accepted malformed place projection: %v", err)
		}
	})
}

func placeStateDirectoryIdentity(t *testing.T, path string) fsboundary.Identity {
	t.Helper()
	directory, err := fsboundary.OpenDir(path)
	if err != nil {
		t.Fatal(err)
	}
	identity := directory.Identity()
	if err := directory.Close(); err != nil {
		t.Fatal(err)
	}
	return identity
}
