package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
	inspectiontest "github.com/Adyen/agent-marketplace/internal/testutil/marketplace/inspection"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type inspectionFixture = inspectiontest.Fixture

func newInspectionFixture(t *testing.T, mutate inspectiontest.Mutate) *inspectionFixture {
	return inspectiontest.NewFixture(t, mutate)
}

func newInspectionFixtureContent(t *testing.T, primary []byte, support map[string][]byte, mutate inspectiontest.Mutate) *inspectionFixture {
	return inspectiontest.NewFixtureContent(t, primary, support, mutate)
}

func inspectionFixtureRow(f *inspectionFixture) row {
	endpoint := f.Endpoint()
	item := f.Item()
	return row{
		Artifact: resolver.ResolvedArtifact{
			Entry:                 catalog.Entry{Kind: ir.KindSkill, Name: item.Name, Description: item.Description},
			IsMarketplace:         true,
			MarketplaceURL:        endpoint.URL,
			MarketplaceInstanceID: endpoint.InstanceID,
			MarketplaceRegistryID: item.RegistryID,
		},
		IsAvailable:          true,
		MarketplaceDiscovery: &marketplaceDiscovery{Endpoint: endpoint, Item: item},
	}
}

func inspectionModel(t *testing.T, r row) Model {
	t.Helper()
	m := modelWithSelectedRow(t, r)
	m.width = 100
	m.height = 30
	return m
}

func inspectionModelDirs(t *testing.T, r row, repoRoot, homeDir string, opts ...Option) Model {
	t.Helper()
	m := NewModel(repoRoot, homeDir, opts...)
	m.loading = false
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	m.width = 100
	m.height = 30
	return m
}

// spec: tui-navigation/discovery-only-marketplace-collection-inspection-stays-read-only-and-fails-closed
func TestMarketplaceCollectionInspectionLoadsVerifiedMembersReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name         string
		tamper       bool
		fetchFailure bool
	}{
		{name: "verified"},
		{name: "digest mismatch", tamper: true},
		{name: "manifest 404", fetchFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest, payload, digest := marketplaceCollectionManifest(t)
			if tc.name == "verified" {
				manifest.Members[0].Name = "genai-psx\u202e"
				var err error
				payload, digest, err = hccartifact.EncodeCollectionManifest(manifest)
				if err != nil {
					t.Fatalf("encode hostile collection member: %v", err)
				}
			}
			if tc.tamper {
				payload = append(append([]byte(nil), payload...), ' ')
			}
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch r.URL.Path {
				case "/.well-known/agent-marketplace.json":
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("instance-1", "https://market.example"))
				case "/api/v1/manifests/" + digest:
					if tc.fetchFailure {
						http.Error(w, "manifest unavailable", http.StatusNotFound)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(payload)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			repoRoot, homeDir := t.TempDir(), t.TempDir()
			controlled := map[string][]byte{
				filepath.Join(repoRoot, ".agents", "config.yaml"):                     []byte("apiVersion: agent-marketplace/v4\n"),
				filepath.Join(repoRoot, ".agents", "lock.yaml"):                       []byte("apiVersion: v4\nresources: []\n"),
				filepath.Join(repoRoot, ".factory", "skills", "retained", "SKILL.md"): []byte("# Retained\n"),
				filepath.Join(homeDir, ".agents", "config.yaml"):                      []byte("apiVersion: agent-marketplace/v4\n"),
			}
			for path, data := range controlled {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			discovered := marketplaceDiscoveryRow("collection", manifest.Name, mpapi.AvailabilityAvailable)
			discovered.MarketplaceDiscovery.Endpoint.URL = server.URL
			discovered.MarketplaceDiscovery.Item.Revision = digest
			discovered.MarketplaceDiscovery.Item.ContentDigest = digest
			m := inspectionModelDirs(t, discovered, repoRoot, homeDir)
			m, cmd := enterDetail(t, m)
			if cmd == nil {
				t.Fatal("opening a collection did not schedule its verified manifest inspection")
			}
			updated, _ := m.Update(cmd())
			m = updated.(Model)
			out := ansi.Strip(m.detailViewport.View())
			if tc.tamper || tc.fetchFailure {
				for _, want := range []string{"unavailable", "No verified members", "space to install"} {
					if !strings.Contains(out, want) {
						t.Fatalf("unverified collection detail missing %q:\n%s", want, out)
					}
				}
				if !strings.Contains(out, "Marketplace collection inspection unavailable:") {
					t.Fatalf("failed inspection omitted its actionable error:\n%s", out)
				}
				if !strings.Contains(out, "No verified members were loaded. Press space to install this exact revision, or esc to go back.") {
					t.Fatalf("failed inspection error was not actionable:\n%s", out)
				}
				if tc.fetchFailure && !strings.Contains(out, "404") {
					t.Fatalf("fetch failure omitted its HTTP status:\n%s", out)
				}
				for _, want := range []string{"genai-suite", "Revision:", "Digest:", digest} {
					if !strings.Contains(out, want) {
						t.Fatalf("failed inspection hid collection metadata %q:\n%s", want, out)
					}
				}
				if got := strings.Count(out, digest); got != 2 {
					t.Fatalf("failed inspection digest count = %d, want complete revision and digest:\n%s", got, out)
				}
				for _, forbidden := range []string{"mcpserver:", "skill:", "genai-psx", "code-review"} {
					if strings.Contains(out, forbidden) {
						t.Fatalf("failed inspection exposed unverified member %q:\n%s", forbidden, out)
					}
				}
			} else {
				if !strings.Contains(out, "Collection manifest verified.") || strings.Contains(out, "Discovery metadata only") {
					t.Fatalf("verified collection detail has incorrect verification status:\n%s", out)
				}
				lines := strings.Split(out, "\n")
				memberLines := []string{
					"Contains (2):",
					"  • mcpserver: genai-psx\\u202E",
					"  • skill: code-review",
				}
				foundMembers := false
				for i, line := range lines {
					if strings.TrimRight(line, " ") != memberLines[0] || i+len(memberLines) > len(lines) {
						continue
					}
					foundMembers = true
					for j, want := range memberLines[1:] {
						if got := strings.TrimRight(lines[i+j+1], " "); got != want {
							foundMembers = false
							break
						}
					}
					if foundMembers {
						break
					}
				}
				if !foundMembers {
					t.Fatalf("verified collection members were not rendered on separate lines:\n%s", out)
				}
				if strings.Contains(out, `\x0A`) || strings.ContainsRune(out, '\u202e') {
					t.Fatalf("verified collection member summary was not safely rendered:\n%s", out)
				}
				current, found := m.rowByKey(m.detailKey)
				if !found {
					t.Fatal("verified collection row disappeared after manifest inspection")
				}
				var member row
				for _, candidate := range collectionMemberRows(current, m.rows) {
					if candidate.Artifact.Kind == ir.KindSkill && candidate.Artifact.Name == "code-review" {
						member = candidate
					}
				}
				options, err := discoveryInstallOptions(homeDir, member, scopeGlobal)
				if err != nil {
					t.Fatalf("Marketplace member install options: %v", err)
				}
				if options.ExactMarketplaceLocator == nil || options.ExactMarketplaceLocator.Registry != "registry-1" ||
					options.ExactMarketplaceLocator.Name != "code-review" || options.ExactMarketplaceLocator.Revision != "sha256:"+strings.Repeat("c", 64) {
					t.Fatalf("Marketplace member locator = %#v", options.ExactMarketplaceLocator)
				}
			}
			if len(requests) != 2 || requests[0] != "GET /.well-known/agent-marketplace.json" || requests[1] != "GET /api/v1/manifests/"+digest {
				t.Fatalf("collection inspection requests = %v", requests)
			}
			for path, want := range controlled {
				if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
					t.Fatalf("inspection mutated %s (err=%v)", path, err)
				}
			}
		})
	}
}

// spec: tui-navigation/discovery-only-marketplace-collection-inspection-stays-read-only-and-fails-closed
// spec: tui-interactions/installing-a-single-member-from-an-available-collection
func TestMarketplaceCollectionMemberMatchesExactEndpointAndRevision(t *testing.T) {
	const endpoint = "https://market.example"
	const revision = "sha256:target"
	parent := marketplaceDiscoveryRow("collection", "suite", mpapi.AvailabilityAvailable)
	parent.MarketplaceDiscovery.Endpoint.URL = endpoint
	parent.Artifact.Resource = ir.NewCollection("suite", "", "", []ir.CollectionMember{
		{Name: "review", Kind: ir.KindSkill, Registry: "leaf-registry", Ref: revision},
	}, nil, nil, ir.SourceRef{})
	candidate := func(url, ref string) row {
		r := marketplaceDiscoveryRow("skill", "review", mpapi.AvailabilityAvailable)
		r.MarketplaceDiscovery.Endpoint.URL = url
		r.MarketplaceDiscovery.Item.RegistryID = "leaf-registry"
		r.MarketplaceDiscovery.Item.Revision = ref
		r.MarketplaceDiscovery.Item.ContentDigest = ref
		return r
	}
	wrongRevision := candidate(endpoint, "sha256:other")
	wrongEndpoint := candidate("https://other.example", revision)
	wrongKind := marketplaceDiscoveryRow("mcpserver", "review", mpapi.AvailabilityAvailable)
	wrongKind.MarketplaceDiscovery.Endpoint.URL = endpoint
	wrongKind.MarketplaceDiscovery.Item.RegistryID = "leaf-registry"
	wrongKind.MarketplaceDiscovery.Item.Revision = revision
	wrongKind.MarketplaceDiscovery.Item.ContentDigest = revision
	wrongKind.Artifact.MarketplaceRegistryID = "leaf-registry"
	wrongKind.Artifact.MarketplaceResourceName = "review"
	wrongKind.Artifact.MarketplaceDigest = revision
	exact := candidate(endpoint, revision)

	members := collectionMemberRows(parent, []row{wrongKind, wrongRevision, wrongEndpoint, exact})
	if len(members) != 1 || members[0].MarketplaceDiscovery == nil {
		t.Fatalf("matched Marketplace members = %#v, want the exact discovered row", members)
	}
	if members[0].Artifact.Kind != ir.KindSkill {
		t.Fatalf("matched Marketplace member kind = %s, want %s", members[0].Artifact.Kind, ir.KindSkill)
	}
	got := members[0].MarketplaceDiscovery
	if got.Endpoint.URL != endpoint || got.Item.Revision != revision || got.Item.ContentDigest != revision {
		t.Fatalf("member identity = endpoint %q revision %q digest %q", got.Endpoint.URL, got.Item.Revision, got.Item.ContentDigest)
	}
}

// spec: tui-navigation/discovery-only-marketplace-collection-inspection-stays-read-only-and-fails-closed
// spec: tui-interactions/installing-a-single-member-from-an-available-collection
func TestMarketplaceCollectionBareMemberDoesNotBorrowDiscoveryLocator(t *testing.T) {
	parent := marketplaceDiscoveryRow("collection", "suite", mpapi.AvailabilityAvailable)
	parent.Artifact.Resource = ir.NewCollection("suite", "", "", []ir.CollectionMember{
		{Name: "review", Kind: ir.KindSkill},
	}, nil, nil, ir.SourceRef{})
	candidate := marketplaceDiscoveryRow("skill", "review", mpapi.AvailabilityAvailable)

	members := collectionMemberRows(parent, []row{candidate})
	if len(members) != 1 || members[0].MarketplaceDiscovery != nil || !members[0].PendingVerification {
		t.Fatalf("bare Marketplace member = %#v, want a non-installable pending row", members)
	}
	if got := session.RowToggleDenialFor(members[0]); got != session.RowTogglePendingVerification {
		t.Fatalf("bare Marketplace member toggle denial = %v, want pending verification", got)
	}
}

// spec: tui-interactions/installing-a-single-member-from-an-available-collection
func TestMarketplaceSyntheticMemberDoesNotInheritCollectionSelection(t *testing.T) {
	const revision = "sha256:member"
	parent := marketplaceDiscoveryRow("collection", "suite", mpapi.AvailabilityAvailable)
	parent.EnabledProject = true
	parent.MarketplaceSelections = map[config.Source]marketplaceSelection{
		config.RepoCommitted: {Entry: parent.MarketplaceDiscovery.Endpoint, Pin: config.MarketplaceResourcePin{
			RegistryID: "collection-registry", Kind: "collection", Name: "suite", Revision: revision, Digest: revision, Direct: true,
		}},
	}
	parent.Artifact.Resource = ir.NewCollection("suite", "", "", []ir.CollectionMember{
		{Name: "review", Kind: ir.KindSkill, Registry: "leaf-registry", Ref: revision},
	}, nil, nil, ir.SourceRef{})

	members := collectionMemberRows(parent, nil)
	if len(members) != 1 {
		t.Fatalf("synthetic Marketplace members = %#v", members)
	}
	member := members[0]
	if !member.IsAvailable || member.EnabledProject || len(member.MarketplaceSelections) != 0 {
		t.Fatalf("synthetic member inherited collection state: %#v", member)
	}
	if member.MarketplaceDiscovery == nil || member.MarketplaceDiscovery.Endpoint.URL != parent.MarketplaceDiscovery.Endpoint.URL ||
		member.MarketplaceDiscovery.Item.RegistryID != "leaf-registry" ||
		member.MarketplaceDiscovery.Item.Name != "review" ||
		member.MarketplaceDiscovery.Item.Revision != revision ||
		member.MarketplaceDiscovery.Item.ContentDigest != revision {
		t.Fatalf("synthetic member locator = %#v", member.MarketplaceDiscovery)
	}
}

func marketplaceCollectionManifest(t *testing.T) (hccartifact.CollectionManifest, []byte, string) {
	t.Helper()
	manifest := hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1,
		Name:          "genai-suite",
		Members: []hccartifact.CollectionMemberEntry{
			{Kind: "mcpserver", Name: "genai-psx", RegistryID: "registry-1", Revision: "sha256:" + strings.Repeat("b", 64)},
			{Kind: "skill", Name: "code-review", RegistryID: "registry-1", Revision: "sha256:" + strings.Repeat("c", 64)},
		},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	}
	payload, digest, err := hccartifact.EncodeCollectionManifest(manifest)
	if err != nil {
		t.Fatalf("encode collection manifest: %v", err)
	}
	return manifest, payload, digest
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
func TestMarketplaceSkillInspectionLoadsExactRevision(t *testing.T) {
	f := newInspectionFixture(t, nil)
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	configBytes := []byte("apiVersion: agent-marketplace/v4\n")
	lockBytes := []byte("apiVersion: v4\nresources: []\n")
	controlled := map[string][]byte{
		filepath.Join(repoRoot, ".agents", "config.yaml"): configBytes,
		filepath.Join(repoRoot, ".agents", "lock.yaml"):   lockBytes,
		filepath.Join(homeDir, ".agents", "config.yaml"):  append([]byte(nil), configBytes...),
		filepath.Join(homeDir, ".agents", "lock.yaml"):    append([]byte(nil), lockBytes...),
	}
	for path, data := range controlled {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var mutations []string
	lifecycle := DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) (pipeline.SyncResult, error) {
			mutations = append(mutations, "sync")
			return pipeline.SyncResult{}, errors.New("sync must not run")
		},
		Update: func(context.Context, pipeline.Workspace) (pipeline.SyncResult, error) {
			mutations = append(mutations, "update")
			return pipeline.SyncResult{}, errors.New("update must not run")
		},
		Mutate: func(context.Context, pipeline.Workspace, func(pipeline.ConfigPathMapper) error) error {
			mutations = append(mutations, "mutate")
			return errors.New("mutate must not run")
		},
	}
	m := inspectionModelDirs(t, inspectionFixtureRow(f), repoRoot, homeDir, WithDependencyLifecycle(lifecycle))

	m, cmd := enterDetail(t, m)
	if cmd == nil {
		t.Fatal("enter on a discovery-only skill did not schedule inspection")
	}
	if m.bundle != nil {
		t.Fatal("inspection produced a bundle before the response arrived")
	}
	if out := ansi.Strip(m.detailViewport.View()); !contains(out, "Loading") {
		t.Fatalf("pending inspection did not show a loading state:\n%s", out)
	}
	if got := f.Log(); len(got) != 0 {
		t.Fatalf("inspection performed HTTP before the command ran: %v", got)
	}

	msg, ok := cmd().(marketplaceDetailMsg)
	if !ok {
		t.Fatalf("inspection command returned %T, want marketplaceDetailMsg", msg)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if m.bundle == nil {
		t.Fatalf("accepted inspection left no bundle; viewport:\n%s", ansi.Strip(m.detailViewport.View()))
	}
	if m.detailSelectedPath != inspection.PrimarySkillPath {
		t.Fatalf("initial inspection selection = %q, want SKILL.md", m.detailSelectedPath)
	}
	var names []string
	for _, entry := range m.bundle.entries() {
		names = append(names, entry.RelPath)
	}
	for _, want := range []string{inspection.PrimarySkillPath, "support/notes.md", "support/logo.png"} {
		found := false
		for _, got := range names {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("inspection tree missing %q in %v", want, names)
		}
	}
	out := ansi.Strip(m.detailViewport.View())
	if !contains(out, "Primary inspection.") {
		t.Fatalf("inspection primary preview missing body:\n%s", out)
	}
	for _, path := range f.Log() {
		if strings.Contains(path, "/inspection/content/") {
			t.Fatalf("support file %q was fetched eagerly; requests: %v", path, f.Log())
		}
	}

	next, cmd := m.moveBundleCursor(-1)
	m = next.(Model)
	if m.detailSelectedPath != "support/notes.md" {
		t.Fatalf("move selected %q, want support/notes.md", m.detailSelectedPath)
	}
	if cmd == nil {
		t.Fatal("selecting a support file did not schedule a preview")
	}
	updated, _ = m.Update(cmd().(detailContentMsg))
	m = updated.(Model)
	out = ansi.Strip(m.detailViewport.View())
	if !contains(out, "Supporting inspection.") {
		t.Fatalf("support file preview missing body:\n%s", out)
	}
	next, cmd = m.moveBundleCursor(-1)
	m = next.(Model)
	if m.detailSelectedPath != "support/logo.png" {
		t.Fatalf("move selected %q, want support/logo.png", m.detailSelectedPath)
	}
	updated, _ = m.Update(cmd().(detailContentMsg))
	m = updated.(Model)
	out = ansi.Strip(m.detailViewport.View())
	if contains(out, "PNG\r\n") || !contains(out, "logo.png") {
		t.Fatalf("image file preview leaked bytes or lost identity:\n%s", out)
	}
	var fetched []string
	for _, path := range f.Log() {
		if strings.Contains(path, "/inspection/content/") {
			fetched = append(fetched, path)
		}
	}
	notesDigest := ""
	for _, file := range f.Manifest.Files {
		if file.Path == "support/notes.md" {
			notesDigest = file.ContentDigest
		}
	}
	if len(fetched) != 1 || !strings.HasSuffix(fetched[0], notesDigest) {
		t.Fatalf("lazy file requests = %v, want exactly the notes.md digest %q", fetched, notesDigest)
	}
	for _, path := range f.Log() {
		if strings.Contains(path, "/artifacts") || strings.Contains(path, "/install") || strings.HasSuffix(path, "/sync") {
			t.Fatalf("inspection triggered a mutating request %q", path)
		}
	}
	if len(mutations) != 0 {
		t.Fatalf("inspection invoked mutating callbacks: %v", mutations)
	}
	for path, want := range controlled {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("inspection mutated %s (err=%v)", path, err)
		}
	}
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
func TestMarketplaceSkillInspectionFailsVisibly(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(f *inspectionFixture, wk *mpapi.WellKnown, rev *store.AssessedResourceRevision, manifestPayload *[]byte, bodies map[string][]byte)
		row    func(f *inspectionFixture) row
	}{
		{name: "endpoint identity mismatch", mutate: func(_ *inspectionFixture, wk *mpapi.WellKnown, _ *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			wk.InstanceID = "other-instance"
		}},
		{name: "revision provenance mismatch", mutate: func(_ *inspectionFixture, _ *mpapi.WellKnown, rev *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			rev.ContentDigest = store.RevisionDigest("sha256:" + strings.Repeat("e", 64))
		}},
		{name: "no inspection support", mutate: func(_ *inspectionFixture, _ *mpapi.WellKnown, rev *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			rev.Assessment.InspectionManifestDigest = ""
		}},
		{name: "revision endpoint returns 404", row: func(f *inspectionFixture) row {
			r := inspectionFixtureRow(f)
			r.MarketplaceDiscovery.Item.Revision = "sha256:" + strings.Repeat("d", 64)
			return r
		}},
		{name: "inspection transport failure", row: func(f *inspectionFixture) row {
			f.Server.Close()
			return inspectionFixtureRow(f)
		}},
		{name: "discovery content digest mismatch", row: func(f *inspectionFixture) row {
			r := inspectionFixtureRow(f)
			r.MarketplaceDiscovery.Item.ContentDigest = "sha256:" + strings.Repeat("f", 64)
			return r
		}},
		{name: "primary body hash mismatch", mutate: func(f *inspectionFixture, _ *mpapi.WellKnown, _ *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			f.Primary = []byte("corrupted primary body")
		}},
		{name: "manifest digest mismatch", mutate: func(_ *inspectionFixture, _ *mpapi.WellKnown, _ *store.AssessedResourceRevision, manifestPayload *[]byte, _ map[string][]byte) {
			*manifestPayload = append(append([]byte(nil), *manifestPayload...), ' ')
		}},
		{name: "incomplete discovery coordinates", row: func(f *inspectionFixture) row {
			r := inspectionFixtureRow(f)
			r.MarketplaceDiscovery.Item.Revision = ""
			return r
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newInspectionFixture(t, tc.mutate)
			r := inspectionFixtureRow(f)
			if tc.row != nil {
				r = tc.row(f)
			}
			m := inspectionModel(t, r)
			m, cmd := enterDetail(t, m)
			if cmd == nil {
				t.Fatal("enter did not schedule inspection")
			}
			msg, ok := cmd().(marketplaceDetailMsg)
			if !ok {
				t.Fatalf("inspection command returned %T", msg)
			}
			if msg.err == nil {
				t.Fatal("inspection succeeded for an invalid fixture")
			}
			updated, _ := m.Update(msg)
			m = updated.(Model)
			if m.bundle != nil || m.detailSelectedPath != "" {
				t.Fatal("failed inspection left a bundle or selection behind")
			}
			out := ansi.Strip(m.detailViewport.View())
			if !contains(out, "unavailable") {
				t.Fatalf("failed inspection is not actionable:\n%s", out)
			}
		})
	}

	t.Run("primary success control", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		_, cmd := enterDetail(t, m)
		msg, ok := cmd().(marketplaceDetailMsg)
		if !ok || msg.err != nil {
			t.Fatalf("control inspection failed: %v", msg.err)
		}
	})
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
func TestMarketplaceSkillInspectionTruncatesOversizeBodies(t *testing.T) {
	oversize := func(sentinel string) []byte {
		data := make([]byte, maxDetailSourceBytes+64)
		for i := range data {
			data[i] = 'x'
		}
		copy(data, "# Big\n\n")
		copy(data[len(data)-len(sentinel):], sentinel)
		return data
	}

	t.Run("primary", func(t *testing.T) {
		f := newInspectionFixtureContent(t, oversize("TAIL-SENTINEL-P"), map[string][]byte{"notes.md": []byte("# Notes\n\nSupporting inspection.")}, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		_, cmd := enterDetail(t, m)
		msg := cmd().(marketplaceDetailMsg)
		if msg.err != nil {
			t.Fatalf("oversize primary inspection failed: %v", msg.err)
		}
		out := ansi.Strip(msg.content)
		if !contains(out, "truncated: source exceeds") {
			t.Fatalf("oversize primary lost the truncation marker:\n%.200s", out)
		}
		if contains(out, "TAIL-SENTINEL-P") {
			t.Fatal("oversize primary leaked bytes past the cap")
		}
	})

	t.Run("lazy support file", func(t *testing.T) {
		f := newInspectionFixtureContent(t,
			[]byte("# Grill with docs\n\nPrimary inspection."),
			map[string][]byte{"notes.md": oversize("TAIL-SENTINEL-S")}, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(cmd().(marketplaceDetailMsg))
		m = updated.(Model)
		next, pending := m.moveBundleCursor(-1)
		m = next.(Model)
		if m.detailSelectedPath != "support/notes.md" {
			t.Fatalf("support selection = %q", m.detailSelectedPath)
		}
		preview := pending().(detailContentMsg)
		out := ansi.Strip(preview.content)
		if !contains(out, "truncated: source exceeds") {
			t.Fatalf("oversize support file lost the truncation marker:\n%.200s", out)
		}
		if contains(out, "TAIL-SENTINEL-S") {
			t.Fatal("oversize support file leaked bytes past the cap")
		}
	})

	t.Run("small support control", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(cmd().(marketplaceDetailMsg))
		m = updated.(Model)
		next, pending := m.moveBundleCursor(-1)
		m = next.(Model)
		updated, _ = m.Update(pending().(detailContentMsg))
		out := ansi.Strip(updated.(Model).detailViewport.View())
		if !contains(out, "Supporting inspection.") || contains(out, "truncated") {
			t.Fatalf("small support file control failed:\n%s", out)
		}
	})
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
func TestMarketplaceSkillInspectionRejectsCorruptSupportFile(t *testing.T) {
	f := newInspectionFixture(t, func(f *inspectionFixture, _ *mpapi.WellKnown, _ *store.AssessedResourceRevision, _ *[]byte, bodies map[string][]byte) {
		bodies[inspection.Digest(f.Support["notes.md"])] = []byte("corrupted notes body")
	})
	m := inspectionModel(t, inspectionFixtureRow(f))
	m, cmd := enterDetail(t, m)
	msg := cmd().(marketplaceDetailMsg)
	if msg.err != nil {
		t.Fatalf("initial inspection failed: %v", msg.err)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	next, pending := m.moveBundleCursor(-1)
	m = next.(Model)
	updated, _ = m.Update(pending().(detailContentMsg))
	out := ansi.Strip(updated.(Model).detailViewport.View())
	if contains(out, "corrupted notes body") || !contains(out, "Error reading file") {
		t.Fatalf("corrupt support file did not fail visibly:\n%s", out)
	}
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
func TestMarketplaceSkillInspectionStaleResponses(t *testing.T) {
	t.Run("result after escape is ignored", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		msg := cmd().(marketplaceDetailMsg)
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(Model)
		updated, _ = m.Update(msg)
		m = updated.(Model)
		if m.pane != paneList || m.bundle != nil {
			t.Fatalf("stale inspection was applied after escape: pane=%v bundle=%v", m.pane, m.bundle != nil)
		}
	})

	t.Run("result for another resource is ignored", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		msg := cmd().(marketplaceDetailMsg)
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(Model)
		other := inspectionFixtureRow(f)
		other.Artifact.Name = "other"
		other.MarketplaceDiscovery.Item.Name = "other"
		m.rows = []row{other}
		m.filteredRows = m.rows
		m.rebuildFlatItems()
		m.cursor = 0
		m, _ = enterDetail(t, m)
		updated, _ = m.Update(msg)
		m = updated.(Model)
		if m.bundle != nil {
			t.Fatal("stale inspection replaced another resource's detail")
		}
	})

	t.Run("resize while initial load pending reflows at current width", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		m = updated.(Model)
		msg := cmd().(marketplaceDetailMsg)
		updated, reflow := m.Update(msg)
		m = updated.(Model)
		if m.bundle == nil {
			t.Fatal("accepted inspection lost its tree on stale width")
		}
		if reflow == nil {
			t.Fatal("stale-width acceptance did not schedule a reflow")
		}
		contentMsg, ok := reflow().(detailContentMsg)
		if !ok {
			t.Fatalf("reflow did not produce a detailContentMsg: %v", msg)
		}
		if contentMsg.width != previewWidthFor(120) {
			t.Fatalf("reflow width = %d, want %d", contentMsg.width, previewWidthFor(120))
		}
		updated, _ = m.Update(contentMsg)
		m = updated.(Model)
		if !contains(ansi.Strip(m.detailViewport.View()), "Primary inspection.") {
			t.Fatalf("reflowed primary missing:\n%s", ansi.Strip(m.detailViewport.View()))
		}
	})

	t.Run("support response after escape and reopen is ignored", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(cmd().(marketplaceDetailMsg))
		m = updated.(Model)
		next, pending := m.moveBundleCursor(-1)
		m = next.(Model)
		if m.detailSelectedPath != "support/notes.md" || pending == nil {
			t.Fatalf("support selection = %q cmd=%v", m.detailSelectedPath, pending)
		}
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(Model)
		other := inspectionFixtureRow(f)
		other.Artifact.Name = "other"
		other.MarketplaceDiscovery.Item.Name = "other"
		m.rows = []row{other}
		m.filteredRows = m.rows
		m.rebuildFlatItems()
		m.cursor = 0
		m, _ = enterDetail(t, m)
		stale := pending().(detailContentMsg)
		updated, _ = m.Update(stale)
		m = updated.(Model)
		if contains(ansi.Strip(m.detailViewport.View()), "Supporting inspection.") {
			t.Fatalf("stale support fetch overwrote another resource's detail:\n%s", ansi.Strip(m.detailViewport.View()))
		}
	})

	t.Run("resize while support fetch pending rejects the stale result", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(cmd().(marketplaceDetailMsg))
		m = updated.(Model)
		next, pending := m.moveBundleCursor(-1)
		m = next.(Model)
		updated, reflow := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		m = updated.(Model)
		if reflow == nil {
			t.Fatal("resize did not schedule a reflow for the pending support file")
		}
		before := m.detailViewport.View()
		stale := pending().(detailContentMsg)
		if stale.width == previewWidthFor(120) {
			t.Fatalf("control is vacuous: pending fetch captured current width %d", stale.width)
		}
		updated, _ = m.Update(stale)
		m = updated.(Model)
		if m.detailViewport.View() != before {
			t.Fatal("stale-width support fetch overwrote the resized detail")
		}
	})

	t.Run("same row snapshot preserves the accepted tree", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(cmd().(marketplaceDetailMsg))
		m = updated.(Model)
		bundle := m.bundle
		before := ansi.Strip(m.detailViewport.View())
		m.applySnapshotData(snapshotLoadedMsg{rows: m.rows, driftAssessment: driftReady})
		if m.bundle != bundle || m.detailSelectedPath != inspection.PrimarySkillPath {
			t.Fatal("same-row snapshot discarded the accepted inspection tree")
		}
		if ansi.Strip(m.detailViewport.View()) != before || !contains(before, "Primary inspection.") {
			t.Fatalf("same-row snapshot lost the accepted preview content:\n%s", ansi.Strip(m.detailViewport.View()))
		}
	})
}
