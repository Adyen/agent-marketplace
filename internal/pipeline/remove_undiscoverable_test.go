package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestRemoveUndiscoverableConfiguredKinds(t *testing.T) {
	for _, kind := range []string{"skill", "command", "subagent", "hook", "mcp"} {
		for _, from := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/from=%t", kind, from), func(t *testing.T) {
				isolateConsumerPipeline(t)
				repo, source := t.TempDir(), t.TempDir()
				t.Chdir(repo)
				writeSkill(t, source, "keep", "Keep selected", nil)
				path := config.ProjectConfigPath(repo)
				writeFile(t, path, fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  team: {directory: %q, source_harness: factory}\nresources:\n  team: [%s/gone, skill/keep]\n", source, kind))
				opts := RemoveOptions{RepoRoot: repo, ResourceName: "gone", Scope: ScopeRepo}
				if from {
					opts.FromURL = source
				}
				if _, err := Remove(context.Background(), opts); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil || strings.Contains(string(data), kind+"/gone") || !strings.Contains(string(data), "skill/keep") {
					t.Fatalf("wrong selection edited: %v\n%s", err, data)
				}
			})
		}
	}
}

// spec: resource-removal/ambiguous-undiscoverable-selections-require-disambiguation
func TestRemoveUndiscoverableKindAmbiguity(t *testing.T) {
	isolateConsumerPipeline(t)
	repo, source := t.TempDir(), t.TempDir()
	t.Chdir(repo)
	writeFile(t, filepath.Join(source, "commands", "shared.md"), "---\ndescription: Live command\n---\nCommand body\n")
	path := config.ProjectConfigPath(repo)
	initial := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  team: {directory: %q, source_harness: factory}\nresources:\n  team: [skill/shared, command/shared]\n", source)
	writeFile(t, path, initial)
	if _, err := Remove(context.Background(), RemoveOptions{RepoRoot: repo, ResourceName: "shared", Scope: ScopeRepo}); err == nil || !strings.Contains(err.Error(), "--kind") {
		t.Fatalf("expected kind disambiguation, got %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != initial {
		t.Fatalf("ambiguous removal changed config: %v\n%s", err, data)
	}
	if _, err := Remove(context.Background(), RemoveOptions{RepoRoot: repo, ResourceName: "shared", Scope: ScopeRepo, Kind: ir.KindSkill, HasKind: true}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "skill/shared") || !strings.Contains(string(data), "command/shared") {
		t.Fatalf("kind disambiguation edited wrong selection: %v\n%s", err, data)
	}
}

// spec: resource-removal/ambiguous-undiscoverable-selections-require-disambiguation
func TestRemoveUndiscoverableSourceAmbiguity(t *testing.T) {
	for _, crossScope := range []bool{false, true} {
		t.Run(fmt.Sprintf("crossScope=%t", crossScope), func(t *testing.T) {
			home := isolateConsumerPipeline(t)
			repo, first, second := t.TempDir(), t.TempDir(), t.TempDir()
			t.Chdir(repo)
			writeSkill(t, first, "keep", "Keep first source", nil)
			writeSkill(t, second, "keep-other", "Keep second source", nil)
			root := config.ProjectConfigPath(repo)
			body := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  first: {directory: %q}\nresources:\n  first: [skill/gone, skill/keep]\n", first)
			otherPath := root
			if crossScope {
				otherPath = filepath.Join(home, ".agents", "config.yaml")
				writeFile(t, otherPath, fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  second: {directory: %q}\nresources:\n  second: [skill/gone, skill/keep-other]\n", second))
			} else {
				body = fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  first: {directory: %q}\n  second: {directory: %q}\nresources:\n  first: [skill/gone, skill/keep]\n  second: [skill/gone, skill/keep-other]\n", first, second)
			}
			writeFile(t, root, body)
			otherBefore, err := os.ReadFile(otherPath)
			if err != nil {
				t.Fatal(err)
			}
			opts := RemoveOptions{RepoRoot: repo, ResourceName: "gone", Scope: ScopeRepo}
			if _, err := Remove(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "--from") || !strings.Contains(err.Error(), "--scope") {
				t.Fatalf("expected source disambiguation, got %v", err)
			}
			data, err := os.ReadFile(root)
			if err != nil || string(data) != body {
				t.Fatalf("ambiguity changed root: %v\n%s", err, data)
			}
			if crossScope {
				opts.ScopeSpecified = true
			} else {
				opts.FromURL = first
			}
			if _, err := Remove(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			data, err = os.ReadFile(root)
			if err != nil || strings.Contains(string(data), "skill/gone, skill/keep]") || !strings.Contains(string(data), "skill/keep") {
				t.Fatalf("wrong source removed: %v\n%s", err, data)
			}
			if crossScope {
				after, err := os.ReadFile(otherPath)
				if err != nil || string(after) != string(otherBefore) {
					t.Fatalf("other scope changed: %v\n%s", err, after)
				}
			} else if !strings.Contains(string(data), "skill/gone, skill/keep-other") {
				t.Fatalf("other source selection changed:\n%s", data)
			}
		})
	}
}

// spec: resource-removal/undiscoverable-removal-respects-scope-and-required-policy
func TestRemoveUndiscoverableRefusals(t *testing.T) {
	for _, condition := range []string{"required", "machine-boundary", "wrong-scope", "automatic", "collection-member", "excluded"} {
		t.Run(condition, func(t *testing.T) {
			home := isolateConsumerPipeline(t)
			repo, source := t.TempDir(), t.TempDir()
			t.Chdir(repo)
			writeSkill(t, source, "keep", "Keep selected", nil)
			path := config.ProjectConfigPath(repo)
			group := "{include: [skill/gone, skill/keep]}"
			opts := RemoveOptions{RepoRoot: repo, ResourceName: "gone", Scope: ScopeRepo}
			switch condition {
			case "required":
				group = "{include: [skill/gone, skill/keep], required: true}"
			case "machine-boundary":
				writeFile(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
				path = filepath.Join(home, ".agents", "config.yaml")
				opts.ScopeSpecified = true
			case "wrong-scope":
				opts.Scope, opts.ScopeSpecified = ScopeUser, true
			case "automatic":
				group = "{all: true}"
			case "collection-member":
				writeFile(t, filepath.Join(source, "collections", "team", "COLLECTION.yaml"), "name: team\nresources:\n  - name: gone\n    kind: skill\n")
				group = "{include: [collection/team, skill/keep]}"
			case "excluded":
				group = "{include: [skill/gone, skill/keep], exclude: [skill/gone]}"
			}
			body := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  team: {directory: %q, source_harness: factory}\nresources:\n  team: %s\n", source, group)
			writeFile(t, path, body)
			before := Resolve(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repo), SkipInventory: true})
			if len(before.Resolved) != 1 || before.Resolved[0].Name != "keep" || (condition != "required" && len(before.Errors) != 0) {
				t.Fatalf("invalid refusal fixture: %#v / %v", before.Resolved, before.Errors)
			}
			_, err := Remove(context.Background(), opts)
			if err == nil {
				t.Fatal("expected removal refusal")
			}
			if condition == "required" {
				var missing *resolver.SelectedNotFoundError
				if !errors.As(err, &missing) || !missing.Required {
					t.Fatalf("expected required missing-resource refusal, got %v", err)
				}
			} else if condition == "machine-boundary" {
				if !strings.Contains(err.Error(), "cannot exclude") || !strings.Contains(err.Error(), "managed at USER") {
					t.Fatalf("expected machine scope-boundary refusal, got %v", err)
				}
			} else if !strings.Contains(err.Error(), "not currently enabled") {
				t.Fatalf("expected not-enabled refusal, got %v", err)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil || string(data) != body {
				t.Fatalf("refusal changed config: %v\n%s", readErr, data)
			}
		})
	}
}

// spec: resource-removal/undiscoverable-removal-respects-scope-and-required-policy
func TestRemoveUndiscoverableIncludedSelection(t *testing.T) {
	isolateConsumerPipeline(t)
	repo, source := t.TempDir(), t.TempDir()
	t.Chdir(repo)
	writeSkill(t, source, "keep", "Keep selected", nil)
	root := config.ProjectConfigPath(repo)
	include := filepath.Join(filepath.Dir(root), "team.yaml")
	body := fmt.Sprintf("sources:\n  team: {directory: %q, source_harness: factory}\nresources:\n  team: [skill/gone, skill/keep]\n", source)
	writeFile(t, include, body)
	writeFile(t, root, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\ninclude: team.yaml\n")
	if _, err := Remove(context.Background(), RemoveOptions{RepoRoot: repo, ResourceName: "gone", Scope: ScopeRepo}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(include)
	if err != nil || string(after) != body {
		t.Fatalf("included file changed: %v\n%s", err, after)
	}
	result := Resolve(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repo), SkipInventory: true})
	if len(result.Errors) != 0 || len(result.ResolveErrors) != 0 || len(result.Resolved) != 1 || result.Resolved[0].Name != "keep" {
		t.Fatalf("included selection not suppressed: %v, %v, %#v", result.Errors, result.ResolveErrors, result.Resolved)
	}
}

func TestRemoveUndiscoverableFromParentRegistry(t *testing.T) {
	for _, from := range []bool{false, true} {
		t.Run(fmt.Sprintf("from=%t", from), func(t *testing.T) {
			isolateConsumerPipeline(t)
			repo, source := t.TempDir(), t.TempDir()
			t.Chdir(repo)
			writeSkill(t, filepath.Join(source, "plugins", "first"), "keep", "Keep first bundle", nil)
			writeSkill(t, filepath.Join(source, "plugins", "second"), "keep-other", "Keep second bundle", nil)
			path := config.ProjectConfigPath(repo)
			writeFile(t, path, fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  team: {directory: %q, source_harness: factory}\nresources:\n  team: [skill/gone, skill/keep, skill/keep-other]\n", source))
			before := Resolve(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repo), SkipInventory: true})
			if _, err := resolvedResourceByName(removalResourceCandidates(before, "keep"), "keep", ir.KindSkill, true, before.MergedCfg); err != nil {
				t.Fatalf("a live parent selection must not gain duplicate missing candidates: %v", err)
			}
			opts := RemoveOptions{RepoRoot: repo, ResourceName: "gone", Scope: ScopeRepo}
			if from {
				opts.FromURL = source
			}
			if _, err := Remove(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadRootConfigFromPath(path)
			if err != nil || cfg == nil || len(cfg.Registries) != 1 || len(cfg.Registries[0].Skills.Include) != 2 {
				t.Fatalf("parent selection was not edited: %#v / %v", cfg, err)
			}
		})
	}
}
