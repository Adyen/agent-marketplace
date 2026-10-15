package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestV4EditorRepeatedClearIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	const original = "apiVersion: agent-marketplace/v4\nsources:\n  repo: {git: https://example.test/repo.git, ref: main}\nresources:\n  repo: {include: [skill/keep], exclude: [skill/drop]}\n"
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	editor, err := OpenLayerEditor(LayerTarget{Source: RepoCommitted, StagedPath: path, DeclaringPath: path})
	if err != nil {
		t.Fatal(err)
	}
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	locator := ResourceLocator{Kind: ir.KindSkill, Name: "drop"}
	if err := editor.ClearInstallExclude(entry, locator); err != nil {
		t.Fatal(err)
	}
	first, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed || bytes.Equal(first.Bytes, []byte(original)) {
		t.Fatal("first clear did not remove the authored exclusion")
	}
	if err := editor.ClearInstallExclude(entry, locator); err != nil {
		t.Fatal(err)
	}
	second, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes, second.Bytes) {
		t.Fatalf("repeat changed the candidate:\nfirst:\n%ssecond:\n%s", first.Bytes, second.Bytes)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != original {
		t.Fatalf("candidate editing changed the source file: %q, %v", actual, err)
	}
}
