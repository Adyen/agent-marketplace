package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var rootOnce sync.Once

func chdirRoot(t *testing.T) {
	t.Helper()
	var err error
	rootOnce.Do(func() { err = os.Chdir("../..") })
	if err != nil {
		t.Fatal(err)
	}
}

// spec: harness-tool-catalogs/stale-generated-reference-fails-the-check
func TestGeneratedReferenceIsCurrent(t *testing.T) {
	chdirRoot(t)
	doc, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("%s is missing: run make catalog-docs", docPath)
	}
	if string(current) != string(doc) {
		t.Fatalf("%s is stale: run make catalog-docs", docPath)
	}
}

func TestCheckFailsOnStaleDocument(t *testing.T) {
	chdirRoot(t)
	doc, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(doc), "# Hook and tool names", "# Stale tool names", 1)
	if stale == string(doc) {
		t.Fatal("generated document lacks the expected title")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hook-and-tool-names.md")
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) == string(doc) {
		t.Fatal("stale document compared equal to generated output")
	}
}
