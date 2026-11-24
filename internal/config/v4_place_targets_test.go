package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

func TestV4MachinePlaceTargetSeparation(t *testing.T) {
	t.Run("projection preserves nil and explicit empty compiled lists", func(t *testing.T) {
		if got := (Settings{}).config().Harnesses; got != nil {
			t.Fatalf("omitted compiled targets = %#v, want nil", got)
		}
		if got := (Settings{Harnesses: []string{}}).config().Harnesses; got == nil || len(got) != 0 {
			t.Fatalf("explicit empty compiled targets = %#v, want nonnil empty", got)
		}
	})

	t.Run("path-only ParseV4 remains compiled-only", func(t *testing.T) {
		if _, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\ntargets: [invented-app]\n")); err == nil ||
			!strings.Contains(err.Error(), "v4.harness") {
			t.Fatalf("path-only ParseV4 error = %v, want compiled-target refusal", err)
		}
	})

	t.Run("mixed compiled and place targets", func(t *testing.T) {
		fixture := newV4OnlyFixture(t)
		userConfig := v4PlaceTargetConfig("[factory, invented-app]", true)
		v4OnlyWriteFixture(t, fixture.paths[UserPersonal], userConfig)
		intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
		if len(intent.Errors) != 0 {
			t.Fatalf("ReadIntent errors = %v", intent.Errors)
		}
		if got := intent.Layers[UserPersonal].Settings().Harnesses; !slices.Equal(got, []string{"factory", "invented-app"}) {
			t.Fatalf("authored target list = %v", got)
		}
		targets, source := intent.TargetHarnesses()
		if source != UserPersonal || !slices.Equal(targets, []string{"factory"}) {
			t.Fatalf("pipeline targets = %v from %s", targets, source)
		}

		identity := v4PlaceTargetIdentity()
		doc := v4VirtualProfileLock(t, identity, &identity)
		var evidence Evidence
		evidence.RecordDeployedLock(UserPersonal, &doc, ConfigRead{})
		effective := Bind(intent, evidence)
		if len(effective.Errors) != 0 {
			t.Fatalf("Bind errors = %v", effective.Errors)
		}
		if !slices.Equal(effective.PlaceTargets, []string{"invented-app"}) {
			t.Fatalf("effective place targets = %v", effective.PlaceTargets)
		}
		if !slices.Equal(effective.Merged.Harnesses, []string{"factory"}) {
			t.Fatalf("merged compiled targets = %v", effective.Merged.Harnesses)
		}
		selected, err := effective.Merged.SelectedMarketplaceProfiles()
		if err != nil || selected["invented-app"] != identity {
			t.Fatalf("selected machine profile = %#v, %v", selected, err)
		}
	})

	t.Run("profile-only user list wins without compiled fallback", func(t *testing.T) {
		fixture := newV4OnlyFixture(t)
		v4OnlyWriteFixture(t, fixture.paths[Organization], "apiVersion: agent-marketplace/v4\ntargets: [factory]\n")
		v4OnlyWriteFixture(t, fixture.paths[UserPersonal], v4PlaceTargetConfig("[invented-app]", true))
		intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
		if len(intent.Errors) != 0 {
			t.Fatalf("ReadIntent errors = %v", intent.Errors)
		}
		for name, get := range map[string]func() ([]string, Source){
			"project": intent.TargetHarnesses,
			"machine": intent.MachineTargetHarnesses,
		} {
			targets, source := get()
			if source != UserPersonal || len(targets) != 0 {
				t.Errorf("%s targets = %v from %s, want empty from USER", name, targets, source)
			}
		}

		identity := v4PlaceTargetIdentity()
		doc := v4VirtualProfileLock(t, identity, &identity)
		var evidence Evidence
		evidence.RecordDeployedLock(UserPersonal, &doc, ConfigRead{})
		effective := Bind(intent, evidence)
		if len(effective.Errors) != 0 {
			t.Fatalf("Bind errors = %v", effective.Errors)
		}
		if len(effective.TargetHarnesses) != 0 || effective.TargetHarnessSource != UserPersonal ||
			!slices.Equal(effective.PlaceTargets, []string{"invented-app"}) {
			t.Fatalf("effective targets=%v from %s place=%v", effective.TargetHarnesses, effective.TargetHarnessSource, effective.PlaceTargets)
		}
		if effective.Merged.Harnesses == nil || len(effective.Merged.Harnesses) != 0 {
			t.Fatalf("merged compiled targets = %#v, want explicit empty", effective.Merged.Harnesses)
		}
	})

	t.Run("profile-only organization list remains an explicit target set", func(t *testing.T) {
		fixture := newV4OnlyFixture(t)
		v4OnlyWriteFixture(t, fixture.paths[Organization], v4PlaceTargetConfig("[invented-app]", true))
		intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
		if len(intent.Errors) != 0 {
			t.Fatalf("ReadIntent errors = %v", intent.Errors)
		}
		targets, source := intent.TargetHarnesses()
		if source != Organization || len(targets) != 0 {
			t.Fatalf("project targets = %v from %s, want empty from ORG", targets, source)
		}
		targets, source = intent.MachineTargetHarnesses()
		if source != Organization || len(targets) != 0 {
			t.Fatalf("machine targets = %v from %s, want empty from ORG", targets, source)
		}

		identity := v4PlaceTargetIdentity()
		doc := v4VirtualProfileLock(t, identity, &identity)
		doc.Scope = lockfile.ScopeOrganization
		var evidence Evidence
		evidence.RecordDeployedLock(Organization, &doc, ConfigRead{})
		effective := Bind(intent, evidence)
		if len(effective.Errors) != 0 {
			t.Fatalf("Bind errors = %v", effective.Errors)
		}
		if len(effective.TargetHarnesses) != 0 || effective.TargetHarnessSource != Organization ||
			!slices.Equal(effective.PlaceTargets, []string{"invented-app"}) {
			t.Fatalf("effective targets=%v from %s place=%v", effective.TargetHarnesses, effective.TargetHarnessSource, effective.PlaceTargets)
		}
		machineHarnesses, machineSource := MachineHarnesses(effective.User.Config, effective.Organization.Config)
		if machineSource != Organization || len(machineHarnesses) != 0 {
			t.Fatalf("runtime machine targets = %v from %s, want empty from ORG", machineHarnesses, machineSource)
		}

		v4OnlyWriteFixture(t, fixture.paths[RepoCommitted], "apiVersion: agent-marketplace/v4\ntargets: [factory]\n")
		withRepoTarget := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
		machineTargets, machineSource := withRepoTarget.MachineTargetHarnesses()
		if machineSource != Organization || len(machineTargets) != 0 {
			t.Fatalf("machine targets with repo config = %v from %s, want empty from ORG", machineTargets, machineSource)
		}
		projectTargets, projectSource := withRepoTarget.TargetHarnesses()
		if projectSource != RepoCommitted || !slices.Equal(projectTargets, []string{"factory"}) {
			t.Fatalf("project targets with repo config = %v from %s, want repo factory", projectTargets, projectSource)
		}
	})

	t.Run("enabled place target requires a selected profile lock", func(t *testing.T) {
		config := &Config{HasTargets: true, PlaceTargets: []string{"invented-app"}}
		effective := finishBind(Intent{}, Evidence{}, EffectiveConfig{
			User: LayerResult{Source: UserPersonal, Config: config},
		})
		if !strings.Contains(strings.Join(errorStrings(effective.Errors), "\n"), "marketplace.profile.selection-invalid") ||
			!strings.Contains(strings.Join(errorStrings(effective.Errors), "\n"), "invented-app") {
			t.Fatalf("finishBind errors = %v, want missing selected profile refusal", effective.Errors)
		}
	})

	t.Run("unselected user place target is refused", func(t *testing.T) {
		fixture := newV4OnlyFixture(t)
		v4OnlyWriteFixture(t, fixture.paths[UserPersonal], "apiVersion: agent-marketplace/v4\ntargets: [invented-app]\n")
		intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
		if !strings.Contains(strings.Join(errorStrings(intent.Errors), "\n"), "marketplace.profile.selection-invalid") {
			t.Fatalf("ReadIntent errors = %v, want profile selection refusal", intent.Errors)
		}
		if !strings.Contains(strings.Join(errorStrings(intent.Errors), "\n"), "targets[0]") {
			t.Fatalf("ReadIntent error lacks target-list path: %v", intent.Errors)
		}
	})

	t.Run("repository target list remains compiled-only", func(t *testing.T) {
		fixture := newV4OnlyFixture(t)
		v4OnlyWriteFixture(t, fixture.paths[UserPersonal], v4PlaceTargetConfig("[invented-app]", true))
		v4OnlyWriteFixture(t, fixture.paths[RepoCommitted], "apiVersion: agent-marketplace/v4\ntargets: [invented-app]\n")
		intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
		if !strings.Contains(strings.Join(errorStrings(intent.Errors), "\n"), "v4.harness") {
			t.Fatalf("ReadIntent errors = %v, want repository compiled-target refusal", intent.Errors)
		}
	})

	t.Run("blocked endpoint does not admit a place target", func(t *testing.T) {
		fixture := newV4OnlyFixture(t)
		userConfig := v4PlaceTargetConfig("[invented-app]", true)
		userConfig = strings.Replace(userConfig, "sources: {m:", "allowed_marketplaces: ['^https://blocked.example$']\nsources: {m:", 1)
		v4OnlyWriteFixture(t, fixture.paths[UserPersonal], userConfig)
		intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
		if !strings.Contains(strings.Join(errorStrings(intent.Errors), "\n"), "marketplace.profile.selection-invalid") {
			t.Fatalf("ReadIntent errors = %v, want blocked profile endpoint refusal", intent.Errors)
		}
	})

	t.Run("malformed target identifier remains invalid", func(t *testing.T) {
		fixture := newV4OnlyFixture(t)
		v4OnlyWriteFixture(t, fixture.paths[UserPersonal], "apiVersion: agent-marketplace/v4\ntargets: [invented/app]\n")
		intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
		if !strings.Contains(strings.Join(errorStrings(intent.Errors), "\n"), "v4.harness") {
			t.Fatalf("ReadIntent errors = %v, want malformed target refusal", intent.Errors)
		}
	})

	t.Run("nested resource target filters remain compiled-only", func(t *testing.T) {
		nested := strings.Replace(v4PlaceMarketplaceYAML,
			"    include:\n", "    harnesses: [invented-app]\n    include:\n", 1)
		if _, err := ParseV4([]byte(nested)); err == nil || !strings.Contains(err.Error(), "v4.harness") {
			t.Fatalf("nested place target filter error = %v, want compiled-target refusal", err)
		}
	})
}

func v4PlaceTargetConfig(targets string, selectProfile bool) string {
	config := strings.Replace(v4PlaceMarketplaceYAML,
		"apiVersion: agent-marketplace/v4\n",
		"apiVersion: agent-marketplace/v4\ntargets: "+targets+"\n", 1)
	if selectProfile {
		config += "      - tools/target-profile/invented-app\n"
	}
	return config
}

func v4PlaceTargetIdentity() targetprofile.Identity {
	return targetprofile.Identity{
		TargetID: "invented-app", SourceID: "builtin:agent-marketplace", RevisionDigest: v4sha("d"),
	}
}

func errorStrings(errs []error) []string {
	out := make([]string, len(errs))
	for i, err := range errs {
		out[i] = err.Error()
	}
	return out
}
