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
	want := PlaceProjection{
		Packages: []PlacePackage{{
			ID: strings.Repeat("b", 64), Kind: "herdr-app/mod", Name: "sample",
			Binding: binding, GenerationKey: strings.Repeat("c", 64),
			Files:    []PlaceFile{{Path: "mods/a.txt", Digest: Digest([]byte("payload")), Mode: 0o644}},
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
	got.Packages[0].Requires[0] = strings.Repeat("e", 64)
	got.Directories[0].Path = "changed"
	got.Directories[0].Identity.Inode++
	again, err := st.PlaceFromSnapshot(snapshot)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("mutating projection view changed the next view: %#v, %v", again, err)
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
