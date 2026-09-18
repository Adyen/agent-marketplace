package analyze

import (
	"context"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// AnalyzeFunc is an artifact analysis adapter. It receives the canonical content
// digest and may implement cache-aware execution. If nil, the selected Engine
// is used.
type AnalyzeFunc func(context.Context, Subject, string) (Report, error)

// Options configures a backwards-compatible analyzer run.
//
// The new Analyze API accepts a Subject, but Options is retained so existing
// callers in pkg/marketplace/artifact can keep their signatures.
type Options struct {
	Policy        Policy
	Registry      Registry
	Limits        Limits
	Analyzer      string
	Version       string
	Registrations []Registration

	// Analyze is an optional adapter invoked by artifact builders. The digest
	// argument is the canonical content digest of the artifact or collection
	// manifest, computed before the audit runs. A nil value runs the Engine
	// selected from Registrations.
	Analyze AnalyzeFunc
}

// DefaultOptions returns a public-policy analyzer configuration.
func DefaultOptions() Options {
	return Options{
		Policy:   PublicPolicy(),
		Limits:   DefaultLimits(),
		Analyzer: "marketplace.analyze",
		Version:  AnalyzerVersionV1,
	}
}

// Registration binds one analyzer to its runtime role, opaque config, and policy.
//
// The Engine owns identity and role: it stamps AnalyzerID/Version/Role from the
// registration after each audit, so an analyzer cannot spoof another or change
// its configured role at runtime.
type Registration struct {
	Analyzer Analyzer
	Role     string
	Config   AnalyzerConfig
	Policy   Policy
}

// RunnerConfig is the validated static configuration for an external analyzer.
//
// Command must be an absolute path. Existence, regular-file, and executable
// checks are performed at Analyze time so construction cannot fail because of
// a missing binary. Timeout and MaxOutputBytes must be positive.
// ImplementationDigest must be a canonical lower-case sha256:<64-hex> digest
// of the executable's bytes. EnvPassthrough lists environment variable names
// whose current values are passed through to the runner; all other environment
// variables are stripped.
type RunnerConfig struct {
	Kinds                []ir.ResourceKind
	Command              string
	Args                 []string
	Timeout              time.Duration
	EnvPassthrough       []string
	ImplementationDigest string
	MaxOutputBytes       int64
}
