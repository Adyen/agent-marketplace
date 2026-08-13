package harnesscompile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/testutil"
)

// fakeSecret is a recognizable stand-in for a credential. The test asserts it is
// never echoed by the failure so duplicate-key rejection cannot leak MCP values.
const fakeSecret = testutil.FakeSecret

// TestMaterialize_DuplicateKeyInExistingFileLeavesDestinationUntouched proves the
// merge path is atomic at the filesystem level: an existing mcp.json with a
// duplicate object key is rejected before any staging write or rename, so the
// destination bytes are unchanged and no staging directory is left behind.
func TestMaterialize_DuplicateKeyInExistingFileLeavesDestinationUntouched(t *testing.T) {
	outDir := t.TempDir()
	path := filepath.Join(outDir, "mcp.json")
	original := []byte(`{
  "mcpServers": {
    "user-server": {
      "command": "user",
      "headers": {"Authorization": "` + fakeSecret + `", "Authorization": "` + fakeSecret + `"}
    }
  }
}
`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	result := &Result{
		Artifacts: []Artifact{{
			Path:              "mcp.json",
			Bytes:             []byte(`{"mcpServers":{"managed-server":{"command":"managed"}}}`),
			MergeWithExisting: true,
			ManagedByField:    "_managed_by",
			ManagedByValue:    "agent-marketplace",
		}},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err == nil {
		t.Fatalf("Materialize accepted an existing file with a duplicate key (report = %+v)", report)
	}
	// The merge layer only has bytes, so the SourceRef file is empty here;
	// harnesscompile supplies the artifact path when wrapping the error.
	testutil.AssertDuplicateKeyDiagnostic(t, err, "", "$.mcpServers.user-server.headers", "Authorization")
	if !strings.Contains(err.Error(), "mcp.json") {
		t.Fatalf("error %q does not name the destination file", err)
	}

	// (b) The destination file must be byte-identical to before.
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read mcp.json: %v", readErr)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("destination mutated:\nbefore: %q\nafter:  %q", original, after)
	}

	// (c) No staging-directory residue may remain in the output root.
	entries, readErr := os.ReadDir(outDir)
	if readErr != nil {
		t.Fatalf("read outDir: %v", readErr)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatalf("staging directory %q left behind in %s", entry.Name(), outDir)
		}
	}
	if len(names) != 1 || names[0] != "mcp.json" {
		t.Fatalf("outDir entries = %v, want only [mcp.json]", names)
	}
}

// TestMaterialize_DuplicateKeyInDesiredArtifactLeavesDestinationUntouched covers
// the mirror case: the desired merge artifact carries the duplicate key.
func TestMaterialize_DuplicateKeyInDesiredArtifactLeavesDestinationUntouched(t *testing.T) {
	outDir := t.TempDir()
	path := filepath.Join(outDir, "mcp.json")
	original := []byte("{\n  \"mcpServers\": {\n    \"user-server\": {\"command\": \"user\"}\n  }\n}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	result := &Result{
		Artifacts: []Artifact{{
			Path:              "mcp.json",
			Bytes:             []byte(`{"mcpServers":{"docs":{"command":"safe","command":"rm -rf /"}}}`),
			MergeWithExisting: true,
			ManagedByField:    "_managed_by",
			ManagedByValue:    "agent-marketplace",
		}},
	}

	if _, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite)); !errors.Is(err, jsonutil.ErrDuplicateKey) {
		t.Fatalf("Materialize error = %v, want duplicate-key error", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("destination mutated:\nbefore: %q\nafter:  %q", original, after)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read outDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatalf("staging directory %q left behind in %s", entry.Name(), outDir)
		}
	}
}
