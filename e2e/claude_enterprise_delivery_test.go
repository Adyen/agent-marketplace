package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const claudeDeliveryEndpointEnv = "AGENT_MARKETPLACE_E2E_CLAUDE_PLUGINS_ENDPOINT"
const claudeDeliveryKeyEnv = "CLAUDE_ENTERPRISE_TEST_KEY"

// spec: claude-enterprise-delivery/disabled-delivery-makes-no-outbound-request
func TestClaudeEnterpriseDeliveryDisabledMakesNoOutboundRequest(t *testing.T) {
	env, _ := newServeReindexEnvWithURL(t)
	fake := newClaudePluginsAPIFake(t)
	result := runClaudeDeliveryOnce(env, fake.URL())
	if result.ExitCode != 0 {
		t.Fatalf("disabled delivery sync exited %d:\n%s", result.ExitCode, result.Combined)
	}
	if got := fake.requestCount(); got != 0 {
		t.Fatalf("disabled delivery made %d Plugins API requests", got)
	}
	metadata := openServeMetadata(t, env)
	defer func() { _ = metadata.Close() }()
	rows, err := metadata.ListClaudeDeliveryPlugins(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatalf("disabled delivery created rows: %+v %v", rows, err)
	}
}

// spec: claude-enterprise-delivery/publication-completion-enqueues-delivery
func TestClaudeEnterpriseDeliveryMirrorsChangesAndWithdrawsAvailabilityOnly(t *testing.T) {
	env, registryURL := newServeReindexEnvWithURL(t)
	fake := newClaudePluginsAPIFake(t)
	operations := publicGateEphemeralAddress(t)
	baseConfig := strings.Replace(env.ReadFile("/workspace/marketplace.yaml"),
		"  canonical_url: https://example.com\n",
		"  canonical_url: https://example.com\noperations_listen: "+operations+"\n", 1)
	env.CreateBareRepo("serve-reg-rejected", map[string]string{
		".agents/skills/rejected/SKILL.md": "---\nname: rejected\ndescription: This license is not allowed\nlicense: GPL-3.0-only\n---\n\n# rejected\n",
	})
	rejectedURL := strings.TrimSuffix(registryURL, "/serve-reg.git") + "/serve-reg-rejected.git"
	baseConfig = appendMetricsRegistry(baseConfig, "src-rejected", "rejected-source", rejectedURL, false)
	config := baseConfig + `
claude_enterprise:
  enabled: true
  api_key:
    env: CLAUDE_ENTERPRISE_TEST_KEY
  api_key_id: apikey_e2e
  marketplace_id: marketplace_e2e
  interval: 2s
`
	replaceServeConfig(t, env, config)
	configPath := env.mustResolvePath("/workspace/marketplace.yaml")
	process := startServeTestProcess(t, env, configPath, "worker", map[string]string{
		claudeDeliveryEndpointEnv: fake.URL(),
		claudeDeliveryKeyEnv:      "sk-ant-e2e-secret",
	})
	metadata := openServeMetadata(t, env)
	defer func() { _ = metadata.Close() }()
	waitForServeCondition(t, func() bool {
		desired, err := metadata.ListClaudeDeliveryDesired(t.Context(), metadata, "e2e-serve-mp", []store.RegistryID{"src-serve"})
		return err == nil && len(desired) == 1
	}, "initial current delivery resource")
	desired, err := metadata.ListClaudeDeliveryDesired(t.Context(), metadata, "e2e-serve-mp", []store.RegistryID{"src-serve"})
	if err != nil || len(desired) != 1 {
		t.Fatalf("initial current delivery set = %+v, %v", desired, err)
	}
	rejectedCoordinate := store.ResourceCoordinate{
		Marketplace: "e2e-serve-mp", Registry: "src-rejected", Kind: "skill", Name: "rejected",
	}
	waitForServeCondition(t, func() bool {
		_, err := metadata.GetLatestResourceRevision(t.Context(), rejectedCoordinate)
		return err == nil
	}, "rejected registry resource")
	rejected, err := metadata.GetLatestResourceRevision(t.Context(), rejectedCoordinate)
	if err != nil || rejected.Assessment.Decision != store.AssessmentDecisionReject {
		t.Fatalf("rejected fixture assessment = %+v, %v", rejected.Assessment, err)
	}
	rejectedDesired, err := metadata.ListClaudeDeliveryDesired(t.Context(), metadata, "e2e-serve-mp", []store.RegistryID{"src-rejected"})
	if err != nil || len(rejectedDesired) != 0 {
		t.Fatalf("rejected current resource entered delivery set: %+v, %v", rejectedDesired, err)
	}
	first := waitForClaudeDeliverySnapshot(t, process, fake, func(snapshot claudePluginsSnapshot) bool {
		return snapshot.createdPlugins == 1 && equalStrings(snapshot.preferences, []string{"available"})
	}, "initial plugin mirror")
	if first.createdVersions != 0 || first.deletes != 0 || first.writeRequests != 2 {
		t.Fatalf("first mirror did not only create one plugin and set its availability: %+v", first)
	}

	unchanged := waitForClaudeDeliverySnapshot(t, process, fake, func(snapshot claudePluginsSnapshot) bool {
		return snapshot.requests >= first.requests+2
	}, "a repeated reconcile pass over unchanged publication")
	if unchanged.writeRequests != first.writeRequests || unchanged.createdPlugins != first.createdPlugins ||
		unchanged.createdVersions != 0 || !equalStrings(unchanged.preferences, first.preferences) {
		t.Fatalf("unchanged pass wrote to Plugins API: %+v", unchanged)
	}

	env.CreateBareRepo("serve-reg-updated", map[string]string{
		".agents/skills/review/SKILL.md": "---\nname: review\ndescription: Review changed code carefully\nlicense: MIT\n---\n\n# review\n",
	})
	updatedURL := strings.TrimSuffix(registryURL, "/serve-reg.git") + "/serve-reg-updated.git"
	config = strings.Replace(env.ReadFile("/workspace/marketplace.yaml"), registryURL, updatedURL, 1)
	replaceServeConfig(t, env, config)
	if result := env.RunAM("serve", "reindex", "--config", "marketplace.yaml"); result.ExitCode != 0 {
		t.Fatalf("changed publication exited %d:\n%s", result.ExitCode, result.Combined)
	}
	changed := waitForClaudeDeliverySnapshot(t, process, fake, func(snapshot claudePluginsSnapshot) bool {
		return snapshot.createdVersions == 1
	}, "delivery of the changed publication")
	if changed.createdPlugins != 1 || changed.createdVersions != 1 || changed.deletes != 0 ||
		changed.writeRequests != unchanged.writeRequests+1 || !equalStrings(changed.preferences, first.preferences) {
		t.Fatalf("changed resource did not create exactly one version: %+v", changed)
	}

	config = env.ReadFile("/workspace/marketplace.yaml") + `
revocations:
  coordinates:
    - e2e-serve-mp/src-serve/skill/review
`
	replaceServeConfig(t, env, config)
	if result := env.RunAM("serve", "reindex", "--config", "marketplace.yaml"); result.ExitCode != 0 {
		t.Fatalf("revocation publication exited %d:\n%s", result.ExitCode, result.Combined)
	}
	withdrawn := waitForClaudeDeliverySnapshot(t, process, fake, func(snapshot claudePluginsSnapshot) bool {
		return equalStrings(snapshot.preferences, []string{"available", "not_available"})
	}, "revocation withdrawal")
	if withdrawn.createdPlugins != 1 || withdrawn.createdVersions != 1 || withdrawn.deletes != 0 ||
		withdrawn.writeRequests != changed.writeRequests+1 ||
		!equalStrings(withdrawn.preferences, []string{"available", "not_available"}) {
		t.Fatalf("withdrawal was not availability-only: %+v", withdrawn)
	}
}

func waitForClaudeDeliverySnapshot(t *testing.T, process *serveTestProcess, fake *claudePluginsAPIFake, ready func(claudePluginsSnapshot) bool, description string) claudePluginsSnapshot {
	t.Helper()
	deadline := time.Now().Add(workerMetricWait)
	for time.Now().Before(deadline) {
		select {
		case err := <-process.done:
			process.waitErr = err
			process.stopped = true
			process.cancel()
			t.Fatalf("serve worker exited before %s: %v\n%s", description, err, process.logs())
		default:
		}
		snapshot := fake.snapshot()
		if ready(snapshot) {
			return snapshot
		}
		time.Sleep(25 * time.Millisecond)
	}
	logs := process.stop(t)
	t.Fatalf("timed out waiting for %s; latest Plugins API state: %+v\n%s", description, fake.snapshot(), logs)
	return claudePluginsSnapshot{}
}

func runClaudeDeliveryOnce(env *Env, endpoint string) ExecResult {
	return env.RunAMWithEnv(map[string]string{
		claudeDeliveryEndpointEnv: endpoint,
		claudeDeliveryKeyEnv:      "sk-ant-e2e-secret",
	}, "serve", "claude-delivery", "sync", "--config", "marketplace.yaml", "--once")
}

type claudePluginsAPIFake struct {
	t *testing.T

	mu              sync.Mutex
	plugin          map[string]any
	createdPlugins  int
	createdVersions int
	writeRequests   int
	preferences     []string
	deletes         int
	requests        int
	server          *httptest.Server
}

func newClaudePluginsAPIFake(t *testing.T) *claudePluginsAPIFake {
	t.Helper()
	fake := &claudePluginsAPIFake{t: t}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *claudePluginsAPIFake) URL() string { return f.server.URL }

func (f *claudePluginsAPIFake) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if r.Method != http.MethodGet {
		f.writeRequests++
	}
	if r.Header.Get("x-api-key") != "sk-ant-e2e-secret" ||
		r.Header.Get("anthropic-version") != "2026-10-21" ||
		r.Header.Get("anthropic-beta") != "ce-plugins-2026-09-01" {
		http.Error(w, `{"error":"unexpected headers"}`, http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/plugin_marketplaces/marketplace_e2e":
		writeClaudeJSON(w, map[string]any{
			"type": "plugin_marketplace", "id": "marketplace_e2e", "name": "E2E",
			"source": "manual", "owner": map[string]any{"type": "organization"},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/plugins":
		data := []map[string]any{}
		if f.plugin != nil {
			data = append(data, f.plugin)
		}
		writeClaudeJSON(w, map[string]any{"data": data, "next_page": nil})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/plugins":
		f.createdPlugins++
		f.plugin = claudePluginRecord("version_1", "not_available", true)
		writeClaudeJSON(w, f.plugin)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/plugins/plugin_e2e/versions":
		f.createdVersions++
		versionID := fmt.Sprintf("version_%d", f.createdVersions+1)
		f.plugin = claudePluginRecord(versionID, "available", false)
		writeClaudeJSON(w, map[string]any{
			"type": "plugin_version", "id": versionID, "plugin_id": "plugin_e2e",
			"content_scan": map[string]any{"status": "completed"},
			"reach":        "organization",
		})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/plugins/plugin_e2e":
		writeClaudeJSON(w, f.plugin)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/plugins/plugin_e2e/installation_settings/organization":
		var setting struct {
			InstallationPreference string `json:"installation_preference"`
		}
		if err := json.NewDecoder(r.Body).Decode(&setting); err != nil {
			http.Error(w, `{"error":"invalid setting"}`, http.StatusBadRequest)
			return
		}
		f.preferences = append(f.preferences, setting.InstallationPreference)
		f.plugin["organization_installation_preference"] = setting.InstallationPreference
		f.plugin["organization_installation_preference_inherited"] = false
		writeClaudeJSON(w, map[string]any{"installation_preference": setting.InstallationPreference})
	case r.Method == http.MethodDelete && r.URL.Path == "/v1/organizations/plugins/plugin_e2e/installation_settings/organization":
		f.deletes++
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, `{"error":"unexpected endpoint"}`, http.StatusNotFound)
	}
}

func claudePluginRecord(versionID, preference string, inherited bool) map[string]any {
	return map[string]any{
		"type": "plugin", "id": "plugin_e2e", "name": "am-serve-source-skill-review",
		"display_name": "Review", "description": "Review code carefully",
		"served_version_id": versionID, "served_version_pinned": false,
		"latest_version_id": versionID, "manifest_version": "",
		"owner":                                map[string]any{"type": "organization"},
		"marketplace_id":                       "marketplace_e2e",
		"created_by":                           map[string]any{"type": "api_actor", "api_key_id": "apikey_e2e"},
		"organization_installation_preference": preference,
		"organization_installation_preference_inherited": inherited,
		"content_scan": map[string]any{"status": "completed", "assessment": "pass", "reason": nil},
		"components":   []map[string]any{{"type": "skill", "name": "review", "description": "Review code carefully"}},
		"reach":        "organization",
	}
}

func writeClaudeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, `{"error":"encode response"}`, http.StatusInternalServerError)
	}
}

type claudePluginsSnapshot struct {
	createdPlugins  int
	createdVersions int
	requests        int
	writeRequests   int
	preferences     []string
	deletes         int
}

func (f *claudePluginsAPIFake) snapshot() claudePluginsSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return claudePluginsSnapshot{
		createdPlugins: f.createdPlugins, createdVersions: f.createdVersions,
		requests: f.requests, writeRequests: f.writeRequests,
		preferences: append([]string(nil), f.preferences...), deletes: f.deletes,
	}
}

func (f *claudePluginsAPIFake) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
