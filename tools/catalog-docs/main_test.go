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

// TestCheckFailsOnStaleDocument drives the same check path the `-check` gate
// runs, against a stale document and a fresh copy, so the regression proves
// the check is non-vacuous in both directions.
func TestCheckFailsOnStaleDocument(t *testing.T) {
	chdirRoot(t)
	doc, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	freshPath := filepath.Join(dir, "fresh.md")
	if err := os.WriteFile(freshPath, doc, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkFresh(doc, freshPath); err != nil {
		t.Fatalf("freshly generated document failed the check: %v", err)
	}
	stale := strings.Replace(string(doc), "# Hook and tool names", "# Stale tool names", 1)
	if stale == string(doc) {
		t.Fatal("generated document lacks the expected title")
	}
	stalePath := filepath.Join(dir, "hook-and-tool-names.md")
	if err := os.WriteFile(stalePath, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	err = checkFresh(doc, stalePath)
	if err == nil {
		t.Fatal("check path accepted a stale document")
	}
	if !strings.Contains(err.Error(), stalePath) {
		t.Fatalf("check error must name the stale document, got %v", err)
	}
}
