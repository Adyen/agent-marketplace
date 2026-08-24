package telemetry

// Release-configurable defaults for the project telemetry endpoint. Core logic
// intentionally leaves the actual values empty so release configuration can set
// them via -X linker flags without editing this package.
var (
	DefaultProjectEndpoint     = ""
	DefaultProjectRetentionURL = ""
)

const (
	DefaultInstallationIDSubpath = "telemetry/installation-id"

	// DefaultInstallationIDCardinalityBudget is the maximum number of unique
	// installation.id values the community telemetry receiver accepts. It is
	// shared between the receiver default and the client-side validation
	// helper that documents the cardinality contract.
	DefaultInstallationIDCardinalityBudget = 10000
)

// Environment variables that may override user telemetry preferences when the
// corresponding organization policy field is not locked. Standard OTEL_*
// variables are ignored for policy resolution.
const (
	EnvEndpoint           = "AGENT_MARKETPLACE_TELEMETRY_ENDPOINT"
	EnvLevel              = "AGENT_MARKETPLACE_TELEMETRY_LEVEL"
	EnvCommunityReporting = "AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING"
)
