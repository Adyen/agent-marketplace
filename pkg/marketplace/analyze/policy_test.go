package analyze

import "testing"

func TestPublicPolicyDefaults(t *testing.T) {
	p := PublicPolicy()
	cases := map[string]string{
		FindingTerminalControl:       OutcomeReject,
		FindingUnsafeBidi:            OutcomeReject,
		FindingAssetPathEscape:       OutcomeReject,
		FindingExecutableAsset:       OutcomeReject,
		FindingObfuscatedInstruction: OutcomeReject,
		FindingHTMLComment:           OutcomeWarn,
		FindingZeroWidth:             OutcomeWarn,
		FindingDangerousURL:          OutcomeWarn,
		FindingExecutableScript:      OutcomeWarn,
		FindingDynamicExecution:      OutcomeWarn,
		FindingSuspiciousPath:        OutcomeWarn,
		FindingToolGrant:             OutcomeWarn,
		FindingHookCapability:        OutcomeWarn,
		FindingMcpCapability:         OutcomeWarn,
		FindingRemoteReference:       OutcomeWarn,
	}
	for code, want := range cases {
		if got := p.OutcomeFor(code); got != want {
			t.Errorf("OutcomeFor(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestPolicyUnknownCode(t *testing.T) {
	p := PublicPolicy()
	if got := p.OutcomeFor("marketplace.find.unknown"); got != OutcomeAllow {
		t.Errorf("unknown outcome = %q, want allow", got)
	}
}
