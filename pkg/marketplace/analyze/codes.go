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
	FindingExecutableScript      = "marketplace.find.executable-script"
	FindingAssetPathEscape       = "marketplace.find.asset-path-escape"
	FindingDynamicExecution      = "marketplace.find.dynamic-execution"
	FindingToolGrant             = "marketplace.find.tool-grant"
	FindingHookCapability        = "marketplace.find.hook-capability"
	FindingMcpCapability         = "marketplace.find.mcp-capability"
	FindingSuspiciousPath        = "marketplace.find.suspicious-path"
	FindingObfuscatedInstruction = "marketplace.find.obfuscated-instruction"
)

// Stable finding codes emitted by operator policy enforcement during ingest.
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
	// PolicyAuditUnavailable reports a gating analyzer that could not complete
	// its audit. It is a non-consentable warning: the resource is visible but
	// not installable until the analyzer succeeds.
	PolicyAuditUnavailable = "marketplace.policy.audit-unavailable"
)

// AnalyzerVersionV1 is the legacy suite version retained for assessment
// compatibility. Built-in and rulepack construction use their own version
// constants so each analyzer can be bumped independently.
const AnalyzerVersionV1 = "1.2.0"

// Built-in analyzer identifiers.
const (
	AnalyzerHiddenContent = "marketplace.hidden-content"
	AnalyzerObfuscation   = "marketplace.obfuscation"
	AnalyzerAssets        = "marketplace.assets"
	AnalyzerReferences    = "marketplace.references"
	AnalyzerCapabilities  = "marketplace.capabilities"
)

// Built-in analyzer version constants. Each built-in can be bumped independently.
const (
	AnalyzerHiddenContentVersionV1 = "1.2.0"
	AnalyzerObfuscationVersionV1   = "1.2.0"
	AnalyzerAssetsVersionV1        = "1.2.0"
	AnalyzerReferencesVersionV1    = "1.2.0"
	AnalyzerCapabilitiesVersionV1  = "1.2.0"
)

// AnalyzerRulepackVersionV1 is the version used for the built-in rulepack
// analyzer. It is separate from the legacy suite version.
const AnalyzerRulepackVersionV1 = "1.0.0"

// Analyzer roles.
const (
	RoleGating   = "gating"
	RoleAdvisory = "advisory"
)

// Per-audit verdicts.
const (
	VerdictPass    = "pass"
	VerdictWarn    = "warn"
	VerdictFail    = "fail"
	VerdictError   = "error"
	VerdictSkipped = "skipped"
)

// Derived rollup decisions.
const (
	DecisionAllow  = "allow"
	DecisionWarn   = "warn"
	DecisionReject = "reject"
)

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
//
// Consentable is omitted (nil) for ordinary, consentable findings that
// --accept-risk can override, and emitted as false for findings that cannot be
// consented to (for example, marketplace.policy.audit-unavailable). Scanners
// and analyzers that emit ordinary findings do not need to set it.
type Finding struct {
	Analyzer        string       `json:"analyzer"`
	AnalyzerVersion string       `json:"analyzer_version"`
	Code            string       `json:"code"`
	Severity        string       `json:"severity"`       // reject | warn | info
	PolicyOutcome   string       `json:"policy_outcome"` // reject | warn | allow
	Message         string       `json:"message,omitempty"`
	SourceRange     *SourceRange `json:"source_range,omitempty"`
	Consentable     *bool        `json:"consentable,omitempty"`
}

// FindingCodes returns the complete set of stable finding codes.
func FindingCodes() []string {
	return []string{
		FindingHTMLComment,
		FindingZeroWidth,
		FindingTerminalControl,
		FindingUnsafeBidi,
		FindingDangerousURL,
		FindingRemoteReference,
		FindingExecutableAsset,
		FindingExecutableScript,
		FindingAssetPathEscape,
		FindingDynamicExecution,
		FindingToolGrant,
		FindingHookCapability,
		FindingMcpCapability,
		FindingSuspiciousPath,
		FindingObfuscatedInstruction,
	}
}

// PolicyCodes returns the complete set of stable policy finding codes.
func PolicyCodes() []string {
	return []string{
		PolicyLicenseNotAllowed,
		PolicyLicenseUndeclared,
		PolicyArtifactTooLarge,
		PolicyAuditUnavailable,
	}
}

// BuiltinMetadata describes a built-in analyzer for plugin registration and UI
// consumers.
type BuiltinMetadata struct {
	ID           string
	Version      string
	DefaultRole  string
	FindingCodes []string
}
