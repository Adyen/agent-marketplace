package materialize

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// spec: claude-native-configuration/relocated-cleanup-ignores-unrelated-files
func TestClaudeConfigurationPrunePreservesUnrelatedFiles(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			_, repo, custom := claudeDirectoryFixture(t)
			options := []RunOption{WithMaterializationModes(config.MaterializationModes{Machine: mode})}
			resources := []resolver.ResolvedArtifact{
				claudeDirectorySkill("stale", config.UserPersonal, hcc.TargetClaude),
			}
			if report, err := Run(context.Background(), repo, resources, options...); err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("seed Claude output: %v, %+v", err, report)
			}
			raw := `{"mcpServers":{"other":{"_managed_by":"agent-marketplace","command":"keep"}},"account":"other"}`
			plugin := filepath.Join(custom, ".claude-plugin/plugin.json")
			if !bytes.Contains(claudeDirectoryRead(t, plugin), []byte("stale")) {
				t.Fatal("seed did not create a stale Claude aggregate entry")
			}
			if err := os.Chmod(plugin, 0o640); err != nil {
				t.Fatal(err)
			}
			files := []string{"other-account/.claude.json", "other-repository/.mcp.json", "unrelated.json", ".claude-plugin/unrelated.json", "hook-support/other-account/.claude.json"}
			before := make(map[string]os.FileInfo, len(files))
			for _, rel := range files {
				claudeDirectoryWrite(t, filepath.Join(custom, rel), raw)
				info, err := os.Stat(filepath.Join(custom, rel))
				if err != nil {
					t.Fatal(err)
				}
				before[rel] = info
			}
			report, err := Run(context.Background(), repo, nil, options...)
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("prune Claude output: %v, %+v", err, report)
			}
			if _, err := os.Lstat(filepath.Join(custom, "skills/stale")); !os.IsNotExist(err) {
				t.Fatalf("stale Claude output not pruned: %v", err)
			}
			for _, rel := range files {
				if string(claudeDirectoryRead(t, filepath.Join(custom, rel))) != raw {
					t.Errorf("Claude cleanup changed unrelated file %s", rel)
				}
				info, err := os.Stat(filepath.Join(custom, rel))
				if err != nil || !os.SameFile(before[rel], info) || before[rel].Mode() != info.Mode() || !before[rel].ModTime().Equal(info.ModTime()) {
					t.Errorf("Claude cleanup changed unrelated metadata %s: %v", rel, err)
				}
			}
			afterPlugin, err := os.ReadFile(plugin)
			if err != nil && !os.IsNotExist(err) || bytes.Contains(afterPlugin, []byte("stale")) {
				t.Fatalf("stale Claude aggregate entry survived: %v, %s", err, afterPlugin)
			}
			if info, err := os.Stat(plugin); err != nil || info.Mode().Perm() != 0o640 {
				t.Fatalf("stale-entry cleanup changed shared permissions: %v", err)
			}
		})
	}
}

// spec: claude-native-configuration/admitted-root-alias-supports-native-hooks
func TestClaudeConfigurationAliasInstallsNativeHooks(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			_, repo, custom := claudeDirectoryFixture(t)
			if err := os.MkdirAll(custom, 0o700); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(custom, alias); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CLAUDE_CONFIG_DIR", alias)
			hook := resolverHook(t, makeHookSupportSource(t, "echo guard", 0o600), "guard")
			hook.Source, hook.Harnesses = config.UserPersonal, []hcc.Target{hcc.TargetClaude}
			resources := append(nativeMCPServerResources(t, hcc.TargetClaude, config.UserPersonal)[:1], hook)
			options := []RunOption{WithMaterializationModes(config.MaterializationModes{Machine: mode})}
			report, err := Run(context.Background(), repo, resources, options...)
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("install through admitted root alias: %v, %+v", err, report)
			}
			claudeDirectoryRead(t, filepath.Join(custom, "settings.json"))
			claudeDirectoryRead(t, filepath.Join(custom, ".claude.json"))
			report, err = Run(context.Background(), repo, resources, append(options, WithDryRun())...)
			if err != nil || !report.InSync() {
				t.Fatalf("alias drift check: %v, %+v", err, report)
			}
		})
	}
}

// spec: claude-native-configuration/relocated-cleanup-preserves-an-unsafe-aggregate-parent
func TestClaudeConfigurationPrunePreservesAliasedComponent(t *testing.T) {
	_, repo, custom := claudeDirectoryFixture(t)
	resources := []resolver.ResolvedArtifact{claudeDirectorySkill("stale", config.UserPersonal, hcc.TargetClaude)}
	if report, err := Run(context.Background(), repo, resources); err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("seed Claude output: %v, %+v", err, report)
	}
	component := filepath.Join(custom, ".claude-plugin")
	retained := filepath.Join(t.TempDir(), "retained")
	foreign := t.TempDir()
	file := filepath.Join(foreign, "plugin.json")
	raw := `{"skills":[{"name":"foreign","_managed_by":"agent-marketplace"}]}`
	claudeDirectoryWrite(t, file, raw)
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	report, err := Run(context.Background(), repo, nil, func(cfg *runConfig) {
		cfg.pruneEnumerationHook = func(path string) {
			if path != component || hit {
				return
			}
			hit = true
			if err := os.Rename(component, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(foreign, component); err != nil {
				t.Fatal(err)
			}
		}
	})
	if !hit {
		t.Fatal("component prune boundary did not execute")
	}
	if err != nil || !diag.HasErrors(report.Diags) {
		t.Fatalf("unsafe component did not refuse: %v, %+v", err, report)
	}
	if string(claudeDirectoryRead(t, file)) != raw {
		t.Fatal("cleanup crossed an unadmitted component alias")
	}
	after, err := os.Stat(file)
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("cleanup changed foreign metadata: %v", err)
	}
	if err := os.Remove(component); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, component); err != nil {
		t.Fatal(err)
	}
	report, err = Run(context.Background(), repo, nil)
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("healthy cleanup after restoring the component: %v, %+v", err, report)
	}
}

// spec: claude-native-configuration/unset-or-empty-variable-keeps-the-default-roots
func TestClaudeConfigurationDefaultAliasPrunesAggregate(t *testing.T) {
	home, repo, custom := claudeDirectoryFixture(t)
	if err := os.MkdirAll(custom, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(custom, filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	resources := []resolver.ResolvedArtifact{claudeDirectorySkill("stale", config.UserPersonal, hcc.TargetClaude)}
	if report, err := Run(context.Background(), repo, resources); err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("seed default alias: %v, %+v", err, report)
	}
	file := filepath.Join(custom, ".claude-plugin/plugin.json")
	if !bytes.Contains(claudeDirectoryRead(t, file), []byte("stale")) {
		t.Fatal("seed did not create a stale aggregate entry")
	}
	report, err := Run(context.Background(), repo, nil)
	if err != nil || diag.HasErrors(report.Diags) || bytes.Contains(claudeDirectoryRead(t, file), []byte("stale")) {
		t.Fatalf("cleanup through the default alias: %v, %+v", err, report)
	}
}

// spec: claude-native-configuration/git-administrative-roots-refuse
func TestClaudeConfigurationGitRefusalPreservesOtherTargets(t *testing.T) {
	home, repo, _ := claudeDirectoryFixture(t)
	control := t.TempDir()
	root := filepath.Join(control, "account")
	t.Setenv("GIT_DIR", control)
	t.Setenv("GIT_COMMON_DIR", "")
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	resources := []resolver.ResolvedArtifact{
		claudeDirectorySkill("refused", config.UserPersonal, hcc.TargetClaude),
		claudeDirectorySkill("safe", config.UserPersonal, hcc.TargetFactory),
	}
	report, err := Run(context.Background(), repo, resources)
	if err != nil || !diag.HasErrors(report.Diags) {
		t.Fatalf("Git administrative root was not refused: %v, %+v", err, report)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("refused Claude root was created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "safe", "SKILL.md")); err != nil {
		t.Fatalf("independent Factory selection was blocked: %v", err)
	}
}

// spec: materialization/unused-non-directory-components-do-not-block-cleanup
func TestClaudeConfigurationUnusedNondirectoryComponent(t *testing.T) {
	for _, desired := range []bool{false, true} {
		t.Run(map[bool]string{false: "cleanup", true: "desired-output"}[desired], func(t *testing.T) {
			_, repo, root := claudeDirectoryFixture(t)
			resources := []resolver.ResolvedArtifact{claudeDirectorySkill("stale", config.UserPersonal, hcc.TargetClaude)}
			if report, err := Run(context.Background(), repo, resources); err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("seed output: %v, %+v", err, report)
			}
			component := filepath.Join(root, ".claude-plugin")
			if err := os.RemoveAll(component); err != nil {
				t.Fatal(err)
			}
			claudeDirectoryWrite(t, component, "unrelated user file")
			before, err := os.Stat(component)
			if err != nil {
				t.Fatal(err)
			}
			var selections []resolver.ResolvedArtifact
			if desired {
				selections = resources
			}
			report, err := Run(context.Background(), repo, selections)
			if err != nil || diag.HasErrors(report.Diags) != desired {
				t.Fatalf("component admission: %v, %+v", err, report)
			}
			after, err := os.Stat(component)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("unrelated component metadata changed: %v", err)
			}
			if string(claudeDirectoryRead(t, component)) != "unrelated user file" {
				t.Fatal("unrelated component bytes changed")
			}
			if !desired {
				if _, err := os.Lstat(filepath.Join(root, "skills", "stale")); !os.IsNotExist(err) {
					t.Fatalf("unused non-directory component blocked stale skill cleanup: %v", err)
				}
			}
		})
	}
}
