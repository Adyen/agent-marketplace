package artifact

import (
	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// ToCanonicalFinding converts an analyze.Finding into a CanonicalFinding for
// inclusion in a canonical artifact manifest.
func ToCanonicalFinding(f analyze.Finding) hccartifact.CanonicalFinding {
	return analyze.ToCanonicalFinding(f)
}

// ToCanonicalFindings converts a slice of analyze findings.
func ToCanonicalFindings(findings []analyze.Finding) []hccartifact.CanonicalFinding {
	if len(findings) == 0 {
		return nil
	}
	out := make([]hccartifact.CanonicalFinding, len(findings))
	for i, f := range findings {
		out[i] = ToCanonicalFinding(f)
	}
	return out
}
