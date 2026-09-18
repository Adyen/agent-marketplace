package analyze

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// FileSet is a set of supporting files keyed by relative path.
type FileSet map[string][]byte

// Registry records where the analyzed content came from.
type Registry struct {
	RegistryURL string
	Ref         string
	Path        string
	ID          string
	Slug        string
	License     string
}

// Limits cap analyzer inputs so hostile archives cannot exhaust memory.
type Limits struct {
	MaxFileBytes  int64
	MaxTotalBytes int64
	MaxFiles      int
	MaxFindings   int
}

// DefaultLimits returns conservative analyzer input limits.
func DefaultLimits() Limits {
	return Limits{
		MaxFileBytes:  2 * 1024 * 1024,
		MaxTotalBytes: 16 * 1024 * 1024,
		MaxFiles:      256,
		MaxFindings:   1000,
	}
}

// Subject is the immutable content one analyzer inspects.
//
// The Engine precomputes public text samples once per call and stores them in
// the unexported samples field so that built-ins and the runner do not repeat
// the same extraction.
type Subject struct {
	Resource ir.Resource
	Files    FileSet
	Registry Registry
	Limits   Limits
	samples  []textSample
}

// AnalyzerConfig is an analyzer's opaque configuration.
type AnalyzerConfig map[string]any

// Audit is one analyzer's verdict.
type Audit struct {
	AnalyzerID      string
	AnalyzerVersion string
	Role            string
	Verdict         string
	Summary         string
	Findings        []Finding
	Duration        time.Duration
	Error           string
}

// Analyzer is one independently identified, versioned, and configured security
// opinion about a resource revision.
type Analyzer interface {
	ID() string
	Version() string
	Kinds() []ir.ResourceKind
	Analyze(context.Context, Subject, AnalyzerConfig) (Audit, error)
}

// Report is the rollup from one Engine.Analyze run.
type Report struct {
	Audits   []Audit
	Findings []Finding
	Decision string
	Rejected bool
}

// applyPolicy sets Severity and PolicyOutcome on each finding using p.
func applyPolicy(findings []Finding, p Policy) []Finding {
	for i := range findings {
		outcome := p.OutcomeFor(findings[i].Code)
		findings[i].PolicyOutcome = outcome
		switch outcome {
		case OutcomeReject:
			findings[i].Severity = OutcomeReject
		case OutcomeWarn:
			findings[i].Severity = OutcomeWarn
		default:
			findings[i].Severity = "info"
		}
	}
	return findings
}

// verdictFromFindings derives an audit verdict from applied policy outcomes.
func verdictFromFindings(findings []Finding) string {
	hasWarn := false
	for _, f := range findings {
		switch f.PolicyOutcome {
		case OutcomeReject:
			return VerdictFail
		case OutcomeWarn:
			hasWarn = true
		}
	}
	if hasWarn {
		return VerdictWarn
	}
	return VerdictPass
}

// summaryForAudit produces a compact one-sentence summary.
func summaryForAudit(verdict string, findings []Finding) string {
	switch verdict {
	case VerdictPass:
		return "no concerning findings"
	case VerdictWarn:
		return fmt.Sprintf("%d warning-level findings", len(findings))
	case VerdictFail:
		return fmt.Sprintf("%d rejecting findings", len(findings))
	case VerdictError:
		return "audit unavailable"
	default:
		return "audit skipped"
	}
}

// truncateFindings enforces MaxFindings while never discarding a reject
// finding. It keeps all reject findings, then fills the remaining budget with
// warn and info findings in their original stable order.
func truncateFindings(findings []Finding, max int) []Finding {
	if max <= 0 || len(findings) <= max {
		return findings
	}

	var reject, warn, info []Finding
	for _, f := range findings {
		switch f.Severity {
		case OutcomeReject:
			reject = append(reject, f)
		case OutcomeWarn:
			warn = append(warn, f)
		default:
			info = append(info, f)
		}
	}

	if len(reject) >= max {
		slog.Debug("truncated findings: kept all reject findings", "kept", len(reject), "max", max, "analyzer", findings[0].Analyzer)
		return reject
	}

	result := make([]Finding, 0, max)
	result = append(result, reject...)
	remaining := max - len(reject)

	n := len(warn)
	if n > remaining {
		n = remaining
	}
	if n > 0 {
		result = append(result, warn[:n]...)
		remaining -= n
	}

	n = len(info)
	if n > remaining {
		n = remaining
	}
	if n > 0 {
		result = append(result, info[:n]...)
	}

	slog.Debug("truncated findings", "kept", len(result), "dropped", len(findings)-len(result), "max", max)
	return result
}

// validateSubject checks that the subject is within configured limits. It
// computes public text samples if they are not already set.
func validateSubject(s *Subject) error {
	if s.Resource == nil {
		return errors.New("nil resource")
	}
	if len(s.samples) == 0 {
		s.samples = allTextSamples(s.Resource, s.Files)
	}

	var total int64
	for _, sample := range s.samples {
		if sample.isFile {
			continue
		}
		sz := int64(len(sample.content))
		if s.Limits.MaxFileBytes > 0 && sz > s.Limits.MaxFileBytes {
			return fmt.Errorf("primary sample %s exceeds MaxFileBytes limit %d", sample.path, s.Limits.MaxFileBytes)
		}
		total += sz
	}

	if s.Limits.MaxFiles > 0 && len(s.Files) > s.Limits.MaxFiles {
		return fmt.Errorf("file set exceeds MaxFiles limit %d", s.Limits.MaxFiles)
	}
	for path, data := range s.Files {
		sz := int64(len(data))
		if s.Limits.MaxFileBytes > 0 && sz > s.Limits.MaxFileBytes {
			return fmt.Errorf("support file %s exceeds MaxFileBytes limit %d", path, s.Limits.MaxFileBytes)
		}
		total += sz
	}

	if s.Limits.MaxTotalBytes > 0 && total > s.Limits.MaxTotalBytes {
		return fmt.Errorf("input exceeds MaxTotalBytes limit %d", s.Limits.MaxTotalBytes)
	}
	return nil
}

// isValidText reports whether s is valid UTF-8. Binary assets are rejected from
// text scanners to avoid false positives from arbitrary bytes.
func isValidText(s string) bool {
	return utf8.ValidString(s)
}

// textSample is a unit of content that scanners inspect.
type textSample struct {
	path    string
	content string
	isFile  bool
}

// primaryResourceSamples returns the intentionally public text samples derived
// from a resource. For MCP servers, only the name is exposed; other fields may
// contain credentials and must never become persisted finding messages.
func primaryResourceSamples(r ir.Resource) []textSample {
	switch m := r.(type) {
	case *ir.McpServer:
		return []textSample{{path: "mcpserver/" + m.Name() + "/name", content: m.Name()}}
	}
	return extractResourceSamples(r)
}

// fileSamples returns the text support-file samples in deterministic path order.
func fileSamples(files FileSet) []textSample {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var samples []textSample
	for _, p := range paths {
		data := files[p]
		if len(data) == 0 {
			continue
		}
		s := string(data)
		if !isValidText(s) {
			// Asset scanners handle non-text; text scanners skip it.
			continue
		}
		samples = append(samples, textSample{path: p, content: s, isFile: true})
	}
	return samples
}

// allTextSamples returns public resource samples plus valid text support files.
func allTextSamples(r ir.Resource, files FileSet) []textSample {
	samples := primaryResourceSamples(r)
	return append(samples, fileSamples(files)...)
}

// sha256Hex returns the canonical lower-case hex sha256 of a file.
func sha256Hex(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalDigest validates a declared implementation digest. It accepts only
// sha256:<64 lowercase hex characters>.
func canonicalDigest(d string) (string, error) {
	const prefix = "sha256:"
	if !strings.HasPrefix(d, prefix) {
		return "", fmt.Errorf("implementation digest must start with %q", prefix)
	}
	hex := d[len(prefix):]
	if len(hex) != 64 {
		return "", fmt.Errorf("implementation digest must contain 64 lowercase hex characters after %q", prefix)
	}
	for i := 0; i < len(hex); i++ {
		c := hex[i]
		if !isLowerHex(c) {
			return "", fmt.Errorf("implementation digest contains non-hex or uppercase character at position %d", i)
		}
	}
	return hex, nil
}

func isLowerHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

// combineVerdicts returns the stronger of raw and policy. The analyzer's own
// verdict can only be strengthened by policy, never downgraded. An empty raw
// verdict defers to policy.
func combineVerdicts(raw, policy string) string {
	if raw == "" {
		return policy
	}
	r, ok := rankVerdict(raw)
	if !ok {
		return policy
	}
	p, ok := rankVerdict(policy)
	if !ok {
		return raw
	}
	if p > r {
		return policy
	}
	return raw
}

// rankVerdict maps per-audit verdicts to a numeric strength. A higher rank is
// a stronger verdict. Skipped stays skipped unless policy warns or fails.
func rankVerdict(v string) (int, bool) {
	switch v {
	case VerdictPass:
		return 0, true
	case VerdictSkipped:
		return 1, true
	case VerdictWarn:
		return 2, true
	case VerdictFail:
		return 3, true
	}
	return 0, false
}

// ReportFromAudits validates and folds a deterministic ordered audit list into
// the analyzer Report. It clones the audits and their findings so caller mutation
// cannot affect the derived decision.
func ReportFromAudits(audits []Audit) (Report, error) {
	if len(audits) == 0 {
		return Report{
			Audits:   []Audit{},
			Findings: []Finding{},
			Decision: DecisionAllow,
			Rejected: false,
		}, nil
	}

	cloned := make([]Audit, len(audits))
	var allFindings []Finding
	for i, a := range audits {
		if err := validateAuditIdentity(a); err != nil {
			return Report{}, fmt.Errorf("audit %d: %w", i, err)
		}
		cloned[i] = cloneAudit(a)
		allFindings = append(allFindings, cloned[i].Findings...)
	}

	decision := foldDecision(cloned)
	return Report{
		Audits:   cloned,
		Findings: allFindings,
		Decision: decision,
		Rejected: decision == DecisionReject,
	}, nil
}

func validateAuditIdentity(a Audit) error {
	if strings.TrimSpace(a.AnalyzerID) == "" {
		return errors.New("analyzer id is required")
	}
	if strings.TrimSpace(a.AnalyzerVersion) == "" {
		return errors.New("analyzer version is required")
	}
	if a.Role != RoleGating && a.Role != RoleAdvisory {
		return fmt.Errorf("invalid role %q", a.Role)
	}
	switch a.Verdict {
	case VerdictPass, VerdictWarn, VerdictFail, VerdictError, VerdictSkipped:
	default:
		return fmt.Errorf("invalid verdict %q", a.Verdict)
	}
	if a.Verdict == VerdictError {
		if strings.TrimSpace(a.Error) == "" {
			return errors.New("error is required for verdict error")
		}
	} else if a.Error != "" {
		return errors.New("error is only allowed for verdict error")
	}
	if strings.TrimSpace(a.Summary) == "" {
		return errors.New("summary is required")
	}
	return nil
}

func cloneAudit(a Audit) Audit {
	out := a
	out.Findings = make([]Finding, len(a.Findings))
	for i, f := range a.Findings {
		out.Findings[i] = cloneFinding(f)
	}
	return out
}

func cloneFinding(f Finding) Finding {
	out := f
	if f.SourceRange != nil {
		sr := *f.SourceRange
		out.SourceRange = &sr
	}
	out.Consentable = NormalizeConsentable(f.Consentable)
	return out
}

// redactError sanitizes and bounds an error string so it can be stored in an
// audit without leaking details or emitting invalid UTF-8/control characters.
func redactError(msg string) string {
	return SanitizeString(msg, MaxAuditErrorBytes)
}
