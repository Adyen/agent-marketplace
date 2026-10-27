package opencodeprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	compiler "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/opencode"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: opencode-hooks/a-blocking-pre-tool-hook-prevents-the-protected-effect
// spec: opencode-hooks/handler-failure-is-bounded-and-blocks-protected-effects
// spec: opencode-hooks/the-release-gate-proves-hooks-in-a-controlled-runtime
func TestNativeCompiledCommandHooks(t *testing.T) {
	binary := requireNative(t)
	t.Run("TodoWrite", func(t *testing.T) {
		hook := ir.NewHookWithMatcher("todo", "PreToolUse", "TodoWrite", `cat > hook-input.json; printf '{}'`, "command", nil, ir.SourceRef{})
		artifacts, _, err := (compiler.Compiler{}).Translate(hook)
		if err != nil {
			t.Fatal(err)
		}
		files := map[string][]byte{}
		for _, artifact := range artifacts {
			files[artifact.Path] = artifact.Bytes
		}
		result := runNativeArtifacts(t, binary, "todowrite", files)
		var input struct {
			Tool string `json:"tool_name"`
		}
		if !result.called || json.Unmarshal(result.input, &input) != nil || input.Tool != "TodoWrite" {
			t.Fatal("native TodoWrite did not reach its matching callback")
		}
	})
	for _, tc := range []struct {
		name, matcher, command string
		called, effect         bool
	}{
		{"allow", "Bash", `cat > hook-input.json; printf '%s' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}'`, true, true},
		{"block", "Bash", `cat > hook-input.json; printf '%s' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny"}}'`, true, false},
		{"nonmatching", "Read", `cat > hook-input.json; exit 2`, false, true},
		{"failed", "Bash", `cat > hook-input.json; exit 1`, true, false},
		{"malformed", "Bash", `cat > hook-input.json; printf malformed`, true, false},
		{"missing", "Bash", `cat > hook-input.json; missing-synthetic-handler`, true, false},
		{"timeout", "Bash", `cat > hook-input.json; sleep 3; printf '{}'`, true, false},
		{"output below", "Bash", `cat > hook-input.json; printf '{"ignored":"'; head -c 1048561 /dev/zero | tr '\000' a; printf '"}'`, true, true},
		{"output boundary", "Bash", `cat > hook-input.json; printf '{"ignored":"'; head -c 1048562 /dev/zero | tr '\000' a; printf '"}'`, true, true},
		{"output excess", "Bash", `cat > hook-input.json; printf '{"ignored":"'; head -c 1048563 /dev/zero | tr '\000' a; printf '"}'`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := ir.NewHookWithMatcherOptions("check", "PreToolUse", tc.matcher, tc.command, "command", nil, ir.SourceRef{}, ir.WithHookTimeout(1))
			a, d, err := (compiler.Compiler{}).Translate(hook)
			if err != nil || len(d) != 0 || len(a) != 2 {
				t.Fatalf("compiled hook: %v %v", d, err)
			}
			files := map[string][]byte{}
			for _, artifact := range a {
				files[artifact.Path] = artifact.Bytes
			}
			result := runNativeArtifacts(t, binary, tc.name, files)
			if result.called != tc.called || result.effect != tc.effect {
				t.Fatalf("handler=%v effect=%v; want %v %v", result.called, result.effect, tc.called, tc.effect)
			}
			if result.called {
				assertNativeHookInput(t, result.input)
			}
		})
	}
}

func assertNativeHookInput(t *testing.T, input []byte) {
	t.Helper()
	var value struct {
		Event   string `json:"hook_event_name"`
		Tool    string `json:"tool_name"`
		Session string `json:"session_id"`
		Call    string `json:"tool_use_id"`
		Args    struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(input, &value); err != nil || value.Event != "PreToolUse" || value.Tool != "Bash" || value.Session == "" || value.Call == "" || value.Args.Command != "printf protected > effect.txt" {
		t.Fatal("native hook input does not match the documented tool contract")
	}
}

// spec: opencode-hooks/removing-one-contribution-preserves-the-others
func TestNativeInstalledCommandHooks(t *testing.T) {
	binary := requireNative(t)
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			result := runNativeArtifacts(t, binary, "installed", map[string][]byte{}, func(workspace, root string) {
				t.Setenv("HOME", filepath.Join(root, "home"))
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
				t.Setenv("OPENCODE_CONFIG", "")
				t.Setenv("OPENCODE_CONFIG_DIR", "")
				contribution := func(name, command string) resolver.ResolvedArtifact {
					h := ir.NewHookWithMatcherOptions(name, "PreToolUse", "Bash", command, "command", nil, ir.SourceRef{})
					return resolver.ResolvedArtifact{Entry: catalog.Entry{Name: name, Resource: h}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetOpenCode}}
				}
				block := contribution("block", `cat > hook-input.json; printf '%s' '{"decision":"block"}'`)
				allow := contribution("allow", `cat > hook-input.json; printf '{}'`)
				modes := config.MaterializationModes{Project: mode, Machine: mode}
				for _, desired := range [][]resolver.ResolvedArtifact{{allow, block}, {block}} {
					report, err := materialize.Run(context.Background(), workspace, desired, materialize.WithMaterializationModes(modes))
					if err != nil || diag.HasErrors(report.Diags) {
						t.Fatalf("native installation: %v %v", err, report.Diags)
					}
				}
			})
			if !result.called || result.effect {
				t.Fatal("installed blocking hook was not effective after sibling removal")
			}
			assertNativeHookInput(t, result.input)
		})
	}
}

// spec: opencode-hooks/hook-support-is-installed-beside-the-adapter
// spec: opencode-hooks/support-and-project-references-follow-the-adapter-project-directory
func TestNativeInstalledHookSupport(t *testing.T) {
	binary := requireNative(t)
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		for _, subdir := range []string{"", "nested/area"} {
			t.Run(string(mode)+"/"+strings.ReplaceAll(subdir, "/", "-"), func(t *testing.T) {
				result := runNativeCommandIn(t, binary, "installed", subdir, nil, map[string][]byte{}, func(workspace, root string) {
					t.Setenv("HOME", filepath.Join(root, "home"))
					t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
					t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
					t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
					t.Setenv("OPENCODE_CONFIG", "")
					t.Setenv("OPENCODE_CONFIG_DIR", "")
					// The blocking handler lives beside its source definition
					// and is reached only through the installed support copy.
					source := filepath.Join(root, "registry")
					writeFixture(t, filepath.Join(source, "hooks", "hooks.json"), `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"/bin/sh \"${DROID_PLUGIN_ROOT}/hooks/guard.sh\""}]}]}`)
					writeFixture(t, filepath.Join(source, "hooks", "guard.sh"), `cat > "$AGENT_MARKETPLACE_PROJECT_DIR/hook-input.json"; printf '%s' '{"decision":"block"}'`+"\n")
					loaded, err := catalog.Load(source, "factory")
					if err != nil || len(loaded.Entries) != 1 {
						t.Fatalf("source hook: %v %v", err, loaded)
					}
					selected := resolver.ResolvedArtifact{Entry: loaded.Entries[0], RegistryURL: source, CatalogKey: source, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetOpenCode}}
					modes := config.MaterializationModes{Project: mode, Machine: mode}
					report, err := materialize.Run(t.Context(), workspace, []resolver.ResolvedArtifact{selected}, materialize.WithMaterializationModes(modes))
					if err != nil || diag.HasErrors(report.Diags) {
						t.Fatalf("native installation: %v %v", err, report.Diags)
					}
					if err := os.RemoveAll(source); err != nil {
						t.Fatal(err)
					}
				})
				if !result.called || result.effect {
					t.Fatal("installed hook support was not effective")
				}
				assertNativeHookInput(t, result.input)
			})
		}
	}
}

func TestNativeInstalledResourceDiscovery(t *testing.T) {
	binary := requireNative(t)
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		for _, inventory := range []struct {
			name      string
			arguments []string
			values    []string
		}{
			{"skill", []string{"debug", "skill"}, []string{"probe-skill", "Native skill body"}},
			{"agent", []string{"agent", "list"}, []string{"probe-reviewer (subagent)"}},
			{"config", []string{"debug", "config"}, []string{"probe-command", "$ARGUMENTS", "probe-server", "synthetic-native-value"}},
		} {
			t.Run(string(mode)+"/"+inventory.name, func(t *testing.T) {
				result := runNativeCommand(t, binary, "inventory", inventory.arguments, map[string][]byte{}, func(workspace, root string) {
					t.Setenv("HOME", filepath.Join(root, "home"))
					t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
					t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
					t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
					t.Setenv("OPENCODE_CONFIG", "")
					t.Setenv("OPENCODE_CONFIG_DIR", "")
					var desired []resolver.ResolvedArtifact
					for _, resource := range []ir.Resource{
						ir.NewSkill("probe-skill", "Native skill", "Native skill body\n", nil, nil, ir.SourceRef{}),
						ir.NewCommand("probe-command", "Native command", "Check $ARGUMENTS\n", nil, ir.SourceRef{}),
						ir.NewSubagent("probe-reviewer", "Native reviewer", "Review\n", "", nil, "", nil, "", nil, ir.SourceRef{}),
						ir.NewMcpServer("probe-server", "/bin/true", nil, map[string]string{"REFERENCE": "${PROBE_NATIVE_REF}"}, nil, ir.SourceRef{}),
					} {
						desired = append(desired, resolver.ResolvedArtifact{Entry: catalog.Entry{Name: resource.Name(), Resource: resource}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetOpenCode}})
					}
					modes := config.MaterializationModes{Project: mode, Machine: mode}
					if report, err := materialize.Run(t.Context(), workspace, desired, materialize.WithMaterializationModes(modes)); err != nil || diag.HasErrors(report.Diags) {
						t.Fatalf("native install: %v %v", err, report.Diags)
					}
				})
				for _, value := range inventory.values {
					if !strings.Contains(result.output, value) {
						t.Fatalf("native %s inventory did not discover %s (raw output withheld)", inventory.name, value)
					}
				}
			})
		}
	}
}

// spec: opencode-hooks/native-permissions-remain-authoritative-with-other-plugins
func TestNativePermissionsAndPlugins(t *testing.T) {
	binary := requireNative(t)
	for _, mode := range []string{"coexist", "permission-deny"} {
		t.Run(mode, func(t *testing.T) {
			h := ir.NewHookWithMatcher("check", "PreToolUse", "Bash", `cat > hook-input.json; printf '{"decision":"block"}'`, "command", nil, ir.SourceRef{})
			a, _, err := (compiler.Compiler{}).Translate(h)
			if err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{"plugins/handwritten.js": []byte(`import { writeFileSync } from "node:fs"; import { join } from "node:path"; export const Handwritten = async ({directory}) => { writeFileSync(join(directory, "handwritten-active"), "active"); return {}; };`)}
			for _, artifact := range a {
				files[artifact.Path] = artifact.Bytes
			}
			result := runNativeArtifacts(t, binary, mode, files)
			if result.effect || !result.handwritten || result.called != (mode == "coexist") {
				t.Fatal("native permissions or handwritten plugin coexistence failed")
			}
		})
	}
}

// spec: opencode-hooks/runner-budgets-refuse-excess-without-truncation
func TestNativeInvocationBudget(t *testing.T) {
	binary := requireNative(t)
	for _, count := range []int{63, 64, 65} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			files := map[string][]byte{}
			for i := range count {
				h := ir.NewHookWithMatcher(fmt.Sprintf("check-%03d", i), "PreToolUse", "Bash", fmt.Sprintf(`cat > hook-input.json; printf '%%s\n' '%03d' >> hook-runs.txt; printf '{}'`, i), "command", nil, ir.SourceRef{})
				a, _, err := (compiler.Compiler{}).Translate(h)
				if err != nil {
					t.Fatal(err)
				}
				for _, artifact := range a {
					files[artifact.Path] = artifact.Bytes
				}
			}
			result := runNativeArtifacts(t, binary, "budget", files)
			var expected strings.Builder
			if count <= 64 {
				for i := range count {
					fmt.Fprintf(&expected, "%03d\n", i)
				}
			}
			if result.runs != expected.String() {
				t.Fatal("native invocation budget truncated or reordered handlers")
			}
			if result.effect != (count <= 64) || result.called != (count <= 64) {
				t.Fatal("native invocation budget changed the protected effect")
			}
		})
	}
}
