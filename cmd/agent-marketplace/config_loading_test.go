package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func TestRootConfigLoadingModes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	var requests atomic.Int32
	server := newV4MarketplaceLockServerWithHook(t, func() { requests.Add(1) })
	defer server.Close()
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+server.URL+"}\nresources:\n  hosted: {include: [tools/skill/review]}\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	withWorkingDir(t, repo)
	var previous context.Context
	for range 2 {
		root := newRootCmd()
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetIn(strings.NewReader(""))
		root.AddCommand(&cobra.Command{Use: "loader-probe", RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == previous {
				t.Fatal("invocations shared a context")
			}
			previous = ctx
			opts := config.LoadEffectiveOptions{RepoRoot: repo}
			acquired := false
			for _, test := range []struct {
				mode        pipeline.ConfigLoadMode
				wantMissing bool
			}{
				{pipeline.ConfigLoadStrict, true},
				{pipeline.ConfigLoadCached, true},
				{pipeline.ConfigLoadOnline, false},
				{pipeline.ConfigLoadCached, false},
				{pipeline.ConfigLoadStrict, true},
				{pipeline.ConfigLoadOnline, false},
			} {
				before := requests.Load()
				ec := pipeline.LoadEffective(ctx, test.mode, opts, nil)
				if test.wantMissing {
					if !marketplacelock.HasRequiredError(ec.Errors) {
						t.Fatalf("mode %d: want missing-lock error, got %v", test.mode, ec.Errors)
					}
				} else if err := errors.Join(ec.Errors...); err != nil {
					t.Fatalf("mode %d: %v", test.mode, err)
				}
				if test.mode == pipeline.ConfigLoadOnline && !acquired {
					if requests.Load() == before {
						t.Fatal("cold online load did not acquire organization evidence")
					}
					acquired = true
				} else if requests.Load() != before {
					t.Fatalf("mode %d contacted Marketplace instead of reusing evidence or failing closed", test.mode)
				}
			}
			before := requests.Load()
			for _, resolveOpts := range []pipeline.ResolveOptions{
				{ConfigLoadMode: pipeline.ConfigLoadStrict},
				{Offline: true},
				{CatalogMode: catalog.LoadModeCachedNoFetch},
			} {
				resolveOpts.Workspace = mustWorkspace(t, repo)
				result := pipeline.Resolve(ctx, resolveOpts)
				if !marketplacelock.HasRequiredError(result.Errors) {
					t.Fatalf("strict resolve reused warm cache: %v", result.Errors)
				}
			}
			_, _, _, err := prepareRuntimeLocks(ctx, repo, repo, lockMode{check: true})
			if !marketplacelock.HasRequiredError([]error{err}) {
				t.Fatalf("lock --check reused warm cache: %v", err)
			}
			if requests.Load() != before {
				t.Fatal("strict operations contacted Marketplace")
			}
			return nil
		}})
		root.SetArgs([]string{"loader-probe"})
		cmd, err := root.ExecuteC()
		finishTelemetry(cmd, err)
		finishRootLogging(err)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(orgPath), "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("organization lock was created: %v", err)
	}
}

func TestRawEffectiveLoaderBoundary(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate source tree")
	}
	root := filepath.Join(filepath.Dir(source), "..", "..")
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if strings.HasPrefix(filepath.ToSlash(rel), "internal/config/") || filepath.ToSlash(rel) == "internal/pipeline/marketplace_runtime_lock.go" {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				importPath, _ := strconv.Unquote(imp.Path.Value)
				if importPath != "github.com/Adyen/agent-marketplace/internal/config" {
					continue
				}
				alias := "config"
				if imp.Name != nil {
					alias = imp.Name.Name
				}
				if alias == "." {
					t.Errorf("%s: dot-importing config bypasses the loader boundary", rel)
				}
				ast.Inspect(file, func(node ast.Node) bool {
					selector, ok := node.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "LoadEffective" {
						return true
					}
					if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == alias {
						t.Errorf("%s: use pipeline.LoadEffective with an explicit mode", fset.Position(selector.Pos()))
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
