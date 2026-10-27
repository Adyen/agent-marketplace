package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCompatibilityFixtureNamesAreIndependentOfInstallationMode(t *testing.T) {
	server := filepath.Join(t.TempDir(), "local-fixture-server")
	linked := newCompatibilityFixture(newEnv(t), "factory", server)
	copied := newCompatibilityFixture(newEnv(t), "factory", server)
	for _, kind := range []string{"skill", "command", "subagent", "mcp-a", "mcp-b"} {
		want := "amcompat-factory-" + kind
		if linked.name(kind) != want || copied.name(kind) != want {
			t.Fatalf("fixture identity must not confound mode with name length: %q, %q", linked.name(kind), copied.name(kind))
		}
	}
}

func TestCompatibilityTrustBlockerIsResourceAndScopeSpecific(t *testing.T) {
	row := compatibilityResult{Harness: "gemini", Resource: "skill", Scope: "repo"}
	command := compatibilityCommand{ExitCode: 0, Diagnostics: []string{"cli.project-trust-required"}}
	if !compatibilityTrustBlocksDiscovery(row, command) {
		t.Fatal("explicit workspace trust blocker was not classified")
	}
	for _, candidate := range []compatibilityResult{
		{Harness: "gemini", Resource: "skill", Scope: "user"},
		{Harness: "gemini", Resource: "mcp", Scope: "repo"},
		{Harness: "codex", Resource: "mcp", Scope: "repo"},
	} {
		if compatibilityTrustBlocksDiscovery(candidate, command) {
			t.Fatalf("unrelated discovery failure misclassified as trust: %+v", candidate)
		}
	}
	if compatibilityTrustBlocksDiscovery(row, compatibilityCommand{ExitCode: 0}) || compatibilityTrustBlocksDiscovery(row, compatibilityCommand{ExitCode: 1, Diagnostics: command.Diagnostics}) {
		t.Fatal("trust classification requires a completed list and explicit native evidence")
	}
	command.Diagnostics = append(command.Diagnostics, "cli.config-parse-error")
	if compatibilityTrustBlocksDiscovery(row, command) {
		t.Fatal("trust warning hid a configuration parsing error")
	}
}

func TestCompatibilityRunnerCreatesExplicitCodexHome(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "internal-fake-native")
	e.WriteFile(binary, "#!/bin/sh\ntest -d \"$CODEX_HOME\"\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.home, ".codex")); !os.IsNotExist(err) {
		t.Fatal("negative control requires a fresh sandbox without CODEX_HOME")
	}
	command := runCompatibilityCommand(e, binary, nil, time.Second)
	if command.Error != "" || command.ExitCode != 0 {
		t.Fatalf("explicit sandbox CODEX_HOME must exist before native startup: %+v", command)
	}
	if _, err := os.Stat(filepath.Join(e.home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatal("startup created native configuration instead of only the empty home directory")
	}
}

func TestCompatibilityClaudeDetailRequiresOnlyExposedFields(t *testing.T) {
	e := newEnv(t)
	f := compatibilityFixture{prefix: "amcompat-claude", server: filepath.Join(e.repos, "fixture-server")}
	binary := filepath.Join(e.repos, "internal-fake-native")
	e.WriteFile(binary, "#!/bin/sh\ncase \"$2\" in list) printf '%s\\n' '"+f.name("mcp-a")+": Connected' '"+f.name("mcp-b")+": Connected';; get) printf '%s\\n' \"$3\";; esac\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	row := &compatibilityResult{Harness: "claude", Resource: "mcp", Scope: "user", Mode: "file", Status: "config-only-verified"}
	native := compatibilityNative{binary: binary, probes: []compatibilityNativeProbe{{kind: "mcp", list: []string{"mcp", "list"}, names: []string{f.name("mcp-a"), f.name("mcp-b")}}}}
	native.verify(t, e, f, map[string]*compatibilityResult{"mcp": row}, nil)
	if row.Status != "native-verified" || row.Connection == nil || row.Connection.Status != "native-verified" || len(row.Commands) != 4 {
		t.Fatalf("observed identities and both connections must remain required: %+v", row)
	}
	for _, command := range row.Commands {
		for _, value := range command.Observed {
			if value == f.server {
				t.Fatal("unexposed command path was claimed as native evidence")
			}
		}
	}
}

func TestCompatibilityNativeProbeFailuresStayResourceLocal(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "internal-fake-native")
	e.WriteFile(binary, "#!/bin/sh\ncase \"$1\" in --version) printf '1.2.3\\n';; skills) exit 0;; mcp) printf 'Login required\\n'; exit 1;; esac\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HARNESS_COMPAT_NATIVE_BINARY", binary)
	row := compatibilityResult{Harness: "gemini", Resource: "native-cli"}
	native := prepareCompatibilityNative(e, compatibilityFixture{prefix: "amcompat-internal"}, &row, "probe", "", "1.2.3")
	if native == nil || len(native.probes) != 2 {
		t.Fatal("failed MCP inspection suppressed the independent skill probe")
	}
	if native.probes[0].before.Error != "" || native.probes[1].before.Error == "" {
		t.Fatal("per-resource negative-control evidence was not retained")
	}
}

func TestCompatibilityTruncationIsDiagnosedWithoutClaimingDiscovery(t *testing.T) {
	const name = "amcompat-factory-repo-symlink-mcp-a"
	for _, suffix := range []string{"…", "..."} {
		command := compatibilityCommand{ExitCode: 0, output: name[:len(name)-2] + suffix}
		if err := requireCompatibilityObservation(&command, false, name); err == nil || !strings.Contains(err.Error(), "truncated") {
			t.Fatal("truncated fixture display was accepted or not diagnosed")
		}
		if len(command.Observed) != 0 || len(command.Diagnostics) != 1 || command.Diagnostics[0] != "cli.truncated-fixture-value" {
			t.Fatal("truncation lost its evidence boundary")
		}
	}
	command := compatibilityCommand{ExitCode: 0, output: name}
	if err := requireCompatibilityObservation(&command, false, name); err != nil || len(command.Diagnostics) != 0 {
		t.Fatal("complete fixture identifier was not accepted")
	}
}

func TestCompatibilitySuccessfulExitIsNotAuthenticationEvidence(t *testing.T) {
	command := compatibilityCommand{ExitCode: 0, output: "Login required"}
	command.Diagnostics = compatibilityDiagnostics(command.output)
	if err := requireCompatibilityObservation(&command, true, "amcompat-skill"); err == nil {
		t.Fatal("login prompt accepted as an empty resource list")
	}
	command.output = "No skills found"
	command.Diagnostics = compatibilityDiagnostics(command.output)
	if err := requireCompatibilityObservation(&command, true, "amcompat-skill"); err != nil {
		t.Fatal("real empty-list control was rejected")
	}
}

func TestCompatibilitySafeDiagnostics(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "internal-fake-native")
	e.WriteFile(binary, "#!/bin/sh\ncat <<'END'\nUnknown command mcp\nLogin required\nProject is not trusted\nhttps://private-user:private-password@auth.invalid/login?token=private-query#private-fragment\nAuthorization: Bearer private-bearer\n{\"api_key\":\"private-key\",\"access_token\":\"private-token\"}\nEND\nexit 1\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	command := runCompatibilityCommand(e, binary, []string{"mcp", "list"}, time.Second)
	if !strings.Contains(command.output, "private-token") {
		t.Fatal("positive control did not print the secret fixture")
	}
	data, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-", "auth.invalid", "Authorization", "api_key", "access_token"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("unsafe native output reached report: %s", forbidden)
		}
	}
	for _, expected := range []string{"cli.unknown-command", "cli.authentication-required", "cli.project-trust-required"} {
		if !strings.Contains(string(data), expected) {
			t.Errorf("safe diagnostic missing %s: %s", expected, data)
		}
	}
	if command.OutputBytes == 0 || command.Error == "" {
		t.Fatal("native failure lost bounded evidence")
	}
	var output compatibilityOutput
	input := strings.Repeat("x", compatibilityOutputLimit+100)
	if n, err := output.Write([]byte(input)); err != nil || n != len(input) || len(output.data) != compatibilityOutputLimit || output.total != len(input) {
		t.Fatal("native output capture is not bounded")
	}
	e.WriteFile(filepath.Join(e.home, "large-output"), input+"amcompat-hidden-at-tail")
	e.WriteFile(binary, "#!/bin/sh\ncat \"$HOME/large-output\"\n")
	command = runCompatibilityCommand(e, binary, nil, time.Second)
	if !command.OutputTruncated || command.Error == "" || requireCompatibilityObservation(&command, true, "amcompat-hidden-at-tail") == nil {
		t.Fatal("truncated native output was accepted as proof of absence")
	}
}

func TestCompatibilityRuntimeUpdatesDisabled(t *testing.T) {
	e := newEnv(t)
	t.Setenv("DISABLE_AUTOUPDATER", "0")
	t.Setenv("FACTORY_DROID_AUTO_UPDATE_ENABLED", "true")
	environment := strings.Join(compatibilityEnv(e), "\n")
	for _, expected := range []string{
		"DISABLE_AUTOUPDATER=1",
		"FACTORY_DROID_AUTO_UPDATE_ENABLED=false",
		"HTTP_PROXY=http://127.0.0.1:9",
		"HTTPS_PROXY=http://127.0.0.1:9",
		"ALL_PROXY=http://127.0.0.1:9",
		"http_proxy=http://127.0.0.1:9",
		"https_proxy=http://127.0.0.1:9",
		"all_proxy=http://127.0.0.1:9",
	} {
		if !strings.Contains(environment, expected) {
			t.Errorf("runtime control missing: %s", expected)
		}
	}
	path := filepath.Join(e.home, ".config", "devin", "config.json")
	if err := configureCompatibilityRuntime(e, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("non-Devin setup touched Devin config")
	}
	e.WriteFile(path, `{"theme":"light","hooks":{"PreToolUse":[{"command":"sandbox-check"}]}}`)
	if err := configureCompatibilityRuntime(e, "devin"); err != nil {
		t.Fatal(err)
	}
	var document struct {
		AutoUpdate *bool `json:"auto_update"`
		Theme      string
		Hooks      map[string][]map[string]string
	}
	if err := json.Unmarshal([]byte(e.ReadFile(path)), &document); err != nil {
		t.Fatal(err)
	}
	if document.AutoUpdate == nil || *document.AutoUpdate || document.Theme != "light" || document.Hooks["PreToolUse"][0]["command"] != "sandbox-check" {
		t.Fatalf("runtime control erased hook aggregate: %+v", document)
	}
	before := e.ReadFile(path)
	if err := configureCompatibilityRuntime(e, "devin"); err != nil || e.ReadFile(path) != before {
		t.Fatal("runtime setup is not idempotent")
	}
	e.WriteFile(path, "malformed sentinel")
	if err := configureCompatibilityRuntime(e, "devin"); err == nil || e.ReadFile(path) != "malformed sentinel" {
		t.Fatal("malformed user config was overwritten")
	}
}

func TestCompatibilityDevinUpdateControlSurvivesMarketplaceHooks(t *testing.T) {
	e := newEnv(t)
	e.InitGitRepo()
	f := newCompatibilityFixture(e, "devin", buildCompatibilityHelper(t, "compat-mcp"))
	if err := configureCompatibilityRuntime(e, "devin"); err != nil {
		t.Fatal(err)
	}
	e.WriteGlobalConfig("harnesses: [devin]\nmaterialization:\n  mode: file\nsources: {fixture: {directory: '" + f.registry + "', source_harness: claude}}\n")
	command := runCompatibilityCommand(e, e.binary, []string{"install", f.name("collection"), "--kind", "collection", "--scope", "user"}, 30*time.Second)
	if command.Error != "" {
		t.Fatalf("Marketplace fixture install: %s", command.Error)
	}
	text := e.ReadFile(filepath.Join(e.home, ".config", "devin", "config.json"))
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &document); err != nil {
		t.Fatal(err)
	}
	if string(document["auto_update"]) != "false" || len(document["hooks"]) == 0 {
		t.Fatal("Marketplace hooks erased runtime auto-update control")
	}
	for _, value := range []string{f.name("hook-a"), f.name("hook-b"), "PreToolUse", "UserPromptSubmit"} {
		if !strings.Contains(text, value) {
			t.Errorf("user hook aggregate missing %s", value)
		}
	}
}

func TestCompatibilityEvidenceRequiresNegativeControl(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "internal-fake-native")
	e.WriteFile(binary, "#!/bin/sh\nif [ -f \"$HOME/fixture-installed\" ]; then printf '%s\\n' 'amcompat-skill observable-value'; fi\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	before := runCompatibilityCommand(e, binary, []string{"skills", "list"}, time.Second)
	if err := requireCompatibilityObservation(&before, true, "amcompat-skill"); err != nil {
		t.Fatal(err)
	}
	if err := requireCompatibilityObservation(&before, false, "amcompat-skill"); err == nil {
		t.Fatal("generic process success passed without a discovered fixture")
	}
	e.WriteFile(filepath.Join(e.home, "fixture-installed"), "present")
	after := runCompatibilityCommand(e, binary, []string{"skills", "list"}, time.Second)
	if err := requireCompatibilityObservation(&after, false, "amcompat-skill", "observable-value"); err != nil {
		t.Fatal(err)
	}
	if err := requireCompatibilityObservation(&after, true, "amcompat-skill"); err == nil {
		t.Fatal("contaminated negative control passed")
	}
	if err := requireCompatibilityObservation(&after, false, "wrong-value"); err == nil {
		t.Fatal("wrong observable value passed")
	}
	for _, tc := range []struct {
		name     string
		body     string
		timedOut bool
	}{
		{"login-required", "#!/bin/sh\nprintf 'Login required\\n'\nexit 1\n", false},
		{"timeout", "#!/bin/sh\nsleep 10\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.WriteFile(binary, tc.body)
			r := runCompatibilityCommand(e, binary, nil, 100*time.Millisecond)
			if r.Error == "" || r.ExitCode == 0 || r.TimedOut != tc.timedOut {
				t.Fatalf("native failure was hidden: %+v", r)
			}
			if err := requireCompatibilityObservation(&r, true, "amcompat-skill"); err == nil {
				t.Fatal("failed inspection accepted as a negative control")
			}
		})
	}
	r := runCompatibilityCommand(e, filepath.Join(e.repos, "missing"), nil, time.Second)
	if r.Error == "" || r.ExitCode == 0 {
		t.Fatal("missing configured executable did not fail")
	}
}

func TestClaudePluginListCapturesSeparateOutputInReport(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "claude-plugin-list")
	e.WriteFile(binary, "#!/bin/sh\nprintf '%s\\n' '{\"plugins\":[{\"name\":\"native-kit@am-test\",\"enabled\":true}]}'\nprintf '%s\\n' 'plugin-list-stderr' >&2\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	commands, found, enabled, err := claudePluginList(e, binary, "native-kit@", time.Second)
	if err != nil || !found || !enabled || len(commands) != 1 {
		t.Fatalf("Claude plugin list result = commands %d found %t enabled %t err %v", len(commands), found, enabled, err)
	}
	if !strings.Contains(commands[0].Stdout, "native-kit@am-test") || commands[0].Stderr != "plugin-list-stderr\n" {
		t.Fatalf("Claude plugin-list report lost its separate streams: stdout=%q stderr=%q", commands[0].Stdout, commands[0].Stderr)
	}
	data, err := json.Marshal(commands[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{`"stdout":`, `"stderr":`, "native-kit@am-test", "plugin-list-stderr"} {
		if !strings.Contains(string(data), output) {
			t.Errorf("Claude plugin-list report omitted %q: %s", output, data)
		}
	}
}

func TestCompatibilityReportKeepsEvidenceBoundaries(t *testing.T) {
	report := compatibilityReport{Target: "internal-fake", Results: []compatibilityResult{
		{Harness: "devin", Resource: "skill", Scope: "repo", Mode: "file", Status: "native-verified", Version: "internal-fake-1", Reason: "name and path only", Commands: []compatibilityCommand{{Args: []string{"internal-fake", "skills", "list"}, Observed: []string{"amcompat-skill"}, output: "raw-output-not-for-report"}}},
		{Harness: "devin", Resource: "hook", Scope: "repo", Mode: "file", Status: "config-only-verified", Reason: "hooks were not fired"},
		{Harness: "windsurf", Resource: "native-cli", Scope: "repo", Mode: "file", Status: "blocked", Reason: "installer failed"},
	}}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := writeCompatibilityReport(path, report); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "raw-output-not-for-report") {
		t.Fatal("report copied unreviewed native output")
	}
	var decoded compatibilityReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Results) != 3 || decoded.Results[0].Version != "internal-fake-1" || decoded.Results[1].Status != "config-only-verified" || !decoded.failed() {
		t.Fatalf("report erased a gap or failure: %+v", decoded)
	}
}

func TestCompatibilityRecognitionSurvivesPendingTrust(t *testing.T) {
	e := newEnv(t)
	f := compatibilityFixture{prefix: "amcompat-internal"}
	binary := filepath.Join(e.repos, "internal-fake-native")
	e.WriteFile(binary, "#!/bin/sh\nif [ -f \"$HOME/installed\" ]; then printf '%s\\n' 'amcompat-internal-mcp-a: pending project trust' 'amcompat-internal-mcp-b: pending project trust'; else printf 'No MCP servers configured\\n'; fi\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	names := []string{f.name("mcp-a"), f.name("mcp-b")}
	before := runCompatibilityCommand(e, binary, []string{"mcp", "list"}, time.Second)
	if err := requireCompatibilityObservation(&before, true, names...); err != nil {
		t.Fatal(err)
	}
	e.WriteFile(filepath.Join(e.home, "installed"), "present")
	row := &compatibilityResult{Harness: "gemini", Resource: "mcp", Scope: "repo", Mode: "file", Version: "0.0.1", Status: "config-only-verified"}
	native := compatibilityNative{binary: binary, probes: []compatibilityNativeProbe{{kind: "mcp", list: []string{"mcp", "list"}, names: names, before: before}}}
	native.verify(t, e, f, map[string]*compatibilityResult{"mcp": row}, nil)
	if row.Status != "native-verified" || row.Connection == nil || row.Connection.Status != "blocked" {
		t.Fatalf("trust gate erased recognition evidence: %+v", row)
	}
	report := compatibilityReport{Results: []compatibilityResult{*row}}
	if !report.failed() {
		t.Fatal("blocked connection was silently green")
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded compatibilityReport
	if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.failed() || decoded.Results[0].Status != "native-verified" {
		t.Fatal("connection and recognition evidence did not survive reporting")
	}
	command := compatibilityCommand{output: "configured names and commands, no transport probe"}
	if result := compatibilityMCPConnection(&command, "codex"); result.Status != "not-probed" {
		t.Fatalf("invented a transport check: %+v", result)
	}
}

func TestCompatibilityInternalConnectionEvidence(t *testing.T) {
	for _, output := range []string{"first: Connected\nsecond: Failed", "first: Connected\nsecond: Pending", "first: Disconnected\nsecond: Connected", "Login required"} {
		command := compatibilityCommand{output: output}
		if assertCompatibilityConnection(&command, "gemini") == nil {
			t.Errorf("incomplete local-server connection evidence accepted: %q", output)
		}
	}
	command := compatibilityCommand{output: "first: Connected\nsecond: Connected"}
	if err := assertCompatibilityConnection(&command, "gemini"); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibilityInternalModeBoundaries(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "internal-fake-native")
	e.WriteFile(binary, "#!/bin/sh\nprintf 'called\\n' >> \"$HOME/called\"\nprintf '0.0.1\\n'\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HARNESS_COMPAT_NATIVE_BINARY", binary)
	for _, mode := range []string{"unavailable", "blocked"} {
		row := compatibilityResult{Harness: "devin", Resource: "native-cli"}
		if got := prepareCompatibilityNative(e, compatibilityFixture{}, &row, mode, "internal fixture limitation", ""); got != nil {
			t.Fatalf("%s attempted a probe", mode)
		}
		report := compatibilityReport{Results: []compatibilityResult{row}}
		if report.failed() != (mode == "blocked") || row.Reason == "" {
			t.Fatalf("%s lost its evidence boundary: %+v", mode, row)
		}
	}
	if _, err := os.Stat(filepath.Join(e.home, "called")); !os.IsNotExist(err) {
		t.Fatal("unavailable/blocked mode executed a binary")
	}
	row := compatibilityResult{Harness: "devin", Resource: "native-cli"}
	if prepareCompatibilityNative(e, compatibilityFixture{}, &row, "probe", "", "9.9.9") != nil || row.Status != "blocked" {
		t.Fatal("pinned version mismatch did not block")
	}
	if _, err := os.Stat(filepath.Join(e.home, "called")); err != nil {
		t.Fatal("positive control did not execute the internal fake binary")
	}
	for _, status := range []string{"native-verified", "config-only-verified", "unsupported-as-expected", "blocked", "failed"} {
		report := compatibilityReport{Results: []compatibilityResult{{Status: status}}}
		if report.failed() != (status == "blocked" || status == "failed") {
			t.Errorf("wrong suite failure decision for %s", status)
		}
	}
}

func TestCompatibilityLocalMCPFixture(t *testing.T) {
	e := newEnv(t)
	binary := buildCompatibilityHelper(t, "compat-mcp")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = e.workspace
	cmd.Env = compatibilityEnv(e)
	cmd.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call"}
`)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected three MCP replies, got %q", out)
	}
	for i, fields := range [][]string{{`"id":1`, `"protocolVersion":"2025-03-26"`, `"name":"amcompat-local-mcp"`}, {`"id":2`, `"name":"amcompat_echo"`}, {`"id":3`, `"code":-32601`}} {
		if !json.Valid([]byte(lines[i])) {
			t.Errorf("invalid MCP JSON reply: %s", lines[i])
		}
		for _, field := range fields {
			if !strings.Contains(lines[i], field) {
				t.Errorf("MCP reply %d missing %s", i, field)
			}
		}
	}
}

func TestCompatibilityNativeInventoryRunnerSharesExecutions(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "inventory-runner-fixture")
	e.WriteFile(binary, "#!/bin/sh\nprintf '%s %s\\n' \"$1\" \"$2\" >> \"$HOME/probe-invocations\"\nprintf '%s\\n' 'amcompat-skill-a' 'amcompat-skill-b'\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}

	runner := newCompatibilityInventoryRunner()
	probeA := compatibilityNativeProbe{kind: "skill", list: []string{"skills", "list"}}
	probeB := compatibilityNativeProbe{kind: "command", list: []string{"skills", "list"}}
	cmdA := runner.run(e, probeA, binary)
	cmdB := runner.run(e, probeB, binary)

	invocations := e.ReadFile(filepath.Join(e.home, "probe-invocations"))
	lines := strings.Split(strings.TrimSpace(invocations), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly one shared execution, got %d: %q", len(lines), invocations)
	}

	cmdA.Observed = append(cmdA.Observed, "mutation-a")
	cmdA.Diagnostics = append(cmdA.Diagnostics, "diag-a")
	cmdA.Args = append(cmdA.Args, "extra-a")
	if slices.Contains(cmdB.Observed, "mutation-a") || slices.Contains(cmdB.Diagnostics, "diag-a") || slices.Contains(cmdB.Args, "extra-a") {
		t.Fatal("runner returned shared mutable slices instead of independent clones")
	}

	mcp := compatibilityNativeProbe{kind: "mcp", list: []string{"mcp", "list"}}
	runner.run(e, mcp, binary)
	invocationsAfter := e.ReadFile(filepath.Join(e.home, "probe-invocations"))
	if strings.Count(invocationsAfter, "\n") != 2 {
		t.Fatalf("distinct mcp list key did not execute a second time: %q", invocationsAfter)
	}
}

func TestCompatibilityRunnerCredentialIsolation(t *testing.T) {
	e := newEnv(t)
	for _, key := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "DEVIN_API_KEY", "FACTORY_API_KEY", "CURSOR_API_KEY", "AWS_PROFILE", "GOOGLE_APPLICATION_CREDENTIALS", "NODE_OPTIONS", "BASH_ENV", "HTTP_PROXY", "COMPAT_UNKNOWN_SECRET"} {
		t.Setenv(key, "must-not-reach-native-process")
	}
	t.Setenv("XDG_CONFIG_DIRS", "/host/config")
	t.Setenv("XDG_DATA_DIRS", "/host/data")
	t.Setenv("CODEX_HOME", "/host/codex")
	binary := filepath.Join(e.repos, "internal-fake-native")
	e.WriteFile(binary, "#!/bin/sh\n/usr/bin/env\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	r := runCompatibilityCommand(e, binary, nil, time.Second)
	if r.Error != "" || r.ExitCode != 0 {
		t.Fatalf("fake environment inspection failed: %+v", r)
	}
	for _, forbidden := range []string{"must-not-reach-native-process", "/host/config", "/host/data", "/host/codex"} {
		if strings.Contains(r.output, forbidden) {
			t.Fatalf("inherited forbidden environment value %q", forbidden)
		}
	}
	for _, expected := range []string{"HOME=" + e.home, "CODEX_HOME=" + filepath.Join(e.home, ".codex"), "XDG_RUNTIME_DIR=" + filepath.Join(e.home, ".runtime"), "LC_ALL=C", "TMPDIR=" + filepath.Join(e.home, ".tmp")} {
		if !strings.Contains(r.output, expected+"\n") {
			t.Errorf("missing sandbox environment assignment %q", expected)
		}
	}
}
