package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: hook-resources/handwritten-matches-and-unavailable-proof-cannot-authorize-removal
func TestRemoveHookRefusalPreservesConfiguration(t *testing.T) {
	for _, condition := range []string{"missing-state", "corrupt-state", "collection", "source", "from-name", "local", "new-local"} {
		t.Run(condition, func(t *testing.T) {
			home := isolateConsumerPipeline(t)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("TMPDIR", t.TempDir())
			repo := t.TempDir()
			t.Chdir(repo)
			source := t.TempDir()
			const payload = "echo removal-sensitive-token"
			writeFile(t, filepath.Join(source, "hooks", "hooks.json"), `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"`+payload+`"}]}]}}`)
			include := "hook/PreToolUse-command"
			if condition == "collection" {
				include = "collection/team"
				writeFile(t, filepath.Join(source, "collections", "team", "COLLECTION.yaml"), "name: team\nresources:\n  - name: PreToolUse-command\n    kind: hook\n")
			}
			configPath := config.ProjectConfigPath(repo)
			scope := ScopeRepo
			if condition == "local" {
				writeFile(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
				configPath = config.LocalConfigPath(repo)
				scope = ScopeLocal
			}
			data := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  reg: {directory: %q}\nresources:\n  reg: {include: [%s]}\n", source, include)
			if condition != "local" {
				data += "harnesses: [claude]\n"
			}
			writeFile(t, configPath, data)
			installed := Sync(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repo)})
			if len(installed.Errors) != 0 {
				t.Fatalf("sync: %v", installed.Errors)
			}
			nativePath := filepath.Join(repo, ".claude", "settings.json")
			nativeBefore, err := os.ReadFile(nativePath)
			if err != nil || !strings.Contains(string(nativeBefore), payload) {
				t.Fatalf("hook was not installed: %s / %v", nativeBefore, err)
			}
			store, err := ownership.Open(filepath.Dir(nativePath))
			if err != nil {
				t.Fatal(err)
			}
			switch condition {
			case "corrupt-state":
				if err := os.WriteFile(store.StateFile(), []byte("{corrupt"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing-state":
				if err := os.Remove(store.StateFile()); err != nil {
					t.Fatal(err)
				}
			default:
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "teammate-state"))
			}
			opts := RemoveOptions{RepoRoot: repo, ResourceName: "PreToolUse-command", Kind: ir.KindHook, HasKind: true, Scope: scope, ScopeSpecified: true}
			switch condition {
			case "new-local":
				opts.Scope = ScopeLocal
			case "collection":
				opts.ResourceName, opts.Kind = "team", ir.KindCollection
			case "source":
				opts.ResourceName, opts.FromURL = "", source
			case "from-name":
				opts.FromURL = source
			}
			configBefore, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			configInfo, err := os.Stat(configPath)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Remove(context.Background(), opts)
			if err == nil || !strings.Contains(err.Error(), "hook.remove-unowned") || strings.Contains(err.Error(), payload) {
				t.Fatalf("remove error = %v, want value-free ownership refusal", err)
			}
			for path, before := range map[string][]byte{configPath: configBefore, nativePath: nativeBefore} {
				after, err := os.ReadFile(path)
				if err != nil || string(after) != string(before) {
					t.Fatalf("refused removal changed %s: %s / %v", path, after, err)
				}
			}
			afterInfo, err := os.Stat(configPath)
			if err != nil || !os.SameFile(configInfo, afterInfo) || configInfo.Mode() != afterInfo.Mode() || configInfo.ModTime() != afterInfo.ModTime() {
				t.Fatalf("refused removal changed configuration metadata: %v", err)
			}
			assertNoStagingResidue(t)
			if condition == "new-local" {
				for _, path := range []string{config.LocalConfigPath(repo), filepath.Join(repo, ".agents", "lock.local.yaml")} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("refused removal created %s: %v", path, err)
					}
				}
			}
		})
	}
}

func TestRemoveMarketplaceHookRefusalPreservesPins(t *testing.T) {
	for _, collection := range []bool{false, true} {
		t.Run(fmt.Sprintf("collection=%v", collection), func(t *testing.T) {
			home := isolateConsumerPipeline(t)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			fake := newLayerMarketplaceServer(t)
			registry := fake.addRegistry(t, "source")
			hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo marketplace-removal-payload", "command", nil, ir.SourceRef{})
			artifact, err := hccartifact.FromIR(hook)
			if err != nil {
				t.Fatal(err)
			}
			artifact.Manifest.RegistryID = string(registry.ID)
			artifact.Manifest.RegistrySlug = registry.Slug
			artifact.Manifest.Revision = artifact.Manifest.ContentDigest
			raw, archiveDigest, err := hccartifact.Encode(artifact)
			if err != nil {
				t.Fatal(err)
			}
			fake.resources["source/hook/guard"] = layerMarketplaceResource{
				kind: "hook", name: "guard", raw: raw, archiveDigest: archiveDigest,
				revision: layerAssessedRevision(store.ResourceRevision{
					Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(fake.instanceID), Registry: registry.ID, Kind: "hook", Name: "guard"},
					ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
				}),
			}
			kind, name := "hook", "guard"
			if collection {
				fake.addCollection(t, registry, "team", []hccartifact.CollectionMemberEntry{{
					RegistryID: string(registry.ID), Kind: "hook", Name: "guard", Revision: artifact.Manifest.ContentDigest,
				}})
				kind, name = "collection", "team"
			}
			pins := layerPreparePins(t, fake, kind, name)
			repo := t.TempDir()
			t.Chdir(repo)
			configPath := config.ProjectConfigPath(repo)
			writeFile(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
			if _, err := mutateRollbackMarketplace(configPath, config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, pins, nil); err != nil {
				t.Fatal(err)
			}
			result := Sync(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repo)})
			if len(result.Errors) != 0 {
				t.Fatal(result.Errors)
			}
			paths := []string{configPath, filepath.Join(repo, ".agents", "lock.yaml"), filepath.Join(repo, ".claude", "settings.json")}
			before := make(map[string][]byte)
			for _, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = data
			}
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "teammate-state"))
			resourceKind := ir.KindHook
			if collection {
				resourceKind = ir.KindCollection
			}
			_, err = Remove(context.Background(), RemoveOptions{
				RepoRoot: repo, ResourceName: name, Kind: resourceKind, HasKind: true, Scope: ScopeRepo, ScopeSpecified: true,
			})
			if err == nil || !strings.Contains(err.Error(), "hook.remove-unowned") {
				t.Fatalf("remove = %v, want ownership refusal", err)
			}
			for path, original := range before {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != string(original) {
					t.Fatalf("refused Marketplace removal changed %s: %v", path, err)
				}
			}
		})
	}
}

// spec: hook-resources/remaining-contributors-and-owned-removal-stay-supported
func TestRemoveHookOwnedAndAbsentRemainSupported(t *testing.T) {
	for _, target := range hcc.Targets() {
		t.Run(string(target), func(t *testing.T) {
			isolateConsumerPipeline(t)
			repo := t.TempDir()
			t.Chdir(repo)
			source := t.TempDir()
			writeFile(t, filepath.Join(source, "hooks", "hooks.json"), `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo owner-hook"}]}]}}`)
			writeFile(t, config.ProjectConfigPath(repo), fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [%s]\nsources:\n  reg: {directory: %q}\nresources:\n  reg: {include: [hook/PreToolUse-command]}\n", target, source))
			t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), ".codex"))
			opts := RemoveOptions{RepoRoot: repo, ResourceName: "PreToolUse-command", Kind: ir.KindHook, HasKind: true, Scope: ScopeRepo, ScopeSpecified: true}
			result := Sync(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repo)})
			if len(result.Errors) != 0 {
				t.Fatal(result.Errors)
			}
			if _, err := Remove(context.Background(), opts); err != nil {
				t.Fatalf("owned removal: %v", err)
			}
			checked := Sync(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repo), Offline: true})
			if len(checked.Errors) != 0 || len(checked.Resolved) != 0 {
				t.Fatalf("removed selection did not converge: %+v", checked)
			}
		})
	}
}
