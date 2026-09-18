package licensepolicy

import "testing"

func TestEvaluate(t *testing.T) {
	for _, tc := range []struct {
		name, license, undeclared string
		allowlist                 []string
		want                      Decision
	}{
		{"disabled undeclared", "", "reject", nil, Decision{Outcome: "allow"}},
		{"disabled declared", "GPL-3.0", "reject", nil, Decision{Outcome: "allow"}},
		{"allowed", "MIT", "reject", []string{"MIT"}, Decision{Outcome: "allow"}},
		{"rejected", "GPL-3.0", "allow", []string{"MIT"}, Decision{Outcome: "reject", Code: "marketplace.policy.license-not-allowed"}},
		{"exact no trim", " MIT ", "allow", []string{"MIT"}, Decision{Outcome: "reject", Code: "marketplace.policy.license-not-allowed"}},
		{"undeclared reject", " ", "reject", []string{"MIT"}, Decision{Outcome: "reject", Code: "marketplace.policy.license-undeclared"}},
		{"undeclared warn", "", "warn", []string{"MIT"}, Decision{Outcome: "warn", Code: "marketplace.policy.license-undeclared"}},
		{"undeclared allow", "", "allow", []string{"MIT"}, Decision{Outcome: "allow", Code: "marketplace.policy.license-undeclared"}},
		{"undeclared default", "", "", []string{"MIT"}, Decision{Outcome: "reject", Code: "marketplace.policy.license-undeclared"}},
		{"undeclared typo", "", "typo", []string{"MIT"}, Decision{Outcome: "reject", Code: "marketplace.policy.license-undeclared"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Evaluate(tc.license, tc.allowlist, tc.undeclared); got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}
