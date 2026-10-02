package marketplace

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// spec: marketplace-api/mirrored-hook-support-installation
// spec: marketplace-api/mirrored-hook-support-cache-independent-installation
func TestMirrorHookNativeTargets(t *testing.T) {
	for _, target := range []struct {
		target                          hcc.Target
		projectAggregate, userAggregate string
		projectScript, userScript       string
	}{
		{hcc.TargetClaude, ".claude/hooks/hooks.json", ".claude/hooks/hooks.json", ".claude/hook-support", ".claude/hook-support"},
		{hcc.TargetFactory, ".factory/hooks/hooks.json", ".factory/hooks/hooks.json", ".factory/hook-support", ".factory/hook-support"},
		{hcc.TargetCursor, ".cursor/hooks/hooks.json", ".cursor/hooks/hooks.json", ".cursor/hook-support", ".cursor/hook-support"},
		{hcc.TargetWindsurf, ".windsurf/hooks.json", ".codeium/windsurf/hooks.json", ".windsurf/hook-support", ".codeium/windsurf/hook-support"},
		{hcc.TargetGemini, ".gemini/settings.json", ".gemini/settings.json", ".gemini/hook-support", ".gemini/hook-support"},
		{hcc.TargetDevin, ".devin/hooks.v1.json", ".config/devin/config.json", ".devin/hook-support", ".config/devin/hook-support"},
		{hcc.TargetCodex, ".codex/hooks.json", ".codex/hooks.json", ".codex/hook-support", ".codex/hook-support"},
	} {
		for _, machine := range []bool{false, true} {
			name := string(target.target) + "/repo"
			if machine {
				name = string(target.target) + "/user"
			}
			t.Run(name, func(t *testing.T) {
				isolateHarnessState(t)
				fixture := newMirrorPublishFixtureForKind(t, "hook")
				cfg := fixture.consumerConfig()
				pin := &cfg.Marketplaces[0].Resources[0]
				pin.Kind, pin.Name = "hook", "PreToolUse-command"
				if machine {
					cfg.Marketplaces[0].Source = config.UserPersonal
				}
				r := &Resolver{Cache: NewCache(t.TempDir())}
				resolved, errs := r.Resolve(t.Context(), cfg, []hcc.Target{target.target}, resolver.MarketplaceResolveModeRefresh)
				if len(errs) != 0 || len(resolved) != 1 {
					t.Fatalf("resolve: %v, %v", resolved, errs)
				}
				repo := t.TempDir()
				if output, err := exec.CommandContext(t.Context(), "git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
					t.Fatalf("initialize repository: %q, %v", output, err)
				}
				report, err := materialize.Run(t.Context(), repo, resolved)
				if err != nil || diag.HasErrors(report.Diags) {
					t.Fatalf("materialize: %v, %v", report, err)
				}
				root, aggregate, script := repo, target.projectAggregate, target.projectScript
				if machine {
					root, aggregate, script = os.Getenv("HOME"), target.userAggregate, target.userScript
				}
				body, err := os.ReadFile(filepath.Join(root, aggregate))
				if err != nil {
					t.Fatal(err)
				}
				var decoded any
				if err := json.Unmarshal(body, &decoded); err != nil {
					t.Fatal(err)
				}
				command := findMirrorHookCommand(decoded)
				if command == "" || strings.Contains(command, "PLUGIN_ROOT") {
					t.Fatalf("missing or unresolved command: %s", body)
				}
				scripts, err := filepath.Glob(filepath.Join(root, script, "*", "PreToolUse-command", "hooks", "run.sh"))
				if err != nil || len(scripts) != 1 {
					t.Fatalf("installed support script: %v, %v", scripts, err)
				}
				info, err := os.Lstat(scripts[0])
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
					t.Fatalf("support is not an installed executable: %v, %v", info, err)
				}
				if strings.Contains(command, r.Cache.root) {
					t.Fatalf("hook still executes from the cache: %s", command)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				run := exec.CommandContext(ctx, "/bin/sh", "-c", command)
				run.Dir = repo
				run.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+repo, "FACTORY_PROJECT_DIR="+repo, "CURSOR_PROJECT_DIR="+repo, "DEVIN_PROJECT_DIR="+repo, "GEMINI_PROJECT_DIR="+repo)
				if output, err := run.CombinedOutput(); err != nil || string(output) != "mirror-hook\n" {
					t.Fatalf("native hook execution: %q, %v", output, err)
				}
				if report, err := materialize.Run(t.Context(), repo, resolved, materialize.WithDryRun()); err != nil || diag.HasErrors(report.Diags) {
					t.Fatalf("check: %v, %v", report, err)
				}
				if err := os.RemoveAll(r.Cache.root); err != nil {
					t.Fatal(err)
				}
				afterCache := exec.CommandContext(ctx, "/bin/sh", "-c", command)
				afterCache.Dir, afterCache.Env = run.Dir, run.Env
				if output, err := afterCache.CombinedOutput(); err != nil || string(output) != "mirror-hook\n" {
					t.Fatalf("execution after cache deletion: %q, %v", output, err)
				}
				options := []materialize.RunOption{materialize.WithRepoScopeOnly()}
				if machine {
					options = []materialize.RunOption{materialize.WithMachineScopeOnly()}
				}
				if report, err := materialize.Run(t.Context(), repo, nil, options...); err != nil || diag.HasErrors(report.Diags) {
					t.Fatalf("deselect: %v, %v", report, err)
				}
				if _, err := os.Stat(scripts[0]); !os.IsNotExist(err) {
					t.Fatal("deselection retained installed support")
				}
			})
		}
	}
}

// spec: marketplace-api/mirrored-hook-support-independent-sources
func TestMirrorHooksKeepSameNamedScriptsIndependent(t *testing.T) {
	isolateHarnessState(t)
	first := newMirrorPublishFixtureForKind(t, "hook", map[string]hccartifact.SupportFile{"run.sh": {Data: []byte("#!/bin/sh\necho first\n"), Executable: true}})
	second := newMirrorPublishFixtureForKind(t, "hook", map[string]hccartifact.SupportFile{"run.sh": {Data: []byte("#!/bin/sh\necho second\n"), Executable: true}})
	consumer := &Resolver{Cache: NewCache(t.TempDir())}
	var selected []resolver.ResolvedArtifact
	for _, fixture := range []*mirrorPublishFixture{first, second} {
		cfg := fixture.consumerConfig()
		cfg.Marketplaces[0].Resources[0].Kind = "hook"
		cfg.Marketplaces[0].Resources[0].Name = "PreToolUse-command"
		resolved, errs := consumer.Resolve(t.Context(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
		if len(errs) != 0 || len(resolved) != 1 {
			t.Fatalf("resolve independent source: %v, %v", resolved, errs)
		}
		selected = append(selected, resolved...)
	}
	repo := t.TempDir()
	report, err := materialize.Run(t.Context(), repo, selected)
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("independent sources: %v, %v", report, err)
	}
	scripts, err := filepath.Glob(filepath.Join(repo, ".factory", "hook-support", "*", "PreToolUse-command", "hooks", "run.sh"))
	if err != nil || len(scripts) != 2 {
		t.Fatalf("independent installed scripts: %v, %v", scripts, err)
	}
	var hooks map[string][]struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	body, err := os.ReadFile(filepath.Join(repo, ".factory", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &hooks); err != nil {
		t.Fatal(err)
	}
	outputs := map[string]int{}
	for _, group := range hooks["PreToolUse"] {
		for _, handler := range group.Hooks {
			run := exec.CommandContext(t.Context(), "/bin/sh", "-c", handler.Command)
			run.Dir = repo
			run.Env = append(os.Environ(), "FACTORY_PROJECT_DIR="+repo)
			output, err := run.CombinedOutput()
			if err != nil {
				t.Fatalf("independent script execution: %q, %v", output, err)
			}
			outputs[string(output)]++
		}
	}
	if len(outputs) != 2 || outputs["first\n"] != 1 || outputs["second\n"] != 1 {
		t.Fatalf("script identity was lost: %v", outputs)
	}
}

func findMirrorHookCommand(value any) string {
	switch v := value.(type) {
	case map[string]any:
		if command, ok := v["command"].(string); ok && strings.Contains(command, "run.sh") {
			return command
		}
		for _, child := range v {
			if command := findMirrorHookCommand(child); command != "" {
				return command
			}
		}
	case []any:
		for _, child := range v {
			if command := findMirrorHookCommand(child); command != "" {
				return command
			}
		}
	}
	return ""
}
