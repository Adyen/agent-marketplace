package fsboundary

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTargetPropagatesOwnedDescriptorCloseFailures(t *testing.T) {
	for _, scenario := range []struct {
		name                      string
		parent, leaf, readFailure bool
	}{
		{name: "leaf", leaf: true},
		{name: "parent", parent: true},
		{name: "both", parent: true, leaf: true},
		{name: "read and both closes", parent: true, leaf: true, readFailure: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root, path := tempRoot(t)
			file := filepath.Join(path, "marker.md")
			mustWrite(t, file, []byte("captured marker bytes"), 0o640)
			destination, err := CaptureDestination(root, "marker.md", LinkPolicyRepository)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := destination.Close(); err != nil {
					t.Error(err)
				}
			})
			data, id, err := destination.ReadTarget(64)
			if err != nil || string(data) != "captured marker bytes" || id != destination.Identity() {
				t.Fatalf("valid read control: %q %v", data, err)
			}
			limit := int64(64)
			if scenario.readFailure {
				limit = 1
			}
			hit := false
			data, id, err = destination.readTarget(limit, nil, func(parent, leaf *os.File) {
				hit = true
				// Close the exact resources owned by this read, not the outer capture.
				if scenario.parent {
					if err := parent.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if scenario.leaf {
					if err := leaf.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
			var pathErr *os.PathError
			if !hit || !errors.Is(err, fs.ErrClosed) || !errors.As(err, &pathErr) {
				t.Errorf("lost typed close failure: %v", err)
			}
			if data != nil || id != (Identity{}) {
				t.Errorf("failed close returned trusted data: %q %+v", data, id)
			}
			if scenario.readFailure && !errors.Is(err, ErrTooLarge) {
				t.Errorf("close discarded primary read failure: %v", err)
			}
			wantCloses := 1
			if scenario.parent && scenario.leaf {
				wantCloses = 2
			}
			if got := countReadCloseErrors(err); got != wantCloses {
				t.Errorf("reported %d close errors, want %d: %v", got, wantCloses, err)
			}
			if mustRead(t, file) != "captured marker bytes" {
				t.Fatal("failed read changed marker bytes")
			}
			// Outer ownership remains usable after the internal close failure.
			data, _, err = destination.ReadTarget(64)
			if err != nil || string(data) != "captured marker bytes" {
				t.Fatalf("read damaged capture: %q %v", data, err)
			}
		})
	}
}

func countReadCloseErrors(err error) int {
	if err == nil {
		return 0
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		count := 0
		for _, child := range joined.Unwrap() {
			count += countReadCloseErrors(child)
		}
		return count
	}
	if pathErr, ok := err.(*os.PathError); ok && pathErr.Op == "close" && errors.Is(err, fs.ErrClosed) {
		return 1
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return countReadCloseErrors(wrapped.Unwrap())
	}
	return 0
}
