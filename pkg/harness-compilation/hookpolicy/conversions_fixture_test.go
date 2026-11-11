package hookpolicy_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	harnesscompile "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

// spec: hook-resources/devin-postcompaction-keeps-its-spelling-within-devin
// spec: hook-resources/devin-compaction-events-ingest-without-a-diagnostic
// TestConversionFixtures runs the independently authored conversion fixtures
// through the public Compile path. Expected values are
// hand-authored fixture data, never derived from the shipped catalogs, and
// each fixture asserts the structured native artifact — the exact emitted
// event, matcher, command, and timeout for its target's shape — rather than
// arbitrary scalar membership.
func TestConversionFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/conversions.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures conversionFixtures
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fixtures); err != nil {
		t.Fatal(err)
	}
	for _, c := range fixtures.Matchers {
		t.Run("matchers/"+c.Name, func(t *testing.T) {
			runHookCase(t, c)
		})
	}
	for _, c := range fixtures.Events {
		t.Run("events/"+c.Name, func(t *testing.T) {
			runHookCase(t, c)
		})
	}
	for _, c := range fixtures.Env {
		t.Run("env/"+c.Name, func(t *testing.T) {
			runHookCase(t, c)
		})
	}
	for _, c := range fixtures.Timeouts {
		t.Run("timeouts/"+c.Name, func(t *testing.T) {
			runHookCase(t, c)
		})
	}
	for _, c := range fixtures.AllowLists {
		t.Run("allow_lists/"+c.Name, func(t *testing.T) {
			runAllowListCase(t, c)
		})
	}
}

type hookCase struct {
	Name        string `yaml:"name"`
	Source      string `yaml:"source"`
	Target      string `yaml:"target"`
	Event       string `yaml:"event"`
	Matcher     string `yaml:"matcher"`
	Command     string `yaml:"command"`
	Timeout     *int   `yaml:"timeout"`
	WantEvent   string `yaml:"want_event"`
	WantMatcher string `yaml:"want_matcher"`
	WantCommand string `yaml:"want_command"`
	WantTimeout *int   `yaml:"want_timeout"`
	WantWarning string `yaml:"want_warning"`
	WantError   string `yaml:"want_error"`
}

type allowListCase struct {
	Name        string   `yaml:"name"`
	Source      string   `yaml:"source"`
	Target      string   `yaml:"target"`
	Surface     string   `yaml:"surface"`
	Tools       []string `yaml:"tools"`
	WantTools   []string `yaml:"want_tools"`
	WantWarning string   `yaml:"want_warning"`
	WantError   string   `yaml:"want_error"`
}

type conversionFixtures struct {
	Matchers   []hookCase      `yaml:"matchers"`
	Events     []hookCase      `yaml:"events"`
	Env        []hookCase      `yaml:"env"`
	Timeouts   []hookCase      `yaml:"timeouts"`
	AllowLists []allowListCase `yaml:"allow_lists"`
}

func runHookCase(t *testing.T, c hookCase) {
	t.Helper()
	event, matcher := c.Event, c.Matcher
	if event == "" {
		// Event-agnostic fixtures (env rewriting) exercise PreToolUse/Bash, a
		// tool event every fixture target can represent.
		event, matcher = "PreToolUse", "Bash"
	}
	command := c.Command
	if command == "" {
		command = "echo fixture-default-cmd-61d9"
	}
	options := []ir.HookOption{ir.WithHookTimeoutPtr(c.Timeout)}
	if c.Source != "" {
		options = append(options, ir.WithHookSource(c.Source))
	}
	hook := ir.NewHookWithMatcherOptions("fixture", event, matcher, command, "command", nil, ir.SourceRef{}, options...)
	res, err := harnesscompile.Compile(hook, harnesscompile.Target(c.Target))
	if err != nil {
		t.Fatal(err)
	}
	if c.WantError != "" {
		if len(res.Artifacts) != 0 {
			t.Fatalf("a refused hook must emit zero artifacts, got %d", len(res.Artifacts))
		}
		if !hasFixtureErrorCode(res, c.WantError) {
			t.Fatalf("want error-severity %q, diagnostics = %+v", c.WantError, res.Diagnostics)
		}
		return
	}
	if diag.HasErrors(res.Diagnostics) {
		t.Fatalf("unexpected error diagnostics: %+v", res.Diagnostics)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("want exactly one artifact, got %d (diags: %+v)", len(res.Artifacts), res.Diagnostics)
	}
	got := decodeFixtureHook(t, c.Target, res.Artifacts[0].Bytes)
	if c.WantEvent != "" && got.event != c.WantEvent {
		t.Fatalf("event = %q, want %q", got.event, c.WantEvent)
	}
	// The injected default matcher is scaffolding for event-agnostic env
	// fixtures; matcher equality is asserted whenever the fixture itself
	// expresses one (including an explicit want of none).
	if c.Matcher != "" || c.WantMatcher != "" {
		if got.matcher != c.WantMatcher {
			t.Fatalf("matcher = %q, want %q", got.matcher, c.WantMatcher)
		}
	}
	if c.WantCommand != "" && got.command != c.WantCommand {
		t.Fatalf("command = %q, want %q", got.command, c.WantCommand)
	}
	if c.WantTimeout != nil {
		if got.timeout == nil || *got.timeout != *c.WantTimeout {
			t.Fatalf("timeout = %v, want %d", got.timeout, *c.WantTimeout)
		}
	}
	if c.WantWarning != "" && !hasFixtureWarningCode(res, c.WantWarning) {
		t.Fatalf("want warning-severity %q, diagnostics = %v", c.WantWarning, diagnosticCodes(res))
	}
}

func runAllowListCase(t *testing.T, c allowListCase) {
	t.Helper()
	options := []ir.SubagentOption{}
	if c.Source != "" {
		options = append(options, ir.WithSubagentToolSource(c.Source))
	}
	sub := ir.NewSubagentWithOptions("fixture", "desc", "body", "", c.Tools, "", nil, "", nil, ir.SourceRef{}, options...)
	res, err := harnesscompile.Compile(sub, harnesscompile.Target(c.Target))
	if err != nil {
		t.Fatal(err)
	}
	if c.WantError != "" {
		if len(res.Artifacts) != 0 {
			t.Fatalf("a refused subagent must emit zero artifacts, got %d", len(res.Artifacts))
		}
		if !hasFixtureErrorCode(res, c.WantError) {
			t.Fatalf("want error-severity %q, diagnostics = %+v", c.WantError, res.Diagnostics)
		}
		return
	}
	if diag.HasErrors(res.Diagnostics) {
		t.Fatalf("unexpected error diagnostics: %+v", res.Diagnostics)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("want exactly one artifact, got %d (diags: %+v)", len(res.Artifacts), res.Diagnostics)
	}
	got, present := frontmatterToolList(t, res.Artifacts[0].Bytes)
	if c.WantTools == nil {
		if present {
			t.Fatalf("tools must be omitted on warning; emitted %v", got)
		}
	} else {
		if !present {
			t.Fatalf("want tools %v, frontmatter carried no tool list", c.WantTools)
		}
		if strings.Join(got, ",") != strings.Join(c.WantTools, ",") {
			t.Fatalf("tools = %v, want exactly %v", got, c.WantTools)
		}
	}
	if c.WantWarning != "" && !hasFixtureWarningCode(res, c.WantWarning) {
		t.Fatalf("want warning-severity %q, diagnostics = %v", c.WantWarning, diagnosticCodes(res))
	}
}

// fixtureHook is one hook decoded from its target's native artifact shape.
// Where the native format groups handlers under a shared matcher (Factory,
// Devin, Gemini, Codex) the matcher is read from the group envelope; where
// the rule is flat (Claude, Cursor) it is read from the handler; Windsurf
// encodes the tool selection in the event name and emits no matcher member.
type fixtureHook struct {
	event   string
	matcher string
	command string
	timeout *int
}

// decodeFixtureHook parses the produced JSON artifact for target using the
// hand-authored shape for that target and fails on any structural surprise,
// so a changed envelope cannot silently satisfy a fixture.
func decodeFixtureHook(t *testing.T, target string, raw []byte) fixtureHook {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s artifact is not a JSON object: %v\n%s", target, err, raw)
	}
	singleKey := func(m map[string]any, context string) string {
		if len(m) != 1 {
			t.Fatalf("%s %s: want exactly one event key, got %v", target, context, m)
		}
		for k := range m {
			return k
		}
		return ""
	}
	singleElem := func(v any, context string) map[string]any {
		list, ok := v.([]any)
		if !ok || len(list) != 1 {
			t.Fatalf("%s %s: want exactly one element, got %v", target, context, v)
		}
		entry, ok := list[0].(map[string]any)
		if !ok {
			t.Fatalf("%s %s: handler entry is not an object: %v", target, context, list[0])
		}
		return entry
	}

	var event string
	var group, handler map[string]any
	switch target {
	case "claude", "cursor", "windsurf":
		hooks, ok := doc["hooks"].(map[string]any)
		if !ok {
			t.Fatalf("%s: want a top-level hooks object, got %v", target, doc)
		}
		event = singleKey(hooks, "hooks map")
		handler = singleElem(hooks[event], "event handlers")
		group = handler
	case "factory", "devin":
		event = singleKey(doc, "top-level event map")
		group = singleElem(doc[event], "event groups")
		handler = singleElem(group["hooks"], "group handlers")
	case "gemini", "codex":
		hooks, ok := doc["hooks"].(map[string]any)
		if !ok {
			t.Fatalf("%s: want a top-level hooks object, got %v", target, doc)
		}
		event = singleKey(hooks, "hooks map")
		group = singleElem(hooks[event], "event groups")
		handler = singleElem(group["hooks"], "group handlers")
	default:
		t.Fatalf("no hand-authored hook shape for target %s", target)
	}

	out := fixtureHook{event: event}
	if m, ok := group["matcher"].(string); ok {
		out.matcher = m
	}
	if v, ok := handler["command"].(string); ok {
		out.command = v
	}
	if v, ok := handler["timeout"].(float64); ok {
		n := int(v)
		out.timeout = &n
	}
	return out
}

// frontmatterToolList extracts the complete ordered tools or allowed-tools
// list from a markdown artifact's YAML frontmatter. It reports whether a tool
// list member was present at all so fixtures can assert omit-on-warning.
func frontmatterToolList(t *testing.T, raw []byte) ([]string, bool) {
	t.Helper()
	var fm struct {
		Tools        []string `yaml:"tools"`
		AllowedTools []string `yaml:"allowed-tools"`
	}
	body := string(raw)
	if !strings.HasPrefix(body, "---") {
		t.Fatalf("artifact has no frontmatter:\n%s", raw)
	}
	rest := body[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		t.Fatalf("artifact frontmatter is unterminated:\n%s", raw)
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		t.Fatalf("unmarshal frontmatter: %v\n%s", err, rest[:end])
	}
	if fm.AllowedTools != nil {
		return fm.AllowedTools, true
	}
	return fm.Tools, fm.Tools != nil
}

func diagnosticCodes(res *harnesscompile.Result) []string {
	var codes []string
	for _, d := range res.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}

// hasFixtureWarningCode requires the code at warning severity; an error or a
// mere code mention does not satisfy a warned-but-emitted fixture.
func hasFixtureWarningCode(res *harnesscompile.Result, code string) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code && d.Severity == diag.SeverityWarning {
			return true
		}
	}
	return false
}

// hasFixtureErrorCode requires the code at error severity; a warning with the
// same code does not satisfy a refused-hook fixture.
func hasFixtureErrorCode(res *harnesscompile.Result, code string) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code && d.IsError() {
			return true
		}
	}
	return false
}
