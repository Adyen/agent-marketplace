package analyze

import (
	"context"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestRulepackSuccess(t *testing.T) {
	data := []byte(`
apiVersion: agent-marketplace.analyzer/v1
rules:
  - code: todo-marker
    pattern: "TODO:"
    message: "found a TODO marker"
    kinds: [skill]
  - code: acme.custom-warning
    pattern: "DEPRECATED"
    message: "deprecated marker"
`)
	rp, err := NewRulepack("acme.rulepack", "1.0.0", data)
	if err != nil {
		t.Fatalf("NewRulepack: %v", err)
	}
	if rp.ID() != "acme.rulepack" {
		t.Fatalf("id = %q", rp.ID())
	}
	if len(rp.rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rp.rules))
	}
	if rp.Policy().OutcomeFor("marketplace.rulepack.todo-marker") != OutcomeWarn {
		t.Fatalf("expected warn outcome for todo-marker")
	}

	engine := NewEngineWith([]Registration{{Analyzer: rp, Role: RoleAdvisory, Policy: rp.Policy()}})
	report, err := engine.Analyze(context.Background(), Subject{
		Resource: ir.NewSkill("review", "Review", "TODO: fix this", nil, nil, ir.SourceRef{}),
		Limits:   DefaultLimits(),
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(report.Audits) != 1 {
		t.Fatalf("expected 1 audit, got %d", len(report.Audits))
	}
	if report.Audits[0].Verdict != VerdictWarn {
		t.Fatalf("verdict = %q, want warn", report.Audits[0].Verdict)
	}
	if len(report.Audits[0].Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(report.Audits[0].Findings))
	}
	f := report.Audits[0].Findings[0]
	if f.Code != "marketplace.rulepack.todo-marker" {
		t.Fatalf("code = %q", f.Code)
	}
	if f.Message != "found a TODO marker" {
		t.Fatalf("message = %q", f.Message)
	}
	if f.PolicyOutcome != OutcomeWarn {
		t.Fatalf("policy outcome = %q", f.PolicyOutcome)
	}

	// Dotted prefix: acme.custom-warning becomes marketplace.rulepack.acme.custom-warning.
	if rp.rules[1].code != "marketplace.rulepack.acme.custom-warning" {
		t.Fatalf("dotted code = %q", rp.rules[1].code)
	}
}

func TestRulepackOmittedKindsMeansAll(t *testing.T) {
	data := []byte(`
apiVersion: agent-marketplace.analyzer/v1
rules:
  - code: marker
    pattern: "MARK"
    message: "found marker"
`)
	rp, err := NewRulepack("acme.rulepack", "1.0.0", data)
	if err != nil {
		t.Fatalf("NewRulepack: %v", err)
	}
	for _, kind := range []ir.ResourceKind{ir.KindSkill, ir.KindCommand, ir.KindHook} {
		res := kindResource(kind)
		audit, err := rp.Analyze(context.Background(), Subject{Resource: res, Limits: DefaultLimits()}, nil)
		if err != nil {
			t.Fatalf("Analyze(%s): %v", kind, err)
		}
		if len(audit.Findings) == 0 {
			t.Errorf("expected finding for kind %s", kind)
		}
	}
}

func TestRulepackStrictFailures(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "unknown top-level field",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\nunknown: true\n",
			want: "unknown rulepack field",
		},
		{
			name: "unknown rule field",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\nrules:\n  - code: x\n    pattern: x\n    message: x\n    extra: true\n",
			want: "unknown rule field",
		},
		{
			name: "missing message",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\nrules:\n  - code: x\n    pattern: x\n",
			want: "empty message",
		},
		{
			name: "invalid pattern",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\nrules:\n  - code: x\n    pattern: '(?'\n    message: x\n",
			want: "invalid pattern",
		},
		{
			name: "duplicate code",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\nrules:\n  - code: x\n    pattern: a\n    message: a\n  - code: x\n    pattern: b\n    message: b\n",
			want: "duplicate code",
		},
		{
			name: "invalid kind",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\nrules:\n  - code: x\n    pattern: a\n    message: a\n    kinds: [unknownkind]\n",
			want: "invalid kind",
		},
		{
			name: "wrong apiVersion",
			yaml: "apiVersion: v2\nrules: []\n",
			want: "unsupported apiVersion",
		},
		{
			name: "unsafe code identifier",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\nrules:\n  - code: 'x y'\n    pattern: a\n    message: a\n",
			want: "invalid code identifier",
		},
		{
			name: "duplicate top-level key",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\napiVersion: agent-marketplace.analyzer/v1\nrules: []\n",
			want: "duplicate rulepack field",
		},
		{
			name: "duplicate rule key",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\nrules:\n  - code: x\n    code: x\n    pattern: a\n    message: a\n",
			want: "duplicate rule field",
		},
		{
			name: "trailing document",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\nrules: []\n---\n",
			want: "trailing documents",
		},
		{
			name: "wrong pattern scalar type",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\nrules:\n  - code: x\n    pattern: [1, 2]\n    message: a\n",
			want: "rule pattern must be a scalar",
		},
		{
			name: "wrong kinds scalar type",
			yaml: "apiVersion: agent-marketplace.analyzer/v1\nrules:\n  - code: x\n    pattern: a\n    message: a\n    kinds: skill\n",
			want: "rule kinds must be a sequence of strings",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRulepack("acme.rulepack", "1.0.0", []byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestRulepackNoRulesPass(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace.analyzer/v1
rules: []
`)
	rp, err := NewRulepack("acme.rulepack", "1.0.0", data)
	if err != nil {
		t.Fatalf("NewRulepack: %v", err)
	}
	audit, err := rp.Analyze(context.Background(), Subject{
		Resource: ir.NewSkill("review", "Review", "TODO: fix", nil, nil, ir.SourceRef{}),
		Limits:   DefaultLimits(),
	}, nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(audit.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(audit.Findings))
	}
}

func TestRulepackEngineAppliesRegistrationPolicy(t *testing.T) {
	data := []byte(`
apiVersion: agent-marketplace.analyzer/v1
rules:
  - code: marker
    pattern: "MARK"
    message: "found marker"
`)
	rp, err := NewRulepack("acme.rulepack", "1.0.0", data)
	if err != nil {
		t.Fatalf("NewRulepack: %v", err)
	}

	custom := rp.Policy()
	custom.Outcomes["marketplace.rulepack.marker"] = OutcomeAllow
	engine := NewEngineWith([]Registration{{Analyzer: rp, Role: RoleAdvisory, Policy: custom}})
	report, err := engine.Analyze(context.Background(), Subject{
		Resource: ir.NewSkill("review", "Review", "MARK", nil, nil, ir.SourceRef{}),
		Limits:   DefaultLimits(),
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Audits[0].Verdict != VerdictPass {
		t.Fatalf("expected pass after policy override, got %q", report.Audits[0].Verdict)
	}
}

func TestRulepackBoundedMatchEnumeration(t *testing.T) {
	data := []byte(`
apiVersion: agent-marketplace.analyzer/v1
rules:
  - code: marker
    pattern: "MARK"
    message: "found marker"
`)
	rp, err := NewRulepack("acme.rulepack", "1.0.0", data)
	if err != nil {
		t.Fatalf("NewRulepack: %v", err)
	}
	res := ir.NewSkill("x", "X", strings.Repeat("MARK ", 50), nil, nil, ir.SourceRef{})

	t.Run("broad pattern stays within MaxFindings", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxFindings = 3
		audit, err := rp.Analyze(context.Background(), Subject{Resource: res, Limits: limits}, nil)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if len(audit.Findings) != 3 {
			t.Fatalf("findings = %d, want capped at 3", len(audit.Findings))
		}
	})

	t.Run("zero MaxFindings falls back to default bound", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxFindings = 0
		audit, err := rp.Analyze(context.Background(), Subject{Resource: res, Limits: limits}, nil)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if len(audit.Findings) != 50 {
			t.Fatalf("findings = %d, want 50 under the default bound", len(audit.Findings))
		}
	})
}

func kindResource(k ir.ResourceKind) ir.Resource {
	switch k {
	case ir.KindSkill:
		return ir.NewSkill("x", "X", "MARK", nil, nil, ir.SourceRef{})
	case ir.KindCommand:
		return ir.NewCommand("x", "X", "MARK", nil, ir.SourceRef{})
	case ir.KindSubagent:
		return ir.NewSubagent("x", "X", "MARK", "m", nil, "low", nil, "", nil, ir.SourceRef{})
	case ir.KindHook:
		return ir.NewHook("x", "PreToolUse", "MARK", "command", nil, ir.SourceRef{})
	case ir.KindMcpServer:
		return ir.NewMcpServer("x", "npx", nil, nil, nil, ir.SourceRef{})
	case ir.KindCollection:
		return ir.NewCollection("x", "1.0.0", "MARK", nil, nil, nil, ir.SourceRef{})
	case ir.KindRule:
		return ir.NewRule("x", "X", "MARK", nil, false, nil, ir.SourceRef{})
	}
	return ir.NewSkill("x", "X", "MARK", nil, nil, ir.SourceRef{})
}
