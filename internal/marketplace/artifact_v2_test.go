package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func artifactPair(t *testing.T) ([]byte, store.AssessedResourceRevision, artifact.EncodedArtifact) {
	t.Helper()
	source, err := artifact.FromIR(ir.NewSkill("review", "Review", "Review carefully.", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	raw, digest, err := artifact.Encode(source)
	if err != nil {
		t.Fatal(err)
	}
	converted, _, err := artifact.ConvertV1ToV2(raw, artifact.LegacyReference{ArchiveDigest: digest, Kind: "skill", Name: "review", ContentDigest: source.Manifest.ContentDigest}, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := artifact.EncodeV2(converted, nil)
	if err != nil {
		t.Fatal(err)
	}
	rev := assessedRevision(store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(source.Manifest.ContentDigest), ArchiveDigest: store.ArchiveDigest(digest)})
	rev.Artifacts = []artifact.ArtifactReference{encoded.Reference}
	return raw, rev, encoded
}

func TestArtifactV2ExactSelection(t *testing.T) {
	for _, schema := range []string{"unlocked", "lock-v2", "v1", "v2"} {
		t.Run(schema, func(t *testing.T) {
			raw, rev, encoded := artifactPair(t)
			v1Requests, v2Requests := 0, 0
			mux := http.NewServeMux()
			serveWellKnown(mux, "mp")
			mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(rev) })
			mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) { v1Requests++; _, _ = w.Write(raw) })
			mux.HandleFunc("/api/v1/blobs/", func(w http.ResponseWriter, r *http.Request) { v2Requests++; _, _ = w.Write(encoded.Bytes) })
			server := httptest.NewServer(mux)
			defer server.Close()
			pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Digest: string(rev.ContentDigest), Revision: string(rev.ContentDigest)}
			switch schema {
			case "lock-v2":
				pin.LockArchiveDigest = string(rev.ArchiveDigest)
			case "v1":
				pin.LockArtifactSchema = "v1"
				pin.LockArchiveDigest = string(rev.ArchiveDigest)
			case "v2":
				pin.LockArtifactSchema = "v2"
				pin.LockArtifactDigest = string(encoded.Reference.ArtifactDigest)
				pin.LockBlobDigest = string(encoded.Reference.BlobDigest)
			}
			cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
			cache := NewCache(t.TempDir())
			r := &Resolver{Cache: cache}
			resolved, errs := r.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeRefresh)
			if len(errs) > 0 || len(resolved) != 1 || resolved[0].Resource == nil {
				t.Fatalf("resolve: %v %v", resolved, errs)
			}
			wantV2 := schema == "unlocked" || schema == "v2"
			if wantV2 && (v1Requests != 0 || v2Requests != 1) || !wantV2 && (v1Requests != 1 || v2Requests != 0) {
				t.Fatalf("wrong representation: v1=%d v2=%d", v1Requests, v2Requests)
			}
			resolved, errs = r.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeCachedNoFetch)
			if len(errs) > 0 || len(resolved) != 1 {
				t.Fatalf("offline: %v", errs)
			}
			if v1Requests+v2Requests != 1 {
				t.Fatal("offline fetched bytes")
			}
		})
	}
}

func TestArtifactV2CacheReachability(t *testing.T) {
	_, revision, encoded := artifactPair(t)
	cache := NewCache(t.TempDir())
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: string(revision.ContentDigest), Digest: string(revision.ContentDigest), LockArtifactSchema: "v2", LockArtifactDigest: string(encoded.Reference.ArtifactDigest), LockBlobDigest: string(encoded.Reference.BlobDigest)}
	entry.Resources = []config.MarketplaceResourcePin{pin}
	if _, err := cache.WriteResourceRevision(entry.URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, revision); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteArtifactV2(encoded.Reference, encoded.Bytes); err != nil {
		t.Fatal(err)
	}
	orphan, err := artifact.VerifyV2(encoded.Bytes, encoded.Reference, nil)
	if err != nil {
		t.Fatal(err)
	}
	orphan.Manifest.License = "MIT"
	other, err := artifact.EncodeV2(orphan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteArtifactV2(other.Reference, other.Bytes); err != nil {
		t.Fatal(err)
	}
	result, err := cache.GarbageCollect(true, &config.Config{Marketplaces: []config.MarketplaceEntry{entry}})
	if err != nil || len(result.Removed) != 1 || result.Removed[0] != filepath.Dir(cache.ArtifactPath(string(other.Reference.BlobDigest))) {
		t.Fatalf("v2 reachability: %+v %v", result, err)
	}
}

func TestArtifactV2CacheBindingAndTamper(t *testing.T) {
	_, _, encoded := artifactPair(t)
	cache := NewCache(t.TempDir())
	if err := cache.WriteArtifactV2(encoded.Reference, encoded.Bytes); err != nil {
		t.Fatal(err)
	}
	_, raw, err := cache.ReadArtifactV2(encoded.Reference)
	if err != nil || raw == nil {
		t.Fatal("positive control", err)
	}
	other := encoded.Reference
	other.ArtifactDigest = artifact.ArtifactDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, _, err := cache.ReadArtifactV2(other); !errors.Is(err, artifact.ErrInvalidReference) {
		t.Fatalf("binding error: %v", err)
	}
	path := cache.ArtifactPath(string(encoded.Reference.BlobDigest))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteArtifactV2(encoded.Reference, encoded.Bytes); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil || !after.ModTime().Equal(info.ModTime()) {
		t.Fatal("identical cache write rewrote bytes", err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cache.ReadArtifactV2(encoded.Reference); !errors.Is(err, artifact.ErrBlobDigestMismatch) {
		t.Fatalf("tamper error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "verified.marker"), []byte("verified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cache.ReadArtifactV2(encoded.Reference); !errors.Is(err, artifact.ErrInvalidReference) {
		t.Fatalf("unqualified marker: %v", err)
	}
}
