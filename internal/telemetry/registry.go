package telemetry

import "github.com/Adyen/agent-marketplace/internal/registryid"

// SanitizeRegistryTelemetry returns the stable identity and display location
// permitted in telemetry. Local locations collapse completely to "local".
func SanitizeRegistryTelemetry(raw string) (identity, location string, local, ok bool) {
	return registryid.SanitizeRegistryTelemetry(raw)
}
