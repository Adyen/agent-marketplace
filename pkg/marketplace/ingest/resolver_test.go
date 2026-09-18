package ingest

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
)

type fakeFS map[string][]byte

func (f fakeFS) readFile(path string, maxBytes int64) ([]byte, error) {
	data, ok := f[path]
	if !ok {
		return nil, &fakeFSError{path: path}
	}
	return data, nil
}

type fakeFSError struct{ path string }

func (e *fakeFSError) Error() string { return "file not found: " + e.path }

func effectiveTestPolicy() analyze.Policy {
	return analyze.PublicPolicy()
}

func testLimits() config.Limits {
	return config.Limits{FileBytes: 1 << 20, RepositoryBytes: 16 << 20, FileCount: 256}
}

func boolPtr(b bool) *bool { return &b }

func TestResolveAnalyzersDefaults(t *testing.T) {
	policy := config.Policy{Analyzers: nil}
	set, err := resolveAnalyzersFS(policy, effectiveTestPolicy(), testLimits(), fakeFS{})
	if err != nil {
		t.Fatalf("resolveAnalyzers: %v", err)
	}
	if len(set.Entries) != 5 {
		t.Fatalf("expected 5 default analyzers, got %d", len(set.Entries))
	}
	if set.AuditSetVersion == "" {
		t.Fatal("expected audit set version")
	}
	want := []string{
		analyze.AnalyzerHiddenContent,
		analyze.AnalyzerObfuscation,
		analyze.AnalyzerAssets,
		analyze.AnalyzerReferences,
		analyze.AnalyzerCapabilities,
	}
	for i, wantID := range want {
		if got := set.Entries[i].Registration.Analyzer.ID(); got != wantID {
			t.Fatalf("entry[%d].Analyzer.ID = %q, want %q", i, got, wantID)
		}
	}
}

func TestResolveAnalyzersEmptyExplicit(t *testing.T) {
	policy := config.Policy{Analyzers: []config.Analyzer{}}
	set, err := resolveAnalyzersFS(policy, effectiveTestPolicy(), testLimits(), fakeFS{})
	if err != nil {
		t.Fatalf("resolveAnalyzers: %v", err)
	}
	if len(set.Entries) != 0 {
		t.Fatalf("expected 0 analyzers, got %d", len(set.Entries))
	}
	if set.AuditSetVersion == "" {
		t.Fatal("expected audit set version for empty set")
	}
}

func TestResolveAnalyzersRespectsOrderAndDisables(t *testing.T) {
	policy := config.Policy{Analyzers: []config.Analyzer{
		{ID: analyze.AnalyzerReferences, Role: analyze.RoleGating},
		{ID: analyze.AnalyzerHiddenContent, Enabled: boolPtr(false)},
		{ID: analyze.AnalyzerAssets},
	}}
	set, err := resolveAnalyzersFS(policy, effectiveTestPolicy(), testLimits(), fakeFS{})
	if err != nil {
		t.Fatalf("resolveAnalyzers: %v", err)
	}
	if len(set.Entries) != 2 {
		t.Fatalf("expected 2 analyzers, got %d", len(set.Entries))
	}
	if set.Entries[0].Registration.Analyzer.ID() != analyze.AnalyzerReferences {
		t.Fatalf("first analyzer = %q, want references", set.Entries[0].Registration.Analyzer.ID())
	}
	if set.Entries[0].Registration.Role != analyze.RoleGating {
		t.Fatalf("references role = %q, want gating", set.Entries[0].Registration.Role)
	}
	if set.Entries[1].Registration.Analyzer.ID() != analyze.AnalyzerAssets {
		t.Fatalf("second analyzer = %q, want assets", set.Entries[1].Registration.Analyzer.ID())
	}
}

func TestResolveAnalyzersRejectsDuplicate(t *testing.T) {
	policy := config.Policy{Analyzers: []config.Analyzer{
		{ID: analyze.AnalyzerHiddenContent},
		{ID: analyze.AnalyzerHiddenContent},
	}}
	_, err := resolveAnalyzersFS(policy, effectiveTestPolicy(), testLimits(), fakeFS{})
	if err == nil {
		t.Fatal("expected error for duplicate analyzer id")
	}
}

func TestResolveAnalyzersUnknownBuiltin(t *testing.T) {
	policy := config.Policy{Analyzers: []config.Analyzer{{ID: "marketplace.unknown"}}}
	_, err := resolveAnalyzersFS(policy, effectiveTestPolicy(), testLimits(), fakeFS{})
	if err == nil {
		t.Fatal("expected error for unknown analyzer")
	}
}

func TestResolveAnalyzersRulepack(t *testing.T) {
	yaml := []byte(`apiVersion: agent-marketplace.analyzer/v1
rules:
  - code: secret
    pattern: "password"
    message: "found password"
`)
	fs := fakeFS{"/etc/analyzers/secrets.yaml": yaml}
	policy := config.Policy{Analyzers: []config.Analyzer{{
		ID:   rulepackID,
		Role: analyze.RoleGating,
		Rulepack: &config.AnalyzerRulepack{
			Path: "/etc/analyzers/secrets.yaml",
		},
	}}}
	set, err := resolveAnalyzersFS(policy, effectiveTestPolicy(), testLimits(), fs)
	if err != nil {
		t.Fatalf("resolveAnalyzers: %v", err)
	}
	if len(set.Entries) != 1 {
		t.Fatalf("expected 1 analyzer, got %d", len(set.Entries))
	}
	rp, ok := set.Entries[0].Registration.Analyzer.(*analyze.Rulepack)
	if !ok {
		t.Fatalf("expected *analyze.Rulepack, got %T", set.Entries[0].Registration.Analyzer)
	}
	if rp.ID() != rulepackID {
		t.Fatalf("rulepack id = %q, want %q", rp.ID(), rulepackID)
	}
	if set.Entries[0].Registration.Role != analyze.RoleGating {
		t.Fatalf("rulepack role = %q, want gating", set.Entries[0].Registration.Role)
	}
	if set.Entries[0].Registration.Analyzer.Version() != analyze.AnalyzerRulepackVersionV1 {
		t.Fatalf("rulepack version = %q, want %q", set.Entries[0].Registration.Analyzer.Version(), analyze.AnalyzerRulepackVersionV1)
	}
	if set.Entries[0].ConfigDigest == "" {
		t.Fatal("expected rulepack config digest")
	}
}

func TestResolveAnalyzersRulepackMissingPath(t *testing.T) {
	policy := config.Policy{Analyzers: []config.Analyzer{{ID: rulepackID, Role: analyze.RoleGating}}}
	_, err := resolveAnalyzersFS(policy, effectiveTestPolicy(), testLimits(), fakeFS{})
	if err == nil {
		t.Fatal("expected error for rulepack without path")
	}
}

func TestResolveAnalyzersRunner(t *testing.T) {
	policy := config.Policy{Analyzers: []config.Analyzer{{
		ID:      "acme.runner",
		Version: "1.0.0",
		Role:    analyze.RoleAdvisory,
		Run: &config.AnalyzerRun{
			Command:              "/bin/sh",
			Args:                 []string{"-c", "analyze"},
			Timeout:              config.Duration(30 * time.Second),
			ImplementationDigest: "sha256:" + stringsRepeatZero(64),
			EnvPassthrough:       []string{"PATH"},
		},
	}}}
	set, err := resolveAnalyzersFS(policy, effectiveTestPolicy(), testLimits(), fakeFS{})
	if err != nil {
		t.Fatalf("resolveAnalyzers: %v", err)
	}
	if len(set.Entries) != 1 {
		t.Fatalf("expected 1 analyzer, got %d", len(set.Entries))
	}
	if set.Entries[0].Registration.Analyzer.ID() != "acme.runner" {
		t.Fatalf("runner id = %q, want acme.runner", set.Entries[0].Registration.Analyzer.ID())
	}
	if set.Entries[0].Registration.Role != analyze.RoleAdvisory {
		t.Fatalf("runner role = %q, want advisory", set.Entries[0].Registration.Role)
	}
	if set.Entries[0].ConfigDigest == "" {
		t.Fatal("expected runner config digest")
	}
}

func TestResolveAnalyzersRunnerRequiresRoleAndRun(t *testing.T) {
	policy := config.Policy{Analyzers: []config.Analyzer{{ID: "acme.runner"}}}
	_, err := resolveAnalyzersFS(policy, effectiveTestPolicy(), testLimits(), fakeFS{})
	if err == nil {
		t.Fatal("expected error for runner without run block")
	}
}

func TestResolveAnalyzersDuplicateBehindDisabled(t *testing.T) {
	policy := config.Policy{Analyzers: []config.Analyzer{
		{ID: analyze.AnalyzerHiddenContent, Enabled: boolPtr(false)},
		{ID: analyze.AnalyzerHiddenContent},
	}}
	_, err := resolveAnalyzersFS(policy, effectiveTestPolicy(), testLimits(), fakeFS{})
	if err == nil {
		t.Fatal("expected error for duplicate analyzer id hidden behind disabled entry")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestCloneAnalyzerConfigDeepCopiesNested(t *testing.T) {
	original := map[string]any{
		"top": map[string]any{
			"nested": "value",
		},
		"list": []any{
			map[string]any{"item": 1},
			"scalar",
		},
	}
	cloned := cloneAnalyzerConfig(original)

	// Mutate the original after cloning.
	original["top"].(map[string]any)["nested"] = "changed"
	original["list"].([]any)[0].(map[string]any)["item"] = 2
	original["list"] = append(original["list"].([]any), "extra")

	if cloned["top"].(map[string]any)["nested"] != "value" {
		t.Fatalf("nested map was not deep cloned: got %q", cloned["top"].(map[string]any)["nested"])
	}
	if cloned["list"].([]any)[0].(map[string]any)["item"] != 1 {
		t.Fatalf("nested list element was not deep cloned: got %v", cloned["list"].([]any)[0].(map[string]any)["item"])
	}
	if len(cloned["list"].([]any)) != 2 {
		t.Fatalf("cloned list length changed with original: got %d", len(cloned["list"].([]any)))
	}
}

func TestCloneAnalyzerConfigPreservesScalarTypes(t *testing.T) {
	cfg := map[string]any{
		"bool":   true,
		"int":    42,
		"uint":   uint(7),
		"float":  3.14,
		"string": "x",
		"nil":    nil,
	}
	cloned := cloneAnalyzerConfig(cfg)
	if !reflect.DeepEqual(map[string]any(cloned), cfg) {
		t.Fatalf("clone changed scalar values or types: got %+v, want %+v", cloned, cfg)
	}
}

func stringsRepeatZero(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '0'
	}
	return string(b)
}
