package marketplacelock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestResolveLocksExplicitTargetProfile(t *testing.T) {
	profileBytes := []byte(`apiVersion: agent-marketplace.io/target-profile/v1
id: invented-app
label: Invented App
platforms: [macos, linux]
roots: {user: {override: INVENTED_APP_HOME, default: {macos: {base: home, path: .config/invented-app}, linux: {base: xdg-config, path: invented-app}}, allowed_bases: [home, xdg-config]}}
host: {executable: invented-app-bin, version_probe: {argv: [--version], pattern: '(\d+\.\d+\.\d+)'}, verified: [{range: ">=1.0.0 <2.0.0", date: 2026-11-12, evidence: fixture}]}
checks: {initial-load: {scope: eager-package-load, argv: [check, "{subject}"], subject: {under: mods, component: package-name}}}
kinds: {mod: {deliver: place, into: [mods/], verify: initial-load}}
`)
	profileDigest := inspection.Digest(profileBytes)
	identity := targetprofile.Identity{
		TargetID: "invented-app", SourceID: "git:https%3A%2F%2Fgit.example.com%2Ftools@main#profiles%2Finvented-app.yaml",
		RevisionDigest: profileDigest,
	}
	if err := identity.Validate(); err != nil {
		t.Fatalf("validate profile identity: %v", err)
	}
	coordinate := store.ResourceCoordinate{
		Marketplace: "mp", Registry: "src", Kind: "target-profile", Name: identity.TargetID,
	}
	projection, err := inspection.BuildTargetProfile(
		coordinate, identity, profileBytes, 1,
		targetprofile.MaxProfileBytes, targetprofile.MaxProfileBytes,
	)
	if err != nil {
		t.Fatalf("build profile inspection: %v", err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: store.RevisionDigest(profileDigest),
		AnalyzerVersion: "test/v1", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("a", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest(inspection.Digest(projection.Payload)),
		AssessedAt:               time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("finalize profile assessment: %v", err)
	}
	revision := store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{Coordinate: coordinate, ContentDigest: store.RevisionDigest(profileDigest)},
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: coordinate, ContentDigest: store.RevisionDigest(profileDigest),
			Commit: strings.Repeat("e", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		},
	}
	profileContent := projection.Bodies[projection.Manifest.Files[0].ContentDigest]

	var requests atomic.Int32
	revisionPath := "/api/v1/resources/src/target-profile/" + identity.TargetID + "/revisions/" + profileDigest
	inspectionPath := revisionPath + "/inspection"
	contentPath := inspectionPath + "/content"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
		case "/api/v1/registries/tools":
			_ = json.NewEncoder(w).Encode(mpapi.RegistryDTO{
				ID: "src", Slug: "tools", Origins: []string{"https://git.example.com/tools"},
				Ref: "main", Roots: []store.RegistryRoot{{Path: "profiles"}},
				Acquisition: store.AcquisitionMirror, Listed: true,
			})
		case revisionPath:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				store.AssessedResourceRevision
				CurrentAvailability struct {
					Availability string `json:"availability"`
				} `json:"current_availability"`
			}{
				AssessedResourceRevision: revision,
				CurrentAvailability: struct {
					Availability string `json:"availability"`
				}{Availability: "available"},
			})
		case inspectionPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(projection.Payload)
		case contentPath:
			w.Header().Set("Content-Type", inspection.MediaYAML)
			_, _ = w.Write(profileContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home, repoRoot := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".agents"))
	userConfigPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userConfigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	userConfig := fmt.Sprintf(`apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: %s}
resources:
  hosted:
    include:
      - tools/target-profile/invented-app: {revision: %s}
`, server.URL, profileDigest)
	if err := os.WriteFile(userConfigPath, []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	intent := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{
		RepoRoot: repoRoot, LocalConfigDir: repoRoot,
		OrganizationPath: filepath.Join(repoRoot, "missing-org.yaml"),
		GlobalPath:       userConfigPath,
		RepoPath:         filepath.Join(repoRoot, "missing-repo.yaml"),
		LocalPath:        filepath.Join(repoRoot, "missing-local.yaml"),
	})
	layer := intent.Layers[config.UserPersonal]
	if err := layer.Err(); err != nil {
		t.Fatalf("load v4 user intent: %v", err)
	}
	sources := layer.MarketplaceSources()
	if len(sources) != 1 || len(sources[0].Requests) != 1 ||
		sources[0].Requests[0].Kind != "target-profile" ||
		sources[0].Requests[0].Registry != "tools" ||
		sources[0].Requests[0].Name != identity.TargetID ||
		sources[0].Requests[0].Options.Revision != profileDigest {
		t.Fatalf("user Marketplace requests = %#v", sources)
	}

	cache := marketplace.NewCache(t.TempDir())
	resolver := &marketplace.Resolver{HTTPClient: server.Client(), Cache: cache}
	doc, err := Resolve(context.Background(), resolver, layer, nil, nil)
	if err != nil {
		t.Fatalf("Resolve explicit target profile: %v", err)
	}
	if len(doc.Marketplaces) != 1 {
		t.Fatalf("Marketplace locks = %#v, want one endpoint", doc.Marketplaces)
	}
	market := doc.Marketplaces[0]
	if market.URL != server.URL || market.InstanceID != "mp" ||
		!reflect.DeepEqual(market.RegistryBindings, []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "src"}}) {
		t.Fatalf("Marketplace endpoint lock = %+v", market)
	}
	if len(market.Resources) != 1 {
		t.Fatalf("locked resources = %#v, want one virtual profile", market.Resources)
	}
	want := lockfile.MarketplaceResourceLock{
		RegistryName: "tools", RegistryID: "src", Kind: "target-profile", Name: identity.TargetID,
		Revision: profileDigest, ContentDigest: lockfile.Digest(profileDigest),
		ProfileIdentity: &identity, Acquisition: lockfile.MarketplaceAcquisitionMirror,
	}
	if !reflect.DeepEqual(market.Resources[0], want) {
		t.Fatalf("target-profile lock = %+v, want %+v", market.Resources[0], want)
	}

	server.Close()
	got, err := cache.ReadLockedTargetProfile(market.URL, market.InstanceID, market.Resources[0])
	if err != nil {
		t.Fatalf("read exact profile bytes offline: %v", err)
	}
	if !bytes.Equal(got, profileBytes) {
		t.Fatalf("offline profile bytes = %q, want %q", got, profileBytes)
	}
	if got := requests.Load(); got != 5 {
		t.Fatalf("Marketplace requests = %d, want one each for identity, registry, revision, inspection, and content", got)
	}
}
