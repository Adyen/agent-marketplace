package resolver

import (
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

const duplicateSelectionURL = "git@example.com:duplicate-selection.git"

func duplicateSelectionConfig(entries ...config.RegistryEntry) (*config.Config, map[string][]catalog.Entry) {
	cfg := &config.Config{Registries: entries}
	setCatalogKeys(cfg)
	catalogs := make(map[string][]catalog.Entry, len(entries))
	for _, entry := range entries {
		catalogs[entry.CatalogKey] = nil
	}
	return cfg, catalogs
}

func duplicateSelectionMalformed(cfg *config.Config) map[string][]skill.MalformedSkill {
	return malformedFixture(cfg.Registries[0].CatalogKey, skill.MalformedSkill{
		DirName: "broken",
		Path:    "/cache/duplicate-selection/broken",
		Error:   errors.New("invalid frontmatter"),
	})
}

func TestDirectDuplicateSelectionCertificatesAggregateTargetPartitions(t *testing.T) {
	cfg, catalogs := duplicateSelectionConfig(
		config.RegistryEntry{
			URL: duplicateSelectionURL, Ref: "main", Source: config.RepoCommitted,
			Skills: ks("broken"), Harnesses: []string{string(hcc.TargetClaude)},
		},
		config.RegistryEntry{
			URL: duplicateSelectionURL, Ref: "main", Source: config.RepoCommitted,
			Skills: ks("broken"), Harnesses: []string{string(hcc.TargetFactory)},
		},
	)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(duplicateSelectionMalformed(cfg)))
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one aggregated malformed diagnostic", errs)
	}
	var malformed *SelectedMalformedError
	if !errors.As(errs[0], &malformed) {
		t.Fatalf("error = %T, want SelectedMalformedError", errs[0])
	}
	partitions := MalformedSelectionPartitions(errs[0])
	want := []SelectionPartition{
		{Target: hcc.TargetClaude},
		{Target: hcc.TargetFactory},
	}
	if len(partitions) != len(want) || partitions[0] != want[0] || partitions[1] != want[1] {
		t.Fatalf("partitions = %#v, want %#v", partitions, want)
	}
}

func TestDirectDuplicateSelectionCertificatesAggregateScopes(t *testing.T) {
	cfg, catalogs := duplicateSelectionConfig(
		config.RegistryEntry{
			URL: duplicateSelectionURL, Ref: "main", Source: config.RepoCommitted,
			Skills: ks("broken"), Harnesses: []string{string(hcc.TargetClaude)},
		},
		config.RegistryEntry{
			URL: duplicateSelectionURL, Ref: "main", Source: config.UserPersonal,
			Skills: ks("broken"), Harnesses: []string{string(hcc.TargetClaude)},
		},
	)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(duplicateSelectionMalformed(cfg)))
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one aggregated malformed diagnostic", errs)
	}
	partitions := MalformedSelectionPartitions(errs[0])
	want := []SelectionPartition{
		{Target: hcc.TargetClaude, MachineScope: false},
		{Target: hcc.TargetClaude, MachineScope: true},
	}
	if len(partitions) != len(want) || partitions[0] != want[0] || partitions[1] != want[1] {
		t.Fatalf("partitions = %#v, want %#v", partitions, want)
	}
}

func TestDirectDuplicateSelectionCertificatesPreserveRequiredness(t *testing.T) {
	cfg, catalogs := duplicateSelectionConfig(
		config.RegistryEntry{
			URL: duplicateSelectionURL, Ref: "main", Source: config.RepoCommitted,
			Skills: ks("missing"), Harnesses: []string{string(hcc.TargetClaude)},
		},
		config.RegistryEntry{
			URL: duplicateSelectionURL, Ref: "main", Source: config.RepoCommitted,
			Required: true, Skills: ks("missing"), Harnesses: []string{string(hcc.TargetClaude)},
		},
	)

	_, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one aggregated not-found diagnostic", errs)
	}
	var notFound *SelectedNotFoundError
	if !errors.As(errs[0], &notFound) {
		t.Fatalf("error = %T, want SelectedNotFoundError", errs[0])
	}
	if !notFound.Required || IsSoftSelectedNotFound(errs[0]) {
		t.Fatalf("not-found = %#v, want strongest required policy", notFound)
	}
}

func TestDirectExactDuplicateSelectionCertificatesCollapse(t *testing.T) {
	cfg, catalogs := duplicateSelectionConfig(
		config.RegistryEntry{
			URL: duplicateSelectionURL, Ref: "main", Source: config.RepoCommitted,
			Skills: ks("missing"), Harnesses: []string{string(hcc.TargetClaude)},
		},
		config.RegistryEntry{
			URL: duplicateSelectionURL, Ref: "main", Source: config.RepoCommitted,
			Skills: ks("missing"), Harnesses: []string{string(hcc.TargetClaude)},
		},
	)

	_, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exact duplicate collapse to one diagnostic", errs)
	}
}

func TestDirectMultiTargetMalformedSelectionKeepsBothPartitions(t *testing.T) {
	cfg, catalogs := duplicateSelectionConfig(config.RegistryEntry{
		URL: duplicateSelectionURL, Ref: "main", Source: config.RepoCommitted,
		Skills: ks("broken"), Harnesses: []string{string(hcc.TargetClaude), string(hcc.TargetFactory)},
	})

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(duplicateSelectionMalformed(cfg)))
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one malformed diagnostic", errs)
	}
	partitions := MalformedSelectionPartitions(errs[0])
	if len(partitions) != 2 || partitions[0].Target != hcc.TargetClaude || partitions[1].Target != hcc.TargetFactory {
		t.Fatalf("partitions = %#v, want Claude and Factory", partitions)
	}
}

func TestDirectDuplicateNotFoundSelectionKeepsNotFoundRemediation(t *testing.T) {
	cfg, catalogs := duplicateSelectionConfig(
		config.RegistryEntry{
			URL: duplicateSelectionURL, Ref: "main", Source: config.RepoCommitted,
			Skills: ks("missing"), Harnesses: []string{string(hcc.TargetClaude)},
		},
		config.RegistryEntry{
			URL: duplicateSelectionURL, Ref: "main", Source: config.UserPersonal,
			Skills: ks("missing"), Harnesses: []string{string(hcc.TargetFactory)},
		},
	)

	_, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one aggregated not-found diagnostic", errs)
	}
	var notFound *SelectedNotFoundError
	if !errors.As(errs[0], &notFound) {
		t.Fatalf("error = %T, want selected-not-found", errs[0])
	}
	if notFound.Remediation() == "" {
		t.Fatal("not-found remediation must remain available")
	}
}
