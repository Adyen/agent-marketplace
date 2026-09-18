package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
)

const maxAnalyzerVersionBytes = 256

var (
	analyzerIDRE = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
)

// AuditDigest is the content-addressed identity of a resource audit result.
type AuditDigest string

func (d AuditDigest) String() string { return string(d) }

// ResourceAuditKey is the logical cache identity of an analyzer run.
type ResourceAuditKey struct {
	Coordinate      ResourceCoordinate
	SubjectDigest   RevisionDigest
	AnalyzerID      string
	AnalyzerVersion string
	ConfigDigest    PolicyDigest
}

// ResourceAudit is one immutable analyzer result for a resource revision.
type ResourceAudit struct {
	ResourceAuditKey
	ResultDigest AuditDigest
	Role         string
	Verdict      string
	Summary      string
	Findings     []Finding
	Duration     time.Duration
	Error        string
	AssessedAt   time.Time
}

// AssessmentAuditLink connects an assessment to an ordered audit result.
type AssessmentAuditLink struct {
	AssessmentDigest AssessmentDigest
	Ordinal          int
	AuditKey         ResourceAuditKey
	ResultDigest     AuditDigest
}

// FinalizeResourceAudit validates an audit's logical key and result content,
// clones its mutable findings/locations, and returns a value with its canonical
// result digest populated. A supplied ResultDigest must already match.
func FinalizeResourceAudit(audit ResourceAudit) (ResourceAudit, error) {
	if err := audit.ResourceAuditKey.Validate(); err != nil {
		return ResourceAudit{}, err
	}

	// Clone before hashing or returning so caller mutation cannot change the
	// canonical result or the stored value.
	audit.Findings = append([]Finding(nil), audit.Findings...)
	for i := range audit.Findings {
		f, err := validateFinding(audit.Findings[i], i)
		if err != nil {
			return ResourceAudit{}, err
		}
		audit.Findings[i] = f
	}

	if err := validateResourceAuditContent(audit); err != nil {
		return ResourceAudit{}, err
	}

	digest, err := resourceAuditResultDigest(audit)
	if err != nil {
		return ResourceAudit{}, err
	}
	if audit.ResultDigest != "" {
		if err := ValidateSHA256Digest("result digest", string(audit.ResultDigest)); err != nil {
			return ResourceAudit{}, err
		}
		if audit.ResultDigest != digest {
			return ResourceAudit{}, fmt.Errorf("result digest does not match canonical identity")
		}
	}
	audit.ResultDigest = digest
	return audit, nil
}

// Validate checks all required audit fields and verifies that ResultDigest
// matches the audit's canonical content identity.
func (a ResourceAudit) Validate() error {
	if a.ResultDigest == "" {
		return fmt.Errorf("result digest is required")
	}
	_, err := FinalizeResourceAudit(a)
	return err
}

// Equal reports whether two audits have the same immutable fields. AssessedAt
// is intentionally excluded so an exact retry at a later time is idempotent.
func (a ResourceAudit) Equal(other ResourceAudit) bool {
	a.AssessedAt = other.AssessedAt
	return reflect.DeepEqual(a, other)
}

// Validate checks the logical cache key fields.
func (k ResourceAuditKey) Validate() error {
	if err := k.Coordinate.Validate(); err != nil {
		return err
	}
	if err := ValidateSHA256Digest("subject digest", string(k.SubjectDigest)); err != nil {
		return err
	}
	if err := ValidateSHA256Digest("config digest", string(k.ConfigDigest)); err != nil {
		return err
	}
	if err := ValidateAnalyzerIdentifier(k.AnalyzerID); err != nil {
		return err
	}
	if err := ValidateAnalyzerVersion(k.AnalyzerVersion); err != nil {
		return err
	}
	return nil
}

// Validate checks the assessment-audit link fields.
func (l AssessmentAuditLink) Validate() error {
	if err := ValidateSHA256Digest("assessment digest", string(l.AssessmentDigest)); err != nil {
		return err
	}
	if l.Ordinal < 0 {
		return fmt.Errorf("ordinal must not be negative")
	}
	if err := l.AuditKey.Validate(); err != nil {
		return err
	}
	if err := ValidateSHA256Digest("result digest", string(l.ResultDigest)); err != nil {
		return err
	}
	return nil
}

// Equal reports whether two assessment audit links have the same identity and
// referenced audit.
func (l AssessmentAuditLink) Equal(other AssessmentAuditLink) bool {
	return reflect.DeepEqual(l, other)
}

type resourceAuditResultIdentity struct {
	Coordinate      ResourceCoordinate `json:"coordinate"`
	SubjectDigest   RevisionDigest     `json:"subject_digest"`
	AnalyzerID      string             `json:"analyzer_id"`
	AnalyzerVersion string             `json:"analyzer_version"`
	ConfigDigest    PolicyDigest       `json:"config_digest"`
	Role            string             `json:"role"`
	Verdict         string             `json:"verdict"`
	Summary         string             `json:"summary"`
	Findings        []Finding          `json:"findings"`
	Duration        int64              `json:"duration_ns"`
	Error           string             `json:"error"`
}

func resourceAuditResultDigest(audit ResourceAudit) (AuditDigest, error) {
	canonical, err := json.Marshal(resourceAuditResultIdentity{
		Coordinate:      audit.Coordinate,
		SubjectDigest:   audit.SubjectDigest,
		AnalyzerID:      audit.AnalyzerID,
		AnalyzerVersion: audit.AnalyzerVersion,
		ConfigDigest:    audit.ConfigDigest,
		Role:            audit.Role,
		Verdict:         audit.Verdict,
		Summary:         audit.Summary,
		Findings:        audit.Findings,
		Duration:        int64(audit.Duration),
		Error:           audit.Error,
	})
	if err != nil {
		return "", fmt.Errorf("marshal canonical resource audit: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return AuditDigest("sha256:" + hex.EncodeToString(sum[:])), nil
}

func validateResourceAuditContent(audit ResourceAudit) error {
	if audit.Role != analyze.RoleGating && audit.Role != analyze.RoleAdvisory {
		return fmt.Errorf("invalid audit role")
	}
	switch audit.Verdict {
	case analyze.VerdictPass, analyze.VerdictWarn, analyze.VerdictFail, analyze.VerdictError, analyze.VerdictSkipped:
	default:
		return fmt.Errorf("invalid audit verdict")
	}
	if audit.Duration < 0 {
		return fmt.Errorf("duration must not be negative")
	}
	if strings.TrimSpace(audit.Summary) == "" {
		return fmt.Errorf("summary is required")
	}
	if err := validateAuditString(audit.Summary, analyze.MaxAuditSummaryBytes, "summary"); err != nil {
		return err
	}
	switch audit.Verdict {
	case analyze.VerdictError:
		if strings.TrimSpace(audit.Error) == "" {
			return fmt.Errorf("error is required for verdict error")
		}
		if err := validateAuditString(audit.Error, analyze.MaxAuditErrorBytes, "error"); err != nil {
			return err
		}
	default:
		if audit.Error != "" {
			return fmt.Errorf("error is only allowed for verdict error")
		}
	}
	if audit.AssessedAt.IsZero() {
		return fmt.Errorf("assessed at is required")
	}
	return nil
}

func validateAuditString(s string, max int, name string) error {
	if got := analyze.SanitizeString(s, max); got != s {
		return fmt.Errorf("%s is not canonical bounded text", name)
	}
	return nil
}

// validateFinding validates and canonicalizes one finding. It deep-clones the
// Consentable pointer and the Location pointer so the returned value is safe
// from caller mutation, and it validates the optional AnalyzerID, message
// bounds, path bounds/control/UTF-8, and location ranges.
func validateFinding(f Finding, i int) (Finding, error) {
	if f.Code == "" || f.Severity == "" {
		return Finding{}, fmt.Errorf("finding %d requires code and severity", i)
	}
	if f.AnalyzerID != "" {
		if err := ValidateAnalyzerIdentifier(f.AnalyzerID); err != nil {
			return Finding{}, fmt.Errorf("finding %d: %w", i, err)
		}
	}
	consentable, err := analyze.ValidateConsentable(f.Consentable)
	if err != nil {
		return Finding{}, fmt.Errorf("finding %d: %w", i, err)
	}
	if err := validateAuditString(f.Message, analyze.MaxFindingMessageBytes, "finding message"); err != nil {
		return Finding{}, fmt.Errorf("finding %d: %w", i, err)
	}
	if f.Location != nil {
		if err := validateAuditString(f.Location.Path, analyze.MaxSourceFileBytes, "finding location path"); err != nil {
			return Finding{}, fmt.Errorf("finding %d: %w", i, err)
		}
		loc := *f.Location
		if loc.Path == "" || loc.Line < 0 || loc.Column < 0 || loc.StartByte < 0 || loc.EndByte < loc.StartByte {
			return Finding{}, fmt.Errorf("finding %d has invalid location", i)
		}
		f.Location = &loc
	}
	f.Consentable = consentable
	return f, nil
}

func ValidateAnalyzerIdentifier(id string) error {
	if id == "" {
		return fmt.Errorf("analyzer id is required")
	}
	if id != strings.TrimSpace(id) {
		return fmt.Errorf("analyzer id must not have leading or trailing whitespace")
	}
	if !analyzerIDRE.MatchString(id) {
		return fmt.Errorf("analyzer id is not a safe dotted lower-case identifier")
	}
	return nil
}

func ValidateAnalyzerVersion(version string) error {
	if version == "" {
		return fmt.Errorf("analyzer version is required")
	}
	if version != strings.TrimSpace(version) {
		return fmt.Errorf("analyzer version must not have leading or trailing whitespace")
	}
	if !utf8.ValidString(version) {
		return fmt.Errorf("analyzer version is not valid UTF-8")
	}
	for _, r := range version {
		if unicode.IsControl(r) {
			return fmt.Errorf("analyzer version contains control characters")
		}
	}
	if len(version) > maxAnalyzerVersionBytes {
		return fmt.Errorf("analyzer version exceeds %d bytes", maxAnalyzerVersionBytes)
	}
	return nil
}

// FindingToAnalyze converts a stored finding back to the analyzer model.
func FindingToAnalyze(f Finding) analyze.Finding {
	out := analyze.Finding{
		Analyzer:      f.AnalyzerID,
		Code:          f.Code,
		Severity:      f.Severity,
		PolicyOutcome: f.Policy,
		Message:       f.Message,
		Consentable:   analyze.NormalizeConsentable(f.Consentable),
	}
	if f.Location != nil {
		out.SourceRange = &analyze.SourceRange{
			File:   f.Location.Path,
			Line:   f.Location.Line,
			Column: f.Location.Column,
		}
	}
	return out
}

// ResourceAuditFromMappedFindings builds a finalized ResourceAudit from an analyzer
// audit, its logical cache key, and an already-mapped store finding slice. This lets
// ingest compute inspection byte ranges before producing the immutable audit.
func ResourceAuditFromMappedFindings(a analyze.Audit, key ResourceAuditKey, assessedAt time.Time, findings []Finding) (ResourceAudit, error) {
	return FinalizeResourceAudit(ResourceAudit{
		ResourceAuditKey: key,
		Role:             a.Role,
		Verdict:          a.Verdict,
		Summary:          a.Summary,
		Findings:         append([]Finding(nil), findings...),
		Duration:         a.Duration,
		Error:            a.Error,
		AssessedAt:       assessedAt,
	})
}

// ResourceAuditToAnalyze converts a stored resource audit back to the analyzer
// model so cached results can participate in report folding.
func ResourceAuditToAnalyze(a ResourceAudit) analyze.Audit {
	findings := make([]analyze.Finding, len(a.Findings))
	for i, f := range a.Findings {
		findings[i] = FindingToAnalyze(f)
	}
	return analyze.Audit{
		AnalyzerID:      a.AnalyzerID,
		AnalyzerVersion: a.AnalyzerVersion,
		Role:            a.Role,
		Verdict:         a.Verdict,
		Summary:         a.Summary,
		Findings:        findings,
		Duration:        a.Duration,
		Error:           a.Error,
	}
}

// NewResourceAuditSummary maps a stored resource audit to its public summary.
// It validates the full underlying audit before projecting the summary fields.
func NewResourceAuditSummary(a ResourceAudit) (ResourceAuditSummary, error) {
	if err := a.Validate(); err != nil {
		return ResourceAuditSummary{}, err
	}
	s := ResourceAuditSummary{
		AnalyzerID:      a.AnalyzerID,
		AnalyzerVersion: a.AnalyzerVersion,
		Role:            a.Role,
		Verdict:         a.Verdict,
		Summary:         a.Summary,
		FindingCount:    len(a.Findings),
		DurationNS:      a.Duration.Nanoseconds(),
		ResultDigest:    a.ResultDigest,
	}
	if err := s.Validate(); err != nil {
		return ResourceAuditSummary{}, err
	}
	return s, nil
}

// Validate checks that the public audit summary is well-formed and bounded.
func (s ResourceAuditSummary) Validate() error {
	if err := ValidateAnalyzerIdentifier(s.AnalyzerID); err != nil {
		return err
	}
	if err := ValidateAnalyzerVersion(s.AnalyzerVersion); err != nil {
		return err
	}
	if s.Role != analyze.RoleGating && s.Role != analyze.RoleAdvisory {
		return fmt.Errorf("invalid audit role %q", s.Role)
	}
	switch s.Verdict {
	case analyze.VerdictPass, analyze.VerdictWarn, analyze.VerdictFail, analyze.VerdictError, analyze.VerdictSkipped:
	default:
		return fmt.Errorf("invalid audit verdict %q", s.Verdict)
	}
	if strings.TrimSpace(s.Summary) == "" {
		return fmt.Errorf("summary is required")
	}
	if err := validateAuditString(s.Summary, analyze.MaxAuditSummaryBytes, "summary"); err != nil {
		return err
	}
	if s.FindingCount < 0 {
		return fmt.Errorf("finding count must not be negative")
	}
	if s.DurationNS < 0 {
		return fmt.Errorf("duration must not be negative")
	}
	if s.ResultDigest == "" {
		return fmt.Errorf("result digest is required")
	}
	if err := ValidateSHA256Digest("result digest", string(s.ResultDigest)); err != nil {
		return err
	}
	return nil
}
