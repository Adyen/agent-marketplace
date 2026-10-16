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
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestConfiguredDirectoryPromotionPreservesAuthoredScope(t *testing.T) {
	for _, spelling := range []string{"./registry", "registry", "absolute"} {
		for _, name := range []string{"review", "bundle"} {
			t.Run(spelling+"/"+name, func(t *testing.T) {
				home := isolateConsumerPipeline(t)
				repo := canonicalTempDir(t)
				t.Chdir(repo)
				t.Setenv("TMPDIR", t.TempDir())
				registry := filepath.Join(repo, "registry")
				writeSkill(t, registry, "review", "Promotion control", nil)
				writeFile(t, filepath.Join(registry, "collections", "bundle", "COLLECTION.yaml"), "name: bundle\nskills:\n  - name: review\n")
				user := filepath.Join(home, ".agents", "config.yaml")
				writeFileMode(t, user, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n", 0o600)
				raw := spelling
				if spelling == "absolute" {
					raw = registry
				}
				writeFileMode(t, config.ProjectConfigPath(repo), fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: %q}\n", raw), 0o600)
				before, beforeInfo := layerFreezeFile(t, user)
				_, err := Install(context.Background(), InstallOptions{RepoRoot: repo, Scope: ScopeUser, ScopeSpecified: true, Args: []string{name}})
				if spelling != "absolute" {
					var boundary *config.GlobalRelativeRegistryError
					if !errors.As(err, &boundary) || boundary.URL != raw {
						t.Fatalf("promotion error = %v, want authored repo-relative boundary for %q", err, raw)
					}
					layerAssertFileUnchanged(t, user, before, beforeInfo)
					for _, path := range []string{filepath.Join(home, ".agents", "lock.yaml"), filepath.Join(home, ".factory", "skills", "review")} {
						if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("refused promotion created %s: %v", path, err)
						}
					}
				} else {
					if err != nil {
						t.Fatalf("absolute directory promotion control: %v", err)
					}
					if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
						t.Fatalf("absolute control did not install: %v", err)
					}
				}
				assertNoStagingResidue(t)
			})
		}
	}
}

func TestUserRelativeDirectoryRemainsUsableInItsOwningLayer(t *testing.T) {
	home := isolateConsumerPipeline(t)
	repo := canonicalTempDir(t)
	t.Chdir(repo)
	registry := filepath.Join(home, ".agents", "registry")
	writeSkill(t, registry, "review", "User-relative control", nil)
	user := filepath.Join(home, ".agents", "config.yaml")
	writeFileMode(t, user, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  personal: {directory: ./registry}\n", 0o600)
	if _, err := Install(context.Background(), InstallOptions{RepoRoot: repo, Scope: ScopeUser, Args: []string{"review"}}); err != nil {
		t.Fatalf("user-relative source was mistaken for repository promotion: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("user-relative control did not materialize: %v", err)
	}
}

func TestContainedIntermediateConfigAliasRemovesRootSelections(t *testing.T) {
	for _, wholeSource := range []bool{false, true} {
		t.Run(fmt.Sprintf("whole-source-%t", wholeSource), func(t *testing.T) {
			home := isolateConsumerPipeline(t)
			repo := canonicalTempDir(t)
			t.Chdir(repo)
			registry := filepath.Join(repo, "registry")
			writeSkill(t, registry, "review", "Alias removal control", nil)
			writeSkill(t, registry, "keep", "Unrelated selection control", nil)
			writeFileMode(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n", 0o600)
			actual := filepath.Join(repo, ".agents", "actual", "root.yaml")
			writeFileMode(t, actual, fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: %q}\nresources:\n  local: [skill/review, skill/keep]\n", registry), 0o600)
			alias := filepath.Join(repo, ".agents", "alias")
			if err := os.Symlink("actual", alias); err != nil {
				t.Fatal(err)
			}
			logical := config.ProjectConfigPath(repo)
			if err := os.Symlink("alias/root.yaml", logical); err != nil {
				t.Fatal(err)
			}
			publication, err := canonicalConfigDestination(logical)
			if err != nil || publication == actual {
				t.Fatalf("fixture did not distinguish logical publication from full provenance: %s, %v", publication, err)
			}
			loaded, err := config.LoadConfigFromPath(logical)
			if err != nil || len(loaded.Registries) != 1 {
				t.Fatalf("load valid alias: %#v, %v", loaded, err)
			}
			op, err := removalSelectionOperation(logical, loaded.Registries[0], ir.KindSkill, "review")
			if err != nil || op != config.SelectionRemoveInclude {
				t.Fatalf("root-owned selection misclassified as inherited: operation=%v, error=%v", op, err)
			}
			opts := RemoveOptions{RepoRoot: repo, Scope: ScopeRepo, ScopeSpecified: true}
			if wholeSource {
				opts.FromURL = registry
			} else {
				opts.ResourceName = "review"
			}
			result, err := Remove(context.Background(), opts)
			if err != nil {
				t.Fatalf("remove via approved alias: %v", err)
			}
			data, err := os.ReadFile(actual)
			if err != nil || strings.Contains(string(data), "exclude:") || strings.Contains(string(data), "skill/review") {
				t.Fatalf("root removal added suppression or retained selection: %s, %v", data, err)
			}
			if wholeSource {
				if !result.RemovedRegistry || result.ExcludedRegistry || strings.Contains(string(data), "directory:") {
					t.Fatalf("whole-source removal became a shadow: %#v\n%s", result, data)
				}
			} else if !strings.Contains(string(data), "skill/keep") {
				t.Fatalf("unrelated root selection was lost: %s", data)
			}
			for path, want := range map[string]string{logical: "alias/root.yaml", alias: "actual"} {
				if target, err := os.Readlink(path); err != nil || target != want {
					t.Fatalf("approved link changed: %s -> %s, %v", path, target, err)
				}
			}
		})
	}
}

func TestReinstallV4AllClearsExcludeWithoutAddingInclude(t *testing.T) {
	home := isolateConsumerPipeline(t)
	repo := canonicalTempDir(t)
	t.Chdir(repo)
	registry := filepath.Join(repo, "registry")
	for _, name := range []string{"review", "keep", "explicit"} {
		writeSkill(t, registry, name, "All-selection control", nil)
	}
	writeFileMode(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n", 0o600)
	path := config.ProjectConfigPath(repo)
	writeFileMode(t, path, fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: %q}\nresources:\n  local:\n    all: true\n    include: [skill/explicit]\n    exclude: [skill/review]\n", registry), 0o600)
	opts := InstallOptions{RepoRoot: repo, Scope: ScopeRepo, Args: []string{"review"}}
	if _, err := Install(context.Background(), opts); err != nil {
		t.Fatalf("reinstall excluded all-selection: %v", err)
	}
	cfg, err := config.LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	group := cfg.Groups["local"]
	if group.All == nil || !*group.All || len(group.Exclude) != 0 || len(group.Include) != 1 || group.Include[0].Locator != "skill/explicit" {
		t.Fatalf("all-selection reinstall changed intent: %#v", group)
	}
	for _, name := range []string{"review", "keep", "explicit"} {
		if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("reinstall lost enabled sibling %s: %v", name, err)
		}
	}
	before, beforeInfo := layerFreezeFile(t, path)
	if _, err := Install(context.Background(), opts); err != nil {
		t.Fatalf("already-selected reinstall: %v", err)
	}
	layerAssertFileUnchanged(t, path, before, beforeInfo)
}

func TestV4AllReinstallKeepsOrderedInheritedOverride(t *testing.T) {
	home := isolateConsumerPipeline(t)
	repo := canonicalTempDir(t)
	t.Chdir(repo)
	registry := filepath.Join(repo, "registry")
	for _, name := range []string{"review", "keep"} {
		writeSkill(t, registry, name, "Inherited all-selection control", nil)
	}
	writeFileMode(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n", 0o600)
	fragment := filepath.Join(repo, ".agents", "selection.yaml")
	fragmentData := fmt.Sprintf("sources:\n  local: {directory: %q}\nresources:\n  local: {all: true, exclude: [skill/review]}\n", registry)
	writeFileMode(t, fragment, fragmentData, 0o600)
	path := config.ProjectConfigPath(repo)
	writeFileMode(t, path, "apiVersion: agent-marketplace/v4\ninclude: [selection.yaml]\n", 0o600)
	if _, err := Install(context.Background(), InstallOptions{RepoRoot: repo, Scope: ScopeRepo, Args: []string{"review"}}); err != nil {
		t.Fatalf("reinstall inherited exact exclusion: %v", err)
	}
	data, err := os.ReadFile(fragment)
	if err != nil || string(data) != fragmentData {
		t.Fatalf("reinstall edited fragment authority: %s, %v", data, err)
	}
	root, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(root), "skill/review") {
		t.Fatalf("inherited exclusion needs an explicit later root request: %s, %v", root, err)
	}
	for _, name := range []string{"review", "keep"} {
		if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("ordered include lost selected %s: %v", name, err)
		}
	}
}
