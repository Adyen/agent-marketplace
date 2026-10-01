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
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// spec: marketplace-api/mirrored-hook-support-installation
func TestMirrorHookNativeTargets(t *testing.T) {
	for _, target := range []struct {
		target                          hcc.Target
		projectAggregate, userAggregate string
		projectScript, userScript       string
	}{
		{hcc.TargetClaude, ".claude/hooks/hooks.json", ".claude/hooks/hooks.json", ".claude/hooks/run.sh", ".claude/hooks/run.sh"},
		{hcc.TargetFactory, ".factory/hooks/hooks.json", ".factory/hooks/hooks.json", ".factory/hooks/run.sh", ".factory/hooks/run.sh"},
		{hcc.TargetCursor, ".cursor/hooks/hooks.json", ".cursor/hooks/hooks.json", ".cursor/hooks/run.sh", ".cursor/hooks/run.sh"},
		{hcc.TargetWindsurf, ".windsurf/hooks.json", ".codeium/windsurf/hooks.json", ".windsurf/hook-support/run.sh", ".codeium/windsurf/hook-support/run.sh"},
		{hcc.TargetGemini, ".gemini/settings.json", ".gemini/settings.json", "", ""},
		{hcc.TargetDevin, ".devin/hooks.v1.json", ".config/devin/config.json", "", ""},
		{hcc.TargetCodex, ".codex/hooks.json", ".codex/hooks.json", "", ""},
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
				if script != "" && !strings.Contains(command, filepath.Join(root, script)) {
					t.Fatalf("hook references wrong support path: %s", command)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				run := exec.CommandContext(ctx, "/bin/sh", "-c", command)
				run.Dir = t.TempDir()
				if output, err := run.CombinedOutput(); err != nil || string(output) != "mirror-hook\n" {
					t.Fatalf("native hook execution: %q, %v", output, err)
				}
				if report, err := materialize.Run(t.Context(), repo, resolved, materialize.WithDryRun()); err != nil || diag.HasErrors(report.Diags) {
					t.Fatalf("check: %v, %v", report, err)
				}
			})
		}
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
