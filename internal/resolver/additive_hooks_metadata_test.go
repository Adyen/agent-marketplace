package resolver

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

type additiveSourceMetadata struct {
	url      string
	path     string
	ref      string
	source   config.Source
	required bool
	command  string
}

func assertAdditiveSourceMetadata(t *testing.T, artifact ResolvedArtifact, want map[string][]additiveSourceMetadata, command string) {
	t.Helper()
	for _, expected := range want[artifact.RegistryURL] {
		if expected.url != artifact.RegistryURL || expected.path != artifact.RegistryPath ||
			expected.ref != artifact.Ref || expected.source != artifact.Source ||
			expected.required != artifact.Required || expected.command != command {
			continue
		}
		return
	}
	t.Fatalf("resolved metadata = URL %q path %q ref %q source %v required %v command %q does not match fixture", artifact.RegistryURL, artifact.RegistryPath, artifact.Ref, artifact.Source, artifact.Required, command)
}
