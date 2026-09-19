package licensepolicy

import (
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
)

type Decision struct {
	Outcome string
	Code    string
}

func Evaluate(license string, allowlist []string, undeclared string) Decision {
	if len(allowlist) == 0 {
		return Decision{Outcome: analyze.OutcomeAllow}
	}
	if strings.TrimSpace(license) == "" {
		return Decision{Outcome: config.ResolveUndeclaredLicenseOutcome(undeclared), Code: analyze.PolicyLicenseUndeclared}
	}
	if !slices.Contains(allowlist, license) {
		return Decision{Outcome: analyze.OutcomeReject, Code: analyze.PolicyLicenseNotAllowed}
	}
	return Decision{Outcome: analyze.OutcomeAllow}
}
