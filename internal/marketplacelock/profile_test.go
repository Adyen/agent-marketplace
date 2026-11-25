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

func TestResolveLocksExplicitTargetProfileAndPlaceMetadata(t *testing.T) {
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

	packageDigest := "sha256:" + strings.Repeat("b", 64)
	archiveDigest := "sha256:" + strings.Repeat("f", 64)
	binding := targetprofile.Binding{
		PackageContentDigest: packageDigest, DeliveryMappingDigest: "sha256:" + strings.Repeat("c", 64),
		ProfileDigest: profileDigest, ProfileIdentity: identity,
	}
	packageCoordinate := store.ResourceCoordinate{
		Marketplace: "mp", Registry: "src", Kind: "invented-app/mod", Name: "sample",
	}
	packageAssessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: packageCoordinate, ContentDigest: store.RevisionDigest(packageDigest),
		AnalyzerVersion: "test/v1", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("a", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(), HostPackageIdentity: &binding,
	})
	if err != nil {
		t.Fatalf("finalize package assessment: %v", err)
	}
	packageRevision := store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{
			Coordinate: packageCoordinate, ContentDigest: store.RevisionDigest(packageDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		},
		Assessment: packageAssessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: packageCoordinate, ContentDigest: store.RevisionDigest(packageDigest),
			Commit: strings.Repeat("e", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("9", 64)),
		},
	}

	var profileContentRequests, profileRevisionRequests, packageRevisionRequests, archiveRequests, unknownRequests atomic.Int32
	revisionPath := "/api/v1/resources/src/target-profile/" + identity.TargetID + "/revisions/" + profileDigest
	inspectionPath := revisionPath + "/inspection"
	contentPath := inspectionPath + "/content"
	packageRevisionPath := "/api/v1/resources/src/invented-app%2Fmod/sample/revisions/" + packageDigest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
		case "/api/v1/registries/tools":
			_ = json.NewEncoder(w).Encode(mpapi.RegistryDTO{
				ID: "src", Slug: "tools", Origins: []string{"https://git.example.com/tools"},
				Ref: "main", Roots: []store.RegistryRoot{{Path: "profiles"}},
				Acquisition: store.AcquisitionMirror, Listed: true,
			})
		case revisionPath:
			profileRevisionRequests.Add(1)
			writeResourceRevisionResponse(w, revision)
		case inspectionPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(projection.Payload)
		case contentPath:
			profileContentRequests.Add(1)
			w.Header().Set("Content-Type", inspection.MediaYAML)
			_, _ = w.Write(profileContent)
		case packageRevisionPath:
			packageRevisionRequests.Add(1)
			writeResourceRevisionResponse(w, packageRevision)
		default:
			if strings.HasPrefix(r.URL.EscapedPath(), "/api/v1/archives/") {
				archiveRequests.Add(1)
			} else {
				unknownRequests.Add(1)
			}
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
      - tools/invented-app%%2Fmod/sample: {revision: %s}
`, server.URL, profileDigest, packageDigest)
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
	if len(sources) != 1 || len(sources[0].Requests) != 2 {
		t.Fatalf("user Marketplace requests = %#v", sources)
	}
	var sawProfile, sawPackage bool
	for _, request := range sources[0].Requests {
		switch request.Kind {
		case "target-profile":
			sawProfile = request.Registry == "tools" && request.Name == identity.TargetID && request.Options.Revision == profileDigest
		case "invented-app/mod":
			sawPackage = request.Registry == "tools" && request.Name == "sample" && request.Options.Revision == packageDigest
		}
	}
	if !sawProfile || !sawPackage {
		t.Fatalf("user Marketplace requests = %#v", sources[0].Requests)
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
	if len(market.Resources) != 2 {
		t.Fatalf("locked resources = %#v, want one virtual profile and one scoped package", market.Resources)
	}
	wantProfile := lockfile.MarketplaceResourceLock{
		RegistryName: "tools", RegistryID: "src", Kind: "target-profile", Name: identity.TargetID,
		Revision: profileDigest, ContentDigest: lockfile.Digest(profileDigest),
		ProfileIdentity: &identity, Acquisition: lockfile.MarketplaceAcquisitionMirror,
	}
	var profileRecord, packageRecord *lockfile.MarketplaceResourceLock
	for i := range market.Resources {
		switch market.Resources[i].Kind {
		case "target-profile":
			profileRecord = &market.Resources[i]
		case "invented-app/mod":
			packageRecord = &market.Resources[i]
		}
	}
	if profileRecord == nil || !reflect.DeepEqual(*profileRecord, wantProfile) {
		t.Fatalf("target-profile lock = %+v, want %+v", profileRecord, wantProfile)
	}
	wantPackage := lockfile.MarketplaceResourceLock{
		RegistryName: "tools", RegistryID: "src", Kind: "invented-app/mod", Name: "sample",
		Revision: packageDigest, ContentDigest: lockfile.Digest(packageDigest),
		ArchiveDigest: lockfile.Digest(archiveDigest), AssessmentDigest: lockfile.Digest(packageAssessment.AssessmentDigest),
		HostPackageIdentity: &binding, Acquisition: lockfile.MarketplaceAcquisitionMirror,
	}
	if packageRecord == nil || !reflect.DeepEqual(*packageRecord, wantPackage) {
		t.Fatalf("scoped package lock = %+v, want %+v", packageRecord, wantPackage)
	}

	server.Close()
	got, err := cache.ReadLockedTargetProfile(market.URL, market.InstanceID, *profileRecord)
	if err != nil {
		t.Fatalf("read exact profile bytes offline: %v", err)
	}
	if !bytes.Equal(got, profileBytes) {
		t.Fatalf("offline profile bytes = %q, want %q", got, profileBytes)
	}
	if got := profileRevisionRequests.Load(); got != 1 {
		t.Fatalf("profile revision requests = %d, want 1", got)
	}
	if got := profileContentRequests.Load(); got != 1 {
		t.Fatalf("profile content requests = %d, want 1", got)
	}
	if got := packageRevisionRequests.Load(); got != 1 {
		t.Fatalf("scoped revision requests = %d, want 1", got)
	}
	if got := archiveRequests.Load(); got != 0 {
		t.Fatalf("package archive requests = %d, want 0", got)
	}
	if got := unknownRequests.Load(); got != 0 {
		t.Fatalf("unknown Marketplace requests = %d, want 0", got)
	}
	if _, err := os.Stat(cache.PlaceholderArchivePath(archiveDigest)); !os.IsNotExist(err) {
		t.Fatalf("place package archive cache stat error = %v, want not-exist", err)
	}
}

func writeResourceRevisionResponse(w http.ResponseWriter, revision store.AssessedResourceRevision) {
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
}
