package analyze

// Policy maps finding codes to policy outcomes.
//
// A nil or missing map entry falls back to the default outcome for the code.
type Policy struct {
	Name     string
	Outcomes map[string]string
}

// defaultOutcomes is the public-policy mapping. It is used when a Policy does
// not override a code explicitly.
var defaultOutcomes = map[string]string{
	FindingTerminalControl:       OutcomeReject,
	FindingUnsafeBidi:            OutcomeReject,
	FindingExecutableAsset:       OutcomeReject,
	FindingAssetPathEscape:       OutcomeReject,
	FindingObfuscatedInstruction: OutcomeReject,

	FindingHTMLComment:      OutcomeWarn,
	FindingZeroWidth:        OutcomeWarn,
	FindingDangerousURL:     OutcomeWarn,
	FindingRemoteReference:  OutcomeWarn,
	FindingExecutableScript: OutcomeWarn,
	FindingDynamicExecution: OutcomeWarn,
	FindingSuspiciousPath:   OutcomeWarn,
	FindingToolGrant:        OutcomeWarn,
	FindingHookCapability:   OutcomeWarn,
	FindingMcpCapability:    OutcomeWarn,

	PolicyAuditUnavailable: OutcomeWarn,
}

// PublicPolicy returns the default public policy.
func PublicPolicy() Policy {
	return Policy{
		Name:     "public",
		Outcomes: nil,
	}
}

// OutcomeFor returns the policy outcome for a finding code.
func (p Policy) OutcomeFor(code string) string {
	if p.Outcomes != nil {
		if o, ok := p.Outcomes[code]; ok {
			return o
		}
	}
	if o, ok := defaultOutcomes[code]; ok {
		return o
	}
	return OutcomeAllow
}
