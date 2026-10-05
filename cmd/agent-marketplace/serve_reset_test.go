package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestServeResetMaintenance(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	dir := t.TempDir()
	database := filepath.Join(dir, "metadata.db")
	path := filepath.Join(dir, "operator.yaml")
	contents := strings.ReplaceAll(minimalMarketplaceConfig(), "/tmp/marketplace-test.db", database)
	contents = strings.ReplaceAll(contents, "/tmp/blobs-test", filepath.Join(dir, "blobs"))
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		t.Helper()
		cmd := newServeCmd()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(t.Context())
		return output.String(), err
	}
	if _, err := run("reset", "source", "--config", path); err == nil || !strings.Contains(err.Error(), "confirm-maintenance") {
		t.Fatalf("reset without maintenance confirmation: %v", err)
	}
	if _, err := os.Stat(database); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed reset opened database: %v", err)
	}
	meta := sqlite.New(database)
	if err := meta.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	if err := meta.PutRegistry(t.Context(), store.Registry{ID: "source-id", Slug: "source", Ref: "main", Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatal(err)
	}
	revision := store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "test-mp", Registry: "source-id", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)), ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64))}
	if err := meta.PutResourceRevision(t.Context(), revision); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"reset", "--config", path, "--confirm-maintenance"},
		{"reset", "source", "extra", "--config", path, "--confirm-maintenance"},
		{"reset", "missing", "--config", path, "--confirm-maintenance"},
	} {
		if output, err := run(args...); err == nil {
			t.Fatalf("invalid reset %v succeeded: %s", args, output)
		}
	}
	changed := revision
	changed.ArchiveDigest = store.ArchiveDigest("sha256:" + strings.Repeat("c", 64))
	immutable := revision
	immutable.License = "Apache-2.0"
	for _, confirmed := range []string{"false", "true"} {
		if output, err := run("reset", "source", "--config", path, "--dry-run", "--confirm-maintenance="+confirmed); err != nil || !strings.Contains(output, "resource_revisions: 1") {
			t.Fatalf("preview: %v; %s", err, output)
		}
		if err := meta.PutResourceRevision(t.Context(), immutable); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("preview did not retain immutable revision: %v", err)
		}
	}
	if output, err := run("reset", "source-id", "--config", path, "--confirm-maintenance"); err != nil || !strings.Contains(output, "resource_revisions: 1") {
		t.Fatalf("reset: %v; %s", err, output)
	}
	if err := meta.PutResourceRevision(t.Context(), changed); err != nil {
		t.Fatalf("repaired revision still conflicts after reset: %v", err)
	}
}
