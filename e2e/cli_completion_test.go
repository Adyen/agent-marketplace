package e2e_test

import (
	"strings"
	"testing"
)

func TestCLICompletionScriptsFromBuiltBinary(t *testing.T) {
	env := newEnv(t)
	cases := []struct {
		shell  string
		marker string
	}{
		{shell: "bash", marker: "__start_agent-marketplace"},
		{shell: "zsh", marker: "#compdef agent-marketplace"},
		{shell: "fish", marker: "complete -c agent-marketplace"},
		{shell: "powershell", marker: "Register-ArgumentCompleter"},
	}
	for _, tc := range cases {
		t.Run(tc.shell, func(t *testing.T) {
			generated := env.RunAM("completion", tc.shell)
			if generated.ExitCode != 0 || !strings.Contains(generated.Stdout, tc.marker) {
				t.Fatalf("%s completion script status=%d, marker=%q, stdout=%q, stderr=%q", tc.shell, generated.ExitCode, tc.marker, generated.Stdout, generated.Stderr)
			}
			withoutDescriptions := env.RunAM("completion", tc.shell, "--no-descriptions")
			if withoutDescriptions.ExitCode != 0 || !strings.Contains(withoutDescriptions.Stdout, tc.marker) {
				t.Fatalf("%s completion without descriptions status=%d, marker=%q, stdout=%q, stderr=%q", tc.shell, withoutDescriptions.ExitCode, tc.marker, withoutDescriptions.Stdout, withoutDescriptions.Stderr)
			}
			if withoutDescriptions.Stdout == generated.Stdout {
				t.Fatalf("%s --no-descriptions did not change the completion script", tc.shell)
			}
		})
	}
}
