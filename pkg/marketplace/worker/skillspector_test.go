package worker

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/advisory"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

type skillSpectorScannerFunc func(context.Context, map[string][]byte) (advisory.Result, error)

func (f skillSpectorScannerFunc) Scan(ctx context.Context, files map[string][]byte) (advisory.Result, error) {
	return f(ctx, files)
}

type observedSkillSpector struct {
	result advisory.Result
	err    error
	calls  int
	files  map[string][]byte
}

func (s *observedSkillSpector) Scan(_ context.Context, files map[string][]byte) (advisory.Result, error) {
	s.calls++
	s.files = files
	return s.result, s.err
}

// spec: marketplace-analysis/both-tools-report-for-mirrored-and-index-only-skills
// spec: marketplace-analysis/supplementary-analysis-cannot-weaken-rejection
// spec: marketplace-analysis/partial-report-preserves-supported-findings
// spec: marketplace-analysis/supplementary-scanner-fails-or-cannot-start
// spec: marketplace-api/unmapped-and-unsafe-scanner-evidence-is-not-published
func TestRefreshSkillSpectorReports(t *testing.T) {
	t.Run("mirror and index-only preserve intrinsic identities and bound evidence", func(t *testing.T) {
		for _, acquisition := range []string{"mirror", "index-only"} {
			t.Run(acquisition, func(t *testing.T) {
				repo, cfg := skillSpectorRepo(t, acquisition)
				authored := "---\nname: review\ndescription: Review code carefully\nlicense: MIT\n---\nReview the code carefully.\n"
				support := "approved support text\n"
				writeRepoFile(t, repo, ".agents/skills/review/SKILL.md", authored)
				writeRepoFile(t, repo, ".agents/skills/review/references/guide.txt", support)
				commitRepoChanges(t, repo)

				meta, deps, before := publishSkillSpectorBaseline(t, cfg)
				defer func() { _ = meta.Close() }()
				scanner := &observedSkillSpector{
					result: advisory.Result{
						Version: "2.13.6",
						Status:  store.AdvisoryPartial,
						Findings: []advisory.Finding{
							{Code: "SC4", Severity: "HIGH", Path: "references/guide.txt"},
							{Code: "SS0013", Severity: "critical", Path: "SKILL.md"},
							{Code: "raw scanner detail", Severity: "high", Path: "/private/worktree/SKILL.md"},
							{Code: "raw scanner detail", Severity: "high", Path: "../private.txt"},
						},
					},
				}
				deps.SkillSpector = advisory.SkillSpector{Scanner: scanner}
				if err := Refresh(context.Background(), deps, store.Registry{ID: store.RegistryID(cfg.Registries[0].ID)}); err != nil {
					t.Fatalf("Refresh: %v", err)
				}

				after := currentSkillSpectorRevision(t, deps, meta)
				if scanner.calls != 1 {
					t.Fatalf("scanner calls = %d, want 1; report = %#v", scanner.calls, after.Assessment.AdvisoryReports)
				}
				if len(scanner.files) != 2 || string(scanner.files["SKILL.md"]) != authored ||
					string(scanner.files["references/guide.txt"]) != support {
					t.Fatalf("scanner input = %#v", scanner.files)
				}
				assertSameIntrinsicAssessment(t, before, after)
				if after.Assessment.AssessmentDigest == before.Assessment.AssessmentDigest {
					t.Fatal("advisory evidence did not change assessment identity")
				}
				reports := after.Assessment.AdvisoryReports
				if len(reports) != 1 || reports[0].Tool != "skillspector" || reports[0].Version != "2.13.6" ||
					reports[0].Status != store.AdvisoryPartial || reports[0].Reason != "coverage_gaps" {
					t.Fatalf("advisory reports = %#v", reports)
				}
				verifiedPaths := make([]string, 0, len(reports[0].Findings))
				if len(reports[0].Findings) != 3 {
					t.Fatalf("advisory findings = %#v, want deduplicated findings", reports[0].Findings)
				}
				verifiedFinding := false
				for _, finding := range reports[0].Findings {
					if finding.Path != "" {
						verifiedPaths = append(verifiedPaths, finding.Path)
						verifiedFinding = finding.Code == "SC4" && finding.Severity == "high" &&
							finding.Summary == "Dependency risk detected."
					}
					if finding.Code == "raw scanner detail" {
						t.Fatalf("unbounded scanner evidence was published: %#v", finding)
					}
					if finding.Code != "SC4" && finding.Summary != "Potential risky skill behavior." {
						t.Fatalf("uncontrolled summary was published: %#v", finding)
					}
				}
				if len(verifiedPaths) != 1 || verifiedPaths[0] != "support/references/guide.txt" || !verifiedFinding {
					t.Fatalf("advisory findings = %#v", reports[0].Findings)
				}
				if acquisition == "mirror" && after.ArchiveDigest == "" {
					t.Fatal("mirror publication lost its archive")
				}
				if acquisition == "index-only" && after.ArchiveDigest != "" {
					t.Fatalf("index-only archive digest = %q", after.ArchiveDigest)
				}
			})
		}
	})

	t.Run("empty scanner output does not weaken rejection", func(t *testing.T) {
		_, cfg := skillSpectorRepo(t, "mirror")
		cfg.Policy.LicenseAllowlist = []string{"Apache-2.0"}
		meta, deps, before := publishSkillSpectorBaseline(t, cfg)
		defer func() { _ = meta.Close() }()
		scanner := &observedSkillSpector{result: advisory.Result{Version: "2.13.6", Status: store.AdvisoryComplete}}
		deps.SkillSpector = advisory.SkillSpector{Scanner: scanner}
		if err := Refresh(context.Background(), deps, store.Registry{ID: store.RegistryID(cfg.Registries[0].ID)}); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		after := currentSkillSpectorRevision(t, deps, meta)
		if scanner.calls != 1 || after.Assessment.Decision != store.AssessmentDecisionReject ||
			after.Assessment.Decision != before.Assessment.Decision || !reflect.DeepEqual(after.Assessment.Findings, before.Assessment.Findings) {
			t.Fatalf("rejected assessment changed: before=%#v after=%#v calls=%d", before.Assessment, after.Assessment, scanner.calls)
		}
		if len(after.Assessment.AdvisoryReports) != 1 || after.Assessment.AdvisoryReports[0].Status != store.AdvisoryComplete ||
			len(after.Assessment.AdvisoryReports[0].Findings) != 0 {
			t.Fatalf("advisory reports = %#v", after.Assessment.AdvisoryReports)
		}
	})

	t.Run("runner errors and invalid responses publish bounded failures", func(t *testing.T) {
		for _, test := range []struct {
			name    string
			scan    func(*testing.T) advisory.Scanner
			timeout time.Duration
			want    string
		}{
			{
				name: "runner error",
				scan: func(*testing.T) advisory.Scanner {
					return skillSpectorScannerFunc(func(context.Context, map[string][]byte) (advisory.Result, error) {
						return advisory.Result{}, errors.New("raw scanner detail at /private/worktree")
					})
				},
				want: "unavailable",
			},
			{
				name: "timeout",
				scan: func(*testing.T) advisory.Scanner {
					return skillSpectorScannerFunc(func(ctx context.Context, _ map[string][]byte) (advisory.Result, error) {
						<-ctx.Done()
						return advisory.Result{}, ctx.Err()
					})
				},
				timeout: time.Millisecond,
				want:    "timeout",
			},
			{
				name: "unavailable",
				scan: func(t *testing.T) advisory.Scanner {
					listener, err := net.Listen("tcp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					address := listener.Addr().String()
					_ = listener.Close()
					scanner, err := advisory.NewScanner("http://" + address)
					if err != nil {
						t.Fatalf("NewScanner: %v", err)
					}
					return scanner
				},
				want: "unavailable",
			},
			{
				name: "gateway timeout",
				scan: func(t *testing.T) advisory.Scanner {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.WriteHeader(http.StatusGatewayTimeout)
					}))
					t.Cleanup(server.Close)
					scanner, err := advisory.NewScanner(server.URL)
					if err != nil {
						t.Fatalf("NewScanner: %v", err)
					}
					return scanner
				},
				want: "timeout",
			},
			{
				name: "invalid response",
				scan: func(t *testing.T) advisory.Scanner {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte("raw scanner response from /private/worktree"))
					}))
					t.Cleanup(server.Close)
					scanner, err := advisory.NewScanner(server.URL)
					if err != nil {
						t.Fatalf("NewScanner: %v", err)
					}
					return scanner
				},
				want: "invalid_response",
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				_, cfg := skillSpectorRepo(t, "mirror")
				meta, deps, _ := publishSkillSpectorBaseline(t, cfg)
				defer func() { _ = meta.Close() }()
				deps.SkillSpector = advisory.SkillSpector{Scanner: test.scan(t), Timeout: test.timeout}
				if err := Refresh(context.Background(), deps, store.Registry{ID: store.RegistryID(cfg.Registries[0].ID)}); err != nil {
					t.Fatalf("Refresh: %v", err)
				}
				after := currentSkillSpectorRevision(t, deps, meta)
				reports := after.Assessment.AdvisoryReports
				if len(reports) != 1 || reports[0].Status != store.AdvisoryFailed || reports[0].Reason != test.want ||
					reports[0].Version != "" || len(reports[0].Findings) != 0 {
					t.Fatalf("failure report = %#v", reports)
				}
			})
		}
	})

	t.Run("aggregate budget shorter than timeout does not invoke scanner", func(t *testing.T) {
		_, cfg := skillSpectorRepo(t, "mirror")
		meta, deps, _ := publishSkillSpectorBaseline(t, cfg)
		defer func() { _ = meta.Close() }()
		scanner := &observedSkillSpector{result: advisory.Result{Version: "2.13.6", Status: store.AdvisoryComplete}}
		deps.SkillSpector = advisory.SkillSpector{
			Scanner: scanner,
			Timeout: time.Second,
			Budget:  time.Millisecond,
		}
		if err := Refresh(context.Background(), deps, store.Registry{ID: store.RegistryID(cfg.Registries[0].ID)}); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		after := currentSkillSpectorRevision(t, deps, meta)
		if scanner.calls != 0 {
			t.Fatalf("scanner calls = %d, want 0", scanner.calls)
		}
		if len(after.Assessment.AdvisoryReports) != 1 ||
			after.Assessment.AdvisoryReports[0].Status != store.AdvisoryNotRun ||
			after.Assessment.AdvisoryReports[0].Reason != "budget_exhausted" {
			t.Fatalf("advisory reports = %#v", after.Assessment.AdvisoryReports)
		}
	})
}

func skillSpectorRepo(t *testing.T, acquisition string) (string, config.Config) {
	t.Helper()
	repo, cleanup := createTestRepo(t)
	t.Cleanup(cleanup)
	cfg := createTestConfig(t, repo)
	cfg.Registries[0].Acquisition = acquisition
	return repo, cfg
}

func writeRepoFile(t *testing.T, repo, relative, contents string) {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

func commitRepoChanges(t *testing.T, repo string) {
	t.Helper()
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "update skill fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
}

func publishSkillSpectorBaseline(t *testing.T, cfg config.Config) (*sqlite.Store, Deps, store.AssessedResourceRevision) {
	t.Helper()
	meta, blobs := openTestStores(t)
	deps := Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}
	if err := EnsureMarketplace(context.Background(), &deps); err != nil {
		_ = meta.Close()
		t.Fatalf("EnsureMarketplace: %v", err)
	}
	registry := store.Registry{ID: store.RegistryID(cfg.Registries[0].ID)}
	if err := Refresh(context.Background(), deps, registry); err != nil {
		_ = meta.Close()
		t.Fatalf("baseline Refresh: %v", err)
	}
	return meta, deps, currentSkillSpectorRevision(t, deps, meta)
}

func currentSkillSpectorRevision(t *testing.T, deps Deps, meta store.MetadataStore) store.AssessedResourceRevision {
	t.Helper()
	coord := store.ResourceCoordinate{
		Marketplace: deps.Marketplace.ID,
		Registry:    store.RegistryID(deps.Config.Registries[0].ID),
		Kind:        "skill",
		Name:        "review",
	}
	return getRevision(t, context.Background(), meta, coord)
}

func assertSameIntrinsicAssessment(t *testing.T, before, after store.AssessedResourceRevision) {
	t.Helper()
	if before.Assessment.Decision != after.Assessment.Decision ||
		before.ContentDigest != after.ContentDigest ||
		before.ArchiveDigest != after.ArchiveDigest ||
		before.Assessment.InspectionManifestDigest != after.Assessment.InspectionManifestDigest ||
		!reflect.DeepEqual(before.Assessment.Findings, after.Assessment.Findings) {
		t.Fatalf("intrinsic assessment changed: before=%#v after=%#v", before, after)
	}
}
