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
	FindingHTMLComment:           OutcomeReject,
	FindingZeroWidth:             OutcomeReject,
	FindingTerminalControl:       OutcomeReject,
	FindingUnsafeBidi:            OutcomeReject,
	FindingDangerousURL:          OutcomeReject,
	FindingExecutableAsset:       OutcomeReject,
	FindingSuspiciousPath:        OutcomeReject,
	FindingObfuscatedInstruction: OutcomeReject,

	FindingRemoteReference: OutcomeWarn,
	FindingToolGrant:       OutcomeWarn,
	FindingHookCapability:  OutcomeWarn,
	FindingMcpCapability:   OutcomeWarn,
}

// PublicPolicy returns the default public policy.
func PublicPolicy() Policy {
	return Policy{
		Name:     "public",
		Outcomes: nil,
	}
}

// PrivatePolicy returns a policy that downgrades hidden-content findings to
// warnings while keeping all other defaults.
func PrivatePolicy() Policy {
	return Policy{
		Name: "private",
		Outcomes: map[string]string{
			FindingHTMLComment: OutcomeWarn,
			FindingZeroWidth:   OutcomeWarn,
		},
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
