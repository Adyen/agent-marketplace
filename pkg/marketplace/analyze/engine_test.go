package analyze

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestDefaultEngineHasBuiltins(t *testing.T) {
	e := NewEngine()
	ids := make([]string, 0, 5)
	for _, a := range e.Analyzers() {
		ids = append(ids, a.ID())
	}
	want := []string{AnalyzerHiddenContent, AnalyzerObfuscation, AnalyzerAssets, AnalyzerReferences, AnalyzerCapabilities}
	if !slices.Equal(ids, want) {
		t.Fatalf("built-in ids = %v, want %v", ids, want)
	}
	for _, a := range e.Analyzers() {
		if a.Version() == "" {
			t.Errorf("%s has no version", a.ID())
		}
	}
}

func TestBuiltinOwnership(t *testing.T) {
	ctx := context.Background()

	skillWithTools := ir.NewSkillWithOptions("review", "Review", "ok", nil, nil, ir.SourceRef{}, ir.WithSkillAllowedTools([]string{"read_file"}))

	cases := []struct {
		name     string
		resource ir.Resource
		files    FileSet
		wantCode string
		wantID   string
	}{
		{
			name:     "hidden-content owns html-comment",
			resource: ir.NewSkill("review", "Review", "<!-- secret -->", nil, nil, ir.SourceRef{}),
			wantCode: FindingHTMLComment,
			wantID:   AnalyzerHiddenContent,
		},
		{
			name:     "hidden-content owns unsafe-bidi",
			resource: ir.NewSkill("review", "Review", "prefix\u202Eoverride", nil, nil, ir.SourceRef{}),
			wantCode: FindingUnsafeBidi,
			wantID:   AnalyzerHiddenContent,
		},
		{
			name:     "obfuscation owns dynamic-execution",
			resource: ir.NewSkill("review", "Review", "eval(code)", nil, nil, ir.SourceRef{}),
			wantCode: FindingDynamicExecution,
			wantID:   AnalyzerObfuscation,
		},
		{
			name:     "obfuscation owns obfuscated-instruction",
			resource: ir.NewSkill("review", "Review", "ZXZhbA==", nil, nil, ir.SourceRef{}),
			wantCode: FindingObfuscatedInstruction,
			wantID:   AnalyzerObfuscation,
		},
		{
			name:     "references owns dangerous-url",
			resource: ir.NewSkill("review", "Review", "javascript:alert(1)", nil, nil, ir.SourceRef{}),
			wantCode: FindingDangerousURL,
			wantID:   AnalyzerReferences,
		},
		{
			name:     "references owns remote-reference",
			resource: ir.NewSkill("review", "Review", "https://example.com", nil, nil, ir.SourceRef{}),
			wantCode: FindingRemoteReference,
			wantID:   AnalyzerReferences,
		},
		{
			name:     "references owns suspicious-path",
			resource: ir.NewSkill("review", "Review", "see /etc/passwd", nil, nil, ir.SourceRef{}),
			wantCode: FindingSuspiciousPath,
			wantID:   AnalyzerReferences,
		},
		{
			name:     "assets owns asset-path-escape",
			resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}),
			files:    FileSet{"../escape": []byte("bad")},
			wantCode: FindingAssetPathEscape,
			wantID:   AnalyzerAssets,
		},
		{
			name:     "assets owns executable-script",
			resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}),
			files:    FileSet{"run.sh": []byte("#!/bin/sh\necho hi\n")},
			wantCode: FindingExecutableScript,
			wantID:   AnalyzerAssets,
		},
		{
			name:     "capabilities owns tool-grant",
			resource: skillWithTools,
			wantCode: FindingToolGrant,
			wantID:   AnalyzerCapabilities,
		},
		{
			name:     "capabilities owns hook-capability",
			resource: ir.NewHook("pre-tool", "PreToolUse", "echo", "command", nil, ir.SourceRef{}),
			wantCode: FindingHookCapability,
			wantID:   AnalyzerCapabilities,
		},
		{
			name:     "capabilities owns mcp-capability",
			resource: ir.NewMcpServer("server", "npx", []string{"-y"}, nil, nil, ir.SourceRef{}),
			wantCode: FindingMcpCapability,
			wantID:   AnalyzerCapabilities,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report, err := NewEngine().Analyze(ctx, Subject{Resource: tc.resource, Files: tc.files, Limits: DefaultLimits()})
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			var found []Finding
			for _, f := range report.Findings {
				if f.Code == tc.wantCode {
					found = append(found, f)
				}
			}
			if len(found) == 0 {
				t.Fatalf("expected %s finding owned by %s, got findings %v", tc.wantCode, tc.wantID, report.Findings)
			}
			for _, f := range found {
				if f.Analyzer != tc.wantID {
					t.Errorf("code %q attributed to %q, want %q", f.Code, f.Analyzer, tc.wantID)
				}
			}
		})
	}
}

func TestFoldMatrix(t *testing.T) {
	mk := func(id, role, verdict string) Audit {
		return Audit{AnalyzerID: id, Role: role, Verdict: verdict, Findings: []Finding{{Code: FindingToolGrant, Severity: OutcomeWarn, PolicyOutcome: OutcomeWarn}}}
	}

	cases := []struct {
		name   string
		audits []Audit
		want   string
	}{
		{"gating pass", []Audit{mk("a", RoleGating, VerdictPass)}, DecisionAllow},
		{"gating warn", []Audit{mk("a", RoleGating, VerdictWarn)}, DecisionWarn},
		{"gating fail", []Audit{mk("a", RoleGating, VerdictFail)}, DecisionReject},
		{"gating error", []Audit{mk("a", RoleGating, VerdictError)}, DecisionWarn},
		{"gating skipped", []Audit{mk("a", RoleGating, VerdictSkipped)}, DecisionAllow},
		{"advisory pass", []Audit{mk("a", RoleAdvisory, VerdictPass)}, DecisionAllow},
		{"advisory warn", []Audit{mk("a", RoleAdvisory, VerdictWarn)}, DecisionWarn},
		{"advisory fail must warn not reject", []Audit{mk("a", RoleAdvisory, VerdictFail)}, DecisionWarn},
		{"advisory error must allow", []Audit{mk("a", RoleAdvisory, VerdictError)}, DecisionAllow},
		{"advisory skipped", []Audit{mk("a", RoleAdvisory, VerdictSkipped)}, DecisionAllow},
		{"gating fail with advisory fail rejects", []Audit{mk("a", RoleGating, VerdictFail), mk("b", RoleAdvisory, VerdictFail)}, DecisionReject},
		{"advisory fail with gating pass warns", []Audit{mk("a", RoleGating, VerdictPass), mk("b", RoleAdvisory, VerdictFail)}, DecisionWarn},
		{"gating warn with advisory pass warns", []Audit{mk("a", RoleGating, VerdictWarn), mk("b", RoleAdvisory, VerdictPass)}, DecisionWarn},
		{"advisory warn with gating pass warns", []Audit{mk("a", RoleGating, VerdictPass), mk("b", RoleAdvisory, VerdictWarn)}, DecisionWarn},
		{"gating error with advisory pass warns", []Audit{mk("a", RoleGating, VerdictError), mk("b", RoleAdvisory, VerdictPass)}, DecisionWarn},
		{"empty", nil, DecisionAllow},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := foldDecision(tc.audits); got != tc.want {
				t.Fatalf("foldDecision = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEngineVerdictStrengthening(t *testing.T) {
	cases := []struct {
		raw, outcome, want string
	}{
		{VerdictPass, OutcomeAllow, VerdictPass},
		{VerdictPass, OutcomeWarn, VerdictWarn},
		{VerdictPass, OutcomeReject, VerdictFail},
		{VerdictWarn, OutcomeAllow, VerdictWarn},
		{VerdictWarn, OutcomeWarn, VerdictWarn},
		{VerdictWarn, OutcomeReject, VerdictFail},
		{VerdictFail, OutcomeAllow, VerdictFail},
		{VerdictSkipped, OutcomeAllow, VerdictSkipped},
		{VerdictSkipped, OutcomeWarn, VerdictWarn},
		{VerdictSkipped, OutcomeReject, VerdictFail},
		{"", OutcomeWarn, VerdictWarn},
		{"", OutcomeAllow, VerdictPass},
	}

	for _, tc := range cases {
		t.Run(tc.raw+"_"+tc.outcome, func(t *testing.T) {
			rec := &configRecorder{
				verdict:  tc.raw,
				findings: []Finding{{Code: "test.code", Message: "x"}},
			}
			custom := PublicPolicy()
			custom.Outcomes = map[string]string{"test.code": tc.outcome}
			engine := NewEngineWith([]Registration{{Analyzer: rec, Role: RoleAdvisory, Policy: custom}})
			report, err := engine.Analyze(context.Background(), Subject{
				Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}),
				Limits:   DefaultLimits(),
			})
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if report.Audits[0].Verdict != tc.want {
				t.Fatalf("verdict = %q, want %q", report.Audits[0].Verdict, tc.want)
			}
		})
	}
}

func TestEngineAnalyzeIntegration(t *testing.T) {
	ctx := context.Background()
	s := ir.NewSkillWithOptions("review", "Review", "Review <!-- hidden --> carefully, then run eval(code) and see file:///tmp/example", []string{"user"}, nil, ir.SourceRef{}, ir.WithSkillAllowedTools([]string{"read_file"}))

	report, err := NewEngine().Analyze(ctx, Subject{Resource: s, Files: FileSet{"run.sh": []byte("#!/bin/sh\necho hi\n")}, Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	// Decision should be warn because references and capabilities are advisory
	// warnings and the script is a warning, not a reject.
	if report.Decision != DecisionWarn {
		t.Fatalf("decision = %q, want warn: audits=%+v", report.Decision, report.Audits)
	}

	codes := make(map[string]string)
	for _, f := range report.Findings {
		codes[f.Code] = f.Analyzer
	}
	for _, c := range []string{FindingHTMLComment, FindingDynamicExecution, FindingToolGrant, FindingExecutableScript} {
		if _, ok := codes[c]; !ok {
			t.Errorf("missing code %q", c)
		}
	}

	// Audits cover all five built-ins.
	if len(report.Audits) != 5 {
		t.Fatalf("expected 5 audits, got %d", len(report.Audits))
	}
	for _, a := range report.Audits {
		if a.Summary == "" {
			t.Errorf("audit %s has empty summary", a.AnalyzerID)
		}
	}
}

func TestEngineRejectsStructuralAssetRisk(t *testing.T) {
	s := ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{})
	report, err := NewEngine().Analyze(context.Background(), Subject{Resource: s, Files: FileSet{"../escape": []byte("bad")}, Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Decision != DecisionReject {
		t.Fatalf("asset path escape should reject, got %q: %+v", report.Decision, report.Audits)
	}
	if !report.Rejected {
		t.Fatalf("Rejected should be true")
	}
}

func TestEngineZeroApplicableAnalyzers(t *testing.T) {
	// A custom engine whose only analyzer does not apply to skills.
	engine := NewEngineWith([]Registration{{Analyzer: &zeroAnalyzer{}, Role: RoleAdvisory, Policy: PublicPolicy()}})
	report, err := engine.Analyze(context.Background(), Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(report.Audits) != 0 {
		t.Fatalf("expected no audits, got %d", len(report.Audits))
	}
	if report.Decision != DecisionAllow {
		t.Fatalf("expected allow, got %q", report.Decision)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(report.Findings))
	}
}

func TestNewEngineWithEmpty(t *testing.T) {
	engine := NewEngineWith(nil)
	report, err := engine.Analyze(context.Background(), Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(report.Audits) != 0 {
		t.Fatalf("expected zero audits, got %d", len(report.Audits))
	}
	if report.Decision != DecisionAllow {
		t.Fatalf("expected allow, got %q", report.Decision)
	}
}

func TestEngineErrorAuditIsNonConsentable(t *testing.T) {
	engine := NewEngineWith([]Registration{{Analyzer: &failingAnalyzer{}, Role: RoleGating, Policy: PublicPolicy()}})
	s := ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{})
	report, err := engine.Analyze(context.Background(), Subject{Resource: s, Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(report.Audits) != 1 {
		t.Fatalf("expected 1 audit, got %d", len(report.Audits))
	}
	a := report.Audits[0]
	if a.Verdict != VerdictError {
		t.Fatalf("verdict = %q, want error", a.Verdict)
	}
	if a.Role != RoleGating {
		t.Fatalf("role = %q, want gating", a.Role)
	}
	if len(a.Findings) != 1 || a.Findings[0].Code != PolicyAuditUnavailable {
		t.Fatalf("expected audit-unavailable finding, got %v", a.Findings)
	}
	if a.Findings[0].Consentable == nil || *a.Findings[0].Consentable {
		t.Fatalf("audit-unavailable finding must be non-consentable")
	}
	if a.Findings[0].PolicyOutcome != OutcomeWarn {
		t.Fatalf("expected warn policy outcome, got %q", a.Findings[0].PolicyOutcome)
	}
	if report.Decision != DecisionWarn {
		t.Fatalf("gating error should warn, got %q", report.Decision)
	}
}

func TestEngineAdvisoryErrorIsConsentable(t *testing.T) {
	engine := NewEngineWith([]Registration{{Analyzer: &failingAnalyzer{}, Role: RoleAdvisory, Policy: PublicPolicy()}})
	s := ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{})
	report, err := engine.Analyze(context.Background(), Subject{Resource: s, Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(report.Audits) != 1 {
		t.Fatalf("expected 1 audit, got %d", len(report.Audits))
	}
	a := report.Audits[0]
	if a.Verdict != VerdictError {
		t.Fatalf("verdict = %q, want error", a.Verdict)
	}
	if a.Role != RoleAdvisory {
		t.Fatalf("role = %q, want advisory", a.Role)
	}
	if len(a.Findings) != 0 {
		t.Fatalf("advisory error should not create findings, got %v", a.Findings)
	}
	if report.Decision != DecisionAllow {
		t.Fatalf("advisory error should not block consent, got %q", report.Decision)
	}
}

func TestEnginePassesConfig(t *testing.T) {
	recorder := &configRecorder{verdict: VerdictPass}
	engine := NewEngineWith([]Registration{{Analyzer: recorder, Role: RoleAdvisory, Config: AnalyzerConfig{"x": 1}}})
	_, err := engine.Analyze(context.Background(), Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !recorder.called {
		t.Fatal("analyzer was not called")
	}
	if recorder.config["x"] != 1 {
		t.Fatalf("expected config x=1, got %v", recorder.config)
	}
}

func TestEngineCustomPolicy(t *testing.T) {
	custom := PublicPolicy()
	custom.Outcomes = map[string]string{FindingHTMLComment: OutcomeReject}
	engine := NewEngineWith([]Registration{{
		Analyzer: newHiddenContentAnalyzer(),
		Role:     RoleAdvisory,
		Policy:   custom,
	}})
	s := ir.NewSkill("review", "Review", "<!-- secret -->", nil, nil, ir.SourceRef{})
	report, err := engine.Analyze(context.Background(), Subject{Resource: s, Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Audits[0].Verdict != VerdictFail {
		t.Fatalf("expected fail from custom policy, got %q", report.Audits[0].Verdict)
	}
}

func TestEngineSpoofedIdentity(t *testing.T) {
	spoof := &spoofedAnalyzer{}
	engine := NewEngineWith([]Registration{{Analyzer: spoof, Role: RoleAdvisory, Policy: PublicPolicy()}})
	report, err := engine.Analyze(context.Background(), Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	a := report.Audits[0]
	if a.AnalyzerID != "real.analyzer" {
		t.Fatalf("engine did not stamp analyzer id, got %q", a.AnalyzerID)
	}
	if a.AnalyzerVersion != "2.0.0" {
		t.Fatalf("engine did not stamp analyzer version, got %q", a.AnalyzerVersion)
	}
	if a.Findings[0].Analyzer != "real.analyzer" {
		t.Fatalf("finding not re-attributed, got %q", a.Findings[0].Analyzer)
	}
}

func TestEngineInvalidVerdict(t *testing.T) {
	engine := NewEngineWith([]Registration{{Analyzer: &invalidVerdictAnalyzer{}, Role: RoleAdvisory, Policy: PublicPolicy()}})
	report, err := engine.Analyze(context.Background(), Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Audits[0].Verdict != VerdictError {
		t.Fatalf("expected error verdict, got %q", report.Audits[0].Verdict)
	}
}

func TestEngineMcpRedaction(t *testing.T) {
	ctx := context.Background()
	m := ir.NewMcpServerWithOptions("server", "npx", []string{"-y"}, map[string]string{"TOKEN": "super-secret-12345"}, nil, ir.SourceRef{}, ir.WithMcpURL("http://user:pass@example.com"))

	report, err := NewEngine().Analyze(ctx, Subject{Resource: m, Files: nil, Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	for _, f := range report.Findings {
		if strings.Contains(f.Message, "super-secret") || strings.Contains(f.Message, "user:pass") {
			t.Fatalf("finding leaks MCP secret: %v", f)
		}
	}
}

func TestEngineMaxFindingsPerAudit(t *testing.T) {
	ctx := context.Background()
	limits := DefaultLimits()
	limits.MaxFindings = 1

	s := ir.NewSkill("review", "Review", "\u202Ehidden and <!-- comment -->", nil, nil, ir.SourceRef{})
	report, err := NewEngine().Analyze(ctx, Subject{Resource: s, Files: nil, Limits: limits})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	for _, a := range report.Audits {
		if a.AnalyzerID == AnalyzerHiddenContent && len(a.Findings) > 1 {
			t.Fatalf("hidden-content audit should be truncated to 1, got %d", len(a.Findings))
		}
	}
	// Reject finding is preserved.
	if report.Decision != DecisionReject {
		t.Fatalf("expected reject from unsafe bidi, got %q", report.Decision)
	}
}

type zeroAnalyzer struct{}

func (zeroAnalyzer) ID() string               { return "zero" }
func (zeroAnalyzer) Version() string          { return "1.0.0" }
func (zeroAnalyzer) Kinds() []ir.ResourceKind { return []ir.ResourceKind{ir.KindRule} }
func (zeroAnalyzer) Analyze(context.Context, Subject, AnalyzerConfig) (Audit, error) {
	return Audit{}, errors.New("zero analyzer should not be called")
}

type failingAnalyzer struct{}

func (failingAnalyzer) ID() string               { return "failing" }
func (failingAnalyzer) Version() string          { return "1.0.0" }
func (failingAnalyzer) Kinds() []ir.ResourceKind { return nil }
func (failingAnalyzer) Analyze(context.Context, Subject, AnalyzerConfig) (Audit, error) {
	return Audit{}, errors.New("analyzer failed intentionally")
}

type configRecorder struct {
	called   bool
	config   AnalyzerConfig
	verdict  string
	findings []Finding
}

func (c *configRecorder) ID() string               { return "config-recorder" }
func (c *configRecorder) Version() string          { return "1.0.0" }
func (c *configRecorder) Kinds() []ir.ResourceKind { return nil }
func (c *configRecorder) Analyze(_ context.Context, _ Subject, cfg AnalyzerConfig) (Audit, error) {
	c.called = true
	c.config = cfg
	return Audit{Verdict: c.verdict, Findings: c.findings}, nil
}

type spoofedAnalyzer struct{}

func (spoofedAnalyzer) ID() string               { return "real.analyzer" }
func (spoofedAnalyzer) Version() string          { return "2.0.0" }
func (spoofedAnalyzer) Kinds() []ir.ResourceKind { return nil }
func (spoofedAnalyzer) Analyze(context.Context, Subject, AnalyzerConfig) (Audit, error) {
	return Audit{
		AnalyzerID:      "fake.analyzer",
		AnalyzerVersion: "9.9.9",
		Verdict:         VerdictPass,
		Findings:        []Finding{{Code: "marketplace.find.test", Message: "x"}},
	}, nil
}

type invalidVerdictAnalyzer struct{}

func (invalidVerdictAnalyzer) ID() string               { return "invalid" }
func (invalidVerdictAnalyzer) Version() string          { return "1.0.0" }
func (invalidVerdictAnalyzer) Kinds() []ir.ResourceKind { return nil }
func (invalidVerdictAnalyzer) Analyze(context.Context, Subject, AnalyzerConfig) (Audit, error) {
	return Audit{Verdict: "not-a-verdict", Findings: []Finding{}}, nil
}

type secretAnalyzer struct {
	secret string
}

func (a *secretAnalyzer) ID() string               { return "marketplace.secret" }
func (a *secretAnalyzer) Version() string          { return "1.0.0" }
func (a *secretAnalyzer) Kinds() []ir.ResourceKind { return nil }
func (a *secretAnalyzer) Analyze(context.Context, Subject, AnalyzerConfig) (Audit, error) {
	return Audit{Verdict: VerdictError, Error: a.secret}, nil
}

func TestEngineVerdictErrorLogsSecret(t *testing.T) {
	old := slog.Default()
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	secret := "super-secret-12345"
	engine := NewEngineWith([]Registration{{Analyzer: &secretAnalyzer{secret: secret}, Role: RoleGating, Policy: PublicPolicy()}})
	report, err := engine.Analyze(context.Background(), Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Limits: DefaultLimits()})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(report.Audits) != 1 || report.Audits[0].Verdict != VerdictError {
		t.Fatalf("expected one error verdict, got %+v", report.Audits)
	}

	logs := buf.String()
	if !strings.Contains(logs, "analyzer returned error verdict") {
		t.Fatalf("expected warn log entry, got: %s", logs)
	}
	if !strings.Contains(logs, "marketplace.secret") {
		t.Fatalf("expected analyzer id in logs, got: %s", logs)
	}
	if !strings.Contains(logs, "verdict=error") {
		t.Fatalf("expected verdict attribute in logs, got: %s", logs)
	}
	if strings.Contains(logs, secret) {
		t.Fatalf("logs must not contain the secret %q, got: %s", secret, logs)
	}
}
