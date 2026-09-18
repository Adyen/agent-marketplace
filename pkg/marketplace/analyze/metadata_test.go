package analyze

import (
	"slices"
	"testing"
)

func TestBuiltinsCanonicalOrderAndMetadata(t *testing.T) {
	builtins := Builtins()
	want := []struct {
		id      string
		version string
	}{
		{AnalyzerHiddenContent, AnalyzerHiddenContentVersionV1},
		{AnalyzerObfuscation, AnalyzerObfuscationVersionV1},
		{AnalyzerAssets, AnalyzerAssetsVersionV1},
		{AnalyzerReferences, AnalyzerReferencesVersionV1},
		{AnalyzerCapabilities, AnalyzerCapabilitiesVersionV1},
	}
	if len(builtins) != len(want) {
		t.Fatalf("expected %d builtins, got %d", len(want), len(builtins))
	}
	for i, w := range want {
		if builtins[i].ID != w.id {
			t.Fatalf("builtin[%d].ID = %q, want %q", i, builtins[i].ID, w.id)
		}
		if builtins[i].Version != w.version {
			t.Fatalf("builtin[%d].Version = %q, want %q", i, builtins[i].Version, w.version)
		}
		if builtins[i].DefaultRole != RoleGating && builtins[i].DefaultRole != RoleAdvisory {
			t.Fatalf("builtin[%d].DefaultRole = %q, want gating or advisory", i, builtins[i].DefaultRole)
		}
		if len(builtins[i].FindingCodes) == 0 {
			t.Fatalf("builtin[%d].FindingCodes is empty", i)
		}
	}
}

func TestBuiltinsFindingCodesUniqueWithinAnalyzer(t *testing.T) {
	for _, b := range Builtins() {
		seen := make(map[string]struct{})
		for _, c := range b.FindingCodes {
			if _, ok := seen[c]; ok {
				t.Fatalf("builtin %q has duplicate code %q", b.ID, c)
			}
			seen[c] = struct{}{}
		}
	}
}

func TestBuiltinsMatchesDefaultRegistrations(t *testing.T) {
	builtins := Builtins()
	regs := DefaultRegistrations()
	if len(builtins) != len(regs) {
		t.Fatalf("Builtins() and DefaultRegistrations() differ in length: %d vs %d", len(builtins), len(regs))
	}
	for i, b := range builtins {
		if regs[i].Analyzer.ID() != b.ID {
			t.Fatalf("registration[%d].Analyzer.ID = %q, builtin.ID = %q", i, regs[i].Analyzer.ID(), b.ID)
		}
		if regs[i].Analyzer.Version() != b.Version {
			t.Fatalf("registration[%d].Analyzer.Version = %q, builtin.Version = %q", i, regs[i].Analyzer.Version(), b.Version)
		}
		if regs[i].Role != b.DefaultRole {
			t.Fatalf("registration[%d].Role = %q, builtin.DefaultRole = %q", i, regs[i].Role, b.DefaultRole)
		}
		tcodes := make([]string, len(b.FindingCodes))
		copy(tcodes, b.FindingCodes)
		slices.Sort(tcodes)
		if !slices.Equal(tcodes, b.FindingCodes) {
			t.Fatalf("builtin %q FindingCodes are not sorted", b.ID)
		}
	}
}

func TestCapabilitiesDefaultRoleIsAdvisory(t *testing.T) {
	for _, b := range Builtins() {
		if b.ID == AnalyzerCapabilities && b.DefaultRole != RoleAdvisory {
			t.Fatalf("capabilities default role = %q, want %q", b.DefaultRole, RoleAdvisory)
		}
	}
}

func TestDefaultRegistrationsRoleTable(t *testing.T) {
	want := map[string]string{
		AnalyzerHiddenContent: RoleGating,
		AnalyzerObfuscation:   RoleGating,
		AnalyzerAssets:        RoleGating,
		AnalyzerReferences:    RoleAdvisory,
		AnalyzerCapabilities:  RoleAdvisory,
	}
	for _, r := range DefaultRegistrations() {
		id := r.Analyzer.ID()
		wantRole, ok := want[id]
		if !ok {
			t.Fatalf("unexpected analyzer %q in default registrations", id)
		}
		if r.Role != wantRole {
			t.Fatalf("analyzer %q default role = %q, want %q", id, r.Role, wantRole)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("missing default registrations for %v", want)
	}
}
