package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

type requiresFixture struct {
	meta   *sqlite.Store
	public *httptest.Server
	mpID   store.MarketplaceID
}

func newRequiresFixture(t *testing.T) requiresFixture {
	t.Helper()
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	cfg := config.Config{
		Instance:         config.Instance{ID: "mp-test", Name: "Test", CanonicalURL: "https://example.com"},
		Listen:           "127.0.0.1:0",
		OperationsListen: "127.0.0.1:0",
	}
	mpID := store.MarketplaceID(cfg.Instance.ID)
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: mpID, Name: "Test"}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(cfg, meta, fsblob.New(t.TempDir()))
	public := httptest.NewServer(server.PublicHandler())
	t.Cleanup(public.Close)
	return requiresFixture{meta: meta, public: public, mpID: mpID}
}

func (f requiresFixture) addRegistry(t *testing.T, id, slug string, listed bool, roots ...string) store.Registry {
	t.Helper()
	reg := store.Registry{
		ID: store.RegistryID(id), Slug: slug, Ref: "main", Origins: []string{"https://example.com/" + slug + ".git"},
		Acquisition: store.AcquisitionMirror, License: "MIT", Listed: listed,
		Roots:     make([]store.RegistryRoot, 0, len(roots)),
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	for _, root := range roots {
		reg.Roots = append(reg.Roots, store.RegistryRoot{Path: root, SourceHarness: "factory"})
	}
	if err := f.meta.PutRegistry(context.Background(), reg); err != nil {
		t.Fatal(err)
	}
	return reg
}

var requiresDigestSeq int

func requiresDigest(suffix string) store.RevisionDigest {
	requiresDigestSeq++
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d-%s", requiresDigestSeq, suffix)))
	return store.RevisionDigest(fmt.Sprintf("sha256:%x", sum))
}

func requiresStableDigest(seed string) store.RevisionDigest {
	sum := sha256.Sum256([]byte(seed))
	return store.RevisionDigest(fmt.Sprintf("sha256:%x", sum))
}

// seed stores one revision and, unless unassessed, its current occurrence and
// assessment so that it is served by resource detail.
func (f requiresFixture) seed(t *testing.T, reg store.Registry, kind, name, requires, sourcePath string, assessed bool) store.ResourceRevision {
	t.Helper()
	ctx := context.Background()
	rev := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: f.mpID, Registry: reg.ID, Kind: kind, Name: name},
		ContentDigest: requiresDigest(string(kind[0]) + name),
		License:       "MIT",
		Requires:      requires,
	}
	if err := f.meta.PutResourceRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	commit := fmt.Sprintf("%x", sha256.Sum256([]byte("commit-"+string(reg.ID))))[:40]
	manifest := requiresStableDigest("manifest-" + string(reg.ID))
	if err := f.meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: reg.ID, Ref: reg.Ref, Roots: reg.Roots, Commit: commit,
		RegistryRevisionManifestDigest: manifest, Acquisition: reg.Acquisition,
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, Commit: commit,
		ManifestDigest: manifest, SourcePath: sourcePath,
	}); err != nil {
		t.Fatal(err)
	}
	if !assessed {
		return rev
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, AnalyzerVersion: "1.0.0",
		PolicyDigest:             store.PolicyDigest(requiresDigest("p" + name)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: requiresDigest("i" + name), AssessedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.meta.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := f.meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	return rev
}

func (f requiresFixture) detail(t *testing.T, slug, kind, name string) map[string]any {
	t.Helper()
	resp := mustGet(t, f.public.URL+"/api/v1/resources/"+slug+"/"+kind+"/"+name)
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail %s/%s/%s status %d: %s", slug, kind, name, resp.StatusCode, body)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("detail body: %v", err)
	}
	return decoded
}

func detailRequires(body map[string]any) ([]any, bool) {
	raw, present := body["requires"]
	if !present {
		return nil, false
	}
	list, _ := raw.([]any)
	return list, true
}

func detailRequiredBy(body map[string]any) ([]any, bool) {
	raw, present := body["required_by"]
	if !present {
		return nil, false
	}
	list, _ := raw.([]any)
	return list, true
}

func relations(list []any) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		m, _ := item.(map[string]any)
		out = append(out, fmt.Sprintf("%v/%v/%v", m["registry"], m["kind"], m["name"]))
	}
	return out
}

// spec: marketplace-api/indexed-revision-lists-its-prerequisites
// spec: marketplace-api/unhinted-prerequisite-resolves-in-the-same-registry
// spec: marketplace-api/indexed-revision-without-prerequisites-returns-an-empty-list
// spec: marketplace-api/unindexed-revision-omits-prerequisites
func TestResourceDetailRequires(t *testing.T) {
	f := newRequiresFixture(t)
	reg := f.addRegistry(t, "src-a", "source-a", true, ".agents")
	f.seed(t, reg, "skill", "helper", "[]", ".agents/skills/helper/SKILL.md", true)
	requiresJSON := `[{"kind":"skill","name":"helper"},{"kind":"command","name":"missing"}]`
	f.seed(t, reg, "skill", "parent", requiresJSON, ".agents/skills/parent/SKILL.md", true)
	f.seed(t, reg, "skill", "plain", "[]", ".agents/skills/plain/SKILL.md", true)
	f.seed(t, reg, "skill", "legacy", "", ".agents/skills/legacy/SKILL.md", true)

	body := f.detail(t, "source-a", "skill", "parent")
	list, present := detailRequires(body)
	if !present || len(list) != 2 {
		t.Fatalf("requires = %v, want two entries", body["requires"])
	}
	first, _ := list[0].(map[string]any)
	if first["kind"] != "skill" || first["name"] != "helper" {
		t.Fatalf("requires[0] = %v", first)
	}
	resolved, ok := first["resolved"].(map[string]any)
	if !ok || resolved["registry"] != "source-a" || resolved["kind"] != "skill" || resolved["name"] != "helper" {
		t.Fatalf("resolved = %v", first["resolved"])
	}
	second, _ := list[1].(map[string]any)
	if _, ok := second["resolved"]; ok {
		t.Fatalf("unresolved entry carried resolved: %v", second)
	}

	plain := f.detail(t, "source-a", "skill", "plain")
	if list, present := detailRequires(plain); !present || len(list) != 0 {
		t.Fatalf("indexed-empty requires = %v present %v", plain["requires"], present)
	}
	legacy := f.detail(t, "source-a", "skill", "legacy")
	if _, present := detailRequires(legacy); present {
		t.Fatalf("unindexed revision carried requires: %v", legacy["requires"])
	}
}

// spec: marketplace-api/registry-hint-resolves-by-slug-within-the-same-marketplace
// spec: marketplace-api/hidden-or-missing-target-is-unresolved
func TestResourceDetailRequiresRegistryHintAndHidden(t *testing.T) {
	f := newRequiresFixture(t)
	regA := f.addRegistry(t, "src-a", "source-a", true, ".agents")
	regB := f.addRegistry(t, "src-b", "source-b", true, ".agents")
	f.seed(t, regB, "skill", "target", "[]", ".agents/skills/target/SKILL.md", true)
	// The hidden target has a current occurrence but no assessment, so its own
	// detail request fails identically to a missing one.
	f.seed(t, regB, "skill", "hidden-target", "[]", ".agents/skills/hidden/SKILL.md", false)
	requiresJSON := `[{"kind":"skill","name":"target","registry":"source-b"},{"kind":"skill","name":"hidden-target","registry":"src-b"},{"kind":"skill","name":"ghost","registry":"source-b"}]`
	f.seed(t, regA, "skill", "parent", requiresJSON, ".agents/skills/parent/SKILL.md", true)

	body := f.detail(t, "source-a", "skill", "parent")
	list, present := detailRequires(body)
	if !present || len(list) != 3 {
		t.Fatalf("requires = %v", body["requires"])
	}
	first, _ := list[0].(map[string]any)
	resolved, ok := first["resolved"].(map[string]any)
	if !ok || resolved["registry"] != "source-b" || resolved["name"] != "target" {
		t.Fatalf("registry-hint resolved = %v", first["resolved"])
	}
	hidden, _ := list[1].(map[string]any)
	missing, _ := list[2].(map[string]any)
	if _, ok := hidden["resolved"]; ok {
		t.Fatalf("hidden target resolved: %v", hidden)
	}
	if _, ok := missing["resolved"]; ok {
		t.Fatalf("missing target resolved: %v", missing)
	}
	// Hidden and missing are indistinguishable: only the echoed hints differ.
	if len(hidden) != len(missing) {
		t.Fatalf("hidden entry shape differs from missing: %v vs %v", hidden, missing)
	}
}

// spec: marketplace-api/same-registry-dependent-is-listed
// spec: marketplace-api/visible-unindexed-candidate-makes-dependents-unknown
// spec: marketplace-api/hidden-or-unlisted-unindexed-revisions-do-not-affect-completeness
// spec: marketplace-api/resource-without-dependents-returns-an-empty-list
func TestResourceDetailRequiredBy(t *testing.T) {
	f := newRequiresFixture(t)
	regA := f.addRegistry(t, "src-a", "source-a", true, ".agents")
	regB := f.addRegistry(t, "src-b", "source-b", true, ".agents")
	regHidden := f.addRegistry(t, "src-h", "hidden-reg", false, ".agents")
	f.seed(t, regA, "skill", "target", "[]", ".agents/skills/target/SKILL.md", true)
	dep := `[{"kind":"skill","name":"target"}]`
	depOther := `[{"kind":"skill","name":"target","registry":"source-a"}]`
	f.seed(t, regA, "skill", "parent", dep, ".agents/skills/parent/SKILL.md", true)
	f.seed(t, regB, "skill", "other-parent", depOther, ".agents/skills/other/SKILL.md", true)
	// A hidden dependent and an unlisted other-registry dependent are not listed.
	f.seed(t, regA, "skill", "hidden-parent", dep, ".agents/skills/hidden/SKILL.md", false)
	f.seed(t, regHidden, "skill", "unlisted-parent", depOther, ".agents/skills/u/SKILL.md", true)
	// Unindexed revisions that are hidden or unlisted do not suppress completeness.
	f.seed(t, regA, "skill", "hidden-unindexed", "", ".agents/skills/hu/SKILL.md", false)
	f.seed(t, regHidden, "skill", "unlisted-unindexed", "", ".agents/skills/uu/SKILL.md", true)
	f.seed(t, regB, "skill", "lonely", "[]", ".agents/skills/lonely/SKILL.md", true)

	body := f.detail(t, "source-a", "skill", "target")
	list, present := detailRequiredBy(body)
	if !present {
		t.Fatalf("required_by omitted: %v", body)
	}
	got := relations(list)
	want := []string{"source-a/skill/parent", "source-b/skill/other-parent"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("required_by = %v, want %v", got, want)
	}

	lonely := f.detail(t, "source-b", "skill", "lonely")
	if list, present := detailRequiredBy(lonely); !present || len(list) != 0 {
		t.Fatalf("lonely required_by = %v present %v", lonely["required_by"], present)
	}

	// A visible unindexed in-scope revision makes dependents unknown.
	f.seed(t, regA, "skill", "visible-unindexed", "", ".agents/skills/vu/SKILL.md", true)
	body = f.detail(t, "source-a", "skill", "target")
	if _, present := detailRequiredBy(body); present {
		t.Fatalf("required_by present despite visible unindexed revision: %v", body["required_by"])
	}
}

// spec: marketplace-api/search-and-revision-responses-are-unchanged
func TestOtherResourceResponsesDoNotCarryRelations(t *testing.T) {
	f := newRequiresFixture(t)
	reg := f.addRegistry(t, "src-a", "source-a", true, ".agents")
	f.seed(t, reg, "skill", "helper", "[]", ".agents/skills/helper/SKILL.md", true)
	rev := f.seed(t, reg, "skill", "parent", `[{"kind":"skill","name":"helper"}]`, ".agents/skills/parent/SKILL.md", true)

	for _, url := range []string{
		"/api/v1/search?query=parent",
		"/api/v1/resources/source-a/skill/parent/revisions",
		"/api/v1/resources/source-a/skill/parent/revisions/" + string(rev.ContentDigest),
	} {
		resp := mustGet(t, f.public.URL+url)
		body := mustRead(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status %d: %s", url, resp.StatusCode, body)
		}
		if strings.Contains(body, `"requires"`) || strings.Contains(body, `"required_by"`) {
			t.Fatalf("%s carried relation fields: %s", url, body)
		}
	}
}
