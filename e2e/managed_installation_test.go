package e2e_test

import (
	"strings"
	"testing"
)

// TestDoctorReportsManagedInstallationDiagnostics verifies that `doctor`
// surfaces the organization config path, managed state, validation result,
// filesystem metadata, and trust warnings even when the org config is writable.
func TestDoctorReportsManagedInstallationDiagnostics(t *testing.T) {
	env := newEnv(t)

	env.WriteOrgConfigWithMode(`apiVersion: agent-marketplace/v4
harnesses: [factory]
`, 0o666)

	r := env.RunAM("doctor")
	if r.ExitCode != 0 {
		t.Fatalf("doctor should exit 0, got %d:\n%s", r.ExitCode, r.Combined)
	}

	for _, needle := range []string{
		"org managed: false",
		"MANAGED INSTALLATION",
		"state: unmanaged",
		"validation: untrusted",
		"writable",
	} {
		if !strings.Contains(r.Combined, needle) {
			t.Fatalf("expected doctor output to contain %q, got:\n%s", needle, r.Combined)
		}
	}
}
