package analyze

// Stable finding codes emitted by Marketplace security analyzers.
const (
	FindingHTMLComment           = "marketplace.find.html-comment"
	FindingZeroWidth             = "marketplace.find.zero-width"
	FindingTerminalControl       = "marketplace.find.terminal-control"
	FindingUnsafeBidi            = "marketplace.find.unsafe-bidi"
	FindingDangerousURL          = "marketplace.find.dangerous-url"
	FindingRemoteReference       = "marketplace.find.remote-reference"
	FindingExecutableAsset       = "marketplace.find.executable-asset"
	FindingToolGrant             = "marketplace.find.tool-grant"
	FindingHookCapability        = "marketplace.find.hook-capability"
	FindingMcpCapability         = "marketplace.find.mcp-capability"
	FindingSuspiciousPath        = "marketplace.find.suspicious-path"
	FindingObfuscatedInstruction = "marketplace.find.obfuscated-instruction"
)

// Stable finding codes emitted by operator policy enforcement during ingest.
//
// These are not analyzer detections: they record a policy decision about a
// resource that analysis itself accepted. They live here, next to the analyzer
// codes, because a consumer UI keys off the code string, so the vocabulary must
// have exactly one definition rather than an inline literal per call site.
const (
	// PolicyLicenseNotAllowed reports a resource that declares a license which
	// the configured license allowlist does not contain.
	PolicyLicenseNotAllowed = "marketplace.policy.license-not-allowed"
	// PolicyLicenseUndeclared reports a resource that declares no license at
	// all. An undeclared license is unknown, not disallowed, so it is a
	// separate condition with its own operator-configurable outcome.
	PolicyLicenseUndeclared = "marketplace.policy.license-undeclared"
	// PolicyArtifactTooLarge reports an artifact above the configured size limit.
	PolicyArtifactTooLarge = "marketplace.policy.artifact-too-large"
)

// AnalyzerVersionV1 is the version of the Marketplace security analyzer suite.
//
// Bump this whenever detection behaviour changes. Every revision stores the
// version that produced its findings, so leaving it fixed would make two
// different analyzers indistinguishable in the record. 1.1.0 narrowed two
// finding codes:
//
//   - marketplace.find.suspicious-path: prose no longer reports slash-command
//     invocations ("/deploy") or ellipses ("...") as filesystem paths, while
//     FileSet entry paths stay strict.
//   - marketplace.find.dangerous-url: a scheme-looking word followed by a
//     colon is no longer a URL. javascript:/vbscript: now require a code
//     body, data: requires an RFC 2397 body (media type, ";base64" or the
//     mandatory comma) and file: requires a path component, so prose such as
//     "**With data:** User uploads a CSV" or "Attach the file: report.pdf" is
//     clean. Encoded ("javascript%3Aalert(1)", "data&#x3a;text/html,x") and
//     whitespace-padded ("javascript:\n alert(1)") payloads are still
//     reported.
const AnalyzerVersionV1 = "1.1.0"

// Policy outcomes.
const (
	OutcomeReject = "reject"
	OutcomeWarn   = "warn"
	OutcomeAllow  = "allow"
)

// SourceRange identifies a location in a source file.
type SourceRange struct {
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

// Finding is a single analyzer finding.
type Finding struct {
	Analyzer        string       `json:"analyzer"`
	AnalyzerVersion string       `json:"analyzer_version"`
	Code            string       `json:"code"`
	Severity        string       `json:"severity"`       // reject | warn | info
	PolicyOutcome   string       `json:"policy_outcome"` // reject | warn | allow
	Message         string       `json:"message,omitempty"`
	SourceRange     *SourceRange `json:"source_range,omitempty"`
}

// FindingCodes returns the complete set of stable finding codes. It is the
// single source of truth for code constants and is used by tests and the
// documentation catalog.
func FindingCodes() []string {
	return []string{
		FindingHTMLComment,
		FindingZeroWidth,
		FindingTerminalControl,
		FindingUnsafeBidi,
		FindingDangerousURL,
		FindingRemoteReference,
		FindingExecutableAsset,
		FindingToolGrant,
		FindingHookCapability,
		FindingMcpCapability,
		FindingSuspiciousPath,
		FindingObfuscatedInstruction,
	}
}

// PolicyCodes returns the complete set of stable policy finding codes. Like
// FindingCodes it is the single source of truth for the constants above and is
// used by tests and the documentation catalog.
func PolicyCodes() []string {
	return []string{
		PolicyLicenseNotAllowed,
		PolicyLicenseUndeclared,
		PolicyArtifactTooLarge,
	}
}
