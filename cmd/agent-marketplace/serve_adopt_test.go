package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestServeAdoptMaintenance(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	dir := t.TempDir()
	database := filepath.Join(dir, "metadata.db")
	path := filepath.Join(dir, "operator.yaml")
	contents := strings.ReplaceAll(minimalMarketplaceConfig(), "/tmp/marketplace-test.db", database)
	contents = strings.ReplaceAll(contents, "/tmp/blobs-test", filepath.Join(dir, "blobs"))
	contents += "\nmanagement:\n  mode: database\n  allowed_origins:\n    - https://github.com\n"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"adopt", "--config", path, "--actor", "operator", "--reason", "Maintenance cutover", "--idempotency-key", "adopt-test"}
	cmd := newServeCmd()
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("adoption without maintenance acknowledgement succeeded")
	}
	if _, err := os.Stat(database); !os.IsNotExist(err) {
		t.Fatalf("refused adoption opened database: %v", err)
	}
	for range 2 {
		cmd = newServeCmd()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs(append(args, "--confirm-maintenance"))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("adopt: %v; %s", err, output.String())
		}
		if !strings.Contains(output.String(), "revision 1") {
			t.Fatalf("missing accepted revision: %s", output.String())
		}
	}
	meta := sqlite.New(database)
	if err := meta.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	current, err := meta.CurrentManagedConfiguration(context.Background(), "test-mp")
	if err != nil || current.Revision != 1 || current.Actor.Subject != "operator" {
		t.Fatalf("adopted state: %+v %v", current, err)
	}
}
