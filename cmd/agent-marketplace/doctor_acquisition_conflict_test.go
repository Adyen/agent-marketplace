package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// spec: doctor-repair-planning/destructive-lock-regeneration-preserves-backup-and-agreed-pins
func TestDoctorRepairAcquisitionBackupIdentity(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}

	for _, test := range []struct {
		name     string
		replaced bool
	}{
		{name: "matching file"},
		{name: "replacement at backup name", replaced: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent, err := fsboundary.OpenDir(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = parent.Close() }()

			file, name, err := parent.CreateTemp("backup-*.bak")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()

			original := []byte("original lock bytes")
			if n, err := file.Write(original); err != nil || n != len(original) {
				t.Fatalf("write original backup: wrote %d bytes, err = %v", n, err)
			}
			if err := file.Sync(); err != nil {
				t.Fatal(err)
			}

			foreign := []byte("different replacement bytes")
			movedName := name + ".moved"
			if test.replaced {
				if err := os.Rename(filepath.Join(parent.Path(), name), filepath.Join(parent.Path(), movedName)); err != nil {
					t.Fatal(err)
				}
				replacement, err := os.OpenFile(filepath.Join(parent.Path(), name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				if n, err := replacement.Write(foreign); err != nil || n != len(foreign) {
					_ = replacement.Close()
					t.Fatalf("write replacement: wrote %d bytes, err = %v", n, err)
				}
				if err := replacement.Close(); err != nil {
					t.Fatal(err)
				}
			}

			destination, err := captureDoctorAcquisitionBackup(parent, name, file)
			if test.replaced {
				if destination != nil {
					_ = destination.Close()
				}
				if !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("capture error = %v, want generation conflict", err)
				}
				if destination != nil {
					t.Fatal("capture accepted a destination for a different file")
				}
				if got, err := os.ReadFile(filepath.Join(parent.Path(), movedName)); err != nil || !bytes.Equal(got, original) {
					t.Fatalf("moved original backup = %q, %v; want %q", got, err, original)
				}
				if got, err := os.ReadFile(filepath.Join(parent.Path(), name)); err != nil || !bytes.Equal(got, foreign) {
					t.Fatalf("replacement backup = %q, %v; want %q", got, err, foreign)
				}
				return
			}

			if err != nil {
				t.Fatalf("capture matching backup: %v", err)
			}
			if destination == nil {
				t.Fatal("capture returned no destination for the matching file")
			}
			if err := destination.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(filepath.Join(parent.Path(), name)); err != nil || !bytes.Equal(got, original) {
				t.Fatalf("captured backup = %q, %v; want %q", got, err, original)
			}
		})
	}
}

// spec: doctor-repair-planning/destructive-lock-regeneration-preserves-backup-and-agreed-pins
// spec: doctor-repair-planning/destructive-lock-repair-cannot-run-through-safe-automation
func TestDoctorRepairAcquisitionDestructiveLocks(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}

	diagnostics := []string{
		"lock.conflicted",
		"lock.alias-forbidden",
		"lock.merge-forbidden",
		"lock.duplicate-field",
	}
	for _, code := range diagnostics {
		t.Run(code, func(t *testing.T) {
			fixture := setupDoctorAcquisitionPinFixture(t)
			seed, err := os.ReadFile(fixture.lockPath)
			if err != nil {
				t.Fatal(err)
			}
			defect := doctorAcquisitionDefect(t, code, seed)
			for _, mode := range []struct {
				name  string
				tty   bool
				yes   bool
				input string
			}{
				{name: "preview"},
				{name: "terminal-yes", tty: true, yes: true},
				{name: "off-terminal-yes", yes: true},
				{name: "declined", tty: true, input: "n\n"},
			} {
				t.Run(mode.name, func(t *testing.T) {
					writeFile(t, fixture.lockPath, string(defect))
					output, _ := runDoctorAcquisitionRepair(t, mode.tty, mode.yes, mode.input)
					if !strings.Contains(output, "[destructive]") || !strings.Contains(output, code) {
						t.Fatalf("repair did not preserve destructive classification for %s:\n%s", code, output)
					}
					if mode.name == "declined" && !strings.Contains(output, "[y/N]") {
						t.Fatalf("lock replacement lacked a per-lock confirmation:\n%s", output)
					}
					assertDoctorAcquisitionLockBytes(t, fixture.lockPath, defect)
					assertDoctorAcquisitionNoBackup(t, fixture.lockPath)
				})
			}

			writeFile(t, fixture.lockPath, string(defect))
			output, err := runDoctorAcquisitionRepair(t, true, false, "y\n")
			if err != nil {
				t.Fatalf("confirmed %s repair: %v\n%s", code, err, output)
			}
			backups := doctorAcquisitionBackups(t, fixture.lockPath)
			if len(backups) != 1 {
				t.Fatalf("confirmed repair made %d backups, want one: %v", len(backups), backups)
			}
			backupBytes, err := os.ReadFile(backups[0])
			if err != nil || !bytes.Equal(backupBytes, defect) {
				t.Fatalf("backup does not contain the original lock bytes: %v\nwant:\n%s\ngot:\n%s", err, defect, backupBytes)
			}
			backupInfo, err := os.Stat(backups[0])
			if err != nil || backupInfo.Mode().Perm() != 0o600 {
				t.Fatalf("backup mode = %v, %v; want 0600", backupInfo, err)
			}
			after := readLockDocument(t, fixture.lockPath)
			beforeRegistry, beforeRoot := doctorAcquisitionGitRoot(t, fixture.before, "first")
			afterRegistry, afterRoot := doctorAcquisitionGitRoot(t, after, "first")
			beforeResource := doctorAcquisitionGitResource(t, beforeRoot, "skill", "keeper")
			afterResource := doctorAcquisitionGitResource(t, afterRoot, "skill", "keeper")
			if beforeRegistry.Commit != afterRegistry.Commit ||
				beforeRoot.RevisionDigest != afterRoot.RevisionDigest ||
				!reflect.DeepEqual(beforeResource, afterResource) {
				t.Fatalf("destructive repair changed the existing pin:\nbefore: %+v / %+v\nafter: %+v / %+v", beforeRegistry, beforeResource, afterRegistry, afterResource)
			}
			assertNoDoctorAcquisitionLockDiagnostics(t, true)
		})
	}
}

// spec: doctor-repair-planning/divergent-conflict-pins-require-an-explicit-choice
func TestDoctorRepairAcquisitionDivergentPins(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := setupDoctorAcquisitionPinFixture(t)
	leftBytes, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	leftRegistry, _ := doctorAcquisitionGitRoot(t, fixture.before, "first")

	writeSkill(t, filepath.Join(fixture.working, "first"), "keeper", "Keeper on the second commit", nil)
	commitAndPushAll(t, fixture.working, "advance selected registry")
	if err := os.Remove(fixture.lockPath); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("lock", "--scope", "repo")
	if err != nil {
		t.Fatalf("prepare right conflict side: %v\n%s", err, output)
	}
	rightBytes, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	rightDoc := readLockDocument(t, fixture.lockPath)
	rightRegistry, _ := doctorAcquisitionGitRoot(t, rightDoc, "first")
	if leftRegistry.Commit == rightRegistry.Commit {
		t.Fatal("fixture's conflict sides do not have different registry commits")
	}
	writeSkill(t, filepath.Join(fixture.working, "first"), "keeper", "Keeper on the newest commit", nil)
	commitAndPushAll(t, fixture.working, "advance registry beyond both pins")
	divergent := doctorAcquisitionConflict(leftBytes, rightBytes)

	for _, choice := range []struct {
		name string
		want string
	}{
		{name: "left", want: leftRegistry.Commit.String()},
		{name: "right", want: rightRegistry.Commit.String()},
	} {
		t.Run(choice.name, func(t *testing.T) {
			writeFile(t, fixture.lockPath, string(divergent))
			output, err := runDoctorAcquisitionRepair(t, true, false, choice.name+"\ny\n")
			if err != nil {
				t.Fatalf("choose %s exact pin: %v\n%s", choice.name, err, output)
			}
			if !strings.Contains(output, leftRegistry.Commit.String()) || !strings.Contains(output, rightRegistry.Commit.String()) {
				t.Fatalf("pin choice did not show both exact commits:\n%s", output)
			}
			after := readLockDocument(t, fixture.lockPath)
			registry, _ := doctorAcquisitionGitRoot(t, after, "first")
			if got := registry.Commit.String(); got != choice.want {
				t.Fatalf("selected %s commit = %s, want %s", choice.name, got, choice.want)
			}
			assertNoDoctorAcquisitionLockDiagnostics(t, true)
		})
	}

	for _, mode := range []struct {
		name string
		tty  bool
		yes  bool
	}{
		{name: "off-terminal"},
		{name: "terminal-yes", tty: true, yes: true},
		{name: "off-terminal-yes", yes: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			backupsBefore := doctorAcquisitionBackups(t, fixture.lockPath)
			writeFile(t, fixture.lockPath, string(divergent))
			output, _ := runDoctorAcquisitionRepair(t, mode.tty, mode.yes, "")
			if !strings.Contains(output, "[guided]") || !strings.Contains(output, "lock.conflicted") {
				t.Fatalf("unresolved divergent pins were not guided:\n%s", output)
			}
			assertDoctorAcquisitionLockBytes(t, fixture.lockPath, divergent)
			if backupsAfter := doctorAcquisitionBackups(t, fixture.lockPath); len(backupsAfter) != len(backupsBefore) {
				t.Fatalf("deferred repair created a backup: before=%v after=%v", backupsBefore, backupsAfter)
			}
		})
	}

	t.Run("selected and deselected one-sided roots", func(t *testing.T) {
		fixture := setupDoctorAcquisitionPinFixture(t)
		writeSkill(t, filepath.Join(fixture.working, "second"), "added", "Second root", nil)
		writeSkill(t, filepath.Join(fixture.working, "third"), "discarded", "Third root", nil)
		commitAndPushAll(t, fixture.working, "add roots for conflict views")
		writeDoctorAcquisitionRootsConfig(t, fixture, "first", "second", "third")
		if err := os.Remove(fixture.lockPath); err != nil {
			t.Fatal(err)
		}
		output, err := executeCommand("lock", "--scope", "repo")
		if err != nil {
			t.Fatalf("seed all selected roots: %v\n%s", err, output)
		}
		full := readLockDocument(t, fixture.lockPath)
		if len(full.Registries) != 1 || len(full.Registries[0].Roots) != 3 {
			t.Fatalf("fixture did not seed three roots: %+v", full.Registries)
		}
		left := doctorAcquisitionDocumentWithRoots(t, full, "first", "third")
		right := doctorAcquisitionDocumentWithRoots(t, full, "first", "second")
		leftBytes, err := lockfile.Encode(left)
		if err != nil {
			t.Fatal(err)
		}
		rightBytes, err := lockfile.Encode(right)
		if err != nil {
			t.Fatal(err)
		}
		writeDoctorAcquisitionRootsConfig(t, fixture, "first", "second")
		conflict := doctorAcquisitionConflict(leftBytes, rightBytes)
		writeFile(t, fixture.lockPath, string(conflict))

		output, err = runDoctorAcquisitionRepair(t, true, false, "y\n")
		if err != nil {
			t.Fatalf("merge equal-commit root pins: %v\n%s", err, output)
		}
		after := readLockDocument(t, fixture.lockPath)
		registry, _ := doctorAcquisitionGitRoot(t, after, "first")
		if registry.Commit != full.Registries[0].Commit {
			t.Fatalf("merged roots changed the exact registry pin: %s -> %s", full.Registries[0].Commit, registry.Commit)
		}
		first, _ := doctorAcquisitionGitRoot(t, after, "first")
		second, secondRoot := doctorAcquisitionGitRoot(t, after, "second")
		if first.Commit != registry.Commit || second.Commit != registry.Commit || secondRoot.Path != "second" {
			t.Fatalf("one-sided selected root was not retained: %+v", after.Registries)
		}
		for _, root := range after.Registries[0].Roots {
			if root.Path == "third" {
				t.Fatalf("deselected one-sided root was retained: %+v", root)
			}
		}
		assertNoDoctorAcquisitionLockDiagnostics(t, true)
	})
}

// spec: doctor-repair-planning/destructive-lock-regeneration-preserves-backup-and-agreed-pins
// spec: doctor-repair-planning/divergent-conflict-pins-require-an-explicit-choice
func TestDoctorRepairAcquisitionChoicePreservesOneSidedPin(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, selection := range []struct {
		name    string
		useTags bool
	}{
		{name: "explicit-include"},
		{name: "tag-selection", useTags: true},
	} {
		t.Run(selection.name, func(t *testing.T) {
			testDoctorRepairAcquisitionChoicePreservesOneSidedPin(t, selection.useTags)
		})
	}
}

func testDoctorRepairAcquisitionChoicePreservesOneSidedPin(t *testing.T, useTags bool) {
	t.Helper()
	fixture := setupDoctorAcquisitionPinFixture(t)
	leftBytes, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	leftRegistry, leftFirst := doctorAcquisitionGitRoot(t, fixture.before, "first")
	if len(fixture.before.Registries) != 1 || len(fixture.before.Registries[0].Roots) != 1 {
		t.Fatalf("left lock did not contain only the initially selected root: %+v", fixture.before.Registries)
	}
	leftKeeper := doctorAcquisitionGitResource(t, leftFirst, "skill", "keeper")

	writeSkill(t, filepath.Join(fixture.working, "first"), "keeper", "Keeper at commit B", nil)
	var tags []string
	if useTags {
		tags = []string{"wanted"}
	}
	writeSkill(t, filepath.Join(fixture.working, "second"), "added", "Added at commit B", tags)
	commitAndPushAll(t, fixture.working, "advance registry to commit B")
	writeDoctorAcquisitionPinConfigWithSelection(t, fixture, true, nil, useTags)
	if err := os.Remove(fixture.lockPath); err != nil {
		t.Fatalf("remove fixture lock before generating right side: %v", err)
	}
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("generate right conflict side at commit B: %v\n%s", err, output)
	}
	rightBytes, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	right := readLockDocument(t, fixture.lockPath)
	rightRegistry, rightFirst := doctorAcquisitionGitRoot(t, right, "first")
	rightSecondRegistry, rightSecond := doctorAcquisitionGitRoot(t, right, "second")
	rightKeeper := doctorAcquisitionGitResource(t, rightFirst, "skill", "keeper")
	doctorAcquisitionGitResource(t, rightSecond, "skill", "added")
	if leftRegistry.Commit == rightRegistry.Commit || rightRegistry.Commit != rightSecondRegistry.Commit ||
		reflect.DeepEqual(leftKeeper, rightKeeper) || rightSecond.Path != "second" {
		t.Fatalf("fixture did not create changed first and newly selected second pins at commit B: left=%+v right=%+v", leftRegistry, right.Registries)
	}

	writeSkill(t, filepath.Join(fixture.working, "first"), "keeper", "Keeper at commit C", nil)
	writeSkill(t, filepath.Join(fixture.working, "second"), "added", "Added at commit C", nil)
	commitAndPushAll(t, fixture.working, "advance registry beyond both conflict pins")
	conflict := doctorAcquisitionConflict(leftBytes, rightBytes)
	writeFile(t, fixture.lockPath, string(conflict))

	output, repairErr := runDoctorAcquisitionRepair(t, true, false, "left\ny\n")
	if !strings.Contains(output, leftRegistry.Commit.String()) || !strings.Contains(output, rightRegistry.Commit.String()) {
		t.Errorf("repair did not offer both exact pin choices:\n%s", output)
	}
	got, readErr := os.ReadFile(fixture.lockPath)
	if readErr != nil || !bytes.Equal(got, conflict) {
		t.Errorf("repair changed the original conflicted lock bytes: %v\nwant:\n%s\ngot:\n%s", readErr, conflict, got)
	}
	if repairErr == nil {
		t.Errorf("repair did not refuse an unrepresentable selected one-sided pin that conflicts with the chosen left registry commit:\n%s", output)
	}
}

// spec: doctor-repair-planning/policy-change-before-a-confirmed-step-stops-repair
func TestDoctorRepairAcquisitionConflictPolicyChange(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := setupDoctorAcquisitionPinFixture(t)
	seed, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	conflict := doctorAcquisitionConflict(seed, seed)
	writeFile(t, fixture.lockPath, string(conflict))
	reader, writer := io.Pipe()
	watcher := newPromptWatcher()
	done := make(chan error, 1)
	go func() {
		done <- runDoctor(doctorOptions{
			out: watcher, in: reader, isTTY: true, repair: true,
			probe: staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		})
	}()
	select {
	case <-watcher.promptCh:
	case err := <-done:
		t.Fatalf("repair ended before per-lock confirmation: %v\n%s", err, watcher.String())
	case <-time.After(10 * time.Second):
		t.Fatalf("repair did not reach the per-lock confirmation:\n%s", watcher.String())
	}
	if !strings.Contains(watcher.String(), "replace acquisition lock") {
		t.Fatalf("unexpected prompt before lock replacement:\n%s", watcher.String())
	}
	assertDoctorAcquisitionNoBackup(t, fixture.lockPath)
	writeDoctorAcquisitionPinConfig(t, fixture, true, nil)
	if _, err := writer.Write([]byte("y\n")); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	repairErr := <-done
	if !errors.Is(repairErr, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("policy change error = %v, want generation conflict\n%s", repairErr, watcher.String())
	}
	assertDoctorAcquisitionLockBytes(t, fixture.lockPath, conflict)
	assertDoctorAcquisitionNoBackup(t, fixture.lockPath)
}

// spec: doctor-repair-planning/verification-refusal-preserves-acquisition-lock-bytes
func TestDoctorRepairAcquisitionRecoveryRefusals(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, test := range []struct {
		name       string
		diagnostic string
		mutateSide func(*testing.T, []byte) []byte
	}{
		{
			name:       "unknown field",
			diagnostic: "lock.unknown-field",
			mutateSide: func(t *testing.T, seed []byte) []byte {
				return append(append([]byte(nil), seed...), []byte("unknown_lock_field: value\n")...)
			},
		},
		{
			name:       "invalid digest",
			diagnostic: "lock.digest-invalid",
			mutateSide: func(t *testing.T, seed []byte) []byte {
				invalid := bytes.Replace(seed, []byte("sha256:"), []byte("sha256:bad"), 1)
				if bytes.Equal(seed, invalid) {
					t.Fatal("seed lock has no digest to invalidate")
				}
				return invalid
			},
		},
		{
			name:       "cyclic alias",
			diagnostic: "lock.alias-forbidden",
			mutateSide: func(t *testing.T, seed []byte) []byte {
				cyclic := bytes.Replace(seed, []byte("scope: repo\n"), []byte("scope: &cycle [*cycle]\n"), 1)
				if bytes.Equal(seed, cyclic) {
					t.Fatal("seed lock has no scope field to replace")
				}
				return cyclic
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := setupDoctorAcquisitionPinFixture(t)
			seed, err := os.ReadFile(fixture.lockPath)
			if err != nil {
				t.Fatal(err)
			}
			defective := doctorAcquisitionConflict(seed, test.mutateSide(t, seed))
			writeFile(t, fixture.lockPath, string(defective))
			output, _ := runDoctorAcquisitionRepair(t, true, false, "y\n")
			if !strings.Contains(output, "[human-only]") || !strings.Contains(output, test.diagnostic) {
				t.Fatalf("invalid recovery view was not refused with a concrete diagnostic:\n%s", output)
			}
			assertDoctorAcquisitionLockBytes(t, fixture.lockPath, defective)
			assertDoctorAcquisitionNoBackup(t, fixture.lockPath)
		})
	}
}

func runDoctorAcquisitionRepair(t *testing.T, tty, yes bool, input string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	err := runDoctor(doctorOptions{
		out: &output, in: strings.NewReader(input), isTTY: tty, yes: yes, repair: true,
		probe: staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
	})
	return output.String(), err
}

func doctorAcquisitionDefect(t *testing.T, code string, seed []byte) []byte {
	t.Helper()
	switch code {
	case "lock.conflicted":
		return doctorAcquisitionConflict(seed, seed)
	case "lock.alias-forbidden":
		return bytes.Replace(seed, []byte("scope: repo\n"), []byte("scope: &repair_scope repo\n"), 1)
	case "lock.merge-forbidden":
		header, rest, ok := bytes.Cut(seed, []byte("\n"))
		if !ok {
			t.Fatal("seed lock has no header line")
		}
		return bytes.Join([][]byte{header, []byte("<<: {}"), rest}, []byte("\n"))
	case "lock.duplicate-field":
		return bytes.Replace(seed, []byte("scope: repo\n"), []byte("scope: repo\nscope: repo\n"), 1)
	default:
		t.Fatalf("unsupported lock defect %q", code)
		return nil
	}
}

func doctorAcquisitionConflict(left, right []byte) []byte {
	return []byte("<<<<<<< ours\n" + string(left) + "=======\n" + string(right) + ">>>>>>> theirs\n")
}

func doctorAcquisitionBackups(t *testing.T, lockPath string) []string {
	t.Helper()
	matches, err := filepath.Glob(lockPath + ".repair-*.bak")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func assertDoctorAcquisitionNoBackup(t *testing.T, lockPath string) {
	t.Helper()
	if backups := doctorAcquisitionBackups(t, lockPath); len(backups) != 0 {
		t.Fatalf("repair created a backup before replacement consent: %v", backups)
	}
}

func assertDoctorAcquisitionLockBytes(t *testing.T, lockPath string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("acquisition lock bytes changed: %v\nwant:\n%s\ngot:\n%s", err, want, got)
	}
}

func writeDoctorAcquisitionRootsConfig(t *testing.T, fixture doctorAcquisitionPinFixture, roots ...string) {
	t.Helper()
	var content strings.Builder
	content.WriteString("apiVersion: agent-marketplace/v4\nsources:\n")
	for _, root := range roots {
		fmt.Fprintf(&content, "  %s: {git: %s, ref: main, path: %s}\n", root, fixture.origin, root)
	}
	content.WriteString("resources:\n")
	for _, root := range roots {
		name := map[string]string{"first": "keeper", "second": "added", "third": "discarded"}[root]
		fmt.Fprintf(&content, "  %s: {include: [skill/%s]}\n", root, name)
	}
	writeConfig(t, fixture.configPath, content.String())
}

func doctorAcquisitionDocumentWithRoots(t *testing.T, document lockfile.Document, paths ...string) lockfile.Document {
	t.Helper()
	encoded, err := lockfile.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := lockfile.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	keep := make(map[string]bool, len(paths))
	for _, path := range paths {
		keep[path] = true
	}
	for i := range copy.Registries {
		var roots []lockfile.GitRoot
		for _, root := range copy.Registries[i].Roots {
			if keep[root.Path] {
				roots = append(roots, root)
			}
		}
		copy.Registries[i].Roots = roots
	}
	return copy
}
