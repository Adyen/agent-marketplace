package claudedelivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	canonical "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/claudeplugins"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

const (
	testAPIKeyID = "apikey_testActor123"
	testPluginID = "plugin_delivery"
	testMarketID = "marketplace_library"
)

// spec: claude-enterprise-delivery/a-synchronized-marketplace-refuses-the-pass
func TestReconcilePreflightRefusesSynchronizedMarketplaceWithoutStateChanges(t *testing.T) {
	fixture := newDeliveryFixture(t, "guide", "guide text")
	api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
	api.marketplace.Source = "github"
	delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID, MarketplaceID: testMarketID})
	before := seedDeliveryState(t, fixture, "recorded-name")

	if _, err := delivery.Reconcile(context.Background()); errorCode(err) != "marketplace.unusable" {
		t.Fatalf("Reconcile error = %v, want marketplace.unusable", err)
	}
	after, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil || after != before {
		t.Fatalf("delivery state changed on preflight refusal: %+v, %v", after, err)
	}
	if api.writeCount() != 0 {
		t.Fatalf("preflight sent %d write requests", api.writeCount())
	}
}

func TestReconcileResolvesExplicitMarketplace(t *testing.T) {
	fixture := newDeliveryFixture(t, "guide", "guide text")
	api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
	delivery := fixture.reconciler(api, ReconcileTarget{
		APIKeyID: testAPIKeyID, MarketplaceID: testMarketID,
	})
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if api.createCount() != 1 || api.plugins[testPluginID].MarketplaceID != testMarketID {
		t.Fatalf("explicit target create count=%d plugin=%+v", api.createCount(), api.plugins[testPluginID])
	}
}

func TestReconcileRejectsMemberOwnedAndAbsentExplicitMarketplaces(t *testing.T) {
	for _, test := range []struct {
		name      string
		ownerType string
		present   bool
		wantCode  string
	}{
		{name: "member-owned", ownerType: "user", present: true, wantCode: "marketplace.unusable"},
		{name: "absent", ownerType: "organization", present: false, wantCode: "marketplace.not-found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDeliveryFixture(t, "guide", "guide text")
			api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
			api.marketplace.Owner.Type = test.ownerType
			api.marketplacePresent = test.present
			before := seedDeliveryState(t, fixture, "recorded-name")
			delivery := fixture.reconciler(api, ReconcileTarget{
				APIKeyID: testAPIKeyID, MarketplaceID: testMarketID,
			})
			if _, err := delivery.Reconcile(context.Background()); errorCode(err) != test.wantCode {
				t.Fatalf("Reconcile error = %v, want %s", err, test.wantCode)
			}
			after, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
			if err != nil || after != before || api.writeCount() != 0 {
				t.Fatalf("preflight changed state or wrote: after=%+v before=%+v writes=%d err=%v",
					after, before, api.writeCount(), err)
			}
		})
	}
}

// spec: claude-enterprise-delivery/an-invalid-archive-never-reaches-the-api
func TestReconcileSkipsInvalidArchiveAndContinues(t *testing.T) {
	fixture := newDeliveryFixture(t, "bad name", "not a valid resource path")
	fixture.addResource(t, "good", "valid resource", "commit-2", "publication-2")
	api := newPluginAPIFake(t, []string{"am-registry-skill-good"})
	delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})

	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil {
		t.Fatalf("GetClaudeDeliveryPlugin: %v", err)
	}
	if row.State != store.ClaudeDeliveryStateSkipped || row.LastErrorCode == "" {
		t.Fatalf("invalid resource state = %+v", row)
	}
	good, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), store.ResourceCoordinate{
		Marketplace: "managed-test", Registry: "registry", Kind: "skill", Name: "good",
	})
	if err != nil || good.State != store.ClaudeDeliveryStatePublished {
		t.Fatalf("valid sibling was not published: %+v, %v", good, err)
	}
	if api.writeCount() != 2 {
		t.Fatalf("invalid resource or valid sibling sent %d writes, want 2 for the valid sibling", api.writeCount())
	}
}

// spec: claude-enterprise-delivery/an-interrupted-pass-resumes-rather-than-restarts
func TestReconcileRetriesNamelessFailureRows(t *testing.T) {
	fixture := newDeliveryFixture(t, "guide", "guide text")
	fixture.addResource(t, "next", "next skill", "commit-2", "publication-2")
	api := newPluginAPIFake(t, []string{"am-registry-skill-guide", "am-registry-skill-next"})
	delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
	delivery.options.Metadata = &registryReadFailures{
		Store:     fixture.meta,
		remaining: 2,
	}

	first, err := delivery.Reconcile(context.Background())
	if err != nil || first.Failed != 2 {
		t.Fatalf("first Reconcile = %+v, %v; want two recorded failures", first, err)
	}
	for _, name := range []string{"guide", "next"} {
		row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), store.ResourceCoordinate{
			Marketplace: "managed-test", Registry: "registry", Kind: "skill", Name: name,
		})
		if err != nil || row.PluginName != "" {
			t.Fatalf("failed row for %s = %+v, %v; want no derived name", name, row, err)
		}
	}

	second, err := delivery.Reconcile(context.Background())
	if err != nil || second.Published != 2 || second.Conflicts != 0 {
		t.Fatalf("second Reconcile = %+v, %v; want both resources published without conflicts", second, err)
	}
	for _, name := range []string{"guide", "next"} {
		row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), store.ResourceCoordinate{
			Marketplace: "managed-test", Registry: "registry", Kind: "skill", Name: name,
		})
		if err != nil || row.PluginName == "" || row.State != store.ClaudeDeliveryStatePublished {
			t.Fatalf("recovered row for %s = %+v, %v", name, row, err)
		}
	}
	if api.createCount() != 2 {
		t.Fatalf("plugin create count = %d, want 2", api.createCount())
	}
}

// spec: claude-enterprise-delivery/a-newly-created-plugin-becomes-available-to-members
// spec: claude-enterprise-delivery/a-repeated-pass-over-unchanged-content-writes-nothing
// spec: claude-enterprise-delivery/a-changed-resource-uploads-exactly-one-version
// spec: claude-enterprise-delivery/a-renamed-registry-slug-does-not-rename-a-live-plugin
func TestReconcileCreatesSetsPreferenceSkipsUnchangedAndVersionsChangedContent(t *testing.T) {
	fixture := newDeliveryFixture(t, "guide", "first revision")
	api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
	delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})

	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	if api.createCount() != 1 || api.versionCount() != 0 ||
		api.preference(testPluginID) != "available" {
		t.Fatalf("first mirror creates=%d versions=%d preference=%q",
			api.createCount(), api.versionCount(), api.preference(testPluginID))
	}
	row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != store.ClaudeDeliveryStatePublished || row.ContentScanStatus != "processing" ||
		row.Reach != "organization" || row.ServedVersionID != "version_1" {
		t.Fatalf("published version metadata = %+v", row)
	}
	originalVersion := row.PublishedVersionID
	registry, err := fixture.meta.GetRegistry(context.Background(), "registry")
	if err != nil {
		t.Fatal(err)
	}
	registry.Slug = "renamed-registry"
	if err := fixture.meta.PutRegistry(context.Background(), registry); err != nil {
		t.Fatalf("rename registry: %v", err)
	}
	api.mu.Lock()
	plugin := api.plugins[testPluginID]
	plugin.ContentScan = claudeplugins.ContentScan{Status: "completed"}
	plugin.Reach = "workspace"
	plugin.ServedVersionID = "version_served"
	api.plugins[testPluginID] = plugin
	api.mu.Unlock()
	delivery = fixture.reconciler(api, ReconcileTarget{
		APIKeyID: testAPIKeyID, PluginNamePrefix: "new-prefix",
	})
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("unchanged Reconcile: %v", err)
	}
	row, err = fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil {
		t.Fatal(err)
	}
	if api.writeCount() != 2 || row.PublishedVersionID != originalVersion ||
		row.ContentScanStatus != "completed" || row.Reach != "workspace" ||
		row.ServedVersionID != "version_served" {
		t.Fatalf("unchanged pass writes=%d state=%+v", api.writeCount(), row)
	}

	fixture.publish(t, "second revision", "commit-2", "publication-2")
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("changed Reconcile: %v", err)
	}
	if api.createCount() != 1 || api.versionCount() != 1 {
		t.Fatalf("changed resource creates=%d versions=%d", api.createCount(), api.versionCount())
	}
	row, err = fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil || row.PublishedVersionID != "version_2" || row.PublishedArchiveDigest == "" {
		t.Fatalf("updated delivery = %+v, %v", row, err)
	}
	if row.PluginName != "am-registry-skill-guide" {
		t.Fatalf("registry slug change renamed plugin to %q", row.PluginName)
	}
}

// spec: claude-enterprise-delivery/a-foreign-plugin-with-the-same-name-is-never-written-to
// spec: claude-enterprise-delivery/a-plugin-created-by-this-key-is-adopted
func TestReconcileAdoptsOnlyPluginsCreatedByConfiguredAPIKey(t *testing.T) {
	for _, test := range []struct {
		name       string
		keyID      string
		wantState  store.ClaudeDeliveryState
		wantWrites int
	}{
		{name: "matching key is adopted", keyID: testAPIKeyID, wantState: store.ClaudeDeliveryStatePublished, wantWrites: 1},
		{name: "wrong configured key conflicts", keyID: "apikey_otherActor123", wantState: store.ClaudeDeliveryStateConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDeliveryFixture(t, "guide", "first revision")
			api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
			api.addPlugin(pluginRecord("am-registry-skill-guide", testPluginID, &claudeplugins.Actor{
				Type: "api_actor", APIKeyID: testAPIKeyID,
			}))
			delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: test.keyID})

			if _, err := delivery.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
			if err != nil || row.State != test.wantState {
				t.Fatalf("delivery row = %+v, %v", row, err)
			}
			if api.createCount() != 0 || api.versionCount() != test.wantWrites {
				t.Fatalf("writes create=%d version=%d, want 0/%d",
					api.createCount(), api.versionCount(), test.wantWrites)
			}
			if test.wantWrites == 0 && api.settingCount() != 0 {
				t.Fatalf("foreign plugin received %d installation writes", api.settingCount())
			}
		})
	}
}

// spec: claude-enterprise-delivery/a-foreign-plugin-with-the-same-name-is-never-written-to
func TestReconcileConflictsOnPluginNameCollisionAcrossResources(t *testing.T) {
	fixture := newDeliveryFixture(t, "my_skill", "first resource")
	fixture.addResource(t, "my-skill", "second resource", "commit-2", "publication-2")
	api := newPluginAPIFake(t, []string{"am-registry-skill-my-skill"})
	delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})

	result, err := delivery.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	collision, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil {
		t.Fatal(err)
	}
	first, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), store.ResourceCoordinate{
		Marketplace: fixture.coordinate.Marketplace, Registry: fixture.coordinate.Registry,
		Kind: fixture.coordinate.Kind, Name: "my-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Published != 1 || result.Conflicts != 1 ||
		collision.State != store.ClaudeDeliveryStateConflict ||
		collision.LastErrorCode != "plugin.name-collision" ||
		first.State != store.ClaudeDeliveryStatePublished {
		t.Fatalf("colliding delivery rows = first %+v, second %+v, result %+v",
			first, collision, result)
	}
	if api.createCount() != 1 || api.versionCount() != 0 || api.settingCount() != 1 {
		t.Fatalf("colliding resources wrote creates=%d versions=%d settings=%d",
			api.createCount(), api.versionCount(), api.settingCount())
	}
	creates, versions := api.createCount(), api.versionCount()
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	collision, err = fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil {
		t.Fatal(err)
	}
	if api.createCount() != creates || api.versionCount() != versions ||
		collision.PluginID != "" || collision.State != store.ClaudeDeliveryStateConflict ||
		collision.LastErrorCode != "plugin.name-collision" {
		t.Fatalf("second collision pass wrote creates=%d versions=%d, row=%+v",
			api.createCount()-creates, api.versionCount()-versions, collision)
	}
}

// spec: claude-enterprise-delivery/a-revocation-withdraws-a-mirrored-resource
func TestReconcileDoesNotRetireRowsFromAnotherSourceMarketplace(t *testing.T) {
	fixture := newDeliveryFixture(t, "guide", "guide text")
	otherCoordinate := store.ResourceCoordinate{
		Marketplace: "another-source", Registry: "registry", Kind: "skill", Name: "other",
	}
	other := store.ClaudeDeliveryPlugin{
		Coordinate: otherCoordinate, PluginName: "am-registry-skill-other",
		PluginID: "plugin_other_source", TargetMarketplaceID: testMarketID,
		PublishedArchiveDigest: "sha256:old", PublishedContentDigest: "sha256:old-content",
		PublishedVersionID: "version_old", LatestVersionID: "version_old",
		ServedVersionID: "version_old", InstallationPreference: "available",
		State: store.ClaudeDeliveryStatePublished,
	}
	if err := fixture.state.UpsertClaudeDeliveryPublished(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	before, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), otherCoordinate)
	if err != nil {
		t.Fatal(err)
	}
	api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
	delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})

	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	after, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), otherCoordinate)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("out-of-scope delivery state changed: before %+v, after %+v", before, after)
	}
	if api.settingCount() != 1 {
		t.Fatalf("installation setting requests = %d, want only the desired plugin's write", api.settingCount())
	}
}

// spec: claude-enterprise-delivery/a-foreign-plugin-with-the-same-name-is-never-written-to
func TestReconcileHandlesPluginNameTakenRaceWithOwnershipProof(t *testing.T) {
	for _, test := range []struct {
		name          string
		actor         *claudeplugins.Actor
		marketplaceID string
		wantState     store.ClaudeDeliveryState
		wantWrites    int
		wantErrorCode string
	}{
		{
			name: "matching API key", actor: &claudeplugins.Actor{Type: "api_actor", APIKeyID: testAPIKeyID},
			wantState: store.ClaudeDeliveryStatePublished, wantWrites: 2,
		},
		{
			name:          "matching API key in another marketplace",
			actor:         &claudeplugins.Actor{Type: "api_actor", APIKeyID: testAPIKeyID},
			marketplaceID: "marketplace_other", wantState: store.ClaudeDeliveryStateConflict,
			wantErrorCode: "plugin.foreign",
		},
		{
			name: "foreign API key", actor: &claudeplugins.Actor{Type: "api_actor", APIKeyID: "apikey_foreign456"},
			wantState: store.ClaudeDeliveryStateConflict,
		},
		{
			name: "administrator creator", actor: &claudeplugins.Actor{Type: "admin", APIKeyID: testAPIKeyID},
			wantState: store.ClaudeDeliveryStateConflict,
		},
		{name: "unrecorded creator", actor: nil, wantState: store.ClaudeDeliveryStateConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDeliveryFixture(t, "guide", "guide text")
			api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
			plugin := pluginRecord("am-registry-skill-guide", testPluginID, test.actor)
			if test.marketplaceID != "" {
				plugin.MarketplaceID = test.marketplaceID
			}
			api.addPlugin(plugin)
			api.hideInventory = true
			api.createNameTaken = true
			delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
			if _, err := delivery.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
			if err != nil || row.State != test.wantState ||
				(test.wantErrorCode != "" && row.LastErrorCode != test.wantErrorCode) {
				t.Fatalf("delivery row = %+v, %v", row, err)
			}
			if api.createCount() != 1 || api.writeCount() != 1+test.wantWrites {
				t.Fatalf("race result requests create=%d writes=%d, want create 1 and total %d",
					api.createCount(), api.writeCount(), 1+test.wantWrites)
			}
		})
	}
}

func TestReconcileSkipsSkillNameTakenWithoutRetryingOrRenaming(t *testing.T) {
	fixture := newDeliveryFixture(t, "guide", "guide text")
	api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
	api.createSkillNameTaken = true
	delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil || row.State != store.ClaudeDeliveryStateSkipped ||
		row.LastErrorCode != "plugin.skill-name-taken" {
		t.Fatalf("skill_name_taken state = %+v, %v", row, err)
	}
	if api.createCount() != 1 || api.versionCount() != 0 || api.settingCount() != 0 {
		t.Fatalf("skill_name_taken requests create=%d version=%d settings=%d",
			api.createCount(), api.versionCount(), api.settingCount())
	}
}

func TestReconcileHonorsVersionRegistrationPendingRetryHeader(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprintf("should_retry_%t", retry), func(t *testing.T) {
			fixture := newDeliveryFixture(t, "guide", "first revision")
			api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
			delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
			if _, err := delivery.Reconcile(context.Background()); err != nil {
				t.Fatalf("initial Reconcile: %v", err)
			}
			fixture.publish(t, "second revision", "commit-2", "publication-2")
			api.pendingVersionRemaining = 1
			api.pendingVersionRetry = retry
			if _, err := delivery.Reconcile(context.Background()); err != nil {
				t.Fatalf("pending version Reconcile: %v", err)
			}
			wantRequests := 1
			wantVersion := "version_2"
			wantSuccesses := 1
			if retry {
				wantRequests = 2
				wantVersion = "version_3"
				wantSuccesses = 2
			}
			if api.versionCount() != wantRequests || api.successfulVersions != wantSuccesses {
				t.Fatalf("pending version requests=%d successful=%d, want %d/%d",
					api.versionCount(), api.successfulVersions, wantRequests, wantSuccesses)
			}
			row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
			if err != nil || row.PublishedVersionID != wantVersion || row.State != store.ClaudeDeliveryStatePublished {
				t.Fatalf("pending version delivery = %+v, %v", row, err)
			}
		})
	}
}

// spec: claude-enterprise-delivery/an-interrupted-pass-resumes-rather-than-restarts
func TestReconcileResumesAfterRateLimitAndRetriesInstallation503(t *testing.T) {
	t.Run("429 resumes changed version", func(t *testing.T) {
		fixture := newDeliveryFixture(t, "guide", "first revision")
		fixture.addResource(t, "other", "other revision", "commit-2", "publication-2")
		api := newPluginAPIFake(t, []string{"am-registry-skill-guide", "am-registry-skill-other"})
		delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
		if _, err := delivery.Reconcile(context.Background()); err != nil {
			t.Fatalf("initial Reconcile: %v", err)
		}
		otherCoordinate := store.ResourceCoordinate{
			Marketplace: "managed-test", Registry: "registry", Kind: "skill", Name: "other",
		}
		otherResource, otherArchive := makeCanonicalSkill(t, otherCoordinate, "other revision")
		if err := fixture.blobs.Put(context.Background(), otherResource.ArchiveDigest, otherArchive); err != nil {
			t.Fatalf("put unchanged resource artifact: %v", err)
		}
		changedResource, changedArchive := makeCanonicalSkill(t, fixture.coordinate, "second revision")
		if err := fixture.blobs.Put(context.Background(), changedResource.ArchiveDigest, changedArchive); err != nil {
			t.Fatalf("put changed resource artifact: %v", err)
		}
		changedPublication := publicationRequestFor(changedResource, "commit-3", "publication-3")
		otherPublication := publicationRequestFor(otherResource, "commit-3", "publication-3")
		changedPublication.Resources = append(changedPublication.Resources, otherPublication.Resources...)
		changedPublication.ResourceOccurrences = append(changedPublication.ResourceOccurrences, otherPublication.ResourceOccurrences...)
		changedPublication.Assessments = append(changedPublication.Assessments, otherPublication.Assessments...)
		changedPublication.ExpectedHeadCommit = "commit-2"
		if err := fixture.meta.PublishRegistryPublication(context.Background(), changedPublication); err != nil {
			t.Fatalf("publish changed resource: %v", err)
		}
		prior, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
		if err != nil {
			t.Fatalf("read durable delivery state: %v", err)
		}
		api.mu.Lock()
		var otherPluginID string
		for id, plugin := range api.plugins {
			if plugin.Name == "am-registry-skill-other" {
				otherPluginID = id
				plugin.OrganizationInstallationPreference = "not_available"
				api.plugins[id] = plugin
			}
		}
		api.mu.Unlock()
		if otherPluginID == "" {
			t.Fatal("other desired resource was not created")
		}
		api.rateLimitedVersions = 2
		requestsBefore := api.requestLog()
		versionsBefore := api.versionCount()
		settingsBefore := api.settingCount()
		if _, err := delivery.Reconcile(context.Background()); err == nil {
			t.Fatal("rate-limited Reconcile succeeded")
		} else {
			var throttle *claudeplugins.ThrottledError
			if !errors.As(err, &throttle) {
				t.Fatalf("rate-limited Reconcile error = %T %v, want *ThrottledError", err, err)
			}
		}
		row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
		if err != nil || row.LastErrorCode != "claude-api.rate-limited" ||
			row.PublishedArchiveDigest != prior.PublishedArchiveDigest {
			t.Fatalf("rate-limit state = %+v, %v", row, err)
		}
		requestsAfterThrottle := api.requestLog()
		wantRequests := []string{
			"GET /v1/organizations/plugin_marketplaces",
			"GET /v1/organizations/plugins",
			"GET /v1/organizations/plugins/" + testPluginID,
			"POST /v1/organizations/plugins/" + testPluginID + "/versions",
			"POST /v1/organizations/plugins/" + testPluginID + "/versions",
		}
		if !reflect.DeepEqual(requestsAfterThrottle[len(requestsBefore):], wantRequests) ||
			api.versionCount()-versionsBefore != 2 || api.settingCount() != settingsBefore {
			t.Fatalf("requests after rate limit = %v; version writes=%d setting writes=%d",
				requestsAfterThrottle[len(requestsBefore):], api.versionCount()-versionsBefore, api.settingCount()-settingsBefore)
		}

		api.mu.Lock()
		other := api.plugins[otherPluginID]
		other.OrganizationInstallationPreference = "available"
		api.plugins[otherPluginID] = other
		api.mu.Unlock()
		successfulVersionsBefore := api.successfulVersionCount()
		if _, err := delivery.Reconcile(context.Background()); err != nil {
			t.Fatalf("resumed Reconcile: %v", err)
		}
		row, err = fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
		successfulVersions := api.successfulVersionCount()
		if err != nil || row.PublishedVersionID != "version_2" ||
			row.PublishedArchiveDigest == prior.PublishedArchiveDigest ||
			successfulVersions-successfulVersionsBefore != 1 {
			t.Fatalf("resumed version = %+v successes=%d, %v", row, successfulVersions-successfulVersionsBefore, err)
		}
	})

	t.Run("installation setting retries 503", func(t *testing.T) {
		fixture := newDeliveryFixture(t, "guide", "first revision")
		api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
		api.setting503Remaining = 1
		delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
		if _, err := delivery.Reconcile(context.Background()); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if api.settingCount() != 2 || api.preference(testPluginID) != "available" {
			t.Fatalf("installation setting requests=%d preference=%q", api.settingCount(), api.preference(testPluginID))
		}
	})
}

func TestReconcileReturnsClaudeDeliveryThrottlesWithoutStateOrFollowupRequests(t *testing.T) {
	for _, test := range []struct {
		name        string
		readPath    string
		failCreate  bool
		failSetting bool
	}{
		{name: "marketplace preflight", readPath: "/v1/organizations/plugin_marketplaces"},
		{name: "inventory preflight", readPath: "/v1/organizations/plugins"},
		{name: "plugin creation", failCreate: true},
		{name: "installation setting", failSetting: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDeliveryFixture(t, "guide", "first revision")
			api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
			if test.failCreate {
				api.rateLimitedCreates = 2
			} else if test.failSetting {
				api.rateLimitedSettings = 2
			} else {
				api.throttleReadPath = test.readPath
				api.throttleReadRemaining = 2
			}
			delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
			before := api.requestLog()
			if _, err := delivery.Reconcile(context.Background()); err == nil {
				t.Fatal("Reconcile succeeded after a terminal throttle")
			} else {
				var throttle *claudeplugins.ThrottledError
				if !errors.As(err, &throttle) {
					t.Fatalf("Reconcile error = %T %v, want *ThrottledError", err, err)
				}
			}
			rows, err := fixture.state.ListClaudeDeliveryPlugins(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if test.failCreate {
				if len(rows) != 1 || rows[0].LastErrorCode != "claude-api.rate-limited" {
					t.Fatalf("failed create state = %+v", rows)
				}
				requests := api.requestLog()
				if got := requests[len(before):]; len(got) != 4 ||
					got[2] != "POST /v1/organizations/plugins" ||
					got[3] != "POST /v1/organizations/plugins" || api.writeCount() != 2 {
					t.Fatalf("requests after create throttle = %v", got)
				}
			} else if test.failSetting {
				if len(rows) != 1 || rows[0].LastErrorCode != "claude-api.rate-limited" {
					t.Fatalf("failed setting state = %+v", rows)
				}
				requests := api.requestLog()
				if got := requests[len(before):]; len(got) != 5 ||
					got[3] != "POST /v1/organizations/plugins/"+testPluginID+"/installation_settings/organization" ||
					got[4] != "POST /v1/organizations/plugins/"+testPluginID+"/installation_settings/organization" ||
					api.writeCount() != 3 {
					t.Fatalf("requests after installation throttle = %v", got)
				}
			} else if len(rows) != 0 {
				t.Fatalf("preflight throttle persisted delivery rows: %+v", rows)
			}
			if !test.failCreate && !test.failSetting && api.writeCount() != 0 {
				t.Fatalf("preflight throttle sent %d write requests", api.writeCount())
			}
		})
	}
}

func TestReconcileStopsAfterClaudeDeliveryThrottleOnFailSoftVersionRead(t *testing.T) {
	fixture := newDeliveryFixture(t, "guide", "first revision")
	api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
	delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil {
		t.Fatal(err)
	}
	row.LatestVersionID = "version_previous"
	if err := fixture.state.UpsertClaudeDeliveryPublished(context.Background(), row); err != nil {
		t.Fatalf("set recorded version: %v", err)
	}
	api.mu.Lock()
	plugin := api.plugins[testPluginID]
	plugin.LatestVersionID = "version_current"
	api.plugins[testPluginID] = plugin
	api.mu.Unlock()
	fixture.publish(t, "second revision", "commit-2", "publication-2")
	api.throttleReadPath = "/v1/organizations/plugins/" + testPluginID + "/versions/version_current"
	api.throttleReadRemaining = 2
	before := api.requestLog()
	writesBefore := api.writeCount()

	if _, err := delivery.Reconcile(context.Background()); err == nil {
		t.Fatal("Reconcile succeeded after a terminal version read throttle")
	} else {
		var throttle *claudeplugins.ThrottledError
		if !errors.As(err, &throttle) {
			t.Fatalf("Reconcile error = %T %v, want *ThrottledError", err, err)
		}
	}
	row, err = fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil || row.LastErrorCode != "claude-api.rate-limited" {
		t.Fatalf("version read failure state = %+v, %v", row, err)
	}
	requests := api.requestLog()
	got := requests[len(before):]
	want := []string{
		"GET /v1/organizations/plugin_marketplaces",
		"GET /v1/organizations/plugins",
		"GET /v1/organizations/plugins/" + testPluginID,
		"GET /v1/organizations/plugins/" + testPluginID + "/versions/version_current",
		"GET /v1/organizations/plugins/" + testPluginID + "/versions/version_current",
	}
	if !reflect.DeepEqual(got, want) || api.writeCount() != writesBefore {
		t.Fatalf("requests after version read throttle = %v, writes=%d", got, api.writeCount()-writesBefore)
	}
}

// spec: claude-enterprise-delivery/a-revocation-withdraws-a-mirrored-resource
// spec: claude-enterprise-delivery/a-returning-resource-reuses-its-plugin
func TestReconcileWithdrawsAndRestoresTheExistingPlugin(t *testing.T) {
	fixture := newDeliveryFixture(t, "guide", "first revision")
	api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
	delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	revision := fixture.resource
	if err := fixture.meta.PutRevocation(context.Background(), store.Revocation{
		Kind:     store.RevocationRevision,
		TargetID: fmt.Sprintf("%s@%s", revision.Coordinate, revision.ContentDigest),
		Reason:   "revoked",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("withdrawal Reconcile: %v", err)
	}
	row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil || row.State != store.ClaudeDeliveryStateRetired ||
		api.preference(testPluginID) != "not_available" {
		t.Fatalf("withdrawn state=%+v preference=%q err=%v", row, api.preference(testPluginID), err)
	}
	if err := fixture.meta.DeleteRevocation(context.Background(), store.RevocationRevision,
		fmt.Sprintf("%s@%s", revision.Coordinate, revision.ContentDigest)); err != nil {
		t.Fatal(err)
	}
	fixture.publish(t, "second revision", "commit-2", "publication-2")
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("return Reconcile: %v", err)
	}
	row, err = fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil || row.State != store.ClaudeDeliveryStatePublished ||
		api.createCount() != 1 || api.versionCount() != 1 ||
		api.preference(testPluginID) != "available" {
		t.Fatalf("returned state=%+v creates=%d versions=%d preference=%q err=%v",
			row, api.createCount(), api.versionCount(), api.preference(testPluginID), err)
	}

	returnedRevision := fixture.resource
	returnedRevisionID := fmt.Sprintf("%s@%s", returnedRevision.Coordinate, returnedRevision.ContentDigest)
	if err := fixture.meta.PutRevocation(context.Background(), store.Revocation{
		Kind: store.RevocationRevision, TargetID: returnedRevisionID, Reason: "revoked again",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("second withdrawal Reconcile: %v", err)
	}
	if err := fixture.meta.DeleteRevocation(context.Background(), store.RevocationRevision, returnedRevisionID); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	plugin := api.plugins[testPluginID]
	plugin.OrganizationInstallationPreference = "available"
	api.plugins[testPluginID] = plugin
	api.mu.Unlock()
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("unchanged return Reconcile: %v", err)
	}
	row, err = fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil || row.State != store.ClaudeDeliveryStatePublished {
		t.Fatalf("unchanged returned state=%+v, %v", row, err)
	}
}

// spec: claude-enterprise-delivery/a-pending-registration-is-completed-without-a-duplicate-plugin
// spec: claude-enterprise-delivery/a-lost-version-response-does-not-store-a-duplicate-version
func TestReconcileCompletesPendingCreateAndDoesNotDuplicateLostVersion(t *testing.T) {
	t.Run("pending plugin create", func(t *testing.T) {
		fixture := newDeliveryFixture(t, "guide", "first revision")
		api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
		api.pendingCreate = true
		delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
		if _, err := delivery.Reconcile(context.Background()); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if api.createCount() != 1 || api.versionCount() != 1 {
			t.Fatalf("pending registration created=%d versions=%d", api.createCount(), api.versionCount())
		}
	})

	t.Run("lost version response", func(t *testing.T) {
		fixture := newDeliveryFixture(t, "guide", "first revision")
		api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
		delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
		if _, err := delivery.Reconcile(context.Background()); err != nil {
			t.Fatalf("initial Reconcile: %v", err)
		}
		fixture.publish(t, "second revision", "commit-2", "publication-2")
		api.loseNextVersionResponse = true
		if _, err := delivery.Reconcile(context.Background()); err != nil {
			t.Fatalf("lost-response Reconcile: %v", err)
		}
		if api.versionCount() != 1 {
			t.Fatalf("lost response caused %d version writes, want 1", api.versionCount())
		}
	})

	t.Run("lost response before storing version", func(t *testing.T) {
		fixture := newDeliveryFixture(t, "guide", "first revision")
		api := newPluginAPIFake(t, []string{"am-registry-skill-guide"})
		delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID})
		if _, err := delivery.Reconcile(context.Background()); err != nil {
			t.Fatalf("initial Reconcile: %v", err)
		}
		fixture.publish(t, "second revision", "commit-2", "publication-2")
		api.dropBeforeRecord = true
		if _, err := delivery.Reconcile(context.Background()); err != nil {
			t.Fatalf("retry Reconcile: %v", err)
		}
		row, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
		if err != nil || api.versionCount() != 2 ||
			row.State != store.ClaudeDeliveryStatePublished ||
			row.PublishedVersionID != "version_2" || api.successfulVersions != 1 {
			t.Fatalf("retry stored versions=%d row=%+v, %v", api.successfulVersions, row, err)
		}
	})
}

// spec: claude-enterprise-delivery/an-interrupted-pass-resumes-rather-than-restarts
func TestReconcileResumesCompletedResourcesAfterWriteLimiterInterruption(t *testing.T) {
	fixture := newDeliveryFixture(t, "guide", "first revision")
	fixture.addResource(t, "other", "other resource", "commit-2", "publication-2")
	api := newPluginAPIFake(t, []string{"am-registry-skill-guide", "am-registry-skill-other"})
	limiter := &testWriteLimiter{failAt: 3}
	delivery := fixture.reconciler(api, ReconcileTarget{APIKeyID: testAPIKeyID}, limiter)

	if _, err := delivery.Reconcile(context.Background()); err == nil {
		t.Fatal("Reconcile succeeded after limiter interruption")
	}
	firstRow, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil || firstRow.State != store.ClaudeDeliveryStatePublished {
		t.Fatalf("completed resource was not recorded: %+v, %v", firstRow, err)
	}
	limiter.failAt = 0
	if _, err := delivery.Reconcile(context.Background()); err != nil {
		t.Fatalf("resumed Reconcile: %v", err)
	}
	if api.createCount() != 2 || api.versionCount() != 0 {
		t.Fatalf("resumed pass duplicated work: creates=%d versions=%d", api.createCount(), api.versionCount())
	}
	for _, delay := range limiter.delays {
		if delay < time.Second {
			t.Fatalf("write pacing interval = %s, want at least 1s", delay)
		}
	}
}

type testWriteLimiter struct {
	mu     sync.Mutex
	calls  int
	failAt int
	delays []time.Duration
}

func (l *testWriteLimiter) Wait(ctx context.Context, delay time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	l.delays = append(l.delays, delay)
	if l.failAt > 0 && l.calls == l.failAt {
		return errors.New("test limiter interruption")
	}
	return ctx.Err()
}

type deliveryFixture struct {
	t          *testing.T
	meta       *sqlite.Store
	state      store.ClaudeDeliveryStateStore
	blobs      *fsblob.Store
	coordinate store.ResourceCoordinate
	resource   store.ResourceRevision
}

type registryReadFailures struct {
	*sqlite.Store
	remaining int
}

func (s *registryReadFailures) GetRegistry(ctx context.Context, id store.RegistryID) (store.Registry, error) {
	if s.remaining > 0 {
		s.remaining--
		return store.Registry{}, errors.New("registry read failed")
	}
	return s.Store.GetRegistry(ctx, id)
}

func newDeliveryFixture(t *testing.T, name, content string) *deliveryFixture {
	t.Helper()
	dir := t.TempDir()
	meta := sqlite.New(filepath.Join(dir, "metadata.db"))
	if err := meta.Open(context.Background()); err != nil {
		t.Fatalf("open SQLite metadata: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	fixture := &deliveryFixture{
		t: t, meta: meta, state: meta, blobs: fsblob.New(filepath.Join(dir, "blobs")),
		coordinate: store.ResourceCoordinate{Marketplace: "managed-test", Registry: "registry", Kind: "skill", Name: name},
	}
	if err := meta.PutMarketplace(context.Background(), store.Marketplace{
		ID: "managed-test", Name: "Managed", CanonicalURL: "https://marketplace.test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutRegistry(context.Background(), store.Registry{
		ID: "registry", Slug: "registry", Ref: "main", Acquisition: store.AcquisitionIndexOnly,
		Publisher: store.Publisher{Name: "Example Publisher", URL: "https://publisher.example"},
		Listed:    true,
	}); err != nil {
		t.Fatal(err)
	}
	fixture.publish(t, content, "commit-1", "publication-1")
	return fixture
}

func (f *deliveryFixture) addResource(t *testing.T, name, content, commit string, publicationID store.PublicationID) {
	t.Helper()
	coordinate := store.ResourceCoordinate{
		Marketplace: "managed-test", Registry: "registry", Kind: "skill", Name: name,
	}
	resource, raw := makeCanonicalSkill(t, coordinate, content)
	if err := f.blobs.Put(context.Background(), resource.ArchiveDigest, raw); err != nil {
		t.Fatalf("put artifact: %v", err)
	}
	req := publicationRequestFor(f.resource, commit, publicationID)
	addition := publicationRequestFor(resource, commit, publicationID)
	req.Resources = append(req.Resources, addition.Resources...)
	req.ResourceOccurrences = append(req.ResourceOccurrences, addition.ResourceOccurrences...)
	req.Assessments = append(req.Assessments, addition.Assessments...)
	req.ExpectedHeadCommit = "commit-1"
	if err := f.meta.PublishRegistryPublication(context.Background(), req); err != nil {
		t.Fatalf("publish resource: %v", err)
	}
}

func (f *deliveryFixture) publish(t *testing.T, content, commit string, publicationID store.PublicationID) {
	t.Helper()
	resource, raw := makeCanonicalSkill(t, f.coordinate, content)
	if err := f.blobs.Put(context.Background(), resource.ArchiveDigest, raw); err != nil {
		t.Fatalf("put artifact: %v", err)
	}
	req := publicationRequestFor(resource, commit, publicationID)
	if commit != "commit-1" {
		req.ExpectedHeadCommit = "commit-1"
	}
	if err := f.meta.PublishRegistryPublication(context.Background(), req); err != nil {
		t.Fatalf("publish resource: %v", err)
	}
	f.resource = resource
}

func makeCanonicalSkill(t *testing.T, coordinate store.ResourceCoordinate, content string) (store.ResourceRevision, []byte) {
	t.Helper()
	resource, err := canonical.FromIR(ir.NewSkill(coordinate.Name, "A delivery test skill", content, nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("create canonical resource: %v", err)
	}
	resource.Manifest.RegistryID = string(coordinate.Registry)
	resource.Manifest.RegistrySlug = "registry"
	resource.Manifest.Revision = "revision"
	raw, digest, err := canonical.Encode(resource)
	if err != nil {
		t.Fatalf("encode canonical artifact: %v", err)
	}
	return store.ResourceRevision{
		Coordinate: coordinate, ContentDigest: store.RevisionDigest(resource.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(digest), License: "MIT", Description: "A delivery test skill",
	}, raw
}

func publicationRequestFor(resource store.ResourceRevision, commit string, id store.PublicationID) store.RegistryRevisionPublication {
	manifest := "sha256:" + strings.Repeat("d", 64)
	assessment, _ := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: resource.Coordinate, ContentDigest: resource.ContentDigest,
		AnalyzerVersion: "delivery-test/v1", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("b", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("c", 64)),
		AssessedAt:               time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC),
	})
	return store.RegistryRevisionPublication{
		PublicationID: id,
		Occurrence: store.RegistryOccurrence{
			RegistryID: resource.Coordinate.Registry, Ref: "main", Commit: commit,
			Roots:                          []store.RegistryRoot{{Path: "."}},
			RegistryRevisionManifestDigest: store.RevisionDigest(manifest),
			Acquisition:                    store.AcquisitionIndexOnly,
		},
		Resources: []store.ResourceRevision{resource},
		ResourceOccurrences: []store.ResourceOccurrence{{
			Coordinate: resource.Coordinate, ContentDigest: resource.ContentDigest,
			Commit: commit, ManifestDigest: store.RevisionDigest(manifest), SourcePath: resource.Coordinate.Name,
		}},
		Assessments: []store.ResourceAssessment{assessment},
	}
}

func (f *deliveryFixture) reconciler(api *pluginAPIFake, target ReconcileTarget, limiters ...WriteLimiter) *Reconciler {
	f.t.Helper()
	client, err := claudeplugins.NewClient(claudeplugins.Options{
		BaseURL: api.server.URL, APIKey: claudeplugins.NewAPIKey("test-admin-key"),
		HTTPClient: api.server.Client(),
	})
	if err != nil {
		f.t.Fatalf("NewClient: %v", err)
	}
	var limiter WriteLimiter
	if len(limiters) > 0 {
		limiter = limiters[0]
	} else {
		limiter = &testWriteLimiter{}
	}
	return NewReconciler(ReconcileOptions{
		Metadata: f.meta, Blobs: f.blobs, Client: client,
		SourceMarketplaceID: f.coordinate.Marketplace,
		RegistryIDs:         []store.RegistryID{"registry"}, Target: target,
		Limiter: limiter, Now: func() time.Time {
			return time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
		},
	})
}

func seedDeliveryState(t *testing.T, fixture *deliveryFixture, name string) store.ClaudeDeliveryPlugin {
	t.Helper()
	row := store.ClaudeDeliveryPlugin{
		Coordinate: fixture.coordinate, PluginName: name, PluginID: testPluginID,
		TargetMarketplaceID: testMarketID, PublishedArchiveDigest: "sha256:old",
		PublishedContentDigest: "sha256:old-content", PublishedVersionID: "version_old",
		LatestVersionID: "version_old", ServedVersionID: "version_old",
		InstallationPreference: "available", State: store.ClaudeDeliveryStatePublished,
	}
	if err := fixture.state.UpsertClaudeDeliveryPublished(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	got, err := fixture.state.GetClaudeDeliveryPlugin(context.Background(), fixture.coordinate)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func errorCode(err error) string {
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		return coded.ErrorCode()
	}
	return ""
}

type pluginAPIFake struct {
	t                       *testing.T
	server                  *httptest.Server
	mu                      sync.Mutex
	marketplace             claudeplugins.PluginMarketplace
	marketplacePresent      bool
	plugins                 map[string]claudeplugins.Plugin
	names                   []string
	creates                 int
	successfulCreates       int
	versions                int
	successfulVersions      int
	settings                int
	pendingCreate           bool
	loseNextVersionResponse bool
	dropBeforeRecord        bool
	hideInventory           bool
	createNameTaken         bool
	createSkillNameTaken    bool
	pendingVersionRemaining int
	pendingVersionRetry     bool
	rateLimitedCreates      int
	rateLimitedVersions     int
	rateLimitedSettings     int
	throttleReadPath        string
	throttleReadRemaining   int
	requests                []string
	setting503Remaining     int
}

func newPluginAPIFake(t *testing.T, names []string) *pluginAPIFake {
	t.Helper()
	api := &pluginAPIFake{
		t: t, names: names,
		marketplace: claudeplugins.PluginMarketplace{
			Type: "plugin_marketplace", ID: testMarketID, Name: "Library",
			Source: "manual", Owner: claudeplugins.Owner{Type: "organization"},
		},
		marketplacePresent: true,
		plugins:            make(map[string]claudeplugins.Plugin),
	}
	api.server = httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	t.Cleanup(api.server.Close)
	return api
}

func (api *pluginAPIFake) addPlugin(plugin claudeplugins.Plugin) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.plugins[plugin.ID] = plugin
}

func (api *pluginAPIFake) serveHTTP(w http.ResponseWriter, r *http.Request) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.requests = append(api.requests, r.Method+" "+r.URL.Path)
	if api.throttleReadRemaining > 0 && r.URL.Path == api.throttleReadPath {
		api.throttleReadRemaining--
		w.Header().Set("retry-after", "0")
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "")
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/plugin_marketplaces":
		var marketplaces []claudeplugins.PluginMarketplace
		if api.marketplacePresent {
			marketplaces = append(marketplaces, api.marketplace)
		}
		writeJSON(w, map[string]any{"data": marketplaces, "next_page": nil})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/plugin_marketplaces/"+testMarketID:
		if !api.marketplacePresent {
			writeAPIError(w, http.StatusNotFound, "not_found", "")
			return
		}
		writeJSON(w, api.marketplace)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/plugins":
		plugins := make([]claudeplugins.Plugin, 0, len(api.plugins))
		if !api.hideInventory {
			for _, plugin := range api.plugins {
				plugins = append(plugins, plugin)
			}
		}
		writeJSON(w, map[string]any{"data": plugins, "next_page": nil})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/plugins":
		api.creates++
		if api.rateLimitedCreates > 0 {
			api.rateLimitedCreates--
			w.Header().Set("retry-after", "0")
			writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "")
			return
		}
		if api.createNameTaken {
			api.createNameTaken = false
			w.Header().Set("x-should-retry", "false")
			writeAPIError(w, http.StatusConflict, "plugin_name_taken", testPluginID)
			return
		}
		if api.createSkillNameTaken {
			api.createSkillNameTaken = false
			writeAPIError(w, http.StatusConflict, "skill_name_taken", "")
			return
		}
		name := "am-registry-skill-guide"
		if api.successfulCreates < len(api.names) {
			name = api.names[api.successfulCreates]
		}
		pluginID := testPluginID
		if api.successfulCreates > 0 {
			pluginID = fmt.Sprintf("%s_%d", testPluginID, api.successfulCreates+1)
		}
		api.successfulCreates++
		plugin := pluginRecord(name, pluginID, &claudeplugins.Actor{
			Type: "api_actor", APIKeyID: testAPIKeyID,
		})
		plugin.LatestVersionID = "version_1"
		plugin.ServedVersionID = "version_1"
		plugin.ContentScan = claudeplugins.ContentScan{Status: "processing"}
		api.plugins[plugin.ID] = plugin
		if api.pendingCreate {
			api.pendingCreate = false
			w.Header().Set("x-should-retry", "false")
			writeAPIError(w, http.StatusServiceUnavailable, "registration_pending", plugin.ID)
			return
		}
		writeJSON(w, plugin)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/organizations/plugins/") &&
		!strings.Contains(strings.TrimPrefix(r.URL.Path, "/v1/organizations/plugins/"), "/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/organizations/plugins/")
		if plugin, ok := api.plugins[id]; ok {
			writeJSON(w, plugin)
		} else {
			writeAPIError(w, http.StatusNotFound, "not_found", "")
		}
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/installation_settings/organization"):
		api.settings++
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/organizations/plugins/"), "/installation_settings/organization")
		if api.rateLimitedSettings > 0 {
			api.rateLimitedSettings--
			w.Header().Set("retry-after", "0")
			writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "")
			return
		}
		if api.setting503Remaining > 0 {
			api.setting503Remaining--
			writeAPIError(w, http.StatusServiceUnavailable, "temporary_unavailable", id)
			return
		}
		plugin := api.plugins[id]
		var setting struct {
			InstallationPreference string `json:"installation_preference"`
		}
		_ = json.NewDecoder(r.Body).Decode(&setting)
		plugin.OrganizationInstallationPreference = setting.InstallationPreference
		api.plugins[id] = plugin
		writeJSON(w, claudeplugins.InstallationSetting{InstallationPreference: setting.InstallationPreference})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/versions"):
		api.versions++
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/organizations/plugins/"), "/versions")
		plugin := api.plugins[id]
		if api.rateLimitedVersions > 0 {
			api.rateLimitedVersions--
			w.Header().Set("retry-after", "0")
			writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "")
			return
		}
		if api.pendingVersionRemaining > 0 {
			api.pendingVersionRemaining--
			w.Header().Set("x-should-retry", fmt.Sprintf("%t", api.pendingVersionRetry))
			api.successfulVersions++
			versionID := fmt.Sprintf("version_%d", api.successfulVersions+1)
			plugin.LatestVersionID = versionID
			plugin.ContentScan = claudeplugins.ContentScan{Status: "processing"}
			plugin.Reach = "organization"
			api.plugins[id] = plugin
			writeAPIErrorWithVersion(w, http.StatusServiceUnavailable, "registration_pending", id, versionID)
			return
		}
		if api.dropBeforeRecord {
			api.dropBeforeRecord = false
			api.dropVersionResponse(w)
			return
		}
		api.successfulVersions++
		versionID := fmt.Sprintf("version_%d", api.successfulVersions+1)
		plugin.LatestVersionID = versionID
		plugin.ContentScan = claudeplugins.ContentScan{Status: "processing"}
		plugin.Reach = "organization"
		api.plugins[id] = plugin
		if api.loseNextVersionResponse {
			api.loseNextVersionResponse = false
			api.dropVersionResponse(w)
			return
		}
		writeJSON(w, claudeplugins.PluginVersion{
			ID: versionID, PluginID: id, ContentScan: plugin.ContentScan, Reach: plugin.Reach,
		})
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/versions/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/organizations/plugins/")
		parts := strings.Split(id, "/versions/")
		plugin := api.plugins[parts[0]]
		writeJSON(w, claudeplugins.PluginVersion{
			ID: plugin.LatestVersionID, PluginID: plugin.ID,
			ContentScan: plugin.ContentScan, Reach: plugin.Reach,
		})
	default:
		api.t.Errorf("unexpected Plugins API request: %s %s", r.Method, r.URL.RequestURI())
		writeAPIError(w, http.StatusNotFound, "not_found", "")
	}
}

func (api *pluginAPIFake) dropVersionResponse(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		api.t.Error("HTTP response writer does not support hijacking")
		return
	}
	conn, _, err := hijacker.Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func (api *pluginAPIFake) writeCount() int {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.creates + api.versions + api.settings
}

func (api *pluginAPIFake) requestLog() []string {
	api.mu.Lock()
	defer api.mu.Unlock()
	return append([]string(nil), api.requests...)
}

func (api *pluginAPIFake) createCount() int {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.creates
}

func (api *pluginAPIFake) versionCount() int {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.versions
}

func (api *pluginAPIFake) successfulVersionCount() int {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.successfulVersions
}

func (api *pluginAPIFake) settingCount() int {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.settings
}

func (api *pluginAPIFake) preference(pluginID string) string {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.plugins[pluginID].OrganizationInstallationPreference
}

func pluginRecord(name, id string, actor *claudeplugins.Actor) claudeplugins.Plugin {
	return claudeplugins.Plugin{
		Type: "plugin", ID: id, Name: name, DisplayName: name,
		Owner: claudeplugins.Owner{Type: "organization"}, MarketplaceID: testMarketID,
		CreatedBy: actor, ContentScan: claudeplugins.ContentScan{Status: "pass"},
		Reach: "organization", Components: []claudeplugins.Component{},
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, code, pluginID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
		"details": map[string]string{"error_code": code, "plugin_id": pluginID},
	}})
}

func writeAPIErrorWithVersion(w http.ResponseWriter, status int, code, pluginID, versionID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
		"details": map[string]string{
			"error_code": code, "plugin_id": pluginID, "plugin_version_id": versionID,
		},
	}})
}
