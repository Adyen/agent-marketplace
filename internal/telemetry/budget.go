package telemetry

import "fmt"

// ValidateCardinalityBudget checks the supplied installation IDs against the
// cardinality budget. The receiver enforces this server-side; a single-ID
// client can call it with a one-element slice to document the contract.
func ValidateCardinalityBudget(installationIDs []string, budget int) error {
	if budget <= 0 {
		budget = DefaultInstallationIDCardinalityBudget
	}
	unique := make(map[string]struct{}, len(installationIDs))
	for _, id := range installationIDs {
		if id == "" {
			continue
		}
		unique[id] = struct{}{}
	}
	if len(unique) > budget {
		return fmt.Errorf("installation ID cardinality %d exceeds budget %d", len(unique), budget)
	}
	return nil
}

// ValidateCardinalityBudget is a no-op for the single-ID client. The receiver
// applies the cardinality contract to the full set of IDs it has seen.
func (s TelemetryState) ValidateCardinalityBudget() error {
	return nil
}
