package hookpolicy_test

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"testing"

	harnesscompile "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

// TestConversionFixtures runs the independently authored conversion fixtures
// through the public Compile path (OpenSpec task 1.4). The translators do not
// consume the hook/tool catalogs until sections 2-3 land, so the suite is
// gated off by default; AM_HOOK_CONVERSION_FIXTURES=1 runs it and is expected
// to fail on the unchanged translators.
func TestConversionFixtures(t *testing.T) {
	if os.Getenv("AM_HOOK_CONVERSION_FIXTURES") == "" {
		t.Skip("conversion fixtures pending catalog-driven translators (OpenSpec tasks 1.4, 2.x, 3.x); set AM_HOOK_CONVERSION_FIXTURES=1 to run")
	}
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
	if c.Source != "" && c.Source != "claude" {
		t.Skipf("source %q requires HookSource provenance (OpenSpec tasks 1.6/2.3)", c.Source)
	}
	hook := ir.NewHookWithMatcherOptions("fixture", c.Event, c.Matcher, c.Command, "command", nil, ir.SourceRef{}, ir.WithHookTimeoutPtr(c.Timeout))
	res, err := harnesscompile.Compile(hook, harnesscompile.Target(c.Target))
	if err != nil {
		t.Fatal(err)
	}
	got := emittedScalars(t, res)
	if c.WantError != "" {
		if !hasDiagnosticCode(res, c.WantError) {
			t.Fatalf("want error %q, diagnostics = %v", c.WantError, diagnosticCodes(res))
		}
		return
	}
	if c.WantEvent != "" && !slices.Contains(got, c.WantEvent) {
		t.Fatalf("want event %q not emitted; scalars = %v", c.WantEvent, got)
	}
	if c.WantMatcher != "" && !slices.Contains(got, c.WantMatcher) {
		t.Fatalf("want matcher %q not emitted; scalars = %v", c.WantMatcher, got)
	}
	if c.WantCommand != "" && !slices.Contains(got, c.WantCommand) {
		t.Fatalf("want command %q not emitted; scalars = %v", c.WantCommand, got)
	}
	if c.WantTimeout != nil && !slices.Contains(got, strconv.Itoa(*c.WantTimeout)) {
		t.Fatalf("want timeout %d not emitted; scalars = %v", *c.WantTimeout, got)
	}
	if c.WantWarning != "" && !hasDiagnosticCode(res, c.WantWarning) {
		t.Fatalf("want warning %q, diagnostics = %v", c.WantWarning, diagnosticCodes(res))
	}
}

func runAllowListCase(t *testing.T, c allowListCase) {
	t.Helper()
	if c.Source != "" && c.Source != "claude" {
		t.Skipf("source %q requires ToolSource provenance (OpenSpec tasks 1.6/2.3)", c.Source)
	}
	sub := ir.NewSubagent("fixture", "desc", "body", "", c.Tools, "", nil, "", nil, ir.SourceRef{})
	res, err := harnesscompile.Compile(sub, harnesscompile.Target(c.Target))
	if err != nil {
		t.Fatal(err)
	}
	got := emittedScalars(t, res)
	if c.WantError != "" {
		if !hasDiagnosticCode(res, c.WantError) {
			t.Fatalf("want error %q, diagnostics = %v", c.WantError, diagnosticCodes(res))
		}
		return
	}
	for _, tool := range c.WantTools {
		if !slices.Contains(got, tool) {
			t.Fatalf("want tool %q not emitted; scalars = %v", tool, got)
		}
	}
	if c.WantWarning != "" && !hasDiagnosticCode(res, c.WantWarning) {
		t.Fatalf("want warning %q, diagnostics = %v", c.WantWarning, diagnosticCodes(res))
	}
}

// emittedScalars collects every JSON string and number scalar from the
// compiled artifacts so fixtures can assert native spellings without parsing
// each target's aggregate shape.
func emittedScalars(t *testing.T, res *harnesscompile.Result) []string {
	t.Helper()
	var scalars []string
	var walk func(v any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			for key, child := range typed {
				scalars = append(scalars, key)
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case string:
			scalars = append(scalars, typed)
		case float64:
			scalars = append(scalars, strconv.FormatFloat(typed, 'f', -1, 64))
		}
	}
	for _, a := range res.Artifacts {
		var doc any
		if err := json.Unmarshal(a.Bytes, &doc); err != nil {
			continue
		}
		walk(doc)
	}
	if len(scalars) == 0 && len(res.Artifacts) != 0 {
		t.Logf("no JSON scalars in %d artifacts", len(res.Artifacts))
	}
	return scalars
}

func diagnosticCodes(res *harnesscompile.Result) []string {
	var codes []string
	for _, d := range res.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}

func hasDiagnosticCode(res *harnesscompile.Result, code string) bool {
	return slices.Contains(diagnosticCodes(res), code)
}
