package session_test

import (
	"go/build"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The interactive-session core must stay usable by non-TUI surfaces: no
// direct or transitive dependency on Bubble Tea, Lipgloss, Huh, Bubbles, or
// any other charm.land / github.com/charmbracelet package may appear in its
// import graph.
func TestSessionCoreHasNoCharmDependency(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	pkgDir := filepath.Dir(thisFile)

	ctx := build.Default
	pkg, err := ctx.ImportDir(pkgDir, 0)
	if err != nil {
		t.Fatalf("import session package dir: %v", err)
	}

	visited := map[string]bool{}
	var stack []string
	var visit func(importPath string) error
	visit = func(importPath string) error {
		if visited[importPath] || importPath == "C" {
			return nil
		}
		visited[importPath] = true
		p, err := ctx.Import(importPath, pkgDir, 0)
		if err != nil {
			// Test-only and unresolved imports are irrelevant here.
			return nil
		}
		for _, dep := range p.Imports {
			if dep == "C" {
				continue
			}
			if strings.HasPrefix(dep, "charm.land/") || strings.HasPrefix(dep, "github.com/charmbracelet/") {
				stack = append(stack, dep)
				continue
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		return nil
	}
	for _, imp := range pkg.Imports {
		if strings.HasPrefix(imp, "charm.land/") || strings.HasPrefix(imp, "github.com/charmbracelet/") {
			stack = append(stack, imp)
			continue
		}
		if err := visit(imp); err != nil {
			t.Fatalf("walk imports: %v", err)
		}
	}
	if len(stack) > 0 {
		t.Fatalf("internal/session transitively imports Charm packages: %v", stack)
	}
}
