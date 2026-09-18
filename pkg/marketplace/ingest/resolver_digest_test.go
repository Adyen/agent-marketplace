package ingest

import (
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestBuiltinConfigDigestIsolatesChanges(t *testing.T) {
	hidden := analyze.AnalyzerHiddenContent
	obfuscation := analyze.AnalyzerObfuscation

	basePolicy := config.Policy{
		Analyzers: []config.Analyzer{
			{ID: hidden},
			{ID: obfuscation},
		},
	}

	resolve := func(policy config.Policy, effective analyze.Policy) (map[string]store.PolicyDigest, string) {
		set, err := resolveAnalyzersFS(policy, effective, testLimits(), fakeFS{})
		if err != nil {
			t.Fatalf("resolveAnalyzersFS: %v", err)
		}
		digests := make(map[string]store.PolicyDigest, len(set.Entries))
		for _, e := range set.Entries {
			digests[e.Registration.Analyzer.ID()] = e.ConfigDigest
		}
		return digests, set.AuditSetVersion
	}

	base, _ := resolve(basePolicy, analyze.PublicPolicy())

	t.Run("role change alters only hidden-content digest", func(t *testing.T) {
		changedPolicy := config.Policy{
			Analyzers: []config.Analyzer{
				{ID: hidden, Role: analyze.RoleAdvisory},
				{ID: obfuscation},
			},
		}
		got, _ := resolve(changedPolicy, analyze.PublicPolicy())
		if got[hidden] == base[hidden] {
			t.Fatalf("role change did not alter %s config digest", hidden)
		}
		if got[obfuscation] != base[obfuscation] {
			t.Fatalf("role change altered %s config digest unexpectedly", obfuscation)
		}
	})

	t.Run("config change alters only hidden-content digest", func(t *testing.T) {
		changedPolicy := config.Policy{
			Analyzers: []config.Analyzer{
				{ID: hidden, Config: map[string]any{"threshold": 0.5}},
				{ID: obfuscation},
			},
		}
		got, _ := resolve(changedPolicy, analyze.PublicPolicy())
		if got[hidden] == base[hidden] {
			t.Fatalf("config change did not alter %s config digest", hidden)
		}
		if got[obfuscation] != base[obfuscation] {
			t.Fatalf("config change altered %s config digest unexpectedly", obfuscation)
		}
	})

	t.Run("relevant policy change alters only hidden-content digest", func(t *testing.T) {
		effective := analyze.Policy{
			Outcomes: map[string]string{
				analyze.FindingHTMLComment: analyze.OutcomeReject,
			},
		}
		got, _ := resolve(basePolicy, effective)
		if got[hidden] == base[hidden] {
			t.Fatalf("relevant policy change did not alter %s config digest", hidden)
		}
		if got[obfuscation] != base[obfuscation] {
			t.Fatalf("relevant policy change altered %s config digest unexpectedly", obfuscation)
		}
	})

	t.Run("irrelevant policy change alters no builtin digest", func(t *testing.T) {
		effective := analyze.Policy{
			Outcomes: map[string]string{
				analyze.FindingAssetPathEscape: analyze.OutcomeReject,
			},
		}
		got, _ := resolve(basePolicy, effective)
		if got[hidden] != base[hidden] {
			t.Fatalf("irrelevant policy change altered %s config digest", hidden)
		}
		if got[obfuscation] != base[obfuscation] {
			t.Fatalf("irrelevant policy change altered %s config digest unexpectedly", obfuscation)
		}
	})
}

func TestRunnerConfigDigestSensitivity(t *testing.T) {
	base := analyze.RunnerConfig{
		Command:              "/bin/sh",
		Args:                 []string{"-c", "echo ok"},
		Timeout:              30 * time.Second,
		EnvPassthrough:       []string{"PATH"},
		ImplementationDigest: "sha256:" + strings.Repeat("0", 64),
		MaxOutputBytes:       1 << 20,
	}

	baseDigest, err := runnerConfigDigest("acme.runner", "1.0.0", analyze.RoleGating, nil, base)
	if err != nil {
		t.Fatalf("runnerConfigDigest: %v", err)
	}

	cases := []struct {
		name   string
		ver    string
		role   string
		opaque map[string]any
		rc     analyze.RunnerConfig
	}{
		{
			name: "version",
			ver:  "1.0.1",
			rc:   base,
		},
		{
			name: "role",
			role: analyze.RoleAdvisory,
			rc:   base,
		},
		{
			name:   "opaque config",
			opaque: map[string]any{"level": "strict"},
			rc:     base,
		},
		{
			name: "implementation digest",
			rc: withRunner(base, func(c *analyze.RunnerConfig) {
				c.ImplementationDigest = "sha256:" + strings.Repeat("1", 64)
			}),
		},
	}

	for _, tc := range cases {
		version := tc.ver
		if version == "" {
			version = "1.0.0"
		}
		role := tc.role
		if role == "" {
			role = analyze.RoleGating
		}

		digest, err := runnerConfigDigest("acme.runner", version, role, tc.opaque, tc.rc)
		if err != nil {
			t.Fatalf("runnerConfigDigest %s: %v", tc.name, err)
		}
		if digest == baseDigest {
			t.Fatalf("%s change did not alter runner config digest", tc.name)
		}
	}
}

func withRunner(base analyze.RunnerConfig, fn func(*analyze.RunnerConfig)) analyze.RunnerConfig {
	c := base
	fn(&c)
	return c
}

func TestRulepackBytesChangeDigest(t *testing.T) {
	baseYAML := []byte(`apiVersion: agent-marketplace.analyzer/v1
rules:
  - code: secret
    pattern: "password"
    message: "found password"
`)
	changedYAML := []byte(`apiVersion: agent-marketplace.analyzer/v1
rules:
  - code: secret
    pattern: "passwort"
    message: "found password"
`)

	baseDigest, err := rulepackConfigDigest(
		rulepackID,
		analyze.AnalyzerRulepackVersionV1,
		analyze.RoleGating,
		nil,
		sha256Hex(baseYAML),
		[]string{"secret"},
		analyze.PublicPolicy(),
	)
	if err != nil {
		t.Fatalf("rulepackConfigDigest base: %v", err)
	}

	changedDigest, err := rulepackConfigDigest(
		rulepackID,
		analyze.AnalyzerRulepackVersionV1,
		analyze.RoleGating,
		nil,
		sha256Hex(changedYAML),
		[]string{"secret"},
		analyze.PublicPolicy(),
	)
	if err != nil {
		t.Fatalf("rulepackConfigDigest changed: %v", err)
	}

	if baseDigest == changedDigest {
		t.Fatal("rulepack bytes change did not alter config digest")
	}
}

func TestAuditSetVersionStableAndResourcePolicyOrderSensitive(t *testing.T) {
	a := resolvedAnalyzer{
		Registration: analyze.Registration{
			Analyzer: &countingAnalyzer{id: "marketplace.hidden-content", version: "1.2.0"},
			Role:     analyze.RoleGating,
		},
		ConfigDigest: store.PolicyDigest("sha256:" + strings.Repeat("a", 64)),
	}
	b := resolvedAnalyzer{
		Registration: analyze.Registration{
			Analyzer: &countingAnalyzer{id: "acme.runner", version: "1.0.0"},
			Role:     analyze.RoleAdvisory,
		},
		ConfigDigest: store.PolicyDigest("sha256:" + strings.Repeat("b", 64)),
	}

	forward := resolvedAnalyzerSet{Entries: []resolvedAnalyzer{a, b}}
	reversed := resolvedAnalyzerSet{Entries: []resolvedAnalyzer{b, a}}

	if v1, v2 := computeAuditSetVersion(forward.Entries), computeAuditSetVersion(reversed.Entries); v1 != v2 {
		t.Fatalf("AuditSetVersion changed with configured order: %q vs %q", v1, v2)
	}

	license := licensePolicy{allowlist: nil, undeclared: analyze.OutcomeAllow}
	d1, err := resourcePolicyDigest(analyze.PublicPolicy(), testLimits(), license, forward)
	if err != nil {
		t.Fatalf("resourcePolicyDigest forward: %v", err)
	}
	d2, err := resourcePolicyDigest(analyze.PublicPolicy(), testLimits(), license, reversed)
	if err != nil {
		t.Fatalf("resourcePolicyDigest reversed: %v", err)
	}
	if d1 == d2 {
		t.Fatal("resourcePolicyDigest did not change with configured order")
	}
}
