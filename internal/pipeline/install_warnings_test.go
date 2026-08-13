package pipeline

import (
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestFilterRequirementWarnings(t *testing.T) {
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

	filtered := filterRequirementWarnings(warnings, []resourceIdentity{{kind: ir.KindSkill, name: "installed-skill"}})
	if len(filtered) != 1 {
		t.Fatalf("filterRequirementWarnings() returned %d warnings, want 1", len(filtered))
	}
	if !errors.Is(filtered[0], warnings[0]) {
		t.Fatalf("filterRequirementWarnings() returned %v, want %v", filtered[0], warnings[0])
	}

	all := filterRequirementWarnings(warnings, nil)
	if len(all) != 3 {
		t.Fatalf("filterRequirementWarnings(nil installed) returned %d warnings, want 3", len(all))
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

			expanded, err := expandInstalledCollectionResources(tt.installed, available, registries)
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

			filtered := filterRequirementWarnings(warnings, expanded)
			if len(filtered) != len(warnings) {
				t.Fatalf("filterRequirementWarnings() returned %d member warnings, want %d", len(filtered), len(warnings))
			}
		})
	}
}
