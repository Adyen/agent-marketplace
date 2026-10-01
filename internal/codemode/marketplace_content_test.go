package codemode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type mpStub struct {
	*httptest.Server
	mu           sync.Mutex
	paths        []string
	metaPath     string
	meta         []byte
	metaCode     int
	artifactCode int
	artifacts    map[string][]byte
}

func mpMetaRoute(registryID, kind, name, revision string) string {
	return "/api/v1/resources/" + registryID + "/" + kind + "/" + name + "/revisions/" + revision
}

func newMPStub(t *testing.T) *mpStub {
	s := &mpStub{artifacts: map[string][]byte{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.mu.Unlock()
		if digest, ok := strings.CutPrefix(r.URL.Path, "/api/v1/artifacts/sha256/"); ok {
			if s.artifactCode != 0 {
				w.WriteHeader(s.artifactCode)
				_, _ = w.Write([]byte(`{"error":"artifact denied"}`))
				return
			}
			if raw, found := s.artifacts["sha256:"+digest]; found {
				_, _ = w.Write(raw)
				return
			}
			http.Error(w, `{"error":"missing artifact"}`, http.StatusNotFound)
			return
		}
		if r.URL.Path != s.metaPath {
			http.Error(w, `{"error":"unknown route"}`, http.StatusNotFound)
			return
		}
		if s.metaCode != 0 {
			w.WriteHeader(s.metaCode)
			_, _ = w.Write([]byte(`{"error":"metadata unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s.meta)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *mpStub) requested(substr string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, p := range s.paths {
		if strings.Contains(p, substr) {
			n++
		}
	}
	return n
}

func (s *mpStub) reset() {
	s.mu.Lock()
	s.paths = nil
	s.mu.Unlock()
}

func mpIsolateHome(t *testing.T) {
	t.Helper()
	home := snapshotOfflineHome(t)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
}

func mpDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mpArtifact(t testing.TB, res ir.Resource, registryID string, mutate func(*hccartifact.CanonicalArtifact)) (raw []byte, archiveDigest, contentDigest string) {
	t.Helper()
	a, err := hccartifact.FromIR(res)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	a.Manifest.RegistryID = registryID
	if mutate != nil {
		mutate(&a)
	}
	raw, archiveDigest, err = hccartifact.Encode(a)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return raw, archiveDigest, a.Manifest.ContentDigest
}

func mpRevision(t testing.TB, registryID, kind, name, contentDigest, archiveDigest string, decision store.AssessmentDecision) store.AssessedResourceRevision {
	t.Helper()
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: store.RegistryID(registryID), Kind: kind, Name: name}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coord, ContentDigest: store.RevisionDigest(contentDigest),
		AnalyzerVersion: "test/v1", PolicyDigest: store.PolicyDigest(mpDigest("policy")),
		Decision: decision, InspectionManifestDigest: store.RevisionDigest(mpDigest("inspection")),
		AssessedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("assessment: %v", err)
	}
	return store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest)},
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: coord, ContentDigest: store.RevisionDigest(contentDigest),
			Commit: strings.Repeat("a", 40), ManifestDigest: store.RevisionDigest(mpDigest("occurrence")),
		},
	}
}

func mpRevisionBody(t testing.TB, rev store.AssessedResourceRevision, availability string) []byte {
	t.Helper()
	var body struct {
		store.AssessedResourceRevision
		CurrentAvailability *struct {
			Availability string `json:"availability"`
		} `json:"current_availability,omitempty"`
	}
	body.AssessedResourceRevision = rev
	if availability != "" {
		body.CurrentAvailability = &struct {
			Availability string `json:"availability"`
		}{Availability: availability}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return raw
}

func mpResource(baseURL, registryID, kind, name, revision string) Resource {
	res := Resource{Ref: "mp:" + registryID + "/" + kind + "/" + name, Kind: "skill", Name: name, Revision: revision, Assessment: "allow"}
	if kind == "mcpserver" {
		res.Kind = "mcp"
		res.Ref = "mp:" + registryID + "/mcp/" + name
	}
	res.SetOrigin(&Origin{Marketplace: &MarketplaceOrigin{
		Entry:      config.MarketplaceEntry{URL: baseURL, InstanceID: "mp"},
		RegistryID: registryID, Kind: kind, Name: name, Revision: revision,
	}})
	return res
}

func mpEnv(t *testing.T, online bool, res Resource) (*Engine, *fakePool, *marketplace.Cache, *FileSkillLoader) {
	t.Helper()
	cache := marketplace.NewCache(t.TempDir())
	loader := &FileSkillLoader{Cache: cache, Online: online}
	e, _ := testEngine(t, []Resource{res})
	e.Skills = loader
	e.Gate = loader.Gate
	pool := newFakePool()
	e.Pool = pool
	return e, pool, cache, loader
}

func runScript(t *testing.T, e *Engine, code string) (string, error) {
	t.Helper()
	args, err := json.Marshal(map[string]any{"code": code})
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		return "", err
	}
	return string(res.Value), nil
}

func requireCodeErr(t *testing.T, err error, codes ...string) {
	t.Helper()
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected coded error %v, got %v", codes, err)
	}
	for _, c := range codes {
		if ce.Code == c {
			return
		}
	}
	t.Fatalf("expected coded error %v, got %q (%v)", codes, ce.Code, err)
}

func seedMPCache(t testing.TB, cache *marketplace.Cache, baseURL, registryID, kind, name, revision string, rev store.AssessedResourceRevision, raw []byte, verified bool) {
	t.Helper()
	if raw != nil {
		if _, err := cache.WriteArtifact(string(rev.ArchiveDigest), raw); err != nil {
			t.Fatalf("WriteArtifact: %v", err)
		}
		if verified {
			if err := cache.MarkArtifactVerified(string(rev.ArchiveDigest)); err != nil {
				t.Fatalf("MarkArtifactVerified: %v", err)
			}
		}
	}
	if _, err := cache.WriteResourceRevision(baseURL, registryID, kind, name, revision, rev); err != nil {
		t.Fatalf("WriteResourceRevision: %v", err)
	}
}

var mpSkill = ir.NewSkill("s", "desc", "# fresh body", nil, nil, ir.SourceRef{})

// spec: code-mode-mcp/online-marketplace-access-refreshes-exact-revision-metadata
func TestMarketplaceContentOnlineRefresh(t *testing.T) {
	mpIsolateHome(t)
	stub := newMPStub(t)
	support := map[string]hccartifact.SupportFile{"docs/guide.md": {Data: []byte("guide-bytes")}}
	_, staleDigest, contentDigest := mpArtifact(t, mpSkill, "src", func(a *hccartifact.CanonicalArtifact) {
		a.Manifest.Provenance.Generator = "stale"
		a.Support = support
	})
	freshRaw, freshDigest, _ := mpArtifact(t, mpSkill, "src", func(a *hccartifact.CanonicalArtifact) {
		a.Manifest.Provenance.Generator = "fresh"
		a.Support = support
	})

	res := mpResource(stub.URL, "src", "skill", "s", contentDigest)
	e, _, cache, _ := mpEnv(t, true, res)
	seedMPCache(t, cache, stub.URL, "src", "skill", "s", contentDigest,
		mpRevision(t, "src", "skill", "s", contentDigest, staleDigest, store.AssessmentDecisionAllow), nil, false)
	stub.metaPath = mpMetaRoute("src", "skill", "s", contentDigest)
	stub.meta = mpRevisionBody(t, mpRevision(t, "src", "skill", "s", contentDigest, freshDigest, store.AssessmentDecisionAllow), "")
	stub.artifacts[freshDigest] = freshRaw

	got, err := runScript(t, e, `async () => {
		const s = await skills.load("mp:src/skill/s")
		return [s.revision, s.digest, s.files.join(","), await skills.read("mp:src/skill/s", "docs/guide.md")]
	}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := fmt.Sprintf(`[%q,%q,"docs/guide.md","guide-bytes"]`, contentDigest, contentDigest)
	if got != want {
		t.Fatalf("value = %s, want %s", got, want)
	}
	if n := stub.requested(mpMetaRoute("src", "skill", "s", contentDigest)); n == 0 {
		t.Fatal("exact revision metadata route was never requested")
	}
	if n := stub.requested(strings.TrimPrefix(staleDigest, "sha256:")); n != 0 {
		t.Fatalf("obsolete archive requested %d times", n)
	}
	if n := stub.requested(strings.TrimPrefix(freshDigest, "sha256:")); n == 0 {
		t.Fatal("current archive was never fetched")
	}
	cached, ok, err := cache.ReadResourceRevision(stub.URL, "src", "skill", "s", contentDigest)
	if err != nil || !ok || cached.ArchiveDigest != store.ArchiveDigest(freshDigest) {
		t.Fatalf("cached revision = %+v ok=%v err=%v, want archive %s", cached, ok, err, freshDigest)
	}
	if _, verified, err := cache.ReadArtifact(freshDigest); err != nil || !verified {
		t.Fatalf("fresh artifact verified=%v err=%v", verified, err)
	}
}

// spec: code-mode-mcp/marketplace-skill-files-include-canonical-support-and-legacy-assets
func TestMarketplaceContentFiles(t *testing.T) {
	mpIsolateHome(t)
	newCase := func(t *testing.T, support map[string]hccartifact.SupportFile, assets map[string][]byte) *Engine {
		stub := newMPStub(t)
		raw, digest, contentDigest := mpArtifact(t, mpSkill, "src", func(a *hccartifact.CanonicalArtifact) {
			a.Support = support
			a.Assets = assets
		})
		stub.artifacts[digest] = raw
		stub.metaPath = mpMetaRoute("src", "skill", "s", contentDigest)
		stub.meta = mpRevisionBody(t, mpRevision(t, "src", "skill", "s", contentDigest, digest, store.AssessmentDecisionAllow), "")
		e, _, _, _ := mpEnv(t, true, mpResource(stub.URL, "src", "skill", "s", contentDigest))
		return e
	}
	read := func(path string) string {
		return fmt.Sprintf(`async () => { try { return await skills.read("mp:src/skill/s", %q) } catch (e) { return e.code } }`, path)
	}

	t.Run("canonical support and legacy assets union sorted", func(t *testing.T) {
		e := newCase(t, map[string]hccartifact.SupportFile{
			"docs/guide.md":  {Data: []byte("guide-bytes")},
			"scripts/run.sh": {Data: []byte("run-bytes"), Executable: true},
		}, map[string][]byte{"logo.png": []byte("png-bytes")})
		got, err := runScript(t, e, `async () => {
			const s = await skills.load("mp:src/skill/s")
			return [s.files.join(","), await skills.read("mp:src/skill/s","docs/guide.md"),
				await skills.read("mp:src/skill/s","logo.png"), await skills.read("mp:src/skill/s","scripts/run.sh")]
		}`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		want := `["docs/guide.md,logo.png,scripts/run.sh","guide-bytes","png-bytes","run-bytes"]`
		if got != want {
			t.Fatalf("value = %s, want %s", got, want)
		}
	})

	t.Run("duplicate support and asset path fails closed", func(t *testing.T) {
		e := newCase(t, map[string]hccartifact.SupportFile{"shared.txt": {Data: []byte("a")}},
			map[string][]byte{"shared.txt": []byte("b")})
		got, err := runScript(t, e, `async () => {
			let l = "", r = ""
			try { await skills.load("mp:src/skill/s") } catch (e) { l = e.code }
			try { await skills.read("mp:src/skill/s", "shared.txt") } catch (e) { r = e.code }
			return [l, r]
		}`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if got != `["resource.not-found","resource.not-found"]` {
			t.Fatalf("codes = %s, want ambiguous path refused", got)
		}
	})

	t.Run("missing and traversal paths refuse", func(t *testing.T) {
		e := newCase(t, map[string]hccartifact.SupportFile{"ok.txt": {Data: []byte("x")}}, nil)
		for _, p := range []string{"nope.txt", "../escape", "a/../../b", "support/ok.txt"} {
			got, err := runScript(t, e, read(p))
			if err != nil {
				t.Fatalf("Execute(%q): %v", p, err)
			}
			if got != `"resource.not-found"` {
				t.Fatalf("read(%q) = %s, want resource.not-found", p, got)
			}
		}
	})

	t.Run("file listing bounded and sorted at 512", func(t *testing.T) {
		support := map[string]hccartifact.SupportFile{}
		assets := map[string][]byte{}
		for i := 0; i < 256; i++ {
			support[fmt.Sprintf("sup/f%03d.txt", i)] = hccartifact.SupportFile{Data: []byte("s")}
			assets[fmt.Sprintf("a%03d.txt", i)] = []byte("a")
		}
		e := newCase(t, support, assets)
		got, err := runScript(t, e, `async () => {
			const s = await skills.load("mp:src/skill/s")
			return [s.files.length, s.files.every((v,i) => i === 0 || s.files[i-1] < v)]
		}`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if got != `[512,true]` {
			t.Fatalf("files = %s, want 512 sorted union entries", got)
		}
	})

	t.Run("file size boundaries enforced", func(t *testing.T) {
		maxFile := 2 << 20
		e := newCase(t, map[string]hccartifact.SupportFile{"big.bin": {Data: make([]byte, maxFile)}}, nil)
		got, err := runScript(t, e, `async () => {
			const b = await skills.read("mp:src/skill/s", "big.bin")
			return [b.length, b.charCodeAt(0), b.charCodeAt(b.length - 1)]
		}`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if got != fmt.Sprintf(`[%d,0,0]`, maxFile) {
			t.Fatalf("read big.bin = %.80s, want [%d,0,0]", got, maxFile)
		}

		over := newCase(t, map[string]hccartifact.SupportFile{"over.bin": {Data: make([]byte, maxFile+1)}}, nil)
		got, err = runScript(t, over, `async () => { try { await skills.load("mp:src/skill/s"); return "ok" } catch (e) { return e.code } }`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if got != `"codemode.script-error"` {
			t.Fatalf("oversized archive load = %s, want coded refusal", got)
		}
	})
}

// spec: code-mode-mcp/offline-marketplace-access-reuses-verified-exact-content
func TestMarketplaceContentOffline(t *testing.T) {
	mpIsolateHome(t)
	t.Run("verified content reused without requests", func(t *testing.T) {
		stub := newMPStub(t)
		_, staleDigest, contentDigest := mpArtifact(t, mpSkill, "src", func(a *hccartifact.CanonicalArtifact) {
			a.Manifest.Provenance.Generator = "stale"
		})
		freshRaw, freshDigest, _ := mpArtifact(t, mpSkill, "src", func(a *hccartifact.CanonicalArtifact) {
			a.Manifest.Provenance.Generator = "fresh"
			a.Support = map[string]hccartifact.SupportFile{"docs/guide.md": {Data: []byte("guide-bytes")}}
		})
		res := mpResource(stub.URL, "src", "skill", "s", contentDigest)
		e, _, cache, loader := mpEnv(t, true, res)
		seedMPCache(t, cache, stub.URL, "src", "skill", "s", contentDigest,
			mpRevision(t, "src", "skill", "s", contentDigest, staleDigest, store.AssessmentDecisionAllow), nil, false)
		stub.metaPath = mpMetaRoute("src", "skill", "s", contentDigest)
		stub.meta = mpRevisionBody(t, mpRevision(t, "src", "skill", "s", contentDigest, freshDigest, store.AssessmentDecisionAllow), "")
		stub.artifacts[freshDigest] = freshRaw

		if _, err := runScript(t, e, `async () => (await skills.load("mp:src/skill/s")).revision`); err != nil {
			t.Fatalf("online load: %v", err)
		}
		loader.Online = false
		stub.reset()
		got, err := runScript(t, e, `async () => {
			const s = await skills.load("mp:src/skill/s")
			return [s.revision, await skills.read("mp:src/skill/s", "docs/guide.md")]
		}`)
		if err != nil {
			t.Fatalf("offline Execute: %v", err)
		}
		want := fmt.Sprintf(`[%q,"guide-bytes"]`, contentDigest)
		if got != want {
			t.Fatalf("offline value = %s, want %s", got, want)
		}
		if n := len(stub.paths); n != 0 {
			t.Fatalf("offline access made %d requests: %v", n, stub.paths)
		}
	})

	validRaw, validDigest, validContent := mpArtifact(t, mpSkill, "src", nil)
	offlineRes := mpResource("http://offline.invalid", "src", "skill", "s", validContent)

	for _, tc := range []struct {
		name     string
		mutate   func(*testing.T, *store.AssessedResourceRevision)
		artifact bool
		verified bool
	}{
		{"absent metadata", nil, false, false},
		{"absent artifact", func(t *testing.T, r *store.AssessedResourceRevision) {}, false, false},
		{"unverified artifact", func(t *testing.T, r *store.AssessedResourceRevision) {}, true, false},
		{"cached coordinate mismatch", func(t *testing.T, r *store.AssessedResourceRevision) {
			r.Coordinate.Name = "other"
		}, true, true},
		{"cached digest mismatch", func(t *testing.T, r *store.AssessedResourceRevision) {
			r.ContentDigest = store.RevisionDigest(mpDigest("other-content"))
		}, true, true},
		{"malformed assessment", func(t *testing.T, r *store.AssessedResourceRevision) {
			r.Assessment.AssessmentDigest = ""
		}, true, true},
		{"assessment identity mismatch", func(t *testing.T, r *store.AssessedResourceRevision) {
			a := r.Assessment
			a.Coordinate.Name = "other"
			a.AssessmentDigest = ""
			fixed, err := store.FinalizeResourceAssessment(a)
			if err != nil {
				t.Fatalf("FinalizeResourceAssessment: %v", err)
			}
			r.Assessment = fixed
		}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := marketplace.NewCache(t.TempDir())
			loader := &FileSkillLoader{Cache: cache, Online: false}
			res := offlineRes
			if tc.mutate != nil {
				rev := mpRevision(t, "src", "skill", "s", validContent, validDigest, store.AssessmentDecisionAllow)
				tc.mutate(t, &rev)
				var raw []byte
				if tc.artifact {
					raw = validRaw
				}
				seedMPCache(t, cache, "http://offline.invalid", "src", "skill", "s", validContent, rev, raw, tc.verified)
			}
			if _, err := loader.Load(context.Background(), &res); err != nil {
				requireCodeErr(t, err, ErrCodeNotFound)
			} else {
				t.Fatalf("offline load succeeded for %s", tc.name)
			}
		})
	}
}

// spec: code-mode-mcp/online-marketplace-failures-refuse-stale-fallback
func TestMarketplaceContentRefusesStaleFallback(t *testing.T) {
	mpIsolateHome(t)
	staleRevoked := store.Revocation{Kind: store.RevocationRevision, TargetID: "s", Reason: "operator", AppliedAt: time.Unix(2, 0).UTC()}

	setup := func(t *testing.T, mutate func(*mpStub, *store.AssessedResourceRevision)) (*FileSkillLoader, Resource) {
		stub := newMPStub(t)
		staleRaw, staleDigest, contentDigest := mpArtifact(t, mpSkill, "src", func(a *hccartifact.CanonicalArtifact) {
			a.Manifest.Provenance.Generator = "stale"
			a.Support = map[string]hccartifact.SupportFile{"docs/guide.md": {Data: []byte("stale-bytes")}}
		})
		res := mpResource(stub.URL, "src", "skill", "s", contentDigest)
		cache := marketplace.NewCache(t.TempDir())
		seedMPCache(t, cache, stub.URL, "src", "skill", "s", contentDigest,
			mpRevision(t, "src", "skill", "s", contentDigest, staleDigest, store.AssessmentDecisionAllow), staleRaw, true)
		fresh := mpRevision(t, "src", "skill", "s", contentDigest, staleDigest, store.AssessmentDecisionAllow)
		stub.metaPath = mpMetaRoute("src", "skill", "s", contentDigest)
		mutate(stub, &fresh)
		if stub.meta == nil {
			stub.meta = mpRevisionBody(t, fresh, "")
		}
		return &FileSkillLoader{Cache: cache, Online: true}, res
	}

	refuse := func(t *testing.T, loader *FileSkillLoader, res Resource, codes ...string) {
		t.Helper()
		if _, err := loader.Load(context.Background(), &res); err != nil {
			requireCodeErr(t, err, codes...)
		} else {
			t.Fatal("Load returned stale bytes despite refresh failure")
		}
		if _, err := loader.ReadFile(context.Background(), &res, "docs/guide.md"); err != nil {
			requireCodeErr(t, err, codes...)
		} else {
			t.Fatal("ReadFile returned stale bytes despite refresh failure")
		}
	}

	t.Run("metadata endpoint failure", func(t *testing.T) {
		loader, res := setup(t, func(s *mpStub, r *store.AssessedResourceRevision) { s.metaCode = http.StatusInternalServerError })
		refuse(t, loader, res, ErrCodeNotFound)
	})
	t.Run("metadata http forbidden", func(t *testing.T) {
		loader, res := setup(t, func(s *mpStub, r *store.AssessedResourceRevision) { s.metaCode = http.StatusForbidden })
		refuse(t, loader, res, ErrCodeRejected)
	})
	t.Run("metadata http gone", func(t *testing.T) {
		loader, res := setup(t, func(s *mpStub, r *store.AssessedResourceRevision) { s.metaCode = http.StatusGone })
		refuse(t, loader, res, ErrCodeRevoked)
	})
	for _, tc := range []struct {
		name string
		code int
		want string
	}{
		{"artifact http forbidden", http.StatusForbidden, ErrCodeRejected},
		{"artifact http gone", http.StatusGone, ErrCodeRevoked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newMPStub(t)
			staleRaw, staleDigest, contentDigest := mpArtifact(t, mpSkill, "src", func(a *hccartifact.CanonicalArtifact) {
				a.Manifest.Provenance.Generator = "stale"
			})
			_, freshDigest, _ := mpArtifact(t, mpSkill, "src", nil)
			res := mpResource(stub.URL, "src", "skill", "s", contentDigest)
			cache := marketplace.NewCache(t.TempDir())
			seedMPCache(t, cache, stub.URL, "src", "skill", "s", contentDigest,
				mpRevision(t, "src", "skill", "s", contentDigest, staleDigest, store.AssessmentDecisionAllow), staleRaw, true)
			stub.metaPath = mpMetaRoute("src", "skill", "s", contentDigest)
			stub.meta = mpRevisionBody(t, mpRevision(t, "src", "skill", "s", contentDigest, freshDigest, store.AssessmentDecisionAllow), "")
			stub.artifactCode = tc.code
			loader := &FileSkillLoader{Cache: cache, Online: true}
			refuse(t, loader, res, tc.want)
		})
	}
	t.Run("revocation record", func(t *testing.T) {
		loader, res := setup(t, func(s *mpStub, r *store.AssessedResourceRevision) { r.Revocation = &staleRevoked })
		refuse(t, loader, res, ErrCodeRevoked)
	})
	t.Run("effective availability revoked", func(t *testing.T) {
		loader, res := setup(t, func(s *mpStub, r *store.AssessedResourceRevision) {
			s.meta = mpRevisionBody(t, *r, "revoked")
		})
		refuse(t, loader, res, ErrCodeRevoked)
	})
	t.Run("rejected assessment", func(t *testing.T) {
		loader, res := setup(t, func(s *mpStub, r *store.AssessedResourceRevision) {
			*r = mpRevision(t, "src", "skill", "s", string(r.ContentDigest), string(r.ArchiveDigest), store.AssessmentDecisionReject)
		})
		refuse(t, loader, res, ErrCodeRejected)
	})
	for _, availability := range []string{"blocked", "policy-blocked", "operator-exposed"} {
		t.Run("availability "+availability, func(t *testing.T) {
			loader, res := setup(t, func(s *mpStub, r *store.AssessedResourceRevision) {
				s.meta = mpRevisionBody(t, *r, availability)
			})
			refuse(t, loader, res, ErrCodeRejected)
		})
	}
	for _, availability := range []string{"no-artifact", "unknown"} {
		t.Run("availability "+availability, func(t *testing.T) {
			loader, res := setup(t, func(s *mpStub, r *store.AssessedResourceRevision) {
				s.meta = mpRevisionBody(t, *r, availability)
			})
			refuse(t, loader, res, ErrCodeNotFound)
		})
	}

	mismatch := func(t *testing.T, servedRaw []byte, keyDigest string) (*FileSkillLoader, Resource) {
		stub := newMPStub(t)
		staleRaw, staleDigest, _ := mpArtifact(t, mpSkill, "src", func(a *hccartifact.CanonicalArtifact) {
			a.Manifest.Provenance.Generator = "stale"
		})
		evil := ir.NewSkill("s", "desc", "# substituted payload", nil, nil, ir.SourceRef{})
		_, _, evilContent := mpArtifact(t, evil, "src", nil)
		res := mpResource(stub.URL, "src", "skill", "s", evilContent)
		cache := marketplace.NewCache(t.TempDir())
		seedMPCache(t, cache, stub.URL, "src", "skill", "s", evilContent,
			mpRevision(t, "src", "skill", "s", evilContent, staleDigest, store.AssessmentDecisionAllow), staleRaw, true)
		stub.metaPath = mpMetaRoute("src", "skill", "s", evilContent)
		stub.meta = mpRevisionBody(t, mpRevision(t, "src", "skill", "s", evilContent, keyDigest, store.AssessmentDecisionAllow), "")
		if _, err := cache.WriteArtifact(keyDigest, servedRaw); err != nil {
			t.Fatalf("WriteArtifact: %v", err)
		}
		if err := cache.MarkArtifactVerified(keyDigest); err != nil {
			t.Fatalf("MarkArtifactVerified: %v", err)
		}
		return &FileSkillLoader{Cache: cache, Online: true}, res
	}

	t.Run("expected archive digest mismatch refuses", func(t *testing.T) {
		evilRaw, _, _ := mpArtifact(t, ir.NewSkill("s", "desc", "# substituted payload", nil, nil, ir.SourceRef{}), "src", nil)
		loader, res := mismatch(t, evilRaw, mpDigest("expected-archive"))
		if _, err := loader.Load(context.Background(), &res); err != nil {
			requireCodeErr(t, err, ErrCodeScriptError)
		} else {
			t.Fatal("artifact with mismatched expected archive digest loaded")
		}
	})

	t.Run("expected content digest mismatch refuses", func(t *testing.T) {
		otherRaw, otherDigest, _ := mpArtifact(t, ir.NewSkill("s", "desc", "# decoy payload", nil, nil, ir.SourceRef{}), "src", nil)
		loader, res := mismatch(t, otherRaw, otherDigest)
		if _, err := loader.Load(context.Background(), &res); err != nil {
			requireCodeErr(t, err, ErrCodeScriptError)
		} else {
			t.Fatal("artifact with mismatched expected content digest loaded")
		}
	})

	for _, tc := range []struct {
		name   string
		status int
	}{
		{"declared mcp gate refuses without pool side effects", 0},
		{"declared mcp gate http gone refuses without pool side effects", http.StatusGone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newMPStub(t)
			srv := ir.NewMcpServer("remote", "echo", []string{"hi"}, nil, nil, ir.SourceRef{})
			staleRaw, staleDigest, contentDigest := mpArtifact(t, srv, "src", func(a *hccartifact.CanonicalArtifact) {
				a.Manifest.Provenance.Generator = "stale"
			})
			res := mpResource(stub.URL, "src", "mcpserver", "remote", contentDigest)
			e, pool, cache, _ := mpEnv(t, true, res)
			seedMPCache(t, cache, stub.URL, "src", "mcpserver", "remote", contentDigest,
				mpRevision(t, "src", "mcpserver", "remote", contentDigest, staleDigest, store.AssessmentDecisionAllow), staleRaw, true)
			stub.metaPath = mpMetaRoute("src", "mcpserver", "remote", contentDigest)
			if tc.status != 0 {
				stub.metaCode = tc.status
			} else {
				revoked := mpRevision(t, "src", "mcpserver", "remote", contentDigest, staleDigest, store.AssessmentDecisionAllow)
				revoked.Revocation = &staleRevoked
				stub.meta = mpRevisionBody(t, revoked, "")
			}

			args, _ := json.Marshal(map[string]any{
				"servers": []string{res.Ref},
				"code":    `async () => mcp.remote.ping()`,
			})
			_, _, err := e.Execute(context.Background(), args)
			requireCodeErr(t, err, ErrCodeRevoked)
			if len(pool.started) != 0 {
				t.Fatalf("server started or script ran despite refused gate: %v", pool.started)
			}
		})
	}

	for _, tc := range []struct {
		name   string
		mutate func(*store.AssessedResourceRevision)
		code   string
	}{
		{"durable revocation persists offline", func(r *store.AssessedResourceRevision) {
			r.Revocation = &staleRevoked
		}, ErrCodeRevoked},
		{"durable rejection persists offline", func(r *store.AssessedResourceRevision) {
			*r = mpRevision(t, "src", "skill", "s", string(r.ContentDigest), string(r.ArchiveDigest), store.AssessmentDecisionReject)
		}, ErrCodeRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newMPStub(t)
			staleRaw, staleDigest, contentDigest := mpArtifact(t, mpSkill, "src", func(a *hccartifact.CanonicalArtifact) {
				a.Manifest.Provenance.Generator = "stale"
			})
			res := mpResource(stub.URL, "src", "skill", "s", contentDigest)
			cache := marketplace.NewCache(t.TempDir())
			allowed := mpRevision(t, "src", "skill", "s", contentDigest, staleDigest, store.AssessmentDecisionAllow)
			seedMPCache(t, cache, stub.URL, "src", "skill", "s", contentDigest, allowed, staleRaw, true)
			fresh := allowed
			tc.mutate(&fresh)
			stub.metaPath = mpMetaRoute("src", "skill", "s", contentDigest)
			stub.meta = mpRevisionBody(t, fresh, "")
			loader := &FileSkillLoader{Cache: cache, Online: true}
			refuse(t, loader, res, tc.code)

			loader.Online = false
			stub.reset()
			if _, err := loader.Load(context.Background(), &res); err != nil {
				requireCodeErr(t, err, tc.code)
			} else {
				t.Fatal("offline Load reused allowed cache despite durable denial")
			}
			if n := len(stub.paths); n != 0 {
				t.Fatalf("offline access made %d requests: %v", n, stub.paths)
			}
		})
	}
}
