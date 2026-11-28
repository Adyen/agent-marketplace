package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func collectionTestRow() row {
	collection := ir.NewCollection("genai-suite", "", "A verified toolkit",
		[]ir.CollectionMember{
			{Name: "genai-psx", Kind: ir.KindMcpServer},
			{Name: "code-review", Kind: ir.KindSkill},
		},
		[]string{"genai-base"}, nil, ir.SourceRef{})
	parent := mkRow(collection.Name(), withAvailable, withKind(ir.KindCollection))
	parent.Artifact.Description = collection.Description
	parent.Artifact.Resource = collection
	parent.Artifact.RegistryURL = "file:///registries/team.git"
	parent.Artifact.RegistryPath = "skills"
	parent.Artifact.Ref = "main"
	parent.Artifact.Source = config.RepoPersonal
	parent.Artifact.CatalogKey = "team"
	parent.RegistryURL = parent.Artifact.RegistryURL
	parent.RegistryPath = parent.Artifact.RegistryPath
	parent.RegistryRef = parent.Artifact.Ref
	return parent
}

func collectionCatalogMember(name string, kind ir.ResourceKind, url, ref, path, catalogKey string, excluded bool) row {
	member := mkRow(name, withAvailable, withKind(kind))
	member.Artifact.Path = "/catalog/" + catalogKey + "/" + name
	member.Artifact.RegistryURL = url
	member.Artifact.RegistryPath = path
	member.Artifact.Ref = ref
	member.Artifact.Source = config.RepoPersonal
	member.Artifact.CatalogKey = catalogKey
	member.RegistryURL = url
	member.RegistryRef = ref
	member.RegistryPath = path
	member.ExcludedLocally = excluded
	member.IsExcluded = excluded
	return member
}

// mkRow creates a row populated with the given skill name + scope flags for tests.
func mkRow(name string, opts ...func(*row)) row {
	r := row{
		Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: name, Path: "/cache/" + name}},
	}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func withGlobal(r *row)    { r.EnabledGlobal = true }
func withProject(r *row)   { r.EnabledProject = true }
func withLocal(r *row)     { r.EnabledLocal = true }
func withAvailable(r *row) { r.IsAvailable = true }
func withExcluded(r *row)  { r.ExcludedLocally = true; r.IsExcluded = true }
func withRequired(r *row)  { r.Artifact.Required = true }
func withKind(kind ir.ResourceKind) func(*row) {
	return func(r *row) { r.Artifact.Kind = kind }
}
func withHarnesses(harnesses ...string) func(*row) {
	return func(r *row) {
		r.Harnesses = harnesses
		r.Artifact.Harnesses = make([]hcc.Target, 0, len(harnesses))
		for _, h := range harnesses {
			r.Artifact.Harnesses = append(r.Artifact.Harnesses, hcc.Target(h))
		}
	}
}
func withHarnessSource(source string) func(*row) {
	return func(r *row) { r.HarnessSource = source }
}
func withRegistry(url, name string) func(*row) {
	return func(r *row) { r.RegistryURL = url; r.RegistryName = name }
}
func withRegistryMeta(ref, path string) func(*row) {
	return func(r *row) { r.RegistryRef = ref; r.RegistryPath = path }
}
func withMalformed(msg string) func(*row) {
	return func(r *row) { r.IsMalformed = true; r.MalformedMsg = msg }
}
func withDrift(reason materialize.DriftReason) func(*row) {
	return func(r *row) {
		r.DriftItem = &materialize.DriftItem{Name: r.Artifact.Name, Reason: reason}
	}
}

func sectionPaths(items []flatItem) []string {
	var paths []string
	for _, it := range items {
		if it.Kind == flatSection {
			paths = append(paths, it.SectionPath)
		}
	}
	return paths
}

func skillNamesUnder(items []flatItem, sectionPath string) []string {
	var names []string
	inSection := false
	for _, it := range items {
		if it.Kind == flatSection {
			inSection = it.SectionPath == sectionPath ||
				(inSection && isDescendantPath(it.SectionPath, sectionPath))
			continue
		}
		if inSection && it.Kind == flatArtifact {
			names = append(names, it.Row.Artifact.Name)
		}
	}
	return names
}

func isDescendantPath(child, parent string) bool {
	return len(child) > len(parent) &&
		child[:len(parent)] == parent &&
		child[len(parent)] == ':'
}

// spec: tui-navigation/single-status-list-at-minimum-size
func TestBuildStatusTreeGroupsByStatus(t *testing.T) {
	rows := []row{
		mkRow("req-skill", withRequired, withProject),
		mkRow("enabled-skill", withProject),
		mkRow("pending-skill", withProject, withDrift(materialize.DriftMissing)),
		mkRow("broken-skill", withProject, withDrift(materialize.DriftBroken)),
		mkRow("available-skill", withAvailable),
		mkRow("bad-skill", withMalformed("yaml parse error")),
	}

	items := buildStatusTree(rows, nil)

	for path, wantName := range map[string]string{
		"status:required":  "req-skill",
		"status:enabled":   "enabled-skill",
		"status:pending":   "pending-skill",
		"status:broken":    "broken-skill",
		"status:available": "available-skill",
		"status:malformed": "bad-skill",
	} {
		got := skillNamesUnder(items, path)
		if len(got) != 1 || got[0] != wantName {
			t.Fatalf("%s should contain %s, got %v", path, wantName, got)
		}
	}
}

// spec: tui-navigation/expanding-a-collection-shows-its-mcp-servers-and-skills
func TestCollectionMembersAppearUnderParentAcrossStatusAndKind(t *testing.T) {
	parent := collectionTestRow()
	knownRows := []row{
		collectionCatalogMember("genai-psx", ir.KindMcpServer, parent.RegistryURL, parent.RegistryRef, parent.RegistryPath, parent.Artifact.CatalogKey, false),
		collectionCatalogMember("code-review", ir.KindSkill, parent.RegistryURL, parent.RegistryRef, parent.RegistryPath, parent.Artifact.CatalogKey, false),
	}
	want := []string{"genai-suite", "genai-psx", "code-review", "genai-base"}
	for _, settings := range []viewSettings{
		{GroupBy: axisStatus},
		{GroupBy: axisSource},
		{GroupBy: axisKind},
	} {
		items := buildGroupedTreeWithRows([]row{parent}, knownRows, settings, nil)
		var rows []flatItem
		for _, item := range items {
			if item.Kind == flatArtifact {
				rows = append(rows, item)
			}
		}
		if len(rows) != len(want) {
			t.Fatalf("%s grouping rows = %d, want %d: %#v", settings.GroupBy, len(rows), len(want), rows)
		}
		for i, name := range want {
			if rows[i].Row.Artifact.Name != name {
				t.Fatalf("%s grouping row %d = %q, want %q", settings.GroupBy, i, rows[i].Row.Artifact.Name, name)
			}
		}
		if rows[1].Row.Artifact.Kind != ir.KindMcpServer || rows[2].Row.Artifact.Kind != ir.KindSkill || rows[3].Row.Artifact.Kind != ir.KindCollection {
			t.Fatalf("%s grouping member kinds = %v, %v, %v", settings.GroupBy, rows[1].Row.Artifact.Kind, rows[2].Row.Artifact.Kind, rows[3].Row.Artifact.Kind)
		}
		for _, child := range rows[1:] {
			if child.Depth != rows[0].Depth+1 {
				t.Errorf("%s grouping child %q depth = %d, want %d", settings.GroupBy, child.Row.Artifact.Name, child.Depth, rows[0].Depth+1)
			}
		}
	}
}

// spec: tui-navigation/expanding-a-collection-shows-its-mcp-servers-and-skills
// spec: tui-interactions/installing-a-single-member-from-an-available-collection
func TestGitCollectionMemberRequiresEligibleCatalogBinding(t *testing.T) {
	const (
		ownerURL  = "file:///registries/owner.git"
		memberURL = "file:///registries/member.git"
	)
	tests := []struct {
		name     string
		member   ir.CollectionMember
		rows     []row
		eligible bool
		wantURL  string
		wantPath string
		wantKey  string
	}{
		{
			name: "missing catalog member",
			member: ir.CollectionMember{
				Name: "review", Kind: ir.KindSkill, Registry: memberURL, Path: "tools",
			},
		},
		{
			name: "path mismatch",
			member: ir.CollectionMember{
				Name: "review", Kind: ir.KindSkill, Registry: memberURL, Path: "tools",
			},
			rows: []row{collectionCatalogMember("review", ir.KindSkill, memberURL, "main", "skills", "member-skills", false)},
		},
		{
			name: "excluded catalog member",
			member: ir.CollectionMember{
				Name: "review", Kind: ir.KindSkill, Registry: memberURL, Path: "tools",
			},
			rows: []row{collectionCatalogMember("review", ir.KindSkill, memberURL, "main", "tools", "member-tools", true)},
		},
		{
			name: "ambiguous pathless member",
			member: ir.CollectionMember{
				Name: "review", Kind: ir.KindSkill, Registry: memberURL,
			},
			rows: []row{
				collectionCatalogMember("review", ir.KindSkill, memberURL, "main", "tools/a", "member-a", false),
				collectionCatalogMember("review", ir.KindSkill, memberURL, "main", "tools/b", "member-b", false),
			},
		},
		{
			name: "same owner",
			member: ir.CollectionMember{
				Name: "review", Kind: ir.KindSkill,
			},
			rows:     []row{collectionCatalogMember("review", ir.KindSkill, ownerURL, "main", "collections", "owner", false)},
			eligible: true,
			wantURL:  ownerURL,
			wantPath: "collections",
			wantKey:  "owner",
		},
		{
			name: "configured cross registry",
			member: ir.CollectionMember{
				Name: "review", Kind: ir.KindSkill, Registry: memberURL, Ref: "main", Path: "./tools/",
			},
			rows:     []row{collectionCatalogMember("review", ir.KindSkill, "file:///registries/member", "main", "tools", "member-tools", false)},
			eligible: true,
			wantURL:  "file:///registries/member",
			wantPath: "tools",
			wantKey:  "member-tools",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := mkRow("suite", withAvailable, withKind(ir.KindCollection))
			parent.Artifact.Resource = ir.NewCollection("suite", "", "", []ir.CollectionMember{tt.member}, nil, nil, ir.SourceRef{})
			parent.Artifact.RegistryURL = ownerURL
			parent.Artifact.RegistryPath = "collections"
			parent.Artifact.Ref = "main"
			parent.Artifact.Source = config.RepoPersonal
			parent.Artifact.CatalogKey = "owner"
			parent.RegistryURL = ownerURL
			parent.RegistryPath = "collections"
			parent.RegistryRef = "main"

			var got *row
			for _, item := range buildGroupedTreeWithRows([]row{parent}, tt.rows, viewSettings{GroupBy: axisStatus}, nil) {
				if item.Kind == flatArtifact && item.Row.Artifact.Name == "review" {
					got = &item.Row
					break
				}
			}
			if got == nil {
				t.Fatal("collection member was not displayed")
			}
			if !tt.eligible {
				if got.IsAvailable || !got.PendingVerification ||
					got.RegistryURL != "" || got.RegistryPath != "" || got.Artifact.CatalogKey != "" {
					t.Fatalf("ineligible member = %#v, want display-only row without a source", got)
				}
				if denial := session.RowToggleDenialFor(*got); denial != session.RowTogglePendingVerification {
					t.Fatalf("ineligible member toggle denial = %v, want pending verification", denial)
				}
				return
			}
			if !got.IsAvailable || got.RegistryURL != tt.wantURL || got.RegistryPath != tt.wantPath ||
				got.Artifact.CatalogKey != tt.wantKey || got.Artifact.Path == "" {
				t.Fatalf("eligible member = %#v, want configured catalog row %s/%s (%s)", got, tt.wantURL, tt.wantPath, tt.wantKey)
			}
		})
	}
}

// spec: tui-navigation/single-status-list-at-minimum-size
func TestBuildStatusTreeKeepsFixedSectionOrder(t *testing.T) {
	// Inserted in reverse order; the tree must emit the fixed bucket order.
	rows := []row{
		mkRow("bad", withMalformed("parse")),
		mkRow("avail", withAvailable),
		mkRow("req", withRequired),
		mkRow("on", withGlobal),
	}
	items := buildStatusTree(rows, nil)
	want := []string{"status:required", "status:enabled", "status:available", "status:malformed"}
	got := sectionPaths(items)
	if len(got) != len(want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sections = %v, want %v", got, want)
		}
	}
}

func TestStatusBucketPathsUseSharedIDsAndUnknownFallback(t *testing.T) {
	cases := []struct {
		name   string
		bucket statusBucket
		want   string
	}{
		{"required", statusRequired, "status:required"},
		{"enabled", statusEnabled, "status:enabled"},
		{"pending", statusPending, "status:pending"},
		{"stale", statusStale, "status:stale"},
		{"broken", statusBroken, "status:broken"},
		{"available", statusAvailable, "status:available"},
		{"malformed", statusMalformed, "status:malformed"},
		{"negative", statusBucket(-1), "status:unknown"},
		{"large", statusBucket(255), "status:unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.bucket.path(); got != tc.want {
				t.Fatalf("path() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildStatusTreeFoldStateHidesChildren(t *testing.T) {
	rows := []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withAvailable, withRegistry("team.git", "team")),
	}
	foldState := map[string]bool{"status:enabled": true}

	items := buildStatusTree(rows, foldState)

	for _, it := range items {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == "audit" {
			t.Fatalf("collapsed status:enabled should hide audit skill")
		}
	}

	// Other top-level sections remain visible.
	if got := skillNamesUnder(items, "status:available"); len(got) != 1 || got[0] != "deploy" {
		t.Fatalf("status:available must stay visible, got %v", got)
	}

	// The collapsed section header itself is still emitted, with Collapsed=true.
	var header *flatItem
	for i := range items {
		if items[i].Kind == flatSection && items[i].SectionPath == "status:enabled" {
			header = &items[i]
			break
		}
	}
	if header == nil {
		t.Fatalf("collapsed status:enabled header should still be present")
	}
	if !header.Collapsed {
		t.Fatalf("status:enabled header should report Collapsed=true")
	}
}

func TestBuildStatusTreeSectionCountsMatchChildren(t *testing.T) {
	rows := []row{
		mkRow("a", withGlobal, withRegistry("sec.git", "security")),
		mkRow("b", withGlobal, withRegistry("sec.git", "security")),
		mkRow("c", withGlobal, withRegistry("sec.git", "security")),
	}

	items := buildStatusTree(rows, nil)

	for _, it := range items {
		if it.Kind != flatSection {
			continue
		}
		if it.SectionPath == "status:enabled" && it.SectionCount != 3 {
			t.Fatalf("status:enabled count = %d, want 3", it.SectionCount)
		}
	}
}

func TestBuildStatusTreeEmptyInputYieldsNoItems(t *testing.T) {
	items := buildStatusTree(nil, nil)
	if len(items) != 0 {
		t.Fatalf("buildStatusTree(nil) should return empty slice, got %d items", len(items))
	}
}

// A malformed skill keeps the registry it came from, so its label and
// detail name that registry instead of falling back to (local), and
// same-named malformed skills from two registries keep distinct identities.
// spec: tui-rendering/malformed-rows-name-their-registry
func TestBuildRowsMalformedRowsCarryTheirRegistry(t *testing.T) {
	broken := func(dir string) catalog.LoadedCatalog {
		return catalog.LoadedCatalog{Malformed: []skill.MalformedSkill{{DirName: dir, Path: "skills/" + dir, Error: fmt.Errorf("yaml parse error")}}}
	}
	catalogs := map[string]catalog.LoadedCatalog{"fixture-key": broken("broken-skill"), "other-key": broken("broken-skill")}
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: "file:///registries/fixture.git", Ref: "main", Path: "skills", CatalogKey: "fixture-key"},
		{URL: "https://example.invalid/team/other.git", Ref: "main", Path: "skills", CatalogKey: "other-key"},
	}}

	rows := buildRows(cfg, catalogs, nil, nil, materialize.DriftReport{})

	byRegistry := map[string]row{}
	for _, r := range rows {
		if r.IsMalformed {
			byRegistry[r.RegistryName] = r
		}
	}
	fixture, ok := byRegistry["fixture"]
	if !ok || len(byRegistry) != 2 {
		t.Fatalf("malformed rows by registry = %#v, want fixture and other", byRegistry)
	}
	if fixture.RegistryURL != "file:///registries/fixture.git" || fixture.RegistryRef != "main" || fixture.RegistryPath != "skills" || fixture.Artifact.CatalogKey != "fixture-key" {
		t.Fatalf("fixture malformed row = %#v, want its registry metadata and catalog key", fixture)
	}
	if got := resourceLabel(fixture); !strings.Contains(got, "fixture/broken-skill") {
		t.Fatalf("label = %q, want fixture/broken-skill", got)
	}
	if rowKey(fixture) == rowKey(byRegistry["other"]) {
		t.Fatal("same-named malformed skills from two registries share a row key")
	}
	// Without registry metadata the label still falls back to (local).
	for _, r := range buildRows(nil, map[string]catalog.LoadedCatalog{"unknown": broken("orphan-skill")}, nil, nil, materialize.DriftReport{}) {
		if got := resourceLabel(r); r.IsMalformed && !strings.Contains(got, "(local)/orphan-skill") {
			t.Fatalf("label without registry metadata = %q, want (local)/orphan-skill", got)
		}
	}
}

// A malformed row reports the kind its failed load was for in its label and
// detail, and "unknown" when the producer could not classify it.
// spec: tui-rendering/malformed-rows-show-their-kind
func TestBuildRowsMalformedRowsShowTheirKind(t *testing.T) {
	catalogs := map[string]catalog.LoadedCatalog{"k": {Malformed: []skill.MalformedSkill{
		{DirName: "deploy.md", Path: "commands/deploy.md", Kind: ir.KindCommand.String(), Error: fmt.Errorf("ingest.claude.load-command-failed: yaml")},
		{DirName: "catalog.discover.sub-bundles-ignored", Path: "skills", Error: fmt.Errorf("catalog.discover.sub-bundles-ignored: ignored")},
	}}}
	cfg := &config.Config{Registries: []config.RegistryEntry{{URL: "file:///registries/fixture.git", Ref: "main", CatalogKey: "k"}}}
	labels := map[string]string{}
	for _, r := range buildRows(cfg, catalogs, nil, nil, materialize.DriftReport{}) {
		if r.IsMalformed {
			labels[r.Artifact.Name] = resourceLabel(r)
		}
	}
	if got := labels["deploy.md"]; got != "command fixture/deploy.md" {
		t.Fatalf("malformed command label = %q, want command fixture/deploy.md", got)
	}
	if got := labels["catalog.discover.sub-bundles-ignored"]; !strings.HasPrefix(got, "unknown ") {
		t.Fatalf("unclassified malformed label = %q, want an unknown kind", got)
	}
}

func TestBuildRowsIncludesMalformedFromCatalog(t *testing.T) {
	// Regression test: malformed skills from catalog must
	// appear in buildRows output so the TUI renders them.
	catalogs := map[string]catalog.LoadedCatalog{
		"test-reg": {
			Entries: []catalog.Entry{
				{Name: "good-skill", Kind: ir.KindSkill, Path: "skills/good-skill"},
			},
			Malformed: []skill.MalformedSkill{
				{DirName: "broken-skill", Path: "skills/broken-skill", Error: fmt.Errorf("ingest.factory.load-skill-failed: yaml parse error")},
			},
			Resources: map[string]ir.Resource{
				"skill\x00good-skill": ir.NewSkill("good-skill", "desc", "body", nil, nil, ir.SourceRef{File: "skills/good-skill/SKILL.md", Line: 1}),
			},
		},
	}

	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "/tmp/test-reg", Default: true, CatalogKey: "test-reg"},
		},
	}

	rows := buildRows(cfg, catalogs, nil, nil, materialize.DriftReport{})

	malformedNames := map[string]bool{}
	for _, r := range rows {
		if r.IsMalformed {
			malformedNames[r.Artifact.Name] = true
		}
	}
	if !malformedNames["broken-skill"] {
		t.Fatalf("expected malformed entry 'broken-skill' in rows; got malformed names: %v", malformedNames)
	}

	// Also verify the good skill appears as available (not resolved).
	foundGood := false
	for _, r := range rows {
		if r.Artifact.Name == "good-skill" && !r.IsMalformed {
			foundGood = true
		}
	}
	if !foundGood {
		t.Fatalf("expected good-skill entry in rows")
	}

	// Verify the malformed entry appears in the rendered tree.
	items := buildStatusTree(rows, nil)
	malformedPaths := sectionPaths(items)
	foundMalformedSection := false
	for _, p := range malformedPaths {
		if p == "status:malformed" {
			foundMalformedSection = true
		}
	}
	if !foundMalformedSection {
		t.Fatalf("expected status:malformed section in tree; got paths: %v", malformedPaths)
	}

	got := skillNamesUnder(items, "status:malformed")
	if len(got) != 1 || got[0] != "broken-skill" {
		t.Fatalf("status:malformed should contain 'broken-skill', got %v", got)
	}
}
