package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: update-preview/no-update-is-a-successful-preview
func TestUpdatePreviewNoActionableSources(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, hosted := range []bool{false, true} {
		t.Run(strconv.FormatBool(hosted), func(t *testing.T) {
			repo := t.TempDir()
			cfg := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
			if hosted {
				server := newV4MarketplaceCollectionServer(t, "no-op", "bundle", "review")
				t.Cleanup(server.Close)
				cfg += "sources:\n  hosted: {marketplace: " + server.URL + "}\nresources:\n  hosted: {include: [tools/collection/bundle]}\n"
			}
			writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), cfg)
			withWorkingDir(t, repo)
			if hosted {
				if output, err := executeCommand("sync"); err != nil {
					t.Fatalf("initial sync: %v\n%s", err, output)
				}
			}
			before := snapshotConsumerTree(t, repo)
			for _, args := range [][]string{nil, {"--include-diff"}} {
				output, err := updateDryRun(t, args...)
				if err != nil {
					t.Fatalf("no-op preview: %v\n%s", err, output)
				}
				for _, want := range []string{"No versioned updates are available.", "Dry run: no updates were applied.", "This preview does not enumerate every subsequent materialization change."} {
					if !strings.Contains(output, want) {
						t.Errorf("no-op preview missing %q:\n%s", want, output)
					}
				}
				if strings.Contains(output, "@@") {
					t.Errorf("no-op preview printed content hunks:\n%s", output)
				}
				assertTreesIdentical(t, "no-op preview", before, snapshotConsumerTree(t, repo))
			}
		})
	}
}

// spec: update-preview/live-sources-are-not-mistaken-for-unchanged-versioned-resources
func TestUpdatePreviewUnlockedOrganizationDoesNotBorrowWritableLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, _, _, _ := previewTestSetup(t)
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, orgPath, "apiVersion: agent-marketplace/v4\nsources:\n  shared:\n    git: file://"+origin+"\n    ref: main\nresources:\n  shared:\n    include: [skill/vm-lifecycle]\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(orgPath), "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("expected intentionally absent organization lock: %v", err)
	}
	output, err := updateDryRun(t)
	if err != nil {
		t.Fatalf("preview: %v\n%s", err, output)
	}
	_, live, ok := strings.Cut(output, "Sources with no historical locked comparison")
	if !ok || !strings.Contains(live, `organization "file://`+origin+`"`) {
		t.Errorf("unlocked organization source borrowed another layer's revision:\n%s", output)
	}
}

// spec: update-preview/initial-acquisition-is-previewed-without-creating-writable-roots
func TestUpdatePreviewAcquiresNewSourceInLockedLayer(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, _, repo, configPath := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	secondOrigin, secondWorking := createBareRepoWithWorkingClone(t, "new-source.git")
	writeSkill(t, secondWorking, "new-source-skill", "New source initial content", nil)
	commitAndPushAll(t, secondWorking, "seed newly configured source")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nsources:\n  existing:\n    git: file://"+origin+"\n    ref: main\n  added:\n    git: file://"+secondOrigin+"\n    ref: main\nresources:\n  existing:\n    include: [skill/vm-lifecycle]\n  added:\n    include: [skill/new-source-skill]\n")
	before := snapshotConsumerTree(t, repo)
	for _, args := range [][]string{nil, {"--include-diff"}} {
		output, err := updateDryRun(t, args...)
		if err != nil {
			t.Fatalf("preview of new source in locked layer: %v\n%s", err, output)
		}
		for _, want := range []string{"initial acquisition", `skill "new-source-skill"`, "added"} {
			if !strings.Contains(output, want) {
				t.Errorf("initial acquisition missing %q:\n%s", want, output)
			}
		}
		assertTreesIdentical(t, "new-source preview", before, snapshotConsumerTree(t, repo))
	}
	if output, err := executeCommand("update", "--include-diff"); err != nil {
		t.Fatalf("apply new-source acquisition: %v\n%s", err, output)
	}
	if len(readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml")).Registries) != 2 {
		t.Error("applying update did not lock both sources")
	}
	installed := string(readRequiredFile(t, filepath.Join(repo, ".factory", "skills", "new-source-skill", "SKILL.md")))
	if !strings.Contains(installed, "New source initial content") {
		t.Error("applying update did not materialize the new source")
	}
}

// spec: update-output/terminal-controls-and-locator-credentials-are-not-emitted-unsafely
func TestUpdateDiffNeutralizesBodyControls(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working, _, _ := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	writeFile(t, filepath.Join(working, "skills", "vm-lifecycle", "SKILL.md"),
		"---\nname: vm-lifecycle\ndescription: safe review\n---\n\nReadable review \x1b]52;c;body-control-marker\x07\n")
	commitAndPushAll(t, working, "add untrusted body controls")
	for _, args := range [][]string{{"update", "--dry-run", "--include-diff"}, {"update", "--include-diff"}} {
		output, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("controlled body review: %v; output %q", err, output)
		}
		for _, want := range []string{`Readable review \u001b]52;c;body-control-marker\u0007`, "@@"} {
			if !strings.Contains(output, want) {
				t.Errorf("review missing %q: %q", want, output)
			}
		}
		if strings.ContainsAny(output, "\x1b\x07") {
			t.Errorf("review emitted executable terminal controls: %q", output)
		}
	}
}

// spec: update-output/supporting-file-changes-are-not-hidden-by-an-unchanged-definition
func TestUpdateDiffOneSidedBinarySupportFiles(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working, _, _ := previewTestSetup(t)
	payloads := []string{"NUL-BODY-NOT-FOR-OUTPUT\x00", "INVALID-BODY-NOT-FOR-OUTPUT\xff", "ESC-BODY-NOT-FOR-OUTPUT\x1b[2J"}
	for i, data := range payloads {
		writeSupportFile(t, working, "vm-lifecycle", fmt.Sprintf("old-%d.bin", i), data)
	}
	commitAndPushAll(t, working, "seed non-renderable support")
	initial, err := updateDryRun(t, "--include-diff")
	if err != nil {
		t.Fatalf("initial binary preview: %v; output %q", err, initial)
	}
	if !strings.Contains(initial, "executable mode absent -> not executable") {
		t.Errorf("initial binary addition invented a previous mode: %q", initial)
	}
	for i, data := range payloads {
		if !strings.Contains(initial, fmt.Sprintf(`"old-%d.bin": binary content added`, i)) || strings.Contains(initial, data[:10]) {
			t.Errorf("initial acquisition exposed a non-renderable body or hid its addition: %q", initial)
		}
	}
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	for i, data := range payloads {
		if err := os.Remove(filepath.Join(working, "skills", "vm-lifecycle", fmt.Sprintf("old-%d.bin", i))); err != nil {
			t.Fatal(err)
		}
		writeSupportFile(t, working, "vm-lifecycle", fmt.Sprintf("new-%d.bin", i), data+"new")
	}
	commitAndPushAll(t, working, "replace non-renderable support")
	for _, args := range [][]string{{"update", "--dry-run", "--include-diff"}, {"update", "--include-diff"}} {
		output, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("binary support review: %v; output %q", err, output)
		}
		for i, data := range payloads {
			for _, want := range []string{
				fmt.Sprintf(`"old-%d.bin": binary content removed`, i),
				fmt.Sprintf(`"new-%d.bin": binary content added`, i),
				strconv.Itoa(len(data)) + " bytes, sha256:",
				strconv.Itoa(len(data)+3) + " bytes, sha256:",
			} {
				if !strings.Contains(output, want) {
					t.Errorf("binary metadata missing %q: %q", want, output)
				}
			}
			if strings.Contains(output, data[:10]) {
				t.Errorf("non-renderable body was disclosed: %q", output)
			}
		}
	}
}

// spec: update-output/review-does-not-broaden-symlink-or-special-file-admission
func TestPreviewSupportReadConfinesConcurrentParentSwap(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root, outside := t.TempDir(), t.TempDir()
	parent, next := filepath.Join(root, "support"), filepath.Join(root, "next")
	inside := filepath.Join(root, "inside")
	writeFile(t, filepath.Join(inside, "file.txt"), "admitted bytes")
	writeFile(t, filepath.Join(outside, "file.txt"), "foreign bytes")
	if err := os.Symlink("inside", parent); err != nil {
		t.Fatal(err)
	}
	control, err := readPreviewSupportFile(root, "support/file.txt", true)
	if err != nil || string(control.Data) != "admitted bytes" {
		t.Fatalf("valid support control = %q, %v", control.Data, err)
	}
	stop, done := make(chan struct{}), make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			for _, target := range []string{outside, "inside"} {
				if err := os.Symlink(target, next); err != nil {
					done <- err
					return
				}
				if err := os.Rename(next, parent); err != nil {
					done <- err
					return
				}
				runtime.Gosched()
			}
		}
	}()
	defer func() {
		close(stop)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	for range 5000 {
		file, err := readPreviewSupportFile(root, "support/file.txt", true)
		if err == nil && string(file.Data) != "admitted bytes" {
			t.Fatalf("support reader escaped its admitted root: %q", file.Data)
		}
	}
}
