package worker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestManagedActivationPublicationAndLiveWithdrawal(t *testing.T) {
	ctx := context.Background()
	deps, _ := managedFixture(t)
	deps.Config.Registries = nil
	deps.Config.Admin = config.Administration{Enabled: true, TrustedProxies: []string{"127.0.0.1/32"}, Origin: "https://market.example", Issuer: "test", SubjectHeader: "X-Test-Subject", GroupsHeader: "X-Test-Groups", AdminGroups: []string{"operators"}}
	if err := EnsureMarketplace(ctx, &deps); err != nil {
		t.Fatal(err)
	}
	if err := EnsureRevocations(ctx, deps); err != nil {
		t.Fatal(err)
	}
	repo, cleanup := createTestRepo(t)
	defer cleanup()
	commit, err := testCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduleManaged(ctx, deps, "activation"); err != nil {
		t.Fatal(err)
	}
	job, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "activation", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := processManagedRefresh(ctx, deps, job, "activation", func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
		return &acquire.Result{Dir: repo, Commit: commit}, nil
	}); err != nil {
		t.Fatal(err)
	}
	publication, err := deps.MetadataStore.(store.PublicationStore).CurrentRegistryPublication(ctx, "src-test")
	if err != nil {
		t.Fatal(err)
	}
	handler := api.NewServer(deps.Config, deps.MetadataStore, deps.BlobStore).PublicHandler()
	route := "/api/v1/manifests/" + string(publication.Occurrence.RegistryRevisionManifestDigest)
	check := func(status int) {
		t.Helper()
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(method, route, nil))
			if response.Code != status {
				t.Fatalf("%s: %d %s", method, response.Code, response.Body.String())
			}
			if strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
				t.Fatal("mutable permission cached as immutable")
			}
		}
	}
	check(http.StatusOK)
	revision := int64(1)
	for _, step := range []struct {
		action string
		status int
	}{{"pause", 200}, {"withdraw", 410}, {"restore", 200}} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/changes", strings.NewReader(fmt.Sprintf(`{"action":%q,"registry_id":"src-test","reason":%q}`, step.action, step.action)))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Origin", "https://market.example")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Marketplace-Admin", "1")
		request.Header.Set("X-Test-Subject", "operator")
		request.Header.Set("X-Test-Groups", `["operators"]`)
		request.Header.Set("If-Match", fmt.Sprintf(`"%d"`, revision))
		request.Header.Set("Idempotency-Key", step.action)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("%s: %d %s", step.action, response.Code, response.Body.String())
		}
		revision++
		if err := EnsureRevocations(ctx, deps); err != nil {
			t.Fatal(err)
		}
		check(step.status)
	}
	deps.Config.Management.Mode = "file"
	response := httptest.NewRecorder()
	api.NewServer(deps.Config, deps.MetadataStore, deps.BlobStore).PublicHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("legacy restart served managed bytes: %d", response.Code)
	}
}
