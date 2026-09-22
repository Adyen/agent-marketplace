package main

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
	"time"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestArtifactLockReconciliationTransaction(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, scenario := range []string{"upgrade", "locked", "check", "offline", "tamper", "config-race", "lock-race", "transaction-failure"} {
		t.Run(scenario, func(t *testing.T) {
			home := canonicalTempDir(t)
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			repo := canonicalTempDir(t)
			configPath := filepath.Join(repo, ".agents", "config.yaml")
			lockPath := filepath.Join(repo, ".agents", "lock.yaml")
			source, err := artifact.FromIR(ir.NewSkill("review", "Review", "Review carefully", nil, nil, ir.SourceRef{}))
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
			if scenario == "tamper" {
				converted.Assets = map[string][]byte{"changed.txt": []byte("same payload, different protected entries")}
			}
			encoded, err := artifact.EncodeV2(converted, nil)
			if err != nil {
				t.Fatal(err)
			}
			coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "review"}
			content := store.RevisionDigest(source.Manifest.ContentDigest)
			assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{Coordinate: coordinate, ContentDigest: content, AnalyzerVersion: "test", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)), Decision: store.AssessmentDecisionAllow, InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)), AssessedAt: time.Unix(1, 0)})
			if err != nil {
				t.Fatal(err)
			}
			revision := store.AssessedResourceRevision{ResourceRevision: store.ResourceRevision{Coordinate: coordinate, ContentDigest: content, ArchiveDigest: store.ArchiveDigest(digest)}, Assessment: assessment, Occurrence: store.ResourceOccurrence{Coordinate: coordinate, ContentDigest: content, Commit: strings.Repeat("a", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64))}, Artifacts: []artifact.ArtifactReference{encoded.Reference}}
			requests := 0
			mux := http.NewServeMux()
			mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, r *http.Request) {
				requests++
				_ = json.NewEncoder(w).Encode(api.DefaultWellKnown("mp", "http://"+r.Host))
			})
			mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) { requests++; _ = json.NewEncoder(w).Encode(revision) })
			mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) { requests++; _, _ = w.Write(raw) })
			mux.HandleFunc("/api/v1/blobs/", func(w http.ResponseWriter, r *http.Request) { requests++; _, _ = w.Write(encoded.Bytes) })
			server := httptest.NewServer(mux)
			defer server.Close()
			configBytes := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  market:\n    marketplace: " + server.URL + "\nresources:\n  market:\n    include: [reg/skill/review]\n")
			writeConfig(t, configPath, string(configBytes))
			doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{URL: server.URL, InstanceID: "mp", RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "reg", RegistryID: "reg"}}, Resources: []lockfile.MarketplaceResourceLock{{RegistryName: "reg", RegistryID: "reg", Kind: "skill", Name: "review", Revision: string(content), ContentDigest: lockfile.Digest(content), ArchiveDigest: lockfile.Digest(digest), Acquisition: lockfile.MarketplaceAcquisitionMirror}}}}}
			lockBytes, err := lockfile.Encode(doc)
			if err != nil {
				t.Fatal(err)
			}
			writeConfig(t, lockPath, string(lockBytes))
			mode := lockMode{}
			switch scenario {
			case "locked":
				mode.locked = true
			case "check":
				mode.check = true
			case "offline":
				mode.offline = true
			}
			oldCommit := commitDependencyTransaction
			defer func() { commitDependencyTransaction = oldCommit }()
			failure := errors.New("transaction failure")
			commitDependencyTransaction = func(ctx context.Context, state string, files []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
				switch scenario {
				case "transaction-failure":
					return dependencytxn.Result{}, failure
				case "config-race":
					configBytes = append(configBytes, []byte("\n")...)
					if err := os.WriteFile(configPath, configBytes, 0o644); err != nil {
						t.Fatal(err)
					}
				case "lock-race":
					lockBytes = append(lockBytes, []byte("\n")...)
					if err := os.WriteFile(lockPath, lockBytes, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				return oldCommit(ctx, state, files, opts...)
			}
			_, changed, _, err := prepareRuntimeLocks(context.Background(), repo, repo, mode)
			switch scenario {
			case "tamper":
				if !errors.Is(err, artifact.ErrProtectedEntryMismatch) {
					t.Fatalf("tamper error: %v", err)
				}
			case "config-race", "lock-race":
				if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
					t.Fatalf("race error: %v", err)
				}
			case "transaction-failure":
				if !errors.Is(err, failure) {
					t.Fatalf("transaction error: %v", err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
			}
			gotConfig, err := os.ReadFile(configPath)
			if err != nil || !bytes.Equal(gotConfig, configBytes) {
				t.Fatal("config changed", err)
			}
			gotLock, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "upgrade" {
				upgraded, err := lockfile.Decode(gotLock)
				if err != nil {
					t.Fatal(err)
				}
				selected := upgraded.Marketplaces[0].Resources[0]
				if changed != 1 || upgraded.APIVersion != lockfile.APIVersionV3 || selected.Artifact == nil || selected.Artifact.Schema != "v2" || selected.ContentDigest != lockfile.Digest(content) {
					t.Fatalf("upgrade not committed: %+v", upgraded)
				}
			} else if !bytes.Equal(gotLock, lockBytes) {
				t.Fatal("lock changed on non-upgrade")
			}
			if scenario == "locked" || scenario == "check" || scenario == "offline" {
				if requests != 0 {
					t.Fatalf("strict mode issued %d requests", requests)
				}
			}
		})
	}
}
