package analyze

import "testing"

func TestPublicPolicyDefaults(t *testing.T) {
	p := PublicPolicy()
	cases := map[string]string{
		FindingHTMLComment:           OutcomeReject,
		FindingZeroWidth:             OutcomeReject,
		FindingTerminalControl:       OutcomeReject,
		FindingDangerousURL:          OutcomeReject,
		FindingExecutableAsset:       OutcomeReject,
		FindingSuspiciousPath:        OutcomeReject,
		FindingObfuscatedInstruction: OutcomeReject,
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

func TestPrivatePolicyDowngrades(t *testing.T) {
	p := PrivatePolicy()
	if got := p.OutcomeFor(FindingHTMLComment); got != OutcomeWarn {
		t.Errorf("private HTML comment outcome = %q, want warn", got)
	}
	if got := p.OutcomeFor(FindingZeroWidth); got != OutcomeWarn {
		t.Errorf("private zero-width outcome = %q, want warn", got)
	}
	if got := p.OutcomeFor(FindingDangerousURL); got != OutcomeReject {
		t.Errorf("private dangerous URL outcome = %q, want reject", got)
	}
}

func TestPolicyUnknownCode(t *testing.T) {
	p := PublicPolicy()
	if got := p.OutcomeFor("marketplace.find.unknown"); got != OutcomeAllow {
		t.Errorf("unknown outcome = %q, want allow", got)
	}
}
