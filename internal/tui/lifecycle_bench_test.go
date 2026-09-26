package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func benchmarkSnapshotFixture(b *testing.B, git bool) (string, string) {
	b.Helper()
	home, repo, source := b.TempDir(), b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	b.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	b.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	b.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	b.Chdir(repo)
	write := func(path, content string) {
		b.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	for i := range 200 {
		name := fmt.Sprintf("resource-%03d", i)
		write(filepath.Join(source, "skills", name, "SKILL.md"), fmt.Sprintf("---\nname: %s\ndescription: Representative cached resource\n---\n# Resource\nUse this resource to review code.\n", name))
	}
	kind := "directory"
	if git {
		for _, args := range [][]string{{"init", "-b", "main"}, {"add", "."}, {"-c", "user.name=Benchmark", "-c", "user.email=benchmark@example.com", "commit", "-m", "fixture"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = source
			if output, err := cmd.CombinedOutput(); err != nil {
				b.Fatalf("git %v: %v: %s", args, err, output)
			}
		}
		source = "file://" + source
		kind = "git"
		if _, err := cache.CloneOrPull(source, "main"); err != nil {
			b.Fatal(err)
		}
	}
	write(filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory, claude, devin]\nsources: {}\n")
	body := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  fixture:\n    %s: %s\n", kind, source)
	if git {
		body += "    ref: main\n"
	}
	body += "resources:\n  fixture:\n    all: true\n"
	write(config.ProjectConfigPath(repo), body)
	oldOrg, oldGlobal, oldRepo, oldLocal := loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig
	b.Cleanup(func() {
		loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig = oldOrg, oldGlobal, oldRepo, oldLocal
	})
	loadOrgConfig = func() (*config.Config, error) { return nil, nil }
	loadGlobalConfig = func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(home, ".agents", "config.yaml"))
	}
	loadRepoConfig = func() (*config.Config, error) { return config.LoadConfigFromPath(config.ProjectConfigPath(repo)) }
	loadLocalConfig = func(string) (*config.LocalConfig, error) { return nil, nil }
	if git {
		oid, err := cache.ResolveExactGitRef(source, source, "main")
		if err != nil {
			b.Fatal(err)
		}
		snapshot, err := cache.AcquireExactGit(source, source, oid)
		if err != nil {
			b.Fatal(err)
		}
		built, err := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: snapshot})
		if err != nil {
			b.Fatal(err)
		}
		root := lockfile.GitRoot{Path: ".", SourceHarness: built.SourceHarness, RevisionSchema: built.Manifest.SchemaVersion, RevisionDigest: lockfile.Digest(built.Manifest.RevisionDigest)}
		for _, resource := range built.Manifest.Resources {
			root.Resources = append(root.Resources, lockfile.GitResource{Kind: resource.Kind, Name: resource.Name, ContentDigest: lockfile.Digest(resource.ContentDigest), ArchiveDigest: lockfile.Digest(resource.ArchiveDigest)})
		}
		data, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Registries: []lockfile.GitRegistry{{URL: source, Ref: "main", Commit: oid, Roots: []lockfile.GitRoot{root}}}})
		if err != nil {
			b.Fatal(err)
		}
		write(filepath.Join(repo, ".agents", "lock.yaml"), string(data))
	}
	return repo, home
}

func benchmarkReadSnapshot(b *testing.B, repo, home string) snapshotLoadedMsg {
	b.Helper()
	snapshot := readSnapshot(context.Background(), repo, home, catalogLoadCached)
	if len(snapshot.errs) != 0 || len(snapshot.rows) != 200 {
		b.Fatalf("snapshot: rows=%d errors=%v", len(snapshot.rows), snapshot.errs)
	}
	return snapshot
}

func BenchmarkTUIStartupDiskIO(b *testing.B) {
	for _, source := range []string{"directory", "warm_locked_git_no_network"} {
		b.Run(source, func(b *testing.B) {
			defer benchDiscardLogs()()
			repo, home := benchmarkSnapshotFixture(b, source != "directory")
			benchmarkReadSnapshot(b, repo, home)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				snapshot := benchmarkReadSnapshot(b, repo, home)
				m := NewModel(repo, home)
				m.width, m.height, m.loading = 120, 40, false
				m.applySnapshotData(snapshot)
				_ = m.View()
			}
		})
	}
}

// BenchmarkTUIStartupStages measures the progressive startup milestones
// separately on identical disk-backed fixtures: first_visible runs the real
// startup path from readSnapshotStages through the config stage and the
// cached-first Marketplace discovery command it launches until the first
// cached row is applied and rendered in the model (the still-running
// inventory/dry-run stages are not awaited), config_ready_prerequisite is the
// config callback alone, toggle_ready is verified inventory, and
// fully_assessed is the final snapshot.
func BenchmarkTUIStartupStages(b *testing.B) {
	targets := map[string]snapshotStage{
		"config_ready_prerequisite": snapshotStageConfig,
		"toggle_ready":              snapshotStageInventory,
		"fully_assessed":            snapshotStageFinal,
	}
	for _, source := range []string{"directory", "warm_locked_git_no_network"} {
		b.Run(source+"/first_visible", func(b *testing.B) {
			defer benchDiscardLogs()()
			repo, home := benchmarkSnapshotFixture(b, source != "directory")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/agent-marketplace.json" {
					_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("market", "https://market.example"))
					return
				}
				http.NotFound(w, r)
			}))
			b.Cleanup(server.Close)
			endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "market", Alias: "market"}
			seedCachedBrowseFixture(b, endpoint)
			localPath := config.LocalConfigPath(repo)
			if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(localPath, []byte("marketplaces:\n  - url: "+endpoint.URL+"\n    instance_id: "+endpoint.InstanceID+"\n    alias: "+endpoint.Alias+"\n"), 0o600); err != nil {
				b.Fatal(err)
			}
			loadLocalConfig = func(string) (*config.LocalConfig, error) {
				return config.LoadLocalConfigFromPath(localPath)
			}
			benchmarkReadSnapshot(b, repo, home)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				m := NewModel(repo, home)
				m.width, m.height = 120, 40
				stages := readSnapshotStages(context.Background(), repo, home, catalogLoadCached, 1)
				var cfgMsg snapshotLoadedMsg
				for candidate := range stages {
					if candidate.stage == snapshotStageConfig {
						cfgMsg = candidate
						break
					}
				}
				if cfgMsg.mergedCfg == nil {
					b.Fatalf("config stage never delivered a merged config; last = %+v", cfgMsg)
				}
				m.mergedCfg = cfgMsg.mergedCfg
				next := m.beginMarketplaceDiscovery(m.filter, false)
				if next == nil {
					b.Fatal("config stage produced no discovery command")
				}
				for {
					msg := next().(discoveryLoadedMsg)
					if msg.err != nil {
						b.Fatalf("cached discovery failed: %v", msg.err)
					}
					updated, follow := m.Update(msg)
					m = updated.(Model)
					if len(m.rows) > 0 {
						break
					}
					if msg.done || follow == nil {
						b.Fatalf("discovery finished without a visible row: %+v", msg)
					}
					next = follow
				}
				_ = m.View()
				b.StopTimer()
				for range stages {
				}
				b.StartTimer()
			}
		})
		for _, target := range []string{"config_ready_prerequisite", "toggle_ready", "fully_assessed"} {
			b.Run(source+"/"+target, func(b *testing.B) {
				defer benchDiscardLogs()()
				repo, home := benchmarkSnapshotFixture(b, source != "directory")
				benchmarkReadSnapshot(b, repo, home)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					var reached snapshotLoadedMsg
					stages := readSnapshotStages(context.Background(), repo, home, catalogLoadCached, 1)
					for msg := range stages {
						reached = msg
						if msg.stage == targets[target] {
							break
						}
					}
					if reached.stage != targets[target] {
						b.Fatalf("stage %s never delivered; last = %+v", target, reached)
					}
					b.StopTimer()
					for range stages {
					}
					b.StartTimer()
				}
			})
		}
	}
}

func BenchmarkTUIToggleEditorAndReloadDiskIONoReconcile(b *testing.B) {
	defer benchDiscardLogs()()
	repo, home := benchmarkSnapshotFixture(b, false)
	snapshot := benchmarkReadSnapshot(b, repo, home)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var selected row
		for _, candidate := range snapshot.rows {
			if candidate.Artifact.Name == "resource-000" {
				selected = candidate
				break
			}
		}
		act := scopedToggleAction(selected, scopeProject)
		msg := runScopedToggle(repo, act)().(configWrittenMsg)
		if msg.err != nil {
			b.Fatal(msg.err)
		}
		snapshot = benchmarkReadSnapshot(b, repo, home)
	}
}
