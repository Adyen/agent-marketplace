package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

var compatibilityTargets = []string{"claude", "factory", "cursor", "windsurf", "gemini", "devin", "codex"}
var compatibilityResources = []string{"skill", "command", "subagent", "hook", "mcp", "collection", "rule", "native-cli"}

func TestHarnessCompatibility(t *testing.T) {
	target := os.Getenv("HARNESS_COMPAT_TARGET")
	if target == "" {
		t.Skip("opt in with HARNESS_COMPAT_TARGET; vendor CLIs are never required by normal E2E runs")
	}
	targets := []string{target}
	if target == "all" {
		targets = compatibilityTargets
	} else if !slices.Contains(compatibilityTargets, target) {
		t.Fatalf("unknown HARNESS_COMPAT_TARGET %q", target)
	}
	mode := os.Getenv("HARNESS_COMPAT_NATIVE_MODE")
	if mode == "" {
		mode = "probe"
		if target == "all" {
			mode = "unavailable"
		}
	}
	if !slices.Contains([]string{"probe", "unavailable", "blocked"}, mode) || target == "all" && mode == "probe" {
		t.Fatal("native mode must be probe, unavailable, or blocked; all is config-only")
	}
	reason := os.Getenv("HARNESS_COMPAT_NATIVE_REASON")
	if target == "all" && reason == "" {
		reason = "explicit all-target configuration verification; no vendor executable invoked"
	}
	if reason == "" && mode == "unavailable" {
		reason = "explicitly declared missing credential-free inspection capability"
	}
	if reason == "" && mode == "blocked" {
		reason = "native setup/install blocker; no additional reason supplied"
	}
	report := compatibilityReport{Target: target, NativeMode: mode, ExpectedVersion: os.Getenv("HARNESS_COMPAT_NATIVE_VERSION")}
	for _, harness := range targets {
		for _, scope := range []string{"repo", "user"} {
			for _, materialization := range []string{"symlink", "file"} {
				for _, resource := range compatibilityResources {
					report.Results = append(report.Results, compatibilityResult{Harness: harness, Resource: resource, Scope: scope, Mode: materialization, Status: "failed", Reason: "not reached; preceding setup or assertion failed"})
				}
			}
		}
	}
	reportDir := os.Getenv("HARNESS_COMPAT_REPORT_DIR")
	if reportDir == "" {
		reportDir = t.TempDir()
	}
	if err := os.MkdirAll(reportDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if mode == "unavailable" {
			report.Results = slices.DeleteFunc(report.Results, func(result compatibilityResult) bool { return result.Resource == "native-cli" })
		}
		path := filepath.Join(reportDir, "compatibility-"+target+".json")
		if err := writeCompatibilityReport(path, report); err != nil {
			t.Error(err)
		}
		t.Logf("compatibility evidence: %s", path)
	})
	server := buildCompatibilityHelper(t, "compat-mcp")
	rules := buildCompatibilityHelper(t, "compat-rules")
	for offset := 0; offset < len(report.Results); offset += len(compatibilityResources) {
		results := report.Results[offset : offset+len(compatibilityResources)]
		first := results[0]
		t.Run(first.Harness+"/"+first.Scope+"/"+first.Mode, func(t *testing.T) {
			runCompatibilityScenario(t, results, mode, reason, report.ExpectedVersion, server, rules)
		})
	}
	if report.failed() {
		t.Error("compatibility has failed assertions or blocked native setup; see structured report")
	}
}

func runCompatibilityScenario(t *testing.T, results []compatibilityResult, nativeMode, reason, expectedVersion, server, rules string) {
	e := newEnv(t)
	first := results[0]
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	init := runCompatibilityCommand(e, git, []string{"init", "-q"}, 10*time.Second)
	if init.Error != "" {
		t.Fatal(init.Error)
	}
	f := newCompatibilityFixture(e, first.Harness, server)
	paths := compatibilityPaths(e, first.Harness, first.Scope, f)
	otherScope := "user"
	if first.Scope == "user" {
		otherScope = "repo"
	}
	otherPaths := compatibilityPaths(e, first.Harness, otherScope, f)
	for _, entries := range paths {
		for _, path := range entries {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("fixture destination not absent before materialization: %s: %v", path, err)
			}
		}
	}
	for _, destination := range []string{e.home, e.workspace} {
		rel, err := filepath.Rel(f.registry, destination)
		if err != nil || rel == "." || !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatal("registry is inside native discovery ancestry")
		}
	}
	rows := make(map[string]*compatibilityResult)
	for i := range results {
		rows[results[i].Resource] = &results[i]
	}
	native := prepareCompatibilityNative(e, f, rows["native-cli"], nativeMode, reason, expectedVersion)
	for i := range results {
		results[i].Version = rows["native-cli"].Version
	}
	config := fmt.Sprintf("harnesses: [%s]\nmaterialization:\n  mode: %s\n", first.Harness, first.Mode)
	e.WriteGlobalConfig(config)
	config += fmt.Sprintf("registries:\n  - url: %q\n    source_harness: claude\n", f.registry)
	if first.Scope == "user" {
		e.WriteGlobalConfig(config)
	} else {
		e.WriteRepoConfig(config)
	}
	catalog := map[string][]string{
		"skill": {f.name("skill")}, "command": {f.name("command")}, "subagent": {f.name("subagent")},
		"hook": {"PreToolUse-command", "UserPromptSubmit-command"}, "mcp": {f.name("mcp-a"), f.name("mcp-b")}, "collection": {f.name("collection")},
	}
	catalogPassed := make(map[string]bool)
	for _, kind := range compatibilityResources[:6] {
		row := rows[kind]
		catalogPassed[kind] = t.Run("catalog-"+kind, func(t *testing.T) {
			command := runCompatibilityCommand(e, e.binary, []string{"list", "--kind", kind}, 30*time.Second)
			err := requireCompatibilityObservation(&command, false, catalog[kind]...)
			row.Commands = append(row.Commands, command)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	install := runCompatibilityCommand(e, e.binary, []string{"install", f.name("collection"), "--kind", "collection", "--scope", first.Scope}, 30*time.Second)
	rows["collection"].Commands = append(rows["collection"].Commands, install)
	if install.Error != "" {
		t.Fatalf("Marketplace collection install failed: %s\n%s", install.Error, install.output)
	}
	sync := runCompatibilityCommand(e, e.binary, []string{"sync"}, 30*time.Second)
	if sync.Error != "" {
		t.Fatalf("Marketplace sync failed: %s\n%s", sync.Error, sync.output)
	}
	for _, kind := range compatibilityResources[:6] {
		row := rows[kind]
		row.Commands = append(row.Commands, sync)
		ok := t.Run("config-"+kind, func(t *testing.T) {
			assertCompatibilityResource(t, e, f, row, paths[kind], sync)
			for _, path := range otherPaths[kind] {
				if _, err := os.Lstat(path); os.IsNotExist(err) {
					continue
				}
				if kind == "mcp" || kind == "hook" {
					data, err := os.ReadFile(path)
					if err == nil && !strings.Contains(string(data), f.prefix) {
						continue
					}
				}
				t.Errorf("materialization leaked into the opposite scope: %s", path)
			}
		})
		if !ok || !catalogPassed[kind] {
			row.Status = "failed"
			row.Reason = "catalog or generated-output assertions failed; see failing subtest"
			continue
		}
		if row.Status != "unsupported-as-expected" {
			row.Status = "config-only-verified"
		}
		if nativeMode != "probe" {
			row.Reason += "; native: " + reason
		}
	}
	row := rows["rule"]
	if !t.Run("config-rule", func(t *testing.T) { assertCompatibilityRules(t, e, f, rules, row) }) {
		row.Status = "failed"
		row.Reason = "rule compiler or convert assertions failed"
	} else if row.Status != "unsupported-as-expected" {
		row.Status = "config-only-verified"
	}
	check := runCompatibilityCommand(e, e.binary, []string{"sync", "--check"}, 30*time.Second)
	rows["collection"].Commands = append(rows["collection"].Commands, check)
	if check.Error != "" {
		rows["collection"].Status = "failed"
		t.Errorf("post-install sync --check failed: %s\n%s", check.Error, check.output)
	}
	if native != nil {
		native.verify(t, e, f, rows, paths)
		check := runCompatibilityCommand(e, e.binary, []string{"sync", "--check"}, 30*time.Second)
		rows["collection"].Commands = append(rows["collection"].Commands, check)
		if check.Error != "" {
			rows["collection"].Status = "failed"
			rows["collection"].Reason = "native inspection or fixture-only trust introduced materialization drift"
			t.Errorf("post-native sync --check failed: %s", check.Error)
		}
	}
	if _, err := os.Lstat(f.hookMarker); !os.IsNotExist(err) {
		rows["hook"].Status = "failed"
		t.Error("inspection unexpectedly executed a hook")
	}
}

func assertCompatibilityResource(t *testing.T, e *Env, f compatibilityFixture, row *compatibilityResult, paths []string, sync compatibilityCommand) {
	t.Helper()
	row.Reason = "Marketplace catalog, collection expansion, native path and generated fixture values asserted; no session or model execution"
	switch row.Resource {
	case "skill":
		path := paths[0]
		captureCompatibilityFile(t, e, row, path, f.name("skill"), f.description("skill"), "assets/reference.txt")
		captureCompatibilityFile(t, e, row, filepath.Join(filepath.Dir(path), "assets", "reference.txt"), f.name("support"))
		captureCompatibilityFile(t, e, row, filepath.Join(filepath.Dir(path), "scripts", "check.sh"), "compatibility-support")
		assertCompatibilityShape(t, path, row.Mode, true)
	case "command", "subagent":
		if row.Harness == "windsurf" && row.Resource == "subagent" {
			if !strings.Contains(sync.output, "subagent.windsurf.no-primitive") {
				t.Fatal("missing stable unsupported subagent diagnostic")
			}
			base := e.workspace
			if row.Scope == "user" {
				base = e.home
			}
			assertCompatibilityNoNamedFile(t, base, f.name("subagent"))
			row.Status = "unsupported-as-expected"
			row.Reason = "subagent.windsurf.no-primitive and no subagent output asserted"
			return
		}
		path := paths[0]
		captureCompatibilityFile(t, e, row, path, f.name(row.Resource), f.description(row.Resource))
		directory := row.Resource == "command" && (row.Harness == "devin" || row.Harness == "codex")
		if directory {
			captureCompatibilityFile(t, e, row, path, "x-agent-marketplace-kind: command")
		}
		mode := row.Mode
		if row.Harness == "factory" && row.Resource == "command" {
			mode = "file"
		}
		assertCompatibilityShape(t, path, mode, directory)
	case "hook":
		var output strings.Builder
		for _, path := range paths {
			output.WriteString(captureCompatibilityFile(t, e, row, path))
		}
		for _, name := range []string{"hook-a", "hook-b"} {
			quoted, _ := json.Marshal(f.hookCommand(name))
			if strings.Count(output.String(), string(quoted)) != 1 {
				t.Errorf("hook aggregate did not preserve exactly one %s command", name)
			}
		}
		for _, event := range compatibilityHookEvents(row.Harness) {
			if !strings.Contains(output.String(), `"`+event+`"`) {
				t.Errorf("missing mapped hook event %s", event)
			}
		}
		row.Reason = "both harmless sandbox-local hook commands and mapped events asserted in generated output; hooks not fired or natively inspected"
	case "mcp":
		text := captureCompatibilityFile(t, e, row, paths[0], f.name("mcp-a"), f.name("mcp-b"), f.server)
		assertCompatibilityMCP(t, text, row.Harness, f)
	case "collection":
		command := runCompatibilityCommand(e, e.binary, []string{"status"}, 30*time.Second)
		err := requireCompatibilityObservation(&command, false, "collection:"+f.name("collection"), f.name("skill"), f.name("command"), f.name("mcp-a"), f.name("mcp-b"))
		row.Commands = append(row.Commands, command)
		if err != nil {
			t.Fatal(err)
		}
		row.Reason = "Marketplace install --kind collection and status prove multi-kind expansion; collections are not a native harness primitive"
	}
}

func compatibilityHookEvents(target string) []string {
	switch target {
	case "gemini":
		return []string{"BeforeTool", "BeforeAgent"}
	case "windsurf":
		return []string{"pre_run_command", "pre_user_prompt"}
	default:
		return []string{"PreToolUse", "UserPromptSubmit"}
	}
}

func assertCompatibilityMCP(t *testing.T, text, target string, f compatibilityFixture) {
	t.Helper()
	for i, suffix := range []string{"mcp-a", "mcp-b"} {
		name := f.name(suffix)
		setting := []string{"alpha", "beta"}[i]
		if target == "codex" {
			pattern := `(?ms)^\[mcp_servers\."?` + regexp.QuoteMeta(name) + `"?\]\n(.*?)(?:^\[mcp_servers\.[^.\n]+\]\n|\z)`
			match := regexp.MustCompile(pattern).FindStringSubmatch(text)
			if len(match) != 2 {
				t.Fatalf("missing distinct TOML server table %s", name)
			}
			for _, field := range []string{"command = '" + f.server + "'", "args = ['--label=" + name + "']", "AMCOMPAT_SETTING = '" + setting + "'"} {
				if !strings.Contains(match[1], field) {
					t.Errorf("TOML %s missing %q", name, field)
				}
			}
			continue
		}
		var aggregate struct {
			Servers map[string]struct {
				Command string            `json:"command"`
				Args    []string          `json:"args"`
				Env     map[string]string `json:"env"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(text), &aggregate); err != nil {
			t.Fatal(err)
		}
		entry, found := aggregate.Servers[name]
		if !found || entry.Command != f.server || !slices.Equal(entry.Args, []string{"--label=" + name}) || entry.Env["AMCOMPAT_SETTING"] != setting || len(aggregate.Servers) != 2 {
			t.Errorf("wrong independently merged MCP entry %s: %+v", name, entry)
		}
	}
}

func assertCompatibilityNoNamedFile(t *testing.T, root, name string) {
	t.Helper()
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".cache" || d.Name() == ".local") {
			return filepath.SkipDir
		}
		if strings.Contains(d.Name(), name) {
			t.Errorf("unsupported resource emitted at %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
