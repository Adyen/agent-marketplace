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

// AnalyzerVersionV1 is the version of the Marketplace security analyzer suite.
const AnalyzerVersionV1 = "1.0.0"

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
