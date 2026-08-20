package telemetry

// Release-configurable defaults for the project telemetry endpoint. Core logic
// intentionally leaves the actual values empty so release configuration can set
// them without editing this package.
const (
	DefaultProjectEndpoint       = ""
	DefaultProjectRetentionURL   = ""
	DefaultInstallationIDSubpath = "telemetry/installation-id"
)

// Environment variables that may override user telemetry preferences when the
// corresponding organization policy field is not locked. Standard OTEL_*
// variables are ignored for policy resolution.
const (
	EnvEndpoint           = "AGENT_MARKETPLACE_TELEMETRY_ENDPOINT"
	EnvLevel              = "AGENT_MARKETPLACE_TELEMETRY_LEVEL"
	EnvCommunityReporting = "AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING"
)
