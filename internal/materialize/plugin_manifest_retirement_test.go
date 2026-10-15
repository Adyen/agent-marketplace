package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func claudeSkillEntry(name string) resolver.ResolvedArtifact {
	skill := ir.NewSkill(name, "A skill", "Body", nil, nil, ir.SourceRef{})
	return resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Kind: ir.KindSkill, Name: name, Path: filepath.Join("skills", name, "SKILL.md"), Resource: skill},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetClaude},
	}
}

func claudeSubagentEntry(name string) resolver.ResolvedArtifact {
	subagent := ir.NewSubagent(name, "A droid", "You are a worker", "claude-sonnet-4-5-20250929", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})
	return resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: name, Path: filepath.Join("agents", name+".md"), Resource: subagent},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetClaude},
	}
}

// spec: materialization/sync-writes-no-claude-plugin-manifest
func TestSyncWritesNoClaudePluginManifest(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	resolved := []resolver.ResolvedArtifact{
		claudeSkillEntry("my-skill"),
		claudeSubagentEntry("my-agent"),
	}
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("Run: %v", err)
	}
	manifest := filepath.Join(repoRoot, ".claude", ".claude-plugin", "plugin.json")
	if _, err := os.Lstat(manifest); !os.IsNotExist(err) {
		t.Fatalf("expected no plugin manifest, found %s", manifest)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".claude", "skills", "my-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected materialized skill: %v", err)
	}
}

// spec: materialization/sync-retires-earlier-managed-manifest-entries
func TestSyncRetiresManagedManifestEntries(t *testing.T) {
	for _, scope := range []struct {
		name      string
		machine   bool
		pluginDir func(repoRoot, home string) string
	}{
		{
			name:    "repository",
			machine: false,
			pluginDir: func(repoRoot, home string) string {
				return filepath.Join(repoRoot, ".claude", ".claude-plugin")
			},
		},
		{
			name:    "machine",
			machine: true,
			pluginDir: func(repoRoot, home string) string {
				return filepath.Join(home, ".claude", ".claude-plugin")
			},
		},
	} {
		t.Run(scope.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			home := t.TempDir()
			t.Setenv("HOME", home)
			pluginDir := scope.pluginDir(repoRoot, home)
			if err := os.MkdirAll(pluginDir, 0o755); err != nil {
				t.Fatal(err)
			}
			manifestPath := filepath.Join(pluginDir, "plugin.json")
			if err := os.WriteFile(manifestPath, []byte(`{
  "skills": [
    {"name": "user-skill", "description": "keep me"},
    {"name": "managed-skill", "description": "drop me", "_managed_by": "agent-marketplace"}
  ],
  "agents": [
    {"name": "managed-agent", "description": "drop me", "_managed_by": "agent-marketplace"}
  ],
  "name": "user-plugin"
}`), 0o644); err != nil {
				t.Fatal(err)
			}

			resolved := []resolver.ResolvedArtifact{claudeSkillEntry("installed-skill")}
			if scope.machine {
				resolved[0].Source = config.UserPersonal
			}
			if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
				t.Fatalf("Run: %v", err)
			}

			content, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("expected manifest kept for user entries: %v", err)
			}
			got := string(content)
			if !strings.Contains(got, `"user-skill"`) || !strings.Contains(got, `"user-plugin"`) {
				t.Fatalf("expected user entries preserved, got %s", got)
			}
			if strings.Contains(got, `"managed-skill"`) || strings.Contains(got, `"managed-agent"`) {
				t.Fatalf("expected managed entries retired, got %s", got)
			}
			if strings.Contains(got, `"installed-skill"`) {
				t.Fatalf("retired manifest gained an entry for the installed skill: %s", got)
			}
			if strings.Contains(got, `"_managed_by"`) {
				t.Fatalf("retired manifest still carries a managed marker: %s", got)
			}
		})
	}
}

// spec: materialization/sync-retires-earlier-managed-manifest-entries
func TestSyncDeletesManifestWhenOnlyEmptiesRemain(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	pluginDir := filepath.Join(repoRoot, ".claude", ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(`{
  "skills": [
    {"name": "managed-skill", "_managed_by": "agent-marketplace"}
  ],
  "agents": []
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{claudeSkillEntry("installed-skill")}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Lstat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("expected manifest deleted, got err=%v", err)
	}
	if _, err := os.Lstat(pluginDir); !os.IsNotExist(err) {
		t.Fatalf("expected .claude-plugin directory removed, got err=%v", err)
	}
}

// spec: materialization/check-reports-pending-manifest-retirement-without-writing
func TestSyncCheckReportsManifestRetirementDrift(t *testing.T) {
	writeManifest := func(t *testing.T, repoRoot, body string) string {
		t.Helper()
		pluginDir := filepath.Join(repoRoot, ".claude", ".claude-plugin")
		if err := os.MkdirAll(pluginDir, 0o755); err != nil {
			t.Fatal(err)
		}
		manifestPath := filepath.Join(pluginDir, "plugin.json")
		if err := os.WriteFile(manifestPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return manifestPath
	}

	t.Run("pending retirement is the only drift", func(t *testing.T) {
		repoRoot := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		resolved := []resolver.ResolvedArtifact{claudeSkillEntry("installed-skill")}
		if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
			t.Fatalf("Run: %v", err)
		}
		original := `{
  "skills": [
    {"name": "managed-skill", "_managed_by": "agent-marketplace"}
  ]
}`
		manifestPath := writeManifest(t, repoRoot, original)

		report, err := Run(context.Background(), repoRoot, resolved, WithDryRun())
		if err != nil {
			t.Fatalf("Run dry-run: %v", err)
		}
		if report == nil || report.InSync() {
			t.Fatal("expected dry-run to report drift for the pending manifest retirement")
		}
		content, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("read manifest: %v", err)
		}
		if string(content) != original {
			t.Fatalf("dry-run modified the manifest: %s", content)
		}
	})

	t.Run("manifest without managed entries stays in sync", func(t *testing.T) {
		repoRoot := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		resolved := []resolver.ResolvedArtifact{claudeSkillEntry("installed-skill")}
		if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
			t.Fatalf("Run: %v", err)
		}
		original := `{
  "skills": [
    {"name": "user-skill", "description": "keep me"}
  ],
  "name": "user-plugin"
}`
		manifestPath := writeManifest(t, repoRoot, original)

		report, err := Run(context.Background(), repoRoot, resolved, WithDryRun())
		if err != nil {
			t.Fatalf("Run dry-run: %v", err)
		}
		if report == nil || !report.InSync() {
			t.Fatal("expected dry-run in sync for a manifest without managed entries")
		}
		content, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("read manifest: %v", err)
		}
		if string(content) != original {
			t.Fatalf("dry-run modified the manifest: %s", content)
		}
	})
}

// spec: materialization/sync-retires-earlier-managed-manifest-entries
func TestSyncLeavesSymlinkedPluginDirectoryAlone(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "check"}[dryRun], func(t *testing.T) {
			repoRoot := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			outside := t.TempDir()
			outsideManifest := filepath.Join(outside, "plugin.json")
			original := `{
  "skills": [
    {"name": "managed-skill", "_managed_by": "agent-marketplace"}
  ]
}`
			if err := os.WriteFile(outsideManifest, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			claudeDir := filepath.Join(repoRoot, ".claude")
			if err := os.MkdirAll(claudeDir, 0o755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(claudeDir, ".claude-plugin")
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}

			var opts []RunOption
			if dryRun {
				opts = append(opts, WithDryRun())
			}
			if _, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{claudeSkillEntry("installed-skill")}, opts...); err != nil {
				t.Fatalf("Run: %v", err)
			}
			info, err := os.Lstat(link)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("expected .claude-plugin symlink left in place, info=%v err=%v", info, err)
			}
			content, err := os.ReadFile(outsideManifest)
			if err != nil {
				t.Fatalf("read outside manifest: %v", err)
			}
			if string(content) != original {
				t.Fatalf("sync rewrote a manifest outside the target root through a symlink: %s", content)
			}
		})
	}
}

// spec: materialization/sync-retires-earlier-managed-manifest-entries
func TestSyncLeavesSymlinkedPluginManifestAlone(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "check"}[dryRun], func(t *testing.T) {
			repoRoot := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			pluginDir := filepath.Join(repoRoot, ".claude", ".claude-plugin")
			if err := os.MkdirAll(pluginDir, 0o755); err != nil {
				t.Fatal(err)
			}
			outsideManifest := filepath.Join(t.TempDir(), "plugin.json")
			original := `{
  "skills": [
    {"name": "managed-skill", "_managed_by": "agent-marketplace"}
  ]
}`
			if err := os.WriteFile(outsideManifest, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(pluginDir, "plugin.json")
			if err := os.Symlink(outsideManifest, link); err != nil {
				t.Fatal(err)
			}

			var opts []RunOption
			if dryRun {
				opts = append(opts, WithDryRun())
			}
			if _, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{claudeSkillEntry("installed-skill")}, opts...); err != nil {
				t.Fatalf("Run: %v", err)
			}
			info, err := os.Lstat(link)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("expected plugin.json symlink left in place, info=%v err=%v", info, err)
			}
			content, err := os.ReadFile(outsideManifest)
			if err != nil {
				t.Fatalf("read outside manifest: %v", err)
			}
			if string(content) != original {
				t.Fatalf("sync rewrote a manifest outside the target root through a symlink: %s", content)
			}
		})
	}
}

// spec: materialization/factory-plugin-directory-is-left-alone
func TestFactoryPluginDirectoryLeftAlone(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	pluginDir := filepath.Join(repoRoot, ".factory", ".factory-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	original := `{
  "droids": [
    {"name": "stale", "description": "old", "_managed_by": "agent-marketplace"}
  ],
  "name": "user-plugin"
}`
	if err := os.WriteFile(manifestPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	subagent := ir.NewSubagent("worker-generic", "Worker", "You are a worker", "claude-sonnet-4-5-20250929", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: "worker-generic", Path: filepath.Join(t.TempDir(), "worker-generic.md"), Resource: subagent},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("Run: %v", err)
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("expected .factory-plugin/plugin.json left in place: %v", err)
	}
	if string(content) != original {
		t.Fatalf("sync modified the retired .factory-plugin directory: %s", content)
	}
}
