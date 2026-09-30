//go:build unix

package fsboundary

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestNestedOpenFailureClosesCapturedDirectories(t *testing.T) {
	root, path := tempRoot(t)
	mustMkdir(t, filepath.Join(path, "nested"))
	mustWrite(t, filepath.Join(path, "nested", "valid"), []byte("control"), 0o600)
	file, _, err := root.OpenFile("nested/valid")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	countDescriptors := func() int {
		t.Helper()
		directory, err := os.Open("/dev/fd")
		if err != nil {
			t.Fatal(err)
		}
		names, readErr := directory.Readdirnames(-1)
		closeErr := directory.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("descriptor enumeration failed: %v %v", readErr, closeErr)
		}
		return len(names)
	}
	before := countDescriptors()
	for range 64 {
		file, _, err := root.OpenFile("nested/missing/leaf")
		if file != nil {
			_ = file.Close()
			t.Fatal("failed open returned a descriptor")
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("wrong refusal: %v", err)
		}
	}
	after := countDescriptors()
	if after > before+2 {
		t.Fatalf("failed opens leaked descriptors: before=%d after=%d", before, after)
	}
}

func TestPublishRejectsPathShapedStagedNames(t *testing.T) {
	for _, kind := range []string{"parent", "absolute", "nested"} {
		t.Run(kind, func(t *testing.T) {
			root, path := tempRoot(t)
			stagingPath := filepath.Join(path, "staging")
			mustMkdir(t, stagingPath)
			mustMkdir(t, filepath.Join(stagingPath, "nested"))
			victim := filepath.Join(path, "external")
			name := "../external"
			switch kind {
			case "absolute":
				name = victim
			case "nested":
				name = "nested/external"
				victim = filepath.Join(stagingPath, "nested", "external")
			}
			mustWrite(t, victim, []byte("preserve"), 0o640)
			before := identityFromFileInfo(t, statOf(t, victim))
			staging, _ := captureRoot(t, stagingPath)
			dest, err := CaptureDestination(root, "destination", LinkPolicyRepository)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = dest.Close() }()
			if err := dest.Publish(staging, name); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("path-shaped staged name was accepted: %v", err)
			}
			if mustRead(t, victim) != "preserve" || !identityFromFileInfo(t, statOf(t, victim)).SameState(before) {
				t.Fatal("refusal changed the source object")
			}
			if _, err := os.Lstat(filepath.Join(path, "destination")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("refusal created a destination: %v", err)
			}
			valid := stageFile(t, stagingPath, "payload", []byte("valid"))
			if err := dest.Publish(valid, "payload"); err != nil || mustRead(t, filepath.Join(path, "destination")) != "valid" {
				t.Fatalf("valid basename control failed: %v", err)
			}
		})
	}
}

func TestReadTargetRejectsReplacementAfterPrecheck(t *testing.T) {
	root, path := tempRoot(t)
	file := filepath.Join(path, "config.yaml")
	mustWrite(t, file, []byte("captured"), 0o600)
	dest, err := CaptureDestination(root, "config.yaml", LinkPolicyRepository)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dest.Close() }()
	data, id, err := dest.ReadTarget(64)
	if err != nil || string(data) != "captured" || id != dest.Identity() {
		t.Fatalf("valid read control failed: %q %v", data, err)
	}
	data, _, err = dest.readTarget(64, func() {
		mustWrite(t, filepath.Join(path, "replacement"), []byte("foreign"), 0o640)
		if err := os.Rename(filepath.Join(path, "replacement"), file); err != nil {
			t.Fatal(err)
		}
	})
	if !errors.Is(err, ErrGenerationConflict) || data != nil {
		t.Fatalf("replacement bytes escaped capture: %q %v", data, err)
	}
	if mustRead(t, file) != "foreign" {
		t.Fatal("refusal changed replacement bytes")
	}
}

func TestChainedLinkRetargetingRefuses(t *testing.T) {
	for _, policy := range []LinkPolicy{LinkPolicyUser, LinkPolicyRepository} {
		root, path := tempRoot(t)
		mustWrite(t, filepath.Join(path, "original"), []byte("original"), 0o600)
		mustWrite(t, filepath.Join(path, "foreign"), []byte("foreign"), 0o640)
		mustSymlink(t, "original", filepath.Join(path, "second"))
		mustSymlink(t, "second", filepath.Join(path, "first"))
		dest, err := CaptureDestination(root, "first", policy)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = dest.Close() }()
		mustSymlink(t, "foreign", filepath.Join(path, "replacement-link"))
		if err := os.Rename(filepath.Join(path, "replacement-link"), filepath.Join(path, "second")); err != nil {
			t.Fatal(err)
		}
		stagingPath := filepath.Join(path, "staging")
		mustMkdir(t, stagingPath)
		staging := stageFile(t, stagingPath, "payload", []byte("new"))
		if err := dest.Publish(staging, "payload"); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("retargeted intermediate link was not refused: %v", err)
		}
		if _, _, err := dest.ReadTarget(64); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("retargeted intermediate read was not refused: %v", err)
		}
		if err := dest.RemoveLeaf(); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("retargeted intermediate removal was not refused: %v", err)
		}
		if mustRead(t, filepath.Join(path, "original")) != "original" || mustRead(t, filepath.Join(path, "foreign")) != "foreign" || mustRead(t, filepath.Join(stagingPath, "payload")) != "new" {
			t.Fatal("chained-link refusal changed bytes or staging")
		}
		if text, err := os.Readlink(filepath.Join(path, "first")); err != nil || text != "second" {
			t.Fatalf("chained-link refusal changed the logical link: %q %v", text, err)
		}
	}
}

func TestReadLimitCannotOverflow(t *testing.T) {
	root, path := tempRoot(t)
	mustWrite(t, filepath.Join(path, "payload"), []byte("payload"), 0o600)
	file, _, err := root.OpenFile("payload")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if data, _, err := ReadFile(file, math.MaxInt64); !errors.Is(err, ErrTooLarge) || data != nil {
		t.Fatalf("overflowing limit was not refused: bytes=%q error=%v", data, err)
	}
}
