package pipeline

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestFilterInstallWarnings(t *testing.T) {
	t.Parallel()

	warnings := []error{
		&resolver.UnmetRequirementWarning{
			ResourceKind: ir.KindSkill,
			ResourceName: "installed-skill",
			Requirement:  catalog.Requirement{Kind: ir.KindMcpServer, Name: "required-mcp"},
		},
		&resolver.UnmetRequirementWarning{
			ResourceKind: ir.KindSkill,
			ResourceName: "unrelated-skill",
			Requirement:  catalog.Requirement{Kind: ir.KindMcpServer, Name: "other-mcp"},
		},
		&resolver.UnmetRequirementWarning{
			ResourceKind: ir.KindCommand,
			ResourceName: "installed-skill",
			Requirement:  catalog.Requirement{Kind: ir.KindMcpServer, Name: "command-mcp"},
		},
		errors.New("unrelated warning"),
	}

	filtered := filterInstallWarnings(warnings, []resourceIdentity{{kind: ir.KindSkill, name: "installed-skill"}})
	if len(filtered) != 1 {
		t.Fatalf("filterInstallWarnings() returned %d warnings, want 1", len(filtered))
	}
	if !errors.Is(filtered[0], warnings[0]) {
		t.Fatalf("filterInstallWarnings() returned %v, want %v", filtered[0], warnings[0])
	}

	all := filterInstallWarnings(warnings, nil)
	if len(all) != 3 {
		t.Fatalf("filterInstallWarnings(nil installed) returned %d warnings, want 3", len(all))
	}
}

func TestExpandInstalledCollectionResourcesKeepsMemberWarnings(t *testing.T) {
	t.Parallel()

	const registryURL = "git@example.com:team/resources.git"
	available := []collection.Collection{{
		Name:              "operations",
		SourceRegistryURL: registryURL,
		SourceRef:         "main",
		SourcePath:        "resources",
		Resources: []collection.CollectionEntry{
			{Name: "member-skill", Kind: "skill"},
			{Name: "member-command", Kind: "command"},
		},
	}}
	registries := []config.RegistryEntry{{URL: registryURL, Ref: "main", Path: "resources"}}
	warnings := []error{
		&resolver.UnmetRequirementWarning{
			ResourceKind: ir.KindSkill,
			ResourceName: "member-skill",
			Requirement:  catalog.Requirement{Kind: ir.KindMcpServer, Name: "required-mcp"},
		},
		&resolver.UnmetRequirementWarning{
			ResourceKind: ir.KindCommand,
			ResourceName: "member-command",
			Requirement:  catalog.Requirement{Kind: ir.KindSkill, Name: "required-skill"},
		},
		&resolver.NameCollisionWarning{Kind: ir.KindSkill, Name: "member-skill", Winner: "winner", RunnersUp: []string{"other"}},
		&resolver.NameCollisionWarning{Kind: ir.KindCommand, Name: "member-command", Winner: "winner", RunnersUp: []string{"other"}},
	}

	tests := []struct {
		name      string
		installed []resourceIdentity
	}{
		{
			name:      "--all --kind collection",
			installed: []resourceIdentity{{kind: ir.KindCollection, name: "operations"}},
		},
		{
			name: "--all across kinds",
			installed: []resourceIdentity{
				{kind: ir.KindMcpServer, name: "other-mcp"},
				{kind: ir.KindCollection, name: "operations"},
			},
		},
		{
			name: "ad-hoc bundle",
			installed: []resourceIdentity{
				{kind: ir.KindSkill, name: "other-skill"},
				{kind: ir.KindCollection, name: "operations"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			expanded, err := expandInstalledCollectionResources(tt.installed, available, registries, ScopeRepo)
			if err != nil {
				t.Fatalf("expandInstalledCollectionResources() error = %v", err)
			}

			identities := make(map[resourceIdentity]struct{}, len(expanded))
			for _, identity := range expanded {
				identities[identity] = struct{}{}
			}
			for _, want := range []resourceIdentity{
				{kind: ir.KindCollection, name: "operations"},
				{kind: ir.KindSkill, name: "member-skill"},
				{kind: ir.KindCommand, name: "member-command"},
			} {
				if _, ok := identities[want]; !ok {
					t.Errorf("expanded identities do not contain %#v: %#v", want, expanded)
				}
			}

			filtered := filterInstallWarnings(warnings, expanded)
			if len(filtered) != len(warnings) {
				t.Fatalf("filterInstallWarnings() returned %d member warnings, want %d", len(filtered), len(warnings))
			}
		})
	}
}

func TestFilterInstallCollisionWarnings(t *testing.T) {
	t.Parallel()
	requirement := &resolver.UnmetRequirementWarning{ResourceKind: ir.KindSkill, ResourceName: "review", Requirement: catalog.Requirement{Kind: ir.KindMcpServer, Name: "dependency"}}
	collision := &resolver.NameCollisionWarning{Kind: ir.KindSkill, Name: " review ", Winner: "winner", RunnersUp: []string{"other"}}
	var nilCollision *resolver.NameCollisionWarning
	var nilRequirement *resolver.UnmetRequirementWarning
	warnings := []error{requirement, fmt.Errorf("wrapped: %w", collision), &resolver.NameCollisionWarning{Kind: ir.KindCommand, Name: "review"}, &resolver.NameCollisionWarning{Kind: ir.KindSkill, Name: "elsewhere"}, errors.New("unrelated"), nil, nilCollision, nilRequirement}
	original := append([]error(nil), warnings...)
	for _, tc := range []struct {
		name      string
		installed []resourceIdentity
		want      []int
	}{
		{"skill", []resourceIdentity{{kind: ir.KindSkill, name: " review "}}, []int{0, 1}},
		{"command", []resourceIdentity{{kind: ir.KindCommand, name: "review"}}, []int{2}},
		{"nil scope", nil, []int{0, 1, 2, 3}},
		{"empty scope", []resourceIdentity{}, []int{0, 1, 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := filterInstallWarnings(warnings, tc.installed)
			if len(got) != len(tc.want) {
				t.Fatalf("kept %d warnings want %d", len(got), len(tc.want))
			}
			for i, index := range tc.want {
				if got[i] != warnings[index] {
					t.Fatalf("warning %d identity/order changed", i)
				}
			}
			if !reflect.DeepEqual(warnings, original) {
				t.Fatal("filter mutated warning input")
			}
		})
	}
}

func TestPartitionInstalledNamesReportsUnselectedResources(t *testing.T) {
	resources := []resourceIdentity{
		{kind: ir.KindSkill, name: "review"},
		{kind: ir.KindSkill, name: "deploy"},
		{kind: ir.KindCommand, name: "cmd-one"},
		{kind: ir.KindCommand, name: "shared"},
		{kind: ir.KindSkill, name: "shared"},
	}
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "deploy"}},
		{Entry: catalog.Entry{Kind: ir.KindCommand, Name: "cmd-one"}},
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}},
		{Entry: catalog.Entry{Kind: ir.KindCommand, Name: "review"}},
	}
	installed, notSelected := partitionInstalledNames([]string{"cmd-one", "deploy", "review", "shared", "unknown"}, resources, resolved)
	if want := []string{"cmd-one", "deploy", "shared", "unknown"}; !reflect.DeepEqual(installed, want) {
		t.Fatalf("installed = %v, want %v", installed, want)
	}
	// A same-named resource of another kind does not count as the skill.
	if want := []string{"review"}; !reflect.DeepEqual(notSelected, want) {
		t.Fatalf("not selected = %v, want %v", notSelected, want)
	}
}
