package resolver

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestAddNameCollisionSkipsNilWarnings(t *testing.T) {
	var typedNil *NameCollisionWarning
	incoming := &NameCollisionWarning{Kind: ir.KindSkill, Name: "shared"}
	for _, previous := range []error{nil, typedNil, fmt.Errorf("selection: %w", typedNil)} {
		got := addNameCollision([]error{previous}, incoming)
		if len(got) != 2 || got[0] != previous || got[1] != incoming {
			t.Fatalf("nil warning changed the result: %v", got)
		}
	}
}

func TestAddNameCollisionPreservesWrappedWarning(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(error) error
	}{
		{"direct", func(err error) error { return err }},
		{"wrapped", func(err error) error { return fmt.Errorf("selection: %w", err) }},
		{"nested", func(err error) error { return fmt.Errorf("resolution: %w", fmt.Errorf("selection: %w", err)) }},
		{"joined", func(err error) error { return errors.Join(errors.New("other issue"), err) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := &NameCollisionWarning{
				Kind: ir.KindSkill, Name: "shared", WinnerURL: "git@example.com:mandatory.git",
				WinnerPath: "team", WinnerRef: "main", RunnersUp: []string{"first", "repeat"},
			}
			incoming := &NameCollisionWarning{
				Kind: ir.KindSkill, Name: "shared", WinnerURL: "git@example.com:mandatory.git",
				WinnerPath: "team", WinnerRef: "main", RunnersUp: []string{"repeat", "new"},
			}
			unrelated := errors.New("unrelated issue")
			previous := tc.wrap(existing)
			got := addNameCollision([]error{unrelated, previous}, incoming)
			if len(got) != 2 || got[0] != unrelated || got[1] != previous {
				t.Fatalf("warning list changed instead of merging the collision: %v", got)
			}
			if !errors.Is(got[1], existing) {
				t.Fatal("existing collision warning lost its error identity")
			}
			if !slices.Equal(existing.RunnersUp, []string{"first", "repeat", "new"}) {
				t.Fatalf("runners-up = %v, want [first repeat new]", existing.RunnersUp)
			}
			if !slices.Equal(incoming.RunnersUp, []string{"repeat", "new"}) {
				t.Fatalf("incoming warning was modified: %v", incoming.RunnersUp)
			}
		})
	}
}

// sharedSource is one registry entry that selects the skill "shared", either
// directly or through a collection defined by the same registry.
type sharedSource struct {
	url        string
	source     config.Source
	required   bool
	collection bool
}

// sharedCollectionName names each registry's collection after its URL so
// collections never collide with one another; only their members do.
func sharedCollectionName(url string) string {
	name := strings.TrimSuffix(strings.TrimPrefix(url, "git@example.com:"), ".git")
	return "bundle-" + name
}

func resolveSharedSources(t *testing.T, sources []sharedSource) ([]ResolvedArtifact, []error) {
	t.Helper()
	cfg := &config.Config{}
	catalogs := make(map[string][]catalog.Entry)
	for _, s := range sources {
		entry := config.RegistryEntry{URL: s.url, Ref: "main", Source: s.source, Required: s.required}
		if s.collection {
			entry.Collections = ks(sharedCollectionName(s.url))
		} else {
			entry.Skills = ks("shared")
		}
		cfg.Registries = append(cfg.Registries, entry)
	}
	setCatalogKeys(cfg)
	for _, entry := range cfg.Registries {
		name := sharedCollectionName(entry.URL)
		collection := ir.NewCollection(name, "", "", []ir.CollectionMember{{Name: "shared", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
		catalogs[entry.CatalogKey] = []catalog.Entry{
			{Kind: ir.KindSkill, Name: "shared", Path: "/cache/" + entry.URL + "/skills/shared"},
			{Kind: ir.KindCollection, Name: name, Resource: collection, Path: "/cache/" + entry.URL + "/collections/" + name},
		}
	}
	return Resolve(cfg, catalogs, nil)
}

// describeShared renders every resolved "shared" artifact as
// "<scope> <registry> <via>" in a stable order.
func describeShared(artifacts []ResolvedArtifact) []string {
	var out []string
	for _, a := range artifacts {
		if a.Kind != ir.KindSkill || a.Name != "shared" {
			continue
		}
		scope := "repo"
		if a.Source.IsMachineScope() {
			scope = "machine"
		}
		via := "direct"
		if a.Via != "" {
			via = a.Via
		}
		out = append(out, scope+" "+a.RegistryURL+" "+via)
	}
	slices.Sort(out)
	return out
}

func machineOnly(described []string) []string {
	var out []string
	for _, d := range described {
		if strings.HasPrefix(d, "machine ") {
			out = append(out, d)
		}
	}
	return out
}

// collisionWarnings renders name-collision warnings as
// "<winner URL> <- <runner-up labels>" and fails on any other resolver error.
func collisionWarnings(t *testing.T, errs []error) []string {
	t.Helper()
	var out []string
	for _, err := range errs {
		var warning *NameCollisionWarning
		if !errors.As(err, &warning) {
			t.Fatalf("unexpected resolver error: %v", err)
		}
		out = append(out, warning.WinnerURL+" <- "+strings.Join(warning.RunnersUp, ","))
	}
	return out
}

// assertMachineOutputIndependentOfRepository proves the property the drift
// depends on: removing every repository-scope entry, as a sync from the
// user's home does, leaves the machine-scope selection unchanged.
func assertMachineOutputIndependentOfRepository(t *testing.T, sources []sharedSource, withRepository []string) {
	t.Helper()
	var machineSources []sharedSource
	for _, s := range sources {
		if s.source.IsMachineScope() {
			machineSources = append(machineSources, s)
		}
	}
	home, errs := resolveSharedSources(t, machineSources)
	collisionWarnings(t, errs)
	if got, want := machineOnly(withRepository), describeShared(home); !slices.Equal(got, want) {
		t.Fatalf("machine output depends on repository configuration:\nrepository sync %q\nhome sync       %q", got, want)
	}
}

// spec: resource-selection/collection-member-is-not-displaced-by-a-selection-in-the-other-scope
func TestResolveCollectionMembersResolvePerInstallScope(t *testing.T) {
	const (
		user  = "git@example.com:user.git"
		repo  = "git@example.com:repo.git"
		other = "git@example.com:other.git"
	)
	tests := []struct {
		name    string
		sources []sharedSource
		want    []string
	}{
		{
			name: "user collection member and repository skill",
			sources: []sharedSource{
				{url: user, source: config.UserPersonal, collection: true},
				{url: repo, source: config.RepoCommitted},
			},
			want: []string{"machine " + user + " collection:bundle-user", "repo " + repo + " direct"},
		},
		{
			name: "repository collection member and user skill",
			sources: []sharedSource{
				{url: user, source: config.UserPersonal},
				{url: repo, source: config.RepoCommitted, collection: true},
			},
			want: []string{"machine " + user + " direct", "repo " + repo + " collection:bundle-repo"},
		},
		{
			name: "user and repository collection members",
			sources: []sharedSource{
				{url: user, source: config.UserPersonal, collection: true},
				{url: repo, source: config.RepoCommitted, collection: true},
			},
			want: []string{"machine " + user + " collection:bundle-user", "repo " + repo + " collection:bundle-repo"},
		},
		{
			name: "same scope keeps direct precedence",
			sources: []sharedSource{
				{url: user, source: config.UserPersonal, collection: true},
				{url: other, source: config.UserPersonal},
			},
			want: []string{"machine " + other + " direct"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, errs := resolveSharedSources(t, tt.sources)
			if warnings := collisionWarnings(t, errs); len(warnings) != 0 {
				t.Fatalf("complementary selections must not warn: %q", warnings)
			}
			if described := describeShared(got); !slices.Equal(described, tt.want) {
				t.Fatalf("resolved %q, want %q", described, tt.want)
			}
			assertMachineOutputIndependentOfRepository(t, tt.sources, describeShared(got))
		})
	}
}

// spec: resource-selection/required-selection-through-a-collection-suppresses-a-repository-selection
func TestResolveRequiredCollectionSelectionSuppressesRepositorySource(t *testing.T) {
	const (
		mandatory = "git@example.com:mandatory.git"
		repo      = "git@example.com:repo.git"
	)
	deliveries := []struct {
		name                         string
		requiredCollection, repoColl bool
	}{
		{"required collection member and repository skill", true, false},
		{"required collection member and repository collection member", true, true},
		{"required skill and repository collection member", false, true},
	}
	for _, machineSource := range []config.Source{config.Organization, config.UserPersonal} {
		for _, d := range deliveries {
			t.Run(machineSource.String()+"/"+d.name, func(t *testing.T) {
				sources := []sharedSource{
					{url: mandatory, source: machineSource, required: true, collection: d.requiredCollection},
					{url: repo, source: config.RepoCommitted, collection: d.repoColl},
				}
				got, errs := resolveSharedSources(t, sources)
				via := "direct"
				if d.requiredCollection {
					via = "collection:bundle-mandatory"
				}
				if described, want := describeShared(got), []string{"machine " + mandatory + " " + via}; !slices.Equal(described, want) {
					t.Fatalf("resolved %q, want only the required resource %q", described, want)
				}
				if warnings, want := collisionWarnings(t, errs), []string{mandatory + " <- git@example.com:repo"}; !slices.Equal(warnings, want) {
					t.Fatalf("warnings %q, want %q", warnings, want)
				}
				assertMachineOutputIndependentOfRepository(t, sources, describeShared(got))
			})
		}
	}
}

// spec: resource-selection/required-suppression-keeps-the-repository-copy-from-the-required-source
func TestResolveRequiredSuppressionIgnoresSourceOrder(t *testing.T) {
	const mandatory = "git@example.com:mmm.git"
	for _, other := range []string{"git@example.com:aaa.git", "git@example.com:zzz.git"} {
		for _, requiredCollection := range []bool{false, true} {
			name := strings.TrimPrefix(other, "git@example.com:") + "/direct"
			via := "direct"
			if requiredCollection {
				name = strings.TrimPrefix(other, "git@example.com:") + "/collection"
				via = "collection:bundle-mmm"
			}
			t.Run(name, func(t *testing.T) {
				sources := []sharedSource{
					{url: mandatory, source: config.Organization, required: true, collection: requiredCollection},
					{url: mandatory, source: config.RepoCommitted},
					{url: other, source: config.RepoCommitted},
				}
				got, errs := resolveSharedSources(t, sources)
				want := []string{"machine " + mandatory + " " + via, "repo " + mandatory + " direct"}
				if described := describeShared(got); !slices.Equal(described, want) {
					t.Fatalf("resolved %q, want %q", described, want)
				}
				wantWarning := []string{mandatory + " <- " + strings.TrimSuffix(other, ".git")}
				if warnings := collisionWarnings(t, errs); !slices.Equal(warnings, wantWarning) {
					t.Fatalf("warnings %q, want %q", warnings, wantWarning)
				}
				assertMachineOutputIndependentOfRepository(t, sources, describeShared(got))
			})
		}
	}
}
