package ingest

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// licenseTestAllowlist is the allowlist every case below is judged against, so
// the asserted messages can quote it verbatim.
var licenseTestAllowlist = []string{"MIT", "Apache-2.0"}

func builtResult(license string) artifact.BuildResult {
	return artifact.BuildResult{
		Kind:          "skill",
		Name:          "review",
		License:       license,
		ContentDigest: "sha256:content",
		ArchiveDigest: "sha256:archive",
		RawArchive:    []byte("archive-bytes"),
	}
}

func policyLimits() config.Limits {
	return config.Limits{ArtifactBytes: 16 * 1024 * 1024}
}

func findingWithCode(findings []analyze.Finding, code string) (analyze.Finding, bool) {
	for _, f := range findings {
		if f.Code == code {
			return f, true
		}
	}
	return analyze.Finding{}, false
}

// An undeclared license and a disallowed license are different conditions, so
// they must produce different codes, different messages, and — where the
// operator asked for it — different outcomes.
func TestApplyPolicyToResultLicenseConditions(t *testing.T) {
	tests := []struct {
		name        string
		license     string
		allowlist   []string
		undeclared  string
		wantReject  bool
		wantCode    string
		wantSever   string
		wantMessage string
		wantNoCodes []string
	}{
		{
			name:        "declared and allowed license passes",
			license:     "MIT",
			allowlist:   licenseTestAllowlist,
			undeclared:  config.LicenseUndeclaredReject,
			wantNoCodes: []string{analyze.PolicyLicenseNotAllowed, analyze.PolicyLicenseUndeclared},
		},
		{
			name:        "declared but disallowed license is rejected and named",
			license:     "GPL-3.0",
			allowlist:   licenseTestAllowlist,
			undeclared:  config.LicenseUndeclaredReject,
			wantReject:  true,
			wantCode:    analyze.PolicyLicenseNotAllowed,
			wantSever:   analyze.OutcomeReject,
			wantMessage: `declared license "GPL-3.0" is not in the configured license allowlist (MIT, Apache-2.0)`,
			wantNoCodes: []string{analyze.PolicyLicenseUndeclared},
		},
		{
			name:        "undeclared license rejects under reject",
			license:     "",
			allowlist:   licenseTestAllowlist,
			undeclared:  config.LicenseUndeclaredReject,
			wantReject:  true,
			wantCode:    analyze.PolicyLicenseUndeclared,
			wantSever:   analyze.OutcomeReject,
			wantMessage: `resource declares no license, and policy.license_undeclared is "reject": it is not installable while the license allowlist (MIT, Apache-2.0) is configured`,
			wantNoCodes: []string{analyze.PolicyLicenseNotAllowed},
		},
		{
			name:        "undeclared license warns under warn",
			license:     "",
			allowlist:   licenseTestAllowlist,
			undeclared:  config.LicenseUndeclaredWarn,
			wantCode:    analyze.PolicyLicenseUndeclared,
			wantSever:   analyze.OutcomeWarn,
			wantMessage: `resource declares no license, and policy.license_undeclared is "warn": it stays installable but its license is unknown to the configured allowlist (MIT, Apache-2.0)`,
			wantNoCodes: []string{analyze.PolicyLicenseNotAllowed},
		},
		{
			name:        "undeclared license is silent under allow",
			license:     "",
			allowlist:   licenseTestAllowlist,
			undeclared:  config.LicenseUndeclaredAllow,
			wantNoCodes: []string{analyze.PolicyLicenseNotAllowed, analyze.PolicyLicenseUndeclared},
		},
		{
			name:        "whitespace-only license counts as undeclared",
			license:     "   ",
			allowlist:   licenseTestAllowlist,
			undeclared:  config.LicenseUndeclaredReject,
			wantReject:  true,
			wantCode:    analyze.PolicyLicenseUndeclared,
			wantSever:   analyze.OutcomeReject,
			wantNoCodes: []string{analyze.PolicyLicenseNotAllowed},
		},
		{
			name:        "no allowlist accepts an undeclared license",
			license:     "",
			allowlist:   nil,
			undeclared:  config.LicenseUndeclaredReject,
			wantNoCodes: []string{analyze.PolicyLicenseNotAllowed, analyze.PolicyLicenseUndeclared},
		},
		{
			name:        "no allowlist accepts any declared license",
			license:     "Proprietary. LICENSE.txt has complete terms",
			allowlist:   nil,
			undeclared:  config.LicenseUndeclaredReject,
			wantNoCodes: []string{analyze.PolicyLicenseNotAllowed, analyze.PolicyLicenseUndeclared},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lp := licensePolicy{allowlist: tc.allowlist, undeclared: tc.undeclared}
			got := applyPolicyToResult(builtResult(tc.license), store.SourceID("src-test"), policyLimits(), lp, store.AcquisitionMirror)

			if got.Rejected != tc.wantReject {
				t.Fatalf("Rejected = %v, want %v (findings %#v)", got.Rejected, tc.wantReject, got.Findings)
			}
			// A rejected resource must lose its installable artifact; anything
			// else must keep it.
			if tc.wantReject && (got.RawArchive != nil || got.ArchiveDigest != "") {
				t.Fatalf("rejected result kept its artifact: digest %q, %d archive bytes", got.ArchiveDigest, len(got.RawArchive))
			}
			if !tc.wantReject && (got.RawArchive == nil || got.ArchiveDigest == "") {
				t.Fatalf("accepted result lost its artifact: digest %q, %d archive bytes", got.ArchiveDigest, len(got.RawArchive))
			}

			for _, code := range tc.wantNoCodes {
				if f, ok := findingWithCode(got.Findings, code); ok {
					t.Errorf("unexpected finding %q: %+v", code, f)
				}
			}
			if tc.wantCode == "" {
				if len(got.Findings) != 0 {
					t.Fatalf("expected no findings, got %#v", got.Findings)
				}
				return
			}

			f, ok := findingWithCode(got.Findings, tc.wantCode)
			if !ok {
				t.Fatalf("finding %q not emitted; got %#v", tc.wantCode, got.Findings)
			}
			if f.Severity != tc.wantSever {
				t.Errorf("severity = %q, want %q", f.Severity, tc.wantSever)
			}
			if tc.wantMessage != "" && f.Message != tc.wantMessage {
				t.Errorf("message =\n  %q\nwant\n  %q", f.Message, tc.wantMessage)
			}
			if strings.Contains(f.Message, `""`) {
				t.Errorf("message quotes an empty license, which is what this change exists to remove: %q", f.Message)
			}
		})
	}
}

// Regression guard for the default: an operator config that never mentions
// license_undeclared must still reject an undeclared license exactly as it did
// before the option existed. This test must fail if the default is ever
// loosened to unblock a catalog.
func TestApplyPolicyToResultUndeclaredLicenseDefaultsToReject(t *testing.T) {
	// Policy as an existing deployment has it: an allowlist, and no knowledge
	// of the new field at all.
	policy := config.Policy{HiddenContent: "reject", LicenseAllowlist: licenseTestAllowlist}
	if policy.LicenseUndeclared != "" {
		t.Fatalf("test precondition: license_undeclared must be absent, got %q", policy.LicenseUndeclared)
	}

	lp := effectiveLicensePolicy(policy, nil)
	if lp.undeclared != config.LicenseUndeclaredReject {
		t.Fatalf("resolved undeclared outcome = %q, want %q", lp.undeclared, config.LicenseUndeclaredReject)
	}

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	got := applyPolicyToResult(builtResult(""), store.SourceID("src-test"), policyLimits(), lp, store.AcquisitionMirror)
	if !got.Rejected {
		t.Fatalf("undeclared license was not rejected by default: %#v", got)
	}
	if got.RawArchive != nil || got.ArchiveDigest != "" {
		t.Fatalf("default reject did not strip the artifact: digest %q, %d bytes", got.ArchiveDigest, len(got.RawArchive))
	}
	if _, ok := findingWithCode(got.Findings, analyze.PolicyLicenseUndeclared); !ok {
		t.Fatalf("expected %q finding, got %#v", analyze.PolicyLicenseUndeclared, got.Findings)
	}

	// The decision must be observable in the operator's logs, not only in the
	// stored revision.
	for _, want := range []string{
		"license policy rejected resource",
		"code=" + analyze.PolicyLicenseUndeclared,
		"license_declared=false",
		"outcome=" + analyze.OutcomeReject,
		"resource=review",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("policy log missing %q; got:\n%s", want, logs.String())
		}
	}
}

// A per-source policy override may relax or tighten the undeclared outcome, but
// an unrecognised value must never be honoured.
func TestEffectiveLicensePolicyOverrides(t *testing.T) {
	base := config.Policy{LicenseAllowlist: licenseTestAllowlist, LicenseUndeclared: config.LicenseUndeclaredWarn}

	tests := []struct {
		name           string
		overrides      map[string]any
		wantUndeclared string
		wantAllowlist  []string
	}{
		{name: "no overrides", overrides: nil, wantUndeclared: config.LicenseUndeclaredWarn, wantAllowlist: licenseTestAllowlist},
		{
			name:           "override relaxes undeclared and narrows allowlist",
			overrides:      map[string]any{"license_undeclared": "allow", "license_allowlist": []any{"MIT"}},
			wantUndeclared: config.LicenseUndeclaredAllow,
			wantAllowlist:  []string{"MIT"},
		},
		{
			name:           "override tightens undeclared",
			overrides:      map[string]any{"license_undeclared": "reject"},
			wantUndeclared: config.LicenseUndeclaredReject,
			wantAllowlist:  licenseTestAllowlist,
		},
		{
			name:           "unrecognised override keeps the global value",
			overrides:      map[string]any{"license_undeclared": "permit"},
			wantUndeclared: config.LicenseUndeclaredWarn,
			wantAllowlist:  licenseTestAllowlist,
		},
		{
			name:           "non-string override keeps the global value",
			overrides:      map[string]any{"license_undeclared": true},
			wantUndeclared: config.LicenseUndeclaredWarn,
			wantAllowlist:  licenseTestAllowlist,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := effectiveLicensePolicy(base, tc.overrides)
			if got.undeclared != tc.wantUndeclared {
				t.Errorf("undeclared = %q, want %q", got.undeclared, tc.wantUndeclared)
			}
			if strings.Join(got.allowlist, ",") != strings.Join(tc.wantAllowlist, ",") {
				t.Errorf("allowlist = %v, want %v", got.allowlist, tc.wantAllowlist)
			}
		})
	}
}

func unlicensedSource(repoDir string) store.Source {
	return store.Source{
		ID:          store.SourceID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		// No source-level license either: nothing in the chain declares one.
		Roots: []store.SourceRoot{{Path: ".agents", SourceHarness: "factory"}},
	}
}

// End-to-end counterpart of the default guard: a real catalog resource that
// declares no license is still blocked, and the reason it reports is the
// undeclared one rather than the misleading disallowed one.
func TestBuildSourceRevisionUndeclaredLicenseRejectedByDefault(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepoWithSkillLicense(t, "")
	commit := mustRepoCommit(t, repoDir)

	cfg := testConfig(t, repoDir)
	if cfg.Policy.LicenseUndeclared != "" {
		t.Fatalf("test precondition: license_undeclared must be absent, got %q", cfg.Policy.LicenseUndeclared)
	}

	rev, err := BuildSourceRevision(ctx, cfg, store.MarketplaceID("mp-test"), unlicensedSource(repoDir), repoDir, commit)
	if err != nil {
		t.Fatalf("BuildSourceRevision: %v", err)
	}

	skill := resourceRevisionFor(t, rev.Resources, "skill")
	if skill.ArchiveDigest != "" {
		t.Fatalf("undeclared license should not stay installable: archive digest %q", skill.ArchiveDigest)
	}
	var codes []string
	for _, f := range skill.Findings {
		codes = append(codes, f.Code)
		if f.Code == analyze.PolicyLicenseNotAllowed {
			t.Errorf("undeclared license reported as disallowed: %+v", f)
		}
		if f.Code == analyze.PolicyLicenseUndeclared {
			if f.Severity != analyze.OutcomeReject {
				t.Errorf("severity = %q, want %q", f.Severity, analyze.OutcomeReject)
			}
			if !strings.Contains(f.Message, "declares no license") {
				t.Errorf("message does not say the resource declares nothing: %q", f.Message)
			}
		}
	}
	if !slices.Contains(codes, analyze.PolicyLicenseUndeclared) {
		t.Fatalf("expected %q finding, got codes %v", analyze.PolicyLicenseUndeclared, codes)
	}
}

// With warn, the same resource stays installable and the unknown license is
// recorded rather than enforced.
func TestBuildSourceRevisionUndeclaredLicenseWarnStaysInstallable(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepoWithSkillLicense(t, "")
	commit := mustRepoCommit(t, repoDir)

	cfg := testConfig(t, repoDir)
	cfg.Policy.LicenseUndeclared = config.LicenseUndeclaredWarn

	rev, err := BuildSourceRevision(ctx, cfg, store.MarketplaceID("mp-test"), unlicensedSource(repoDir), repoDir, commit)
	if err != nil {
		t.Fatalf("BuildSourceRevision: %v", err)
	}

	skill := resourceRevisionFor(t, rev.Resources, "skill")
	if skill.ArchiveDigest == "" {
		t.Fatalf("warn must keep the resource installable, but the artifact was stripped")
	}
	var found bool
	for _, f := range skill.Findings {
		if f.Code == analyze.PolicyLicenseUndeclared {
			found = true
			if f.Severity != analyze.OutcomeWarn {
				t.Errorf("severity = %q, want %q", f.Severity, analyze.OutcomeWarn)
			}
		}
		if f.Severity == analyze.OutcomeReject {
			t.Errorf("warn must not produce a reject-severity finding: %+v", f)
		}
	}
	if !found {
		t.Fatalf("expected a %q finding, got %#v", analyze.PolicyLicenseUndeclared, skill.Findings)
	}
}

// With allow, nothing is recorded at all: the operator has already decided.
func TestBuildSourceRevisionUndeclaredLicenseAllowRecordsNothing(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepoWithSkillLicense(t, "")
	commit := mustRepoCommit(t, repoDir)

	cfg := testConfig(t, repoDir)
	cfg.Policy.LicenseUndeclared = config.LicenseUndeclaredAllow

	rev, err := BuildSourceRevision(ctx, cfg, store.MarketplaceID("mp-test"), unlicensedSource(repoDir), repoDir, commit)
	if err != nil {
		t.Fatalf("BuildSourceRevision: %v", err)
	}

	skill := resourceRevisionFor(t, rev.Resources, "skill")
	if skill.ArchiveDigest == "" {
		t.Fatalf("allow must keep the resource installable, but the artifact was stripped")
	}
	for _, f := range skill.Findings {
		if f.Code == analyze.PolicyLicenseUndeclared || f.Code == analyze.PolicyLicenseNotAllowed {
			t.Errorf("allow must record no license finding: %+v", f)
		}
	}
}

// Without an allowlist there is no license policy to enforce, so an undeclared
// license is neither rejected nor reported.
func TestBuildSourceRevisionNoAllowlistDoesNotEnforceLicense(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepoWithSkillLicense(t, "")
	commit := mustRepoCommit(t, repoDir)

	cfg := testConfig(t, repoDir)
	cfg.Policy.LicenseAllowlist = nil

	rev, err := BuildSourceRevision(ctx, cfg, store.MarketplaceID("mp-test"), unlicensedSource(repoDir), repoDir, commit)
	if err != nil {
		t.Fatalf("BuildSourceRevision: %v", err)
	}

	skill := resourceRevisionFor(t, rev.Resources, "skill")
	if skill.ArchiveDigest == "" {
		t.Fatalf("no allowlist must not strip the artifact")
	}
	for _, f := range skill.Findings {
		if f.Code == analyze.PolicyLicenseUndeclared || f.Code == analyze.PolicyLicenseNotAllowed {
			t.Errorf("no allowlist must produce no license finding: %+v", f)
		}
	}
}
