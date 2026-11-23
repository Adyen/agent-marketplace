package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

const hostPackageProfileSourceID = "git:https%3A%2F%2Fgit.example.com%2Ftools@main#profiles%2Finvented-app.yaml"

func TestHostPackageAssessmentProfiles(t *testing.T) {
	identity := hostPackageProfileIdentity()
	entry := hostPackageProfileEntry("src-test", "invented-app/mod", "sample", identity.TargetID, identity.SourceID, identity.RevisionDigest)
	cfg, err := ParseConfig(hostPackageProfilesYAML(entry))
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	got, err := cfg.AssessmentProfileFor("src-test", "invented-app/mod", "sample")
	if err != nil || got != identity {
		t.Fatalf("AssessmentProfileFor() = %#v, %v; want %#v", got, err, identity)
	}

	for _, coordinate := range [][3]string{
		{"other-registry", "invented-app/mod", "sample"},
		{"src-test", "invented-app/other", "sample"},
		{"src-test", "invented-app/mod", "other"},
	} {
		_, err := cfg.AssessmentProfileFor(coordinate[0], coordinate[1], coordinate[2])
		if err == nil || !strings.Contains(err.Error(), "marketplace.profile.unconfigured") ||
			!strings.Contains(err.Error(), strings.Join(coordinate[:], "/")) {
			t.Errorf("AssessmentProfileFor(%q, %q, %q) error = %v", coordinate[0], coordinate[1], coordinate[2], err)
		}
	}

	withoutProfiles, err := ParseConfig([]byte(validConfigYAML()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withoutProfiles.AssessmentProfileFor("src-test", "invented-app/mod", "sample"); err == nil ||
		!strings.Contains(err.Error(), "marketplace.profile.unconfigured") {
		t.Fatalf("lookup without section error = %v, want unconfigured", err)
	}

	runtimeRegistryEntry := hostPackageProfileEntry("runtime-only", "invented-app/mod", "sample",
		identity.TargetID, identity.SourceID, identity.RevisionDigest)
	runtimeRegistry, err := ParseConfig(hostPackageProfilesYAML(runtimeRegistryEntry))
	if err != nil {
		t.Fatalf("runtime registry ID without config registry: %v", err)
	}
	if _, err := runtimeRegistry.AssessmentProfileFor("runtime-only", "invented-app/mod", "sample"); err != nil {
		t.Fatalf("lookup for runtime registry ID: %v", err)
	}

	invalid := []struct {
		name, index string
		entries     []string
	}{
		{"duplicate exact coordinate", "1", []string{entry, entry}},
		{"kind target mismatch", "0", []string{hostPackageProfileEntry("src-test", "other-app/mod", "sample",
			identity.TargetID, identity.SourceID, identity.RevisionDigest)}},
		{"wildcard name", "0", []string{hostPackageProfileEntry("src-test", "invented-app/mod", "*",
			identity.TargetID, identity.SourceID, identity.RevisionDigest)}},
		{"invalid source ID", "0", []string{hostPackageProfileEntry("src-test", "invented-app/mod", "sample",
			identity.TargetID, "not-a-source", identity.RevisionDigest)}},
		{"abbreviated digest", "0", []string{hostPackageProfileEntry("src-test", "invented-app/mod", "sample",
			identity.TargetID, identity.SourceID, "sha256:abc")}},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig(hostPackageProfilesYAML(tt.entries...))
			wantPath := "host_package_assessment_profiles[" + tt.index + "]"
			if err == nil || !strings.Contains(err.Error(), wantPath) {
				t.Fatalf("ParseConfig() error = %v, want error naming %s", err, wantPath)
			}
		})
	}

	direct := HostPackageAssessmentProfile{
		RegistryID: "src-test", Kind: "invented-app/mod", Name: "sample", ProfileIdentity: identity,
	}
	literal := Config{HostPackageAssessmentProfiles: []HostPackageAssessmentProfile{direct, direct}}
	if _, err := literal.AssessmentProfileFor("src-test", "invented-app/mod", "sample"); err == nil ||
		!strings.Contains(err.Error(), "host_package_assessment_profiles[1]") {
		t.Fatalf("direct literal duplicate error = %v", err)
	}
}

func hostPackageProfileIdentity() targetprofile.Identity {
	return targetprofile.Identity{
		TargetID: "invented-app", SourceID: hostPackageProfileSourceID,
		RevisionDigest: "sha256:" + strings.Repeat("a", 64),
	}
}

func hostPackageProfileEntry(registryID, kind, name, targetID, sourceID, revisionDigest string) string {
	return fmt.Sprintf(`  - registry_id: %q
    kind: %q
    name: %q
    profile_identity:
      target_id: %q
      source_id: %q
      revision_digest: %q
`, registryID, kind, name, targetID, sourceID, revisionDigest)
}

func hostPackageProfilesYAML(entries ...string) []byte {
	return []byte(validConfigYAML() + "\nhost_package_assessment_profiles:\n" + strings.Join(entries, ""))
}
