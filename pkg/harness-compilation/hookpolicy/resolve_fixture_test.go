package hookpolicy_test

// Resolver-level run of the independently authored conversion fixtures
// (OpenSpec native-hook-integration, task 3.1). The Compile-level runner in
// conversions_fixture_test.go stays env-gated until every target translator
// consumes hookpolicy; this runner verifies the resolver directly against
// the same fixture file.

import (
	"bytes"
	"os"
	"strconv"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"gopkg.in/yaml.v3"
)

func TestResolverConversionFixtures(t *testing.T) {
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
	run := func(section string, cases []hookCase) {
		for _, c := range cases {
			t.Run(section+"/"+c.Name, func(t *testing.T) {
				runResolveCase(t, c)
			})
		}
	}
	run("matchers", fixtures.Matchers)
	run("events", fixtures.Events)
	run("env", fixtures.Env)
	run("timeouts", fixtures.Timeouts)
}

func runResolveCase(t *testing.T, c hookCase) {
	t.Helper()
	var options []ir.HookOption
	if c.Source != "" {
		options = append(options, ir.WithHookSource(c.Source))
	}
	options = append(options, ir.WithHookTimeoutPtr(c.Timeout))
	hook := ir.NewHookWithMatcherOptions("fixture", c.Event, c.Matcher, c.Command, "command", nil, ir.SourceRef{}, options...)
	res := hookpolicy.Resolve(hook, target.Target(c.Target), hookpolicy.ScopeRepository, "")
	if c.WantError != "" {
		if !hasResolveCode(res, c.WantError) {
			t.Fatalf("want error %q, diagnostics = %v", c.WantError, resolveCodes(res))
		}
		return
	}
	if res.Failed() {
		t.Fatalf("resolution suppressed: %+v", res.Diagnostics)
	}
	if c.WantEvent != "" && res.Event != c.WantEvent {
		t.Fatalf("event = %q, want %q", res.Event, c.WantEvent)
	}
	if res.Matcher != c.WantMatcher {
		t.Fatalf("matcher = %q, want %q", res.Matcher, c.WantMatcher)
	}
	if c.WantCommand != "" && res.Command != c.WantCommand {
		t.Fatalf("command = %q, want %q", res.Command, c.WantCommand)
	}
	if c.WantTimeout != nil {
		if res.Timeout == nil || *res.Timeout != *c.WantTimeout {
			got := "nil"
			if res.Timeout != nil {
				got = strconv.Itoa(*res.Timeout)
			}
			t.Fatalf("timeout = %s, want %d", got, *c.WantTimeout)
		}
	}
	if c.WantWarning != "" && !hasResolveCode(res, c.WantWarning) {
		t.Fatalf("want warning %q, diagnostics = %v", c.WantWarning, resolveCodes(res))
	}
}

func resolveCodes(res hookpolicy.Resolved) []string {
	var codes []string
	for _, d := range res.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}

func hasResolveCode(res hookpolicy.Resolved, code string) bool {
	return diag.HasCode(res.Diagnostics, code)
}
