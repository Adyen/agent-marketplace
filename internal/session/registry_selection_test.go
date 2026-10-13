package session

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func registrySelectionTestRow(url string, kind config.V4SourceKind) Row {
	entry := config.RegistryEntry{URL: url, V4Kind: kind}
	ref, path := "main", "skills"
	if entry.IsLocal() {
		ref, path = "", ""
	}
	return Row{
		Artifact: resolver.ResolvedArtifact{
			Entry:        catalog.Entry{Kind: ir.KindSkill, Name: "review", SourceHarness: "devin"},
			RegistryURL:  url,
			RegistryPath: path,
			Ref:          ref,
			V4Kind:       kind,
			Source:       config.UserPersonal,
		},
		RegistryURL:   url,
		RegistryPath:  path,
		RegistryRef:   ref,
		HarnessSource: "devin",
	}
}

// spec: session-interactions/guarded-submission
func TestRegistryResourceSelectionUsesCurrentRowCoordinates(t *testing.T) {
	for _, test := range []struct {
		name string
		row  Row
		want config.V4SourceKind
	}{
		{name: "git", row: registrySelectionTestRow("https://example.test/repo.git", config.V4SourceGit), want: config.V4SourceGit},
		{name: "directory", row: registrySelectionTestRow(t.TempDir(), config.V4SourceDirectory), want: config.V4SourceDirectory},
		{name: "inferred git", row: registrySelectionTestRow("https://example.test/legacy.git", ""), want: ""},
		{name: "inferred directory", row: registrySelectionTestRow(t.TempDir(), ""), want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry, locator, err := RegistryResourceSelection(test.row)
			if err != nil {
				t.Fatalf("RegistryResourceSelection: %v", err)
			}
			if entry.URL != test.row.RegistryURL || entry.Ref != test.row.RegistryRef || entry.Path != test.row.RegistryPath || entry.V4Kind != test.want || entry.SourceHarness != test.row.Artifact.SourceHarness {
				t.Fatalf("registry entry = %#v", entry)
			}
			if entry.IsLocal() != (test.name == "directory" || test.name == "inferred directory") {
				t.Fatalf("entry local classification = %t", entry.IsLocal())
			}
			if locator.Kind != ir.KindSkill || locator.Name != "review" {
				t.Fatalf("resource locator = %#v", locator)
			}
		})
	}
}

// spec: session-interactions/guarded-submission
func TestUserResourceActionForLocalRegistryRows(t *testing.T) {
	directory := registrySelectionTestRow(t.TempDir(), config.V4SourceDirectory)
	git := registrySelectionTestRow("https://example.test/repo.git", config.V4SourceGit)
	relative := registrySelectionTestRow("./repo", config.V4SourceDirectory)
	cases := []struct {
		name string
		row  Row
		want string
	}{
		{name: "available directory installs", row: directory, want: UserMarketplaceActionInstall},
		{name: "enabled git resource removes", row: func() Row { row := git; row.EnabledGlobal = true; return row }(), want: UserMarketplaceActionRemove},
		{name: "required resource is hidden", row: func() Row { row := directory; row.Artifact.Required = true; return row }(), want: UserMarketplaceActionNone},
		{name: "malformed resource is hidden", row: func() Row { row := directory; row.IsMalformed = true; return row }(), want: UserMarketplaceActionNone},
		{name: "pending resource is hidden", row: func() Row { row := directory; row.PendingVerification = true; return row }(), want: UserMarketplaceActionNone},
		{name: "relative source cannot be installed in the user root", row: relative, want: UserMarketplaceActionNone},
		{name: "relative enabled source remains removable", row: func() Row { row := relative; row.EnabledGlobal = true; return row }(), want: UserMarketplaceActionRemove},
		{name: "missing origin is hidden", row: registrySelectionTestRow("", config.V4SourceDirectory), want: UserMarketplaceActionNone},
		{name: "unsupported source kind is hidden", row: registrySelectionTestRow("https://example.test/repo.git", config.V4SourceMarketplace), want: UserMarketplaceActionNone},
		{name: "unsupported resource kind is hidden", row: func() Row { row := directory; row.Artifact.Kind = ir.ResourceKind(99); return row }(), want: UserMarketplaceActionNone},
		{name: "unsupported source harness is hidden", row: func() Row { row := directory; row.Artifact.SourceHarness = "future-harness"; return row }(), want: UserMarketplaceActionNone},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := UserResourceAction(test.row)
			if err != nil || got != test.want {
				t.Fatalf("UserResourceAction() = %q, %v, want %q", got, err, test.want)
			}
		})
	}
}

var benchmarkUserResourceActionResult string

func BenchmarkUserResourceAction(b *testing.B) {
	row := registrySelectionTestRow("/tmp/registry", config.V4SourceDirectory)
	action, err := UserResourceAction(row)
	if err != nil || action != UserMarketplaceActionInstall {
		b.Fatal("available user directory fixture is not installable")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkUserResourceActionResult, _ = UserResourceAction(row)
	}
	b.StopTimer()
	if benchmarkUserResourceActionResult != UserMarketplaceActionInstall {
		b.Fatal("user directory capability changed during measurement")
	}
}
