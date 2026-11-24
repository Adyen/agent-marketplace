package pipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

func TestPreparePlaceVerificationChecksInstalledProjection(t *testing.T) {
	t.Run("exact installed package returns argv without execution or writes", func(t *testing.T) {
		fixture := newInstalledPlaceVerificationFixture(t)
		owner, _ := openPlacePublishOwnership(t, fixture.admission)
		stateBefore, stateInfoBefore := placeVerificationStateSnapshot(t, owner.StateFile())
		targetBytesBefore, targetInfoBefore, err := placeVerificationFileSnapshot(fixture.target)
		if err != nil {
			t.Fatal(err)
		}

		prepared, err := preparePlaceVerification(
			context.Background(), fixture.admission.binding, fixture.admission.raw,
			fixture.admission.env, "invented-app/mod", "sample",
		)
		if err != nil {
			t.Fatalf("prepare installed package verification: %v", err)
		}
		wantSubject := filepath.Join(fixture.admission.root, "mods", "sample")
		if prepared.pkg.ID == "" || prepared.pkg.Kind != "invented-app/mod" || prepared.pkg.Name != "sample" ||
			prepared.checkName != "initial-load" || len(prepared.argv) != 2 ||
			prepared.argv[0] != "fixture-checker" || prepared.argv[1] != wantSubject ||
			prepared.binding.Profile != fixture.admission.binding.Profile || prepared.owner == nil ||
			prepared.rootIdentity.Inode == 0 {
			t.Fatalf("prepared place verification = %#v", prepared)
		}
		projection, err := prepared.owner.PlaceFromSnapshot(prepared.snapshot)
		if err != nil || len(projection.Packages) != 1 || projection.Packages[0].Verification != nil {
			t.Fatalf("preflight changed or lost ownership projection: %#v, %v", projection, err)
		}
		stateAfter, stateInfoAfter := placeVerificationStateSnapshot(t, owner.StateFile())
		targetBytesAfter, targetInfoAfter, err := placeVerificationFileSnapshot(fixture.target)
		if err != nil || !bytes.Equal(stateBefore, stateAfter) ||
			!stateInfoBefore.ModTime().Equal(stateInfoAfter.ModTime()) ||
			!bytes.Equal(targetBytesBefore, targetBytesAfter) ||
			!targetInfoBefore.ModTime().Equal(targetInfoAfter.ModTime()) {
			t.Fatalf("preflight changed installed state: read=%v", err)
		}
		assertPlaceVerificationNotExecuted(t, fixture.marker)
	})

	for _, tc := range []struct {
		name       string
		want       string
		mutate     func(*testing.T, installedPlaceVerificationFixture)
		assertFile func(*testing.T, installedPlaceVerificationFixture)
	}{
		{
			name: "edited bytes",
			want: "target.place.drift",
			mutate: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				if err := os.WriteFile(fixture.target, []byte("edited by user\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			assertFile: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				got, err := os.ReadFile(fixture.target)
				if err != nil || string(got) != "edited by user\n" {
					t.Fatalf("edited package bytes = %q, %v", got, err)
				}
			},
		},
		{
			name: "edited permissions",
			want: "target.place.drift",
			mutate: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				if err := os.Chmod(fixture.target, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			assertFile: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				info, err := os.Stat(fixture.target)
				got, readErr := os.ReadFile(fixture.target)
				if err != nil || readErr != nil || info.Mode().Perm() != 0o600 ||
					!bytes.Equal(got, []byte("fixture package\n")) {
					t.Fatalf("edited package mode/content = %v %q, stat=%v read=%v", info, got, err, readErr)
				}
			},
		},
		{
			name: "missing file",
			want: "target.place.drift",
			mutate: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				if err := os.Remove(fixture.target); err != nil {
					t.Fatal(err)
				}
			},
			assertFile: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				if _, err := os.Lstat(fixture.target); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("missing package file was recreated: %v", err)
				}
			},
		},
		{
			name: "removed subject subtree",
			want: "target.place.drift",
			mutate: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				subject := filepath.Join(fixture.admission.root, "mods", "sample")
				if err := os.Rename(subject, subject+".saved"); err != nil {
					t.Fatal(err)
				}
			},
			assertFile: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				if _, err := os.Lstat(fixture.target); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("removed subject subtree was recreated: %v", err)
				}
				got, err := os.ReadFile(filepath.Join(fixture.admission.root, "mods", "sample.saved", "guide.md"))
				if err != nil || !bytes.Equal(got, []byte("fixture package\n")) {
					t.Fatalf("saved subject subtree contents = %q, %v", got, err)
				}
			},
		},
		{
			name: "symlink",
			want: "target.place.drift",
			mutate: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				if err := os.Remove(fixture.target); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(fixture.admission.env.Home, "outside.txt")
				if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, fixture.target); err != nil {
					t.Fatal(err)
				}
			},
			assertFile: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				info, err := os.Lstat(fixture.target)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("symlink package file changed: %v, %v", info, err)
				}
				outside, err := os.ReadFile(filepath.Join(fixture.admission.env.Home, "outside.txt"))
				if err != nil || string(outside) != "outside\n" {
					t.Fatalf("symlink target changed: %q, %v", outside, err)
				}
			},
		},
		{
			name: "executable changed",
			want: "target.approval.executable-mismatch",
			mutate: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				if err := os.WriteFile(fixture.hostExecutable, []byte("#!/bin/sh\nexit 42\n"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			assertFile: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				got, err := os.ReadFile(fixture.hostExecutable)
				if err != nil || string(got) != "#!/bin/sh\nexit 42\n" {
					t.Fatalf("changed executable bytes = %q, %v", got, err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newInstalledPlaceVerificationFixture(t)
			tc.mutate(t, fixture)
			assertPlaceVerificationRefusal(t, fixture, fixture.admission.binding, tc.want)
			tc.assertFile(t, fixture)
		})
	}

	t.Run("owned package must use the exact selected source identity", func(t *testing.T) {
		fixture := newInstalledPlaceVerificationFixture(t)
		identity := fixture.admission.identity
		identity.SourceID = "directory:%2Fprofiles%2Fcopy"
		profile, err := targetprofile.Parse(fixture.admission.raw, identity)
		if err != nil {
			t.Fatal(err)
		}
		binding, err := targetprofile.ResolveApprovalBinding(profile, identity, fixture.admission.env)
		if err != nil {
			t.Fatal(err)
		}
		if err := targetprofile.RecordApproval(binding); err != nil {
			t.Fatal(err)
		}
		assertPlaceVerificationRefusal(t, fixture, binding, "target.place.profile-identity-mismatch")
	})
}

type installedPlaceVerificationFixture struct {
	admission      placeAdmissionFixture
	target         string
	hostExecutable string
	marker         string
}

func newInstalledPlaceVerificationFixture(t *testing.T) installedPlaceVerificationFixture {
	t.Helper()
	fixture := newPlaceAdmissionFixture(t)
	raw := []byte(strings.Replace(
		placeAdmissionProfileYAML,
		"kinds: {mod: {deliver: place, into: [mods/]}}",
		`host: {executable: fixture-checker}
checks: {initial-load: {scope: eager-package-load, argv: [fixture-checker, "{subject}"], subject: {under: mods, component: package-name}}}
kinds: {mod: {deliver: place, into: [mods/], verify: initial-load}}`,
		1,
	))
	digest := sha256.Sum256(raw)
	fixture.identity.RevisionDigest = "sha256:" + hex.EncodeToString(digest[:])
	profile, err := targetprofile.Parse(raw, fixture.identity)
	if err != nil {
		t.Fatalf("parse verification profile: %v", err)
	}
	hostDir := filepath.Join(fixture.env.Home, "bin")
	if err := os.MkdirAll(hostDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hostExecutable := filepath.Join(hostDir, "fixture-checker")
	marker := filepath.Join(fixture.env.Home, "verification-executed")
	if err := os.WriteFile(hostExecutable, []byte("#!/bin/sh\n: > \"$PLACE_VERIFY_MARKER\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", hostDir)
	t.Setenv("PLACE_VERIFY_MARKER", marker)
	binding, err := targetprofile.ResolveApprovalBinding(profile, fixture.identity, fixture.env)
	if err != nil {
		t.Fatalf("resolve verification approval binding: %v", err)
	}
	fixture.raw = raw
	fixture.binding = binding
	fixture.config.Marketplaces[0].Resources[0].LockProfileIdentity = fixture.identity
	fixture.config.Marketplaces[0].Resources[0].Revision = fixture.identity.RevisionDigest
	fixture.config.Marketplaces[0].Resources[0].Digest = fixture.identity.RevisionDigest
	setPlacePublishEnvironment(t, fixture)
	if err := targetprofile.RecordApproval(binding); err != nil {
		t.Fatalf("record verification approval: %v", err)
	}
	pkg := resolvedPlacePackageForPublish(t, fixture)
	if _, err := CommitPlaceUpserts(
		context.Background(), binding, raw, fixture.env, []marketplace.ResolvedPlacePackage{pkg},
	); err != nil {
		t.Fatalf("install fixture package: %v", err)
	}
	return installedPlaceVerificationFixture{
		admission: fixture, target: filepath.Join(fixture.root, "mods", "sample", "guide.md"),
		hostExecutable: hostExecutable, marker: marker,
	}
}

func assertPlaceVerificationRefusal(
	t *testing.T,
	fixture installedPlaceVerificationFixture,
	binding targetprofile.ApprovalBinding,
	want string,
) {
	t.Helper()
	owner, _ := openPlacePublishOwnership(t, fixture.admission)
	stateBefore, stateInfoBefore := placeVerificationStateSnapshot(t, owner.StateFile())
	_, err := preparePlaceVerification(
		context.Background(), binding, fixture.admission.raw, fixture.admission.env,
		"invented-app/mod", "sample",
	)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("prepare verification error = %v, want %q", err, want)
	}
	if want == "target.place.drift" && !strings.Contains(err.Error(), "mods/sample/guide.md") {
		t.Fatalf("place drift error does not name the mismatched file: %v", err)
	}
	stateAfter, stateInfoAfter := placeVerificationStateSnapshot(t, owner.StateFile())
	if !bytes.Equal(stateBefore, stateAfter) || !stateInfoBefore.ModTime().Equal(stateInfoAfter.ModTime()) {
		t.Fatal("refused verification preflight changed ownership state")
	}
	assertPlaceVerificationNotExecuted(t, fixture.marker)
}

func placeVerificationStateSnapshot(t *testing.T, path string) ([]byte, os.FileInfo) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return data, info
}

func placeVerificationFileSnapshot(path string) ([]byte, os.FileInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(path)
	return data, info, err
}

func assertPlaceVerificationNotExecuted(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Lstat(marker); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("host verification command executed: marker stat error = %v", err)
	}
}
