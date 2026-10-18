package harnesscompile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaterializeWideJSONCObjectWalkPreservesFileAuthority(t *testing.T) {
	var padding strings.Builder
	for i := 79; i >= 0; i-- {
		fmt.Fprintf(&padding, "    // user %d\n    \"user-%02d\": {\"command\": \"mine\", \"number\": 1e0},\n", i, i)
	}
	prefix := "{\n  // header\n  \"mcpServers\": {\n" + padding.String()
	suffix := "\n  },\n  \"untouched\": 1.00\n}\n"
	before := []byte(prefix + "    \"generated\": {\"command\": \"old\", \"_managed_by\": \"agent-marketplace\"}" + suffix)
	want := []byte(prefix + "    \"generated\": {\"command\": \"new\", \"_managed_by\": \"agent-marketplace\"}" + suffix)
	root := t.TempDir()
	path := filepath.Join(root, ".mcp.json")
	if err := os.WriteFile(path, before, 0o640); err != nil {
		t.Fatal(err)
	}
	result := mergeArtifact(".mcp.json", `{"mcpServers":{"generated":{"command":"new"}}}`)
	if _, err := Materialize(result, root, WithWriteMode(WriteModeOverwrite), WithDryRun()); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, before) {
		t.Fatalf("dry run changed the existing generation: %v\n%s", err, got)
	}
	report, err := Materialize(result, root, WithWriteMode(WriteModeOverwrite))
	if err != nil || len(report.Written) != 1 {
		t.Fatalf("actual merge was not written: report=%+v, err=%v", report, err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("wide aggregate lost bytes or the intended update: %v\n%s", err, got)
	}
	stamp := time.Unix(12345, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := Materialize(result, root, WithWriteMode(WriteModeOverwrite))
	if err != nil || len(repeat.Written) != 0 || len(repeat.Skipped) != 1 {
		t.Fatalf("repeat was not a no-op: report=%+v, err=%v", repeat, err)
	}
	second, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Mode().Perm() != 0o640 || second.Mode() != first.Mode() || !os.SameFile(first, second) || !second.ModTime().Equal(first.ModTime()) {
		t.Fatalf("permissions/identity/mtime changed: before=%v, after=%v", first, second)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("repeat changed output: %v\n%s", err, got)
	}
	assertNoStagingResidue(t, root)
}
