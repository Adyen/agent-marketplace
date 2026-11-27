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
	"strconv"
	"strings"
	"testing"
	"time"

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
			prepared.argv[0] != "check" || prepared.argv[1] != wantSubject ||
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

func TestPlaceVerificationProbeDriftStopsCheck(t *testing.T) {
	fixture := newInstalledPlaceVerificationFixtureWithVersionProbe(t, true)
	owner, _ := openPlacePublishOwnership(t, fixture.admission)
	stateBefore, stateInfoBefore := placeVerificationStateSnapshot(t, owner.StateFile())
	repoRoot := filepath.Join(fixture.admission.env.Home, "repo")
	if err := os.Mkdir(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	prepared, err := preparePlaceVerification(
		context.Background(), fixture.admission.binding, fixture.admission.raw,
		fixture.admission.env, "invented-app/mod", "sample",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runPreparedPlaceVerification(
		context.Background(), prepared, fixture.admission.raw, fixture.admission.env,
		repoRoot, "invented-app/mod", "sample", "initial-load",
	); err == nil || !strings.Contains(err.Error(), "target.place.drift") {
		t.Fatalf("probe that edits the installed package error = %v", err)
	}
	stateAfter, stateInfoAfter := placeVerificationStateSnapshot(t, owner.StateFile())
	if !bytes.Equal(stateBefore, stateAfter) || !stateInfoBefore.ModTime().Equal(stateInfoAfter.ModTime()) {
		t.Fatal("probe drift recorded a verification result")
	}
	if data, err := os.ReadFile(fixture.marker); err != nil || string(data) != "version\n" {
		t.Fatalf("version/check markers = %q, %v; named check must not run", data, err)
	}
	if data, err := os.ReadFile(fixture.target); err != nil || string(data) != "changed during version probe\n" {
		t.Fatalf("version probe fixture edit = %q, %v", data, err)
	}
}

func TestRunPlaceVerificationRecordsExactOutcome(t *testing.T) {
	t.Run("pass and fail store bounded results without exposing output", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			exitCode int
			outcome  string
		}{
			{name: "pass", exitCode: 0, outcome: "pass"},
			{name: "fail", exitCode: 17, outcome: "fail"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				fixture := newInstalledPlaceVerificationFixtureWithOutcome(t, tc.exitCode, false)
				repoRoot := filepath.Join(fixture.admission.env.Home, "repo")
				if err := os.Mkdir(repoRoot, 0o755); err != nil {
					t.Fatal(err)
				}
				result, err := runPlaceVerification(
					context.Background(), fixture.admission.binding, fixture.admission.raw,
					fixture.admission.env, repoRoot, "invented-app/mod", "sample",
				)
				if tc.exitCode == 0 && err != nil {
					t.Fatalf("passing check = %#v, %v", result, err)
				}
				if tc.exitCode != 0 && (err == nil || !strings.Contains(err.Error(), "target.place.check-failed") ||
					strings.Contains(err.Error(), "noisy")) {
					t.Fatalf("failing check error = %v; want safe check-failed diagnostic", err)
				}
				if result.Outcome != tc.outcome || result.Check != "initial-load" ||
					result.Scope != "eager-package-load" || result.EvidenceSource != "target-verify" ||
					result.HostVersionStatus != "unknown" || result.HostVersion != "" {
					t.Fatalf("recorded verification = %#v", result)
				}
				owner, snapshot := openPlacePublishOwnership(t, fixture.admission)
				projection, err := owner.PlaceFromSnapshot(snapshot)
				if err != nil || len(projection.Packages) != 1 || projection.Packages[0].Verification == nil ||
					*projection.Packages[0].Verification != result ||
					result.Binding != projection.Packages[0].Binding ||
					result.GenerationKey != projection.Packages[0].GenerationKey {
					t.Fatalf("stored verification = %#v, %v", projection, err)
				}
				assertPlaceVerificationExecuted(t, fixture.marker)
			})
		}
	})

	t.Run("changed files during check are not recorded", func(t *testing.T) {
		fixture := newInstalledPlaceVerificationFixtureWithOutcome(t, 0, true)
		owner, _ := openPlacePublishOwnership(t, fixture.admission)
		stateBefore, stateInfoBefore := placeVerificationStateSnapshot(t, owner.StateFile())
		repoRoot := filepath.Join(fixture.admission.env.Home, "repo")
		if err := os.Mkdir(repoRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := runPlaceVerification(
			context.Background(), fixture.admission.binding, fixture.admission.raw,
			fixture.admission.env, repoRoot, "invented-app/mod", "sample",
		); err == nil || !strings.Contains(err.Error(), "target.place.drift") {
			t.Fatalf("check that edits its installed package error = %v", err)
		}
		stateAfter, stateInfoAfter := placeVerificationStateSnapshot(t, owner.StateFile())
		if !bytes.Equal(stateBefore, stateAfter) || !stateInfoBefore.ModTime().Equal(stateInfoAfter.ModTime()) {
			t.Fatal("check that changed installed files recorded a result")
		}
		got, err := os.ReadFile(fixture.target)
		if err != nil || string(got) != "changed during check\n" {
			t.Fatalf("fixture edit = %q, %v", got, err)
		}
		assertPlaceVerificationExecuted(t, fixture.marker)
	})

	for _, tc := range []struct {
		name     string
		mutate   func(*testing.T, installedPlaceVerificationFixture)
		repoRoot func(installedPlaceVerificationFixture) string
		want     string
	}{
		{
			name: "dirty package refuses before execution",
			mutate: func(t *testing.T, fixture installedPlaceVerificationFixture) {
				if err := os.WriteFile(fixture.target, []byte("edited before check\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			repoRoot: func(fixture installedPlaceVerificationFixture) string {
				return filepath.Join(fixture.admission.env.Home, "repo")
			},
			want: "target.place.drift",
		},
		{
			name:     "repository-local executable is refused",
			repoRoot: func(fixture installedPlaceVerificationFixture) string { return fixture.admission.env.Home },
			want:     "target.place.repository-executable-refused",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newInstalledPlaceVerificationFixture(t)
			if tc.mutate != nil {
				tc.mutate(t, fixture)
			}
			repoRoot := tc.repoRoot(fixture)
			if repoRoot != fixture.admission.env.Home {
				if err := os.Mkdir(repoRoot, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			owner, _ := openPlacePublishOwnership(t, fixture.admission)
			stateBefore, stateInfoBefore := placeVerificationStateSnapshot(t, owner.StateFile())
			if _, err := runPlaceVerification(
				context.Background(), fixture.admission.binding, fixture.admission.raw,
				fixture.admission.env, repoRoot, "invented-app/mod", "sample",
			); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refused check error = %v, want %q", err, tc.want)
			}
			stateAfter, stateInfoAfter := placeVerificationStateSnapshot(t, owner.StateFile())
			if !bytes.Equal(stateBefore, stateAfter) || !stateInfoBefore.ModTime().Equal(stateInfoAfter.ModTime()) {
				t.Fatal("refused check changed ownership state")
			}
			assertPlaceVerificationNotExecuted(t, fixture.marker)
		})
	}
}

func TestRunPlaceVerificationHonorsCallerContext(t *testing.T) {
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(want.Error(), func(t *testing.T) {
			// The fixture approves and installs this exact checker. After validating
			// the named check's argv it writes its marker, then blocks in sleep.
			fixture := newInstalledPlaceVerificationFixtureForProbe(t, "exec /bin/sleep 12", false, false, false)
			admission := fixture.admission
			repoRoot := t.TempDir()
			owner, _ := openPlacePublishOwnership(t, admission)
			before, infoBefore := placeVerificationStateSnapshot(t, owner.StateFile())
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			done := make(chan struct{})
			var verifyErr error
			go func() {
				defer close(done)
				_, verifyErr = runPlaceVerification(ctx, admission.binding, admission.raw, admission.env,
					repoRoot, "invented-app/mod", "sample")
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(15 * time.Second):
					t.Error("verification did not stop during cleanup")
				}
			})

			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				data, err := os.ReadFile(fixture.marker)
				if err == nil && string(data) == "invoked" {
					break
				}
				if err != nil && !errors.Is(err, fs.ErrNotExist) {
					t.Fatal(err)
				}
				select {
				case <-done:
					t.Fatalf("verification returned before checker was ready: %v", verifyErr)
				case <-ctx.Done():
					t.Fatal("checker did not signal readiness before the caller deadline")
				case <-tick.C:
				}
			}
			if deadline, _ := ctx.Deadline(); !time.Now().Before(deadline) {
				t.Fatal("observed checker readiness only after the caller deadline")
			}
			if want == context.Canceled {
				cancel()
			}
			<-ctx.Done()
			select {
			case <-done:
				if !errors.Is(verifyErr, want) {
					t.Fatalf("verification returned %v, want %v", verifyErr, want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("running check did not stop with the caller context")
			}
			after, infoAfter := placeVerificationStateSnapshot(t, owner.StateFile())
			if !bytes.Equal(before, after) || !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
				t.Fatal("interrupted verification changed ownership state")
			}
			owner, snapshot := openPlacePublishOwnership(t, admission)
			projection, err := owner.PlaceFromSnapshot(snapshot)
			if err != nil || len(projection.Packages) != 1 || projection.Packages[0].Verification != nil {
				t.Fatalf("want installed package without a verification result, got %#v, %v", projection, err)
			}
		})
	}
}

type installedPlaceVerificationFixture struct {
	admission      placeAdmissionFixture
	target         string
	hostExecutable string
	marker         string
}

func newInstalledPlaceVerificationFixture(t *testing.T) installedPlaceVerificationFixture {
	return newInstalledPlaceVerificationFixtureWithOutcome(t, 0, false)
}

func newInstalledPlaceVerificationFixtureWithVersionProbe(
	t *testing.T,
	mutateProbe bool,
) installedPlaceVerificationFixture {
	return newInstalledPlaceVerificationFixtureForProbe(t, "exit 0", false, true, mutateProbe)
}

func newInstalledPlaceVerificationFixtureWithOutcome(
	t *testing.T,
	exitCode int,
	mutate bool,
) installedPlaceVerificationFixture {
	return newInstalledPlaceVerificationFixtureForProbe(t, "exit "+strconv.Itoa(exitCode), mutate, false, false)
}

func newInstalledPlaceVerificationFixtureForProbe(
	t *testing.T,
	checkEnd string,
	mutate, withProbe, mutateProbe bool,
) installedPlaceVerificationFixture {
	t.Helper()
	fixture := newPlaceAdmissionFixture(t)
	profileText := strings.Replace(
		placeAdmissionProfileYAML,
		"kinds: {mod: {deliver: place, into: [mods/]}}",
		`host: {executable: fixture-checker}
checks: {initial-load: {scope: eager-package-load, argv: [check, "{subject}"], subject: {under: mods, component: package-name}}}
kinds: {mod: {deliver: place, into: [mods/], verify: initial-load}}`,
		1,
	)
	if withProbe {
		profileText = strings.Replace(profileText, "host: {executable: fixture-checker}",
			`host: {executable: fixture-checker, version_probe: {argv: [--version], pattern: '(\d+\.\d+\.\d+)'}, verified: [{range: ">=1.0.0 <2.0.0", date: 2026-11-12, evidence: fixture}]}`,
			1,
		)
	}
	raw := []byte(profileText)
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
	subject := filepath.Join(fixture.root, "mods", "sample")
	script := "#!/bin/sh\n"
	if withProbe {
		script += "if [ \"$#\" -eq 1 ] && [ \"$1\" = \"--version\" ]; then printf 'version\\n' >> " +
			shellQuote(marker) + "; "
		if mutateProbe {
			script += "printf 'changed during version probe\\n' > " +
				shellQuote(filepath.Join(fixture.root, "mods", "sample", "guide.md")) + "; "
		}
		script += "printf 'version 1.2.3\\n'; exit 0; fi\n"
	}
	script += "if [ \"$#\" -ne 2 ] || [ \"$1\" != \"check\" ] || [ \"$2\" != " + shellQuote(subject) + " ]; then exit 81; fi\n" +
		"if [ \"$HOME\" != " + shellQuote(fixture.env.Home) + " ] || [ \"$LANG\" != \"C\" ] || [ \"${PLACE_VERIFY_MARKER+x}\" = \"x\" ]; then exit 82; fi\n" +
		"if IFS= read -r input; then exit 83; fi\n" +
		"printf 'noisy stdout\\n'\nprintf 'noisy stderr\\n' >&2\n"
	if mutate {
		script += "printf invoked > " + shellQuote(marker) + "\n"
		script += "printf 'changed during check\\n' > " + shellQuote(filepath.Join(fixture.root, "mods", "sample", "guide.md")) + "\n"
	} else if withProbe {
		script += "printf 'check\\n' >> " + shellQuote(marker) + "\n"
	} else {
		script += "printf invoked > " + shellQuote(marker) + "\n"
	}
	script += checkEnd + "\n"
	if err := os.WriteFile(hostExecutable, []byte(script), 0o700); err != nil {
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

func assertPlaceVerificationExecuted(t *testing.T, marker string) {
	t.Helper()
	if data, err := os.ReadFile(marker); err != nil || string(data) != "invoked" {
		t.Fatalf("verification execution marker = %q, %v", data, err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
