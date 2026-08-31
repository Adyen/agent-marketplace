package telemetry

// Release-configurable values are empty in source and injected with linker
// flags by release builds.
var (
	DefaultProjectEndpoint     = ""
	DefaultProjectRetentionURL = ""
	BuildVersion               = ""
)

const (
	DefaultInstallationIDSubpath = "telemetry/installation-id"
	RequestIDHeader              = "X-Agent-Marketplace-Request-ID"

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
