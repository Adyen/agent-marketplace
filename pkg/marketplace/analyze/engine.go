package analyze

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Engine runs an ordered set of analyzer registrations against a resource subject.
//
// A zero-value Engine has no analyzers; use NewEngine for the built-in suite
// or NewEngineWith to install an explicit ordered set.
type Engine struct {
	registrations []Registration
}

// NewEngine returns an Engine with the default built-in registration set.
func NewEngine() *Engine {
	return NewEngineWith(DefaultRegistrations())
}

// NewEngineWith returns an Engine with the supplied ordered registrations.
// An empty slice means zero analyzers.
func NewEngineWith(regs []Registration) *Engine {
	return &Engine{registrations: slices.Clone(regs)}
}

// Registrations returns the configured registrations in order.
func (e *Engine) Registrations() []Registration {
	return slices.Clone(e.registrations)
}

// Analyzers returns the analyzers from each registration in order.
func (e *Engine) Analyzers() []Analyzer {
	out := make([]Analyzer, 0, len(e.registrations))
	for _, r := range e.registrations {
		out = append(out, r.Analyzer)
	}
	return out
}

// Analyze runs every analyzer whose Kinds include the resource kind. It returns
// each per-audit result, the union of findings, and the derived decision.
// A resource kind with no applicable analyzer is valid and yields an allow.
func (e *Engine) Analyze(ctx context.Context, s Subject) (Report, error) {
	if s.Resource == nil {
		return Report{}, errors.New("nil resource")
	}
	if err := validateSubject(&s); err != nil {
		return Report{}, err
	}

	kind := s.Resource.Kind()

	var audits []Audit
	var allFindings []Finding
	start := time.Now()

	for _, reg := range e.registrations {
		if err := validateRegistration(reg); err != nil {
			return Report{}, err
		}
		if !appliesTo(kind, reg.Analyzer.Kinds()) {
			continue
		}

		auditStart := time.Now()
		slog.Info("analyzer start",
			"analyzer", reg.Analyzer.ID(),
			"version", reg.Analyzer.Version(),
			"role", reg.Role)

		audit, err := reg.Analyzer.Analyze(ctx, s, reg.Config)
		if err != nil {
			audit = errorAudit(reg, err)
		} else if audit.Error != "" || audit.Verdict == VerdictError {
			msg := audit.Error
			if msg == "" {
				msg = "external analyzer reported error"
			}
			audit = errorAudit(reg, errors.New(msg))
		} else if audit.Verdict != "" && !isValidVerdict(audit.Verdict) {
			audit = errorAudit(reg, fmt.Errorf("invalid analyzer verdict %q", audit.Verdict))
		} else {
			audit = finalizeAudit(audit, reg, s)
		}

		// Validate that the analyzer did not spoof another identity.
		audit.AnalyzerID = reg.Analyzer.ID()
		audit.AnalyzerVersion = reg.Analyzer.Version()
		audit.Role = reg.Role
		if audit.Duration == 0 && !auditStart.IsZero() {
			audit.Duration = time.Since(auditStart)
		}
		if audit.Summary == "" {
			audit.Summary = summaryForAudit(audit.Verdict, audit.Findings)
		}

		slog.Info("analyzer complete",
			"analyzer", audit.AnalyzerID,
			"version", audit.AnalyzerVersion,
			"role", audit.Role,
			"verdict", audit.Verdict,
			"findings", len(audit.Findings),
			"duration", time.Since(auditStart))

		if audit.Verdict == VerdictError {
			slog.Warn("analyzer returned error verdict",
				"analyzer", audit.AnalyzerID,
				"version", audit.AnalyzerVersion,
				"verdict", audit.Verdict)
		}

		audits = append(audits, audit)
		allFindings = append(allFindings, audit.Findings...)
	}

	decision := foldDecision(audits)
	report := Report{
		Audits:   audits,
		Findings: allFindings,
		Decision: decision,
		Rejected: decision == DecisionReject,
	}

	slog.Info("engine analyze complete",
		"kind", kind,
		"name", s.Resource.Name(),
		"audits", len(audits),
		"findings", len(allFindings),
		"decision", decision,
		"duration", time.Since(start))

	return report, nil
}

// finalizeAudit applies the registration policy to raw analyzer findings and
// derives the authoritative verdict. The analyzer's returned verdict is treated
// as a hint that policy can only strengthen.
func finalizeAudit(audit Audit, reg Registration, s Subject) Audit {
	id := reg.Analyzer.ID()
	ver := reg.Analyzer.Version()
	for i := range audit.Findings {
		audit.Findings[i].Analyzer = id
		audit.Findings[i].AnalyzerVersion = ver
	}
	findings := applyPolicy(audit.Findings, reg.Policy)
	if s.Limits.MaxFindings > 0 {
		findings = truncateFindings(findings, s.Limits.MaxFindings)
	}
	audit.Findings = findings
	audit.Verdict = combineVerdicts(audit.Verdict, verdictFromFindings(findings))
	return sanitizeAudit(audit)
}

// appliesTo reports whether kind is in kinds or if kinds is empty.
func appliesTo(kind ir.ResourceKind, kinds []ir.ResourceKind) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// isValidVerdict reports whether v is a recognized per-audit verdict.
func isValidVerdict(v string) bool {
	switch v {
	case VerdictPass, VerdictWarn, VerdictFail, VerdictError, VerdictSkipped:
		return true
	}
	return false
}

// errorAudit builds the audit for a failed analyzer invocation.
//
// A gating error creates a non-consentable marketplace.policy.audit-unavailable
// finding. An advisory error has VerdictError with a redacted reason and no
// policy finding, so it cannot block consent.
func errorAudit(reg Registration, err error) Audit {
	id := reg.Analyzer.ID()
	version := reg.Analyzer.Version()
	errMsg := redactError(err.Error())

	audit := Audit{
		AnalyzerID:      id,
		AnalyzerVersion: version,
		Role:            reg.Role,
		Verdict:         VerdictError,
		Summary:         "audit unavailable",
		Error:           errMsg,
	}

	if reg.Role == RoleGating {
		nonConsentable := false
		audit.Findings = []Finding{
			{
				Analyzer:        id,
				AnalyzerVersion: version,
				Code:            PolicyAuditUnavailable,
				Message:         fmt.Sprintf("analyzer %s could not complete", id),
				Consentable:     &nonConsentable,
			},
		}
		audit.Findings = applyPolicy(audit.Findings, reg.Policy)
	}

	return sanitizeAudit(audit)
}

// validateRegistration rejects a registration that would panic or silently
// corrupt fold behaviour.
func validateRegistration(reg Registration) error {
	if reg.Analyzer == nil {
		return errors.New("registration has nil analyzer")
	}
	if reg.Role != RoleGating && reg.Role != RoleAdvisory {
		return fmt.Errorf("registration for %s has invalid role %q", reg.Analyzer.ID(), reg.Role)
	}
	return nil
}

// foldDecision derives the rollup decision from the per-audit verdicts and roles.
//
// A gating fail rejects. Otherwise, a gating warn/error or any advisory
// fail/warn warns. Everything else, including advisory errors, allows.
func foldDecision(audits []Audit) string {
	for _, a := range audits {
		if a.Role == RoleGating && a.Verdict == VerdictFail {
			return DecisionReject
		}
	}

	hasWarn := false
	for _, a := range audits {
		if a.Verdict == VerdictWarn {
			hasWarn = true
			break
		}
		if a.Role == RoleGating && a.Verdict == VerdictError {
			hasWarn = true
			break
		}
		if a.Role == RoleAdvisory && (a.Verdict == VerdictFail || a.Verdict == VerdictWarn) {
			hasWarn = true
			break
		}
	}
	if hasWarn {
		return DecisionWarn
	}
	return DecisionAllow
}
