package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// spec: owned-entry-reconciliation/missing-or-invalid-git-acquisition-evidence-refuses-repair
// spec: owned-entry-reconciliation/confirmed-missing-or-stale-writable-lock-precedes-aggregate-repair
// spec: doctor-repair-planning/off-terminal-repair-without-yes-is-preview-only
// spec: doctor-repair-planning/declining-the-repair-plan-changes-nothing
// spec: doctor-repair-planning/repair-with-yes-runs-only-safe-steps
// spec: doctor-repair-planning/repair-exits-one-when-only-blocked-or-human-only-items-remain
func TestDoctorRepairAcquisitionMissingLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}

	t.Run("preview", func(t *testing.T) {
		_, lockPath, aggregatePath, aggregateBefore := setupDoctorRepairMissingLock(t)
		output, err := executeCommand("doctor", "--repair")
		if err == nil {
			t.Fatalf("off-terminal repair accepted missing acquisition authority:\n%s", output)
		}
		assertMissingLockAndAggregateUnchanged(t, lockPath, aggregatePath, aggregateBefore)
	})

	t.Run("decline", func(t *testing.T) {
		_, lockPath, aggregatePath, aggregateBefore := setupDoctorRepairMissingLock(t)
		var out bytes.Buffer
		err := runDoctor(doctorOptions{
			out: &out, in: strings.NewReader("n\n"), isTTY: true, repair: true,
			probe: staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		})
		output := out.String()
		if err == nil {
			t.Fatalf("declining missing-lock repair exited successfully:\n%s", output)
		}
		if strings.Count(output, "[y/N]") != 1 {
			t.Fatalf("terminal repair did not ask for one plan confirmation:\n%s", output)
		}
		assertMissingLockAndAggregateUnchanged(t, lockPath, aggregatePath, aggregateBefore)
	})

	t.Run("safe-only", func(t *testing.T) {
		_, lockPath, aggregatePath, aggregateBefore := setupDoctorRepairMissingLock(t)
		output, err := executeCommand("doctor", "--repair", "--yes")
		if _, statErr := os.Stat(lockPath); statErr != nil {
			t.Fatalf("safe repair did not create an acquisition lock: %v\nrepair error: %v\n%s", statErr, err, output)
		}
		if !strings.Contains(output, `mcp-server "managed"`) {
			t.Fatalf("repair did not leave the destructive aggregate conflict pending:\n%s", output)
		}
		assertAggregateUnchanged(t, aggregatePath, aggregateBefore)
		doc := readLockDocument(t, lockPath)
		if doc.Scope != lockfile.ScopeRepo || len(doc.Registries) != 1 || doc.Registries[0].Commit.String() == "" {
			t.Fatalf("safe repair did not create a verified repository Git lock: %+v", doc)
		}
		checkOutput, checkErr := executeCommand("lock", "--check", "--scope", "repo")
		if checkErr != nil {
			t.Fatalf("lock check after safe repair: %v\n%s", checkErr, checkOutput)
		}
	})
}

// spec: doctor-repair-planning/confirmed-lock-repair-retains-current-pins
// spec: owned-entry-reconciliation/moving-a-git-ref-does-not-change-repair-values
func TestDoctorRepairAcquisitionRetainsPins(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := setupDoctorAcquisitionPinFixture(t)
	working, lockPath := fixture.working, fixture.lockPath
	before := fixture.before
	beforeRegistry, beforeRoot := doctorAcquisitionGitRoot(t, before, "first")
	beforeResource := doctorAcquisitionGitResource(t, beforeRoot, "skill", "keeper")

	writeSkill(t, filepath.Join(working, "first"), "keeper", "Keeper on moved ref", nil)
	writeSkill(t, filepath.Join(working, "second"), "added", "Added on moved ref", nil)
	commitAndPushAll(t, working, "advance registry ref")
	writeDoctorAcquisitionPinConfig(t, fixture, true, nil)

	output, err := executeCommand("doctor", "--repair", "--yes")
	if err != nil {
		t.Fatalf("repair changed writable lock requirements: %v\n%s", err, output)
	}
	after := readLockDocument(t, lockPath)
	afterRegistry, afterRoot := doctorAcquisitionGitRoot(t, after, "first")
	afterResource := doctorAcquisitionGitResource(t, afterRoot, "skill", "keeper")
	if beforeRegistry.Commit.String() != afterRegistry.Commit.String() {
		t.Fatalf("repair changed the existing registry commit: %s -> %s", beforeRegistry.Commit, afterRegistry.Commit)
	}
	if beforeRoot.RevisionSchema != afterRoot.RevisionSchema ||
		beforeRoot.RevisionDigest != afterRoot.RevisionDigest ||
		!reflect.DeepEqual(beforeResource, afterResource) {
		t.Fatalf("repair changed the existing root or resource pin:\nbefore: %+v / %+v\nafter: %+v / %+v", beforeRoot, beforeResource, afterRoot, afterResource)
	}
	_, addedRoot := doctorAcquisitionGitRoot(t, after, "second")
	if len(addedRoot.Resources) == 0 {
		t.Fatalf("repair did not add the newly selected source root: %+v", after.Registries)
	}

	checkOutput, checkErr := executeCommand("sync", "--check")
	if checkErr != nil {
		t.Fatalf("sync --check after pin-preserving repair: %v\n%s", checkErr, checkOutput)
	}
}

// spec: doctor-repair-planning/confirmed-lock-repair-retains-current-pins
func TestDoctorRepairAcquisitionRepairsWritableLockDiagnostics(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name         string
		code         string
		registration bool
		prepare      func(*testing.T, doctorAcquisitionPinFixture)
	}{
		{
			name: "missing",
			code: "lock.missing",
			prepare: func(t *testing.T, fixture doctorAcquisitionPinFixture) {
				if err := os.Remove(fixture.lockPath); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "stale",
			code: "lock.stale",
			prepare: func(t *testing.T, fixture doctorAcquisitionPinFixture) {
				writeDoctorAcquisitionPinConfig(t, fixture, true, nil)
			},
		},
		{
			name: "version-upgrade-required",
			code: "lock.version-upgrade-required",
			prepare: func(t *testing.T, fixture doctorAcquisitionPinFixture) {
				writeDoctorAcquisitionVersion(t, fixture, lockfile.APIVersionV1)
				writeDoctorAcquisitionPinConfig(t, fixture, false, []string{"factory"})
			},
		},
		{
			name: "version-deprecated",
			code: "lock.version-deprecated",
			prepare: func(t *testing.T, fixture doctorAcquisitionPinFixture) {
				writeDoctorAcquisitionVersion(t, fixture, lockfile.APIVersionV1)
				writeDoctorAcquisitionPinConfig(t, fixture, false, []string{})
				writeFile(t, filepath.Join(os.Getenv("HOME"), ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")
			},
		},
		{
			name: "unsupported-known-version",
			code: "lock.unsupported-version",
			prepare: func(t *testing.T, fixture doctorAcquisitionPinFixture) {
				raw, err := os.ReadFile(fixture.lockPath)
				if err != nil {
					t.Fatal(err)
				}
				unsupported := bytes.Replace(raw, []byte(lockfile.APIVersionV2), []byte("agent-marketplace-lock/v99"), 1)
				if bytes.Equal(raw, unsupported) {
					t.Fatal("fixture lock did not contain its supported apiVersion")
				}
				if err := os.WriteFile(fixture.lockPath, unsupported, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "known-shape-version-zero",
			code: "lock.unsupported-version",
			prepare: func(t *testing.T, fixture doctorAcquisitionPinFixture) {
				raw, err := os.ReadFile(fixture.lockPath)
				if err != nil {
					t.Fatal(err)
				}
				unsupported := bytes.Replace(raw, []byte(lockfile.APIVersionV2), []byte("agent-marketplace-lock/v0"), 1)
				if bytes.Equal(raw, unsupported) {
					t.Fatal("fixture lock did not contain its supported apiVersion")
				}
				if err := os.WriteFile(fixture.lockPath, unsupported, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unsupported-version-spelling",
			code: "lock.unsupported-version",
			prepare: func(t *testing.T, fixture doctorAcquisitionPinFixture) {
				raw, err := os.ReadFile(fixture.lockPath)
				if err != nil {
					t.Fatal(err)
				}
				unsupported := bytes.Replace(raw, []byte(lockfile.APIVersionV2), []byte("agent-marketplace-lock/future"), 1)
				if bytes.Equal(raw, unsupported) {
					t.Fatal("fixture lock did not contain its supported apiVersion")
				}
				if err := os.WriteFile(fixture.lockPath, unsupported, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{name: "change-required", code: "lock.change-required", registration: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.registration {
				fixture := newEndpointRegistrationFixture(t, lockfile.ScopeRepo)
				recordedURL, _ := fixture.recordedEndpoint(t)
				writeFile(t, fixture.configPath, "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+fixture.url+"}\n  recorded: {marketplace: "+recordedURL+"}\nharnesses: [factory]\n")
				writeFile(t, fixture.lockPath, "apiVersion: agent-marketplace-lock/v2\nscope: repo\nmarketplaces:\n  - url: "+fixture.url+"\n    instance_id: endpoint-mp\n")
				checkOutput, checkErr := executeCommand("lock", "--check")
				var lockErr *pipeline.LockViewError
				if !errors.As(checkErr, &lockErr) || lockErr.Code != test.code {
					t.Fatalf("lock --check error = %v, want %s\n%s", checkErr, test.code, checkOutput)
				}
				output, err := executeCommand("doctor", "--repair", "--yes")
				assertDoctorAcquisitionRepairStep(t, output, err, test.code)
				if _, err := os.Stat(fixture.lockPath); err != nil {
					t.Fatalf("safe acquisition repair did not create the registration lock: %v\n%s", err, output)
				}
				assertNoDoctorAcquisitionLockDiagnostics(t, true)
				return
			}

			fixture := setupDoctorAcquisitionPinFixture(t)
			test.prepare(t, fixture)
			if test.name == "version-upgrade-required" {
				checkOutput, checkErr := executeCommand("lock", "--check", "--scope", "repo")
				var lockErr *pipeline.LockViewError
				if !errors.As(checkErr, &lockErr) || lockErr.Code != test.code {
					t.Fatalf("lock --check error = %v, want %s\n%s", checkErr, test.code, checkOutput)
				}
			}
			if test.name == "version-deprecated" {
				beforePreview, err := os.ReadFile(fixture.lockPath)
				if err != nil {
					t.Fatal(err)
				}
				preview, previewErr := executeCommand("doctor", "--repair")
				if previewErr == nil {
					t.Fatalf("off-terminal repair accepted deprecated acquisition lock:\n%s", preview)
				}
				var plannedDeprecatedLock bool
				for _, line := range strings.Split(preview, "\n") {
					if strings.Contains(line, "[safe]") && strings.Contains(line, test.code) {
						plannedDeprecatedLock = true
						break
					}
				}
				if !plannedDeprecatedLock {
					t.Fatalf("off-terminal repair did not plan the safe %s step:\n%s", test.code, preview)
				}
				afterPreview, err := os.ReadFile(fixture.lockPath)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(beforePreview, afterPreview) {
					t.Fatalf("off-terminal repair changed acquisition lock bytes:\nbefore: %s\nafter:  %s", beforePreview, afterPreview)
				}
			}
			output, err := executeCommand("doctor", "--repair", "--yes")
			assertDoctorAcquisitionRepairStep(t, output, err, test.code)

			after := readLockDocument(t, fixture.lockPath)
			beforeRegistry, beforeRoot := doctorAcquisitionGitRoot(t, fixture.before, "first")
			afterRegistry, afterRoot := doctorAcquisitionGitRoot(t, after, "first")
			beforeResource := doctorAcquisitionGitResource(t, beforeRoot, "skill", "keeper")
			afterResource := doctorAcquisitionGitResource(t, afterRoot, "skill", "keeper")
			if beforeRegistry.Commit.String() != afterRegistry.Commit.String() {
				t.Fatalf("repair changed the current registry commit: %s -> %s", beforeRegistry.Commit, afterRegistry.Commit)
			}
			if beforeRoot.RevisionSchema != afterRoot.RevisionSchema ||
				beforeRoot.RevisionDigest != afterRoot.RevisionDigest ||
				!reflect.DeepEqual(beforeResource, afterResource) {
				t.Fatalf("repair changed the current root or resource pin:\nbefore: %+v / %+v\nafter: %+v / %+v", beforeRoot, beforeResource, afterRoot, afterResource)
			}
			if test.name == "stale" {
				_, _ = doctorAcquisitionGitRoot(t, after, "second")
			}
			if test.name == "version-deprecated" {
				writeFile(t, filepath.Join(os.Getenv("HOME"), ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			}
			assertNoDoctorAcquisitionLockDiagnostics(t, true)
		})
	}
}

// spec: doctor-repair-planning/confirmed-lock-repair-retains-current-pins
func TestDoctorRepairAcquisitionNormalizesKnownV1MarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newEndpointRegistrationFixture(t, lockfile.ScopeRepo)
	original := fixture.healthyLock()
	before, err := lockfile.Decode([]byte(original))
	if err != nil {
		t.Fatal(err)
	}
	versionlessShape := strings.Replace(original, lockfile.APIVersionV2, lockfile.APIVersionV1, 1)
	writeFile(t, fixture.lockPath, versionlessShape)

	output, err := executeCommand("doctor", "--repair", "--yes")
	assertDoctorAcquisitionRepairStep(t, output, err, "lock.unsupported-version")
	after := readLockDocument(t, fixture.lockPath)
	if len(before.Marketplaces) != 1 || !reflect.DeepEqual(before.Marketplaces, after.Marketplaces) {
		t.Fatalf("known-shape repair changed Marketplace pins:\nbefore: %+v\nafter: %+v", before.Marketplaces, after.Marketplaces)
	}
	if output, err := executeCommand("lock", "--check"); err != nil {
		t.Fatalf("lock --check after known-shape repair: %v\n%s", err, output)
	}
}

// spec: doctor-repair-planning/verification-refusal-preserves-acquisition-lock-bytes
// spec: owned-entry-reconciliation/missing-or-invalid-git-acquisition-evidence-refuses-repair
func TestDoctorRepairAcquisitionVerificationRefusalsPreserveLockBytes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, test := range []struct {
		name string
		code string
	}{
		{name: "invalid", code: "lock.invalid"},
		{name: "digest-invalid", code: "lock.digest-invalid"},
		{name: "manifest-mismatch", code: "lock.manifest-mismatch"},
		{name: "trust", code: "lock.trust"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, lockPath := setupReadOnlyExactRegistry(t)
			lockBefore, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			switch test.name {
			case "invalid":
				if err := os.WriteFile(lockPath, []byte("apiVersion: [\nscope: repo\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "digest-invalid":
				invalid := bytes.Replace(lockBefore, []byte("sha256:"), []byte("sha256:bad"), 1)
				if bytes.Equal(lockBefore, invalid) {
					t.Fatal("fixture lock has no SHA-256 digest to invalidate")
				}
				if err := os.WriteFile(lockPath, invalid, 0o644); err != nil {
					t.Fatal(err)
				}
			case "manifest-mismatch":
				doc := readLockDocument(t, lockPath)
				doc.Registries[0].Roots[0].RevisionDigest = lockfile.Digest("sha256:" + strings.Repeat("f", 64))
				mismatched, err := lockfile.Encode(doc)
				if err != nil {
					t.Fatalf("encode mismatched manifest lock: %v", err)
				}
				if err := os.WriteFile(lockPath, mismatched, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			lockBefore, err = os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}

			var output string
			var repairErr error
			if test.name == "trust" {
				var out bytes.Buffer
				repairErr = runDoctor(doctorOptions{
					out: &out, in: strings.NewReader(""), repair: true, yes: true,
					resolve: func(context.Context, string) ([]resolver.ResolvedArtifact, error) {
						return nil, &pipeline.LockViewError{
							Code: test.code, Scope: lockfile.ScopeRepo, Path: lockPath,
							Err: errors.New("acquisition lock trust verification failed"),
						}
					},
				})
				output = out.String()
			} else {
				output, repairErr = executeCommand("doctor", "--repair", "--yes")
			}
			if repairErr == nil || !strings.Contains(output, "[human-only]") || !strings.Contains(output, test.code) {
				t.Fatalf("repair did not retain the human-only %s refusal: %v\n%s", test.code, repairErr, output)
			}
			lockAfter, err := os.ReadFile(lockPath)
			if err != nil || !bytes.Equal(lockBefore, lockAfter) {
				t.Fatalf("human-only %s repair changed acquisition-lock bytes: %v\nbefore:\n%s\nafter:\n%s", test.code, err, lockBefore, lockAfter)
			}
		})
	}
}

// spec: doctor-repair-planning/organization-lock-repair-remains-human-only
func TestDoctorRepairAcquisitionNeverWritesOrganizationLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupDoctorAcquisitionRepairEnvironment(t)
	origin, working := createV4GitRegistry(t, "doctor-acquisition-organization.git")
	writeSkill(t, filepath.Join(working, "first"), "organization-skill", "Organization locked", nil)
	writeSkill(t, filepath.Join(working, "second"), "organization-skill", "Organization changed", nil)
	commitAndPushAll(t, working, "seed organization roots")

	authority := canonicalTempDir(t)
	orgPath := filepath.Join(authority, "config.yaml")
	lockPath := filepath.Join(authority, "lock.yaml")
	writeDoctorOrganizationAcquisitionConfig(t, orgPath, origin, "first")
	config.SetTestOrganizationConfigPath(t, orgPath)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("lock", "--scope", "organization"); err != nil {
		t.Fatalf("seed organization acquisition lock: %v\n%s", err, output)
	}
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	writeDoctorOrganizationAcquisitionConfig(t, orgPath, origin, "second")

	output, repairErr := executeCommand("doctor", "--repair", "--yes")
	if repairErr == nil || !strings.Contains(output, "human-only") {
		t.Fatalf("organization acquisition lock was not left as a human-only item: %v\n%s", repairErr, output)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(lockBefore, lockAfter) {
		t.Fatalf("doctor changed the organization acquisition lock: %v\nbefore:\n%s\nafter:\n%s", err, lockBefore, lockAfter)
	}
}

// spec: doctor-repair-planning/policy-change-before-a-confirmed-step-stops-repair
func TestDoctorRepairAcquisitionPolicyChangePreventsLockPublication(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, lockPath, aggregatePath, aggregateBefore := setupDoctorRepairMissingLock(t)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	input := doctorPlannerReadFunc(func(p []byte) (int, error) {
		if !changed {
			updated := append(append([]byte(nil), configBefore...), []byte("\n# changed after confirmation\n")...)
			if err := os.WriteFile(configPath, updated, 0o644); err != nil {
				return 0, err
			}
			changed = true
		}
		return strings.NewReader("y\n").Read(p)
	})
	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out: &out, in: input, isTTY: true, repair: true,
		probe: staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
	})
	if !changed || !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("repair error = %v, changed config = %t; want a pre-step policy generation conflict\n%s", err, changed, out.String())
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("policy change created the missing acquisition lock: %v", err)
	}
	assertAggregateUnchanged(t, aggregatePath, aggregateBefore)
}

// spec: doctor-repair-planning/missing-exact-snapshot-is-fetched-from-its-locked-commit
// spec: doctor-repair-planning/snapshot-fetch-failure-stops-repair
func TestDoctorRepairAcquisitionFetchesSnapshot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}

	t.Run("fetches locked snapshot", func(t *testing.T) {
		_, _, _, lockPath := setupReadOnlyExactRegistry(t)
		lockBefore, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		removeDoctorAcquisitionSnapshot(t)

		output, err := executeCommand("doctor", "--repair", "--yes")
		if err != nil {
			t.Fatalf("doctor did not fetch the locked snapshot: %v\n%s", err, output)
		}
		lockAfter, err := os.ReadFile(lockPath)
		if err != nil || !bytes.Equal(lockBefore, lockAfter) {
			t.Fatalf("snapshot repair changed acquisition-lock bytes: %v\nbefore:\n%s\nafter:\n%s", err, lockBefore, lockAfter)
		}
		snapshots := exactSnapshotDirs(t)
		if len(snapshots) != 1 {
			t.Fatalf("exact snapshot count = %d, want one: %v", len(snapshots), snapshots)
		}
		skill, err := os.ReadFile(filepath.Join(snapshots[0], "skills", "locked-skill", "SKILL.md"))
		if err != nil || !bytes.Contains(skill, []byte("Locked")) {
			t.Fatalf("repair did not restore the locked snapshot: %v\n%s", err, skill)
		}
	})

	t.Run("unavailable endpoint", func(t *testing.T) {
		_, origin, _, lockPath := setupReadOnlyExactRegistry(t)
		lockBefore, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		removeDoctorAcquisitionSnapshot(t)
		if err := os.RemoveAll(origin); err != nil {
			t.Fatalf("make local registry unavailable: %v", err)
		}
		if err := os.RemoveAll(cache.CacheDir()); err != nil {
			t.Fatalf("clear the isolated acquisition cache: %v", err)
		}

		output, err := executeCommand("doctor", "--repair", "--yes")
		repairResult := output
		if err != nil {
			repairResult += err.Error()
		}
		if err == nil || !strings.Contains(output, "[safe]") ||
			!strings.Contains(output, "lock.snapshot-unavailable") ||
			!strings.Contains(repairResult, "repair failed:") {
			t.Fatalf("repair did not run and fail the confirmed safe locked-snapshot fetch: %v\n%s", err, output)
		}
		lockAfter, readErr := os.ReadFile(lockPath)
		if readErr != nil || !bytes.Equal(lockBefore, lockAfter) {
			t.Fatalf("failed snapshot repair changed acquisition-lock bytes: %v\nbefore:\n%s\nafter:\n%s", readErr, lockBefore, lockAfter)
		}
	})
}

// spec: doctor-repair-planning/steps-added-by-re-diagnosis-are-confirmed-before-they-run
// spec: doctor-repair-planning/missing-exact-snapshot-is-fetched-from-its-locked-commit
func TestDoctorRepairAcquisitionCacheStepReconfirmation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, testCase := range []struct {
		name      string
		harnesses []string
	}{
		{name: "empty-harnesses", harnesses: []string{}},
		{name: "factory-harness", harnesses: []string{"factory"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			testDoctorRepairAcquisitionCacheStepReconfirmation(t, testCase.harnesses)
		})
	}
}

func testDoctorRepairAcquisitionCacheStepReconfirmation(t *testing.T, harnesses []string) {
	t.Helper()
	setupDoctorAcquisitionRepairEnvironment(t)
	home, repoRoot := os.Getenv("HOME"), filepath.Join(t.TempDir(), "repo")
	userOrigin, userWorking := createV4GitRegistry(t, "doctor-user-cache.git")
	writeSkill(t, userWorking, "user-cache-skill", "User cache snapshot", nil)
	commitAndPushAll(t, userWorking, "seed user cache registry")
	repoOrigin, repoWorking := createV4GitRegistry(t, "doctor-repo-cache.git")
	writeSkill(t, repoWorking, "repo-cache-skill", "Repository cache snapshot", nil)
	commitAndPushAll(t, repoWorking, "seed repository cache registry")

	userConfig := filepath.Join(home, ".agents", "config.yaml")
	repoConfig := filepath.Join(repoRoot, ".agents", "config.yaml")
	userLock := filepath.Join(home, ".agents", "lock.yaml")
	repoLock := filepath.Join(repoRoot, ".agents", "lock.yaml")
	harnessList := strings.Join(harnesses, ", ")
	writeConfig(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: ["+harnessList+"]\nsources:\n  user-cache: {git: "+userOrigin+", ref: main}\nresources:\n  user-cache: {include: [skill/user-cache-skill]}\n")
	writeConfig(t, repoConfig, "apiVersion: agent-marketplace/v4\nharnesses: ["+harnessList+"]\nsources:\n  repo-cache: {git: "+repoOrigin+", ref: main}\nresources:\n  repo-cache: {include: [skill/repo-cache-skill]}\n")
	withWorkingDir(t, repoRoot)
	for _, scope := range []string{"user", "repo"} {
		if output, err := executeCommand("lock", "--scope", scope); err != nil {
			t.Fatalf("create %s acquisition lock: %v\n%s", scope, err, output)
		}
	}

	findSnapshot := func(skill, marker string) string {
		t.Helper()
		var found string
		for _, dir := range exactSnapshotDirs(t) {
			path := filepath.Join(dir, "skills", skill, "SKILL.md")
			content, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Fatalf("read %s snapshot skill: %v", skill, err)
			}
			if !strings.Contains(string(content), marker) {
				t.Fatalf("%s snapshot skill did not contain %q:\n%s", skill, marker, content)
			}
			if found != "" {
				t.Fatalf("found more than one exact snapshot for %s: %s and %s", skill, found, dir)
			}
			found = dir
		}
		if found == "" {
			t.Fatalf("no exact snapshot contains %s", skill)
		}
		return found
	}
	userSnapshot := findSnapshot("user-cache-skill", "User cache snapshot")
	repoSnapshot := findSnapshot("repo-cache-skill", "Repository cache snapshot")
	if userSnapshot == repoSnapshot {
		t.Fatalf("different registry URLs shared an exact snapshot: %s", userSnapshot)
	}
	authoredPaths := []string{userConfig, repoConfig, userLock, repoLock}
	authoredBytes := make(map[string][]byte, len(authoredPaths))
	for _, path := range authoredPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read authored input %s: %v", path, err)
		}
		authoredBytes[path] = data
	}
	assertAuthoredBytes := func() error {
		for path, want := range authoredBytes {
			got, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !bytes.Equal(got, want) {
				return errors.New("doctor changed authored config or lock bytes at " + path)
			}
		}
		return nil
	}
	if err := os.RemoveAll(userSnapshot); err != nil {
		t.Fatalf("remove user exact snapshot: %v", err)
	}

	var out bytes.Buffer
	reads := 0
	input := doctorPlannerReadFunc(func(p []byte) (int, error) {
		reads++
		if err := assertAuthoredBytes(); err != nil {
			return 0, err
		}
		switch reads {
		case 1:
			if _, err := os.Stat(repoSnapshot); err != nil {
				return 0, err
			}
			if err := os.RemoveAll(repoSnapshot); err != nil {
				return 0, err
			}
			return strings.NewReader("y\n").Read(p)
		case 2:
			return strings.NewReader("n\n").Read(p)
		default:
			return 0, io.EOF
		}
	})
	repairErr := runDoctor(doctorOptions{
		out: &out, in: input, isTTY: true, repair: true,
		probe: staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
	})
	output := out.String()
	if repairErr == nil {
		t.Errorf("declining a missing exact snapshot reported success:\n%s", output)
	}
	stepCount := strings.Count(output, "[safe] fetch exact snapshot from its locked commit")
	confirmationCount := strings.Count(output, "[y/N]")
	if reads != 2 || stepCount != 2 || confirmationCount != 2 {
		t.Errorf("repair requested %d reads, showed %d cache steps, and prompted %d times; want two of each (error: %v):\n%s", reads, stepCount, confirmationCount, repairErr, output)
	}
	if err := assertAuthoredBytes(); err != nil {
		t.Error(err)
	}
	userSkill, err := os.ReadFile(filepath.Join(userSnapshot, "skills", "user-cache-skill", "SKILL.md"))
	if err != nil || !strings.Contains(string(userSkill), "User cache snapshot") {
		t.Errorf("user exact snapshot was not restored: %v\n%s", err, userSkill)
	}
	if _, err := os.Stat(repoSnapshot); !os.IsNotExist(err) {
		t.Errorf("declining the repository cache step did not leave its exact snapshot absent: %v", err)
	}
}

type doctorAcquisitionPinFixture struct {
	origin     string
	working    string
	repoRoot   string
	configPath string
	lockPath   string
	before     lockfile.Document
}

func setupDoctorAcquisitionPinFixture(t *testing.T) doctorAcquisitionPinFixture {
	t.Helper()
	setupDoctorAcquisitionRepairEnvironment(t)
	origin, working := createV4GitRegistry(t, "doctor-acquisition-pins.git")
	writeSkill(t, filepath.Join(working, "first"), "keeper", "Keeper locked", nil)
	writeSkill(t, filepath.Join(working, "second"), "added", "Added locked", nil)
	commitAndPushAll(t, working, "seed both roots")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	fixture := doctorAcquisitionPinFixture{
		origin: origin, working: working, repoRoot: repoRoot,
		configPath: filepath.Join(repoRoot, ".agents", "config.yaml"),
		lockPath:   filepath.Join(repoRoot, ".agents", "lock.yaml"),
	}
	writeDoctorAcquisitionPinConfig(t, fixture, false, nil)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("create initial exact lock: %v\n%s", err, output)
	}
	fixture.before = readLockDocument(t, fixture.lockPath)
	return fixture
}

func writeDoctorAcquisitionPinConfig(t *testing.T, fixture doctorAcquisitionPinFixture, includeSecond bool, harnesses []string) {
	writeDoctorAcquisitionPinConfigWithSelection(t, fixture, includeSecond, harnesses, false)
}

func writeDoctorAcquisitionPinConfigWithSelection(t *testing.T, fixture doctorAcquisitionPinFixture, includeSecond bool, harnesses []string, tagSecond bool) {
	t.Helper()
	var content strings.Builder
	content.WriteString("apiVersion: agent-marketplace/v4\n")
	if harnesses != nil {
		content.WriteString("harnesses: [" + strings.Join(harnesses, ", ") + "]\n")
	}
	content.WriteString("sources:\n")
	content.WriteString("  first: {git: " + fixture.origin + ", ref: main, path: first}\n")
	if includeSecond {
		content.WriteString("  second: {git: " + fixture.origin + ", ref: main, path: second}\n")
	}
	content.WriteString("resources:\n")
	content.WriteString("  first: {include: [skill/keeper]}\n")
	if includeSecond {
		if tagSecond {
			content.WriteString("  second: {tags: [wanted]}\n")
		} else {
			content.WriteString("  second: {include: [skill/added]}\n")
		}
	}
	writeConfig(t, fixture.configPath, content.String())
}

func writeDoctorAcquisitionVersion(t *testing.T, fixture doctorAcquisitionPinFixture, version string) {
	t.Helper()
	doc := readLockDocument(t, fixture.lockPath)
	doc.APIVersion = version
	raw, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatalf("encode %s acquisition lock: %v", version, err)
	}
	if err := os.WriteFile(fixture.lockPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDoctorOrganizationAcquisitionConfig(t *testing.T, path, origin, root string) {
	t.Helper()
	writeConfig(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  organization: {git: "+origin+", ref: main, path: "+root+"}\nresources:\n  organization: {include: [skill/organization-skill]}\n")
}

func assertDoctorAcquisitionRepairStep(t *testing.T, output string, err error, code string) {
	t.Helper()
	if err != nil {
		t.Fatalf("doctor repair for %s: %v\n%s", code, err, output)
	}
	if !strings.Contains(output, "[safe]") || !strings.Contains(output, code) {
		t.Fatalf("doctor repair did not plan the safe %s step:\n%s", code, output)
	}
}

var doctorLockDiagnosticToken = regexp.MustCompile(`\block\.[a-z][a-z-]*:`)

func assertNoDoctorAcquisitionLockDiagnostics(t *testing.T, allowDrift bool) {
	t.Helper()
	output, err := executeCommand("sync", "--check")
	result := output
	if err != nil {
		result += "\n" + err.Error()
	}
	if code := doctorLockDiagnosticToken.FindString(result); code != "" {
		t.Fatalf("sync --check still reports acquisition-lock diagnostic %s: %v\n%s", code, err, output)
	}
	if err != nil && (!allowDrift || !strings.Contains(result, "out of sync")) {
		t.Fatalf("sync --check failed outside the allowed drift boundary: %v\n%s", err, output)
	}
}

func setupDoctorRepairMissingLock(t *testing.T) (string, string, string, []byte) {
	t.Helper()
	setupDoctorAcquisitionRepairEnvironment(t)
	origin, working := createV4GitRegistry(t, "doctor-acquisition-missing.git")
	writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"managed":{"command":"generated-binary"}}}`)
	commitAndPushAll(t, working, "seed managed MCP")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [devin]
sources:
  locked:
    git: `+origin+`
    ref: main
resources:
  locked:
    include: [mcp/managed]
`)
	aggregatePath := filepath.Join(repoRoot, ".devin", "mcp_config.json")
	writeFile(t, aggregatePath, `{"mcpServers":{"managed":{"command":"handwritten-binary"}}}`)
	withWorkingDir(t, repoRoot)
	aggregateBefore, err := os.ReadFile(aggregatePath)
	if err != nil {
		t.Fatal(err)
	}
	return repoRoot, filepath.Join(repoRoot, ".agents", "lock.yaml"), aggregatePath, aggregateBefore
}

func setupDoctorAcquisitionRepairEnvironment(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
}

func assertMissingLockAndAggregateUnchanged(t *testing.T, lockPath, aggregatePath string, aggregateBefore []byte) {
	t.Helper()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("repair created an acquisition lock: %v", err)
	}
	assertAggregateUnchanged(t, aggregatePath, aggregateBefore)
}

func assertAggregateUnchanged(t *testing.T, aggregatePath string, aggregateBefore []byte) {
	t.Helper()
	aggregateAfter, err := os.ReadFile(aggregatePath)
	if err != nil || !bytes.Equal(aggregateBefore, aggregateAfter) {
		t.Fatalf("repair changed handwritten aggregate bytes: %v\nbefore:\n%s\nafter:\n%s", err, aggregateBefore, aggregateAfter)
	}
}

func removeDoctorAcquisitionSnapshot(t *testing.T) {
	t.Helper()
	snapshots := exactSnapshotDirs(t)
	if len(snapshots) != 1 {
		t.Fatalf("exact snapshot count = %d, want one: %v", len(snapshots), snapshots)
	}
	if err := os.RemoveAll(snapshots[0]); err != nil {
		t.Fatalf("remove fixture exact snapshot: %v", err)
	}
}

func doctorAcquisitionGitRoot(t *testing.T, doc lockfile.Document, path string) (lockfile.GitRegistry, lockfile.GitRoot) {
	t.Helper()
	for _, registry := range doc.Registries {
		for _, root := range registry.Roots {
			if root.Path == path {
				return registry, root
			}
		}
	}
	t.Fatalf("locked Git root %q not found in %+v", path, doc.Registries)
	return lockfile.GitRegistry{}, lockfile.GitRoot{}
}

func doctorAcquisitionGitResource(t *testing.T, root lockfile.GitRoot, kind, name string) lockfile.GitResource {
	t.Helper()
	for _, resource := range root.Resources {
		if resource.Kind == kind && resource.Name == name {
			return resource
		}
	}
	t.Fatalf("locked Git resource %s/%s not found in %+v", kind, name, root.Resources)
	return lockfile.GitResource{}
}
