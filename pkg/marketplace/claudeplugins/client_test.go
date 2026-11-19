package claudeplugins

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, server *httptest.Server, key string) *Client {
	t.Helper()
	client, err := NewClient(Options{
		BaseURL:    server.URL,
		APIKey:     NewAPIKey(key),
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestGetPluginUsesRequiredHeadersAndRedactsAPIKey(t *testing.T) {
	const key = "claude-admin-secret-for-test"
	var requestHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestHeaders = r.Header.Clone()
		if r.URL.Path != "/v1/organizations/plugins/plugin_test" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"`+key+`"}}`)
	}))
	defer server.Close()

	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })

	client := testClient(t, server, key)
	_, err := client.GetPlugin(context.Background(), "plugin_test")
	if err == nil {
		t.Fatal("GetPlugin unexpectedly succeeded")
	}
	if requestHeaders.Get("x-api-key") != key ||
		requestHeaders.Get("anthropic-version") != AnthropicVersion ||
		requestHeaders.Get("anthropic-beta") != PluginsBeta {
		t.Fatalf("required headers not sent: %v", requestHeaders)
	}
	formatted := fmt.Sprintf("%v\n%+v\n%#v\n%s\n%#v", err, err, err, NewAPIKey(key), client)
	if strings.Contains(formatted, key) || strings.Contains(logs.String(), key) {
		t.Fatalf("credential escaped through error or log: %q %q", formatted, logs.String())
	}
}

func TestListPluginsFollowsOpaqueCursorThroughEmptyPage(t *testing.T) {
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages = append(pages, r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		switch len(pages) {
		case 1:
			_, _ = io.WriteString(w, `{"data":[{"id":"plugin_a","name":"a"}],"next_page":"cursor-one"}`)
		case 2:
			_, _ = io.WriteString(w, `{"data":[],"next_page":"cursor-two"}`)
		case 3:
			_, _ = io.WriteString(w, `{"data":[{"id":"plugin_b","name":"b"}],"next_page":null}`)
		default:
			http.Error(w, "unexpected page", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := testClient(t, server, "key")
	plugins, err := client.ListPlugins(context.Background(), PluginListQuery{
		OwnerType: "organization",
		Limit:     25,
	})
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(plugins) != 2 || plugins[0].ID != "plugin_a" || plugins[1].ID != "plugin_b" {
		t.Fatalf("plugins = %+v", plugins)
	}
	if len(pages) != 3 || pages[0] != "" || pages[1] != "cursor-one" || pages[2] != "cursor-two" {
		t.Fatalf("page cursors = %#v", pages)
	}
}

func TestAPIErrorTypingPreservesDocumentedFieldsAndOpaqueCodes(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		code        string
		details     string
		retry       string
		retryAfter  string
		wantPlugin  string
		wantVersion string
		wantStatus  int
		wantCode    string
	}{
		{name: "plugin name taken", status: 409, code: "plugin_name_taken", details: `"plugin_id":"plugin_existing"`, wantPlugin: "plugin_existing"},
		{name: "registration pending create", status: 503, code: "registration_pending", details: `"plugin_id":"plugin_pending"`, retry: "false", wantPlugin: "plugin_pending"},
		{name: "registration pending version", status: 503, code: "registration_pending", details: `"plugin_id":"plugin_one","plugin_version_id":"pluginver_pending"`, retry: "true", wantPlugin: "plugin_one", wantVersion: "pluginver_pending"},
		{name: "scan pending", status: 409, code: "scan_pending"},
		{name: "skill name taken", status: 409, code: "skill_name_taken", details: `"skill_name":"existing-skill"`},
		{name: "throttled", status: 429, code: "rate_limited", retryAfter: "7"},
		{name: "unknown code remains opaque", status: 418, code: "future_error_code", wantStatus: 418, wantCode: "future_error_code"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.retry != "" {
					w.Header().Set("x-should-retry", test.retry)
				}
				if test.retryAfter != "" {
					w.Header().Set("retry-after", test.retryAfter)
				}
				w.Header().Set("anthropic-ratelimit-requests-limit", "60")
				w.Header().Set("anthropic-ratelimit-requests-remaining", "58")
				w.Header().Set("anthropic-ratelimit-requests-reset", "2026-11-11T16:00:00Z")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				details := fmt.Sprintf(`"error_code":%q`, test.code)
				if test.details != "" {
					details += "," + test.details
				}
				_, _ = fmt.Fprintf(w, `{"error":{"type":"api_error","message":"safe to discard","details":{%s}}}`, details)
			}))
			defer server.Close()

			ctx := context.Background()
			cancel := func() {}
			if test.retryAfter != "" && test.retryAfter != "0" {
				ctx, cancel = context.WithTimeout(ctx, time.Millisecond)
			}
			defer cancel()
			_, err := testClient(t, server, "key").GetPlugin(ctx, "plugin_test")
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %T %v, want *APIError", err, err)
			}
			wantStatus := test.wantStatus
			if wantStatus == 0 {
				wantStatus = test.status
			}
			wantCode := test.wantCode
			if wantCode == "" {
				wantCode = test.code
			}
			if apiErr.StatusCode != wantStatus || apiErr.Code != wantCode {
				t.Fatalf("typed error = status %d, code %q", apiErr.StatusCode, apiErr.Code)
			}
			if apiErr.RateLimits.RequestsLimit != "60" ||
				apiErr.RateLimits.RequestsRemaining != "58" {
				t.Fatalf("rate-limit headers not surfaced: %+v", apiErr.RateLimits)
			}
			if test.retryAfter != "" && apiErr.RetryAfter != 7*time.Second {
				if test.retryAfter != "0" || apiErr.RetryAfter != 0 {
					t.Fatalf("RetryAfter = %s", apiErr.RetryAfter)
				}
			}
			if test.retry != "" && (apiErr.ShouldRetry == nil || strconv.FormatBool(*apiErr.ShouldRetry) != test.retry) {
				t.Fatalf("ShouldRetry = %v", apiErr.ShouldRetry)
			}
			if apiErr.PluginID != test.wantPlugin || apiErr.PluginVersionID != test.wantVersion {
				t.Fatalf("error details = plugin %q, version %q", apiErr.PluginID, apiErr.PluginVersionID)
			}
			if test.code == "skill_name_taken" && apiErr.SkillName != "existing-skill" {
				t.Fatalf("skill_name = %q", apiErr.SkillName)
			}
			if test.status == http.StatusTooManyRequests {
				var throttled *ThrottledError
				if !errors.As(err, &throttled) {
					t.Fatalf("rate limit error = %T, want *ThrottledError", err)
				}
			}
		})
	}
}

func TestCreatePluginUsesDocumentedMultipartAndRetriesThrottleOnce(t *testing.T) {
	var attempts atomic.Int32
	var acceptedWrites atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/organizations/plugins" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		if attempts.Add(1) == 1 {
			w.Header().Set("retry-after", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"details":{"error_code":"rate_limited"}}}`)
			return
		}
		acceptedWrites.Add(1)

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" {
			t.Errorf("multipart Content-Type = %q, %v", mediaType, err)
			return
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		foundArchive, foundNotes := false, false
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("read multipart: %v", err)
				return
			}
			data, err := io.ReadAll(part)
			if err != nil {
				t.Errorf("read part: %v", err)
			}
			switch part.FormName() {
			case "files[]":
				foundArchive = part.FileName() == "plugin.zip" && string(data) == "zip-bytes"
			case "release_notes":
				foundNotes = string(data) == "publication provenance"
			}
		}
		if !foundArchive || !foundNotes {
			t.Errorf("multipart fields: files[]=%t release_notes=%t", foundArchive, foundNotes)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"plugin_created","name":"example"}`)
	}))
	defer server.Close()

	client := testClient(t, server, "key")
	plugin, err := client.CreatePlugin(context.Background(), []byte("zip-bytes"), "", "publication provenance")
	if err != nil {
		t.Fatalf("CreatePlugin: %v", err)
	}
	if plugin.ID != "plugin_created" || attempts.Load() != 2 || acceptedWrites.Load() != 1 {
		t.Fatalf("plugin=%+v request attempts=%d accepted writes=%d", plugin, attempts.Load(), acceptedWrites.Load())
	}
}

func TestClaudeDeliveryRetryAfterSaturatesDuration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("retry-after", "9223372037")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"error":{"details":{"error_code":"rate_limited"}}}`)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := testClient(t, server, "key").GetPlugin(ctx, "plugin_test")
	var throttle *ThrottledError
	if !errors.As(err, &throttle) {
		t.Fatalf("GetPlugin error = %T %v, want *ThrottledError", err, err)
	}
	if got, want := throttle.RetryAfter, time.Duration(1<<63-1); got != want {
		t.Fatalf("throttle RetryAfter = %s, want saturated %s", got, want)
	}
}

// spec: claude-enterprise-delivery/a-newly-created-plugin-becomes-available-to-members
func TestInstallationSettingWritesForOnePluginAreSerialized(t *testing.T) {
	var active, maximum atomic.Int32
	var first sync.Once
	firstStarted := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/organizations/plugins/plugin_one/installation_settings/organization" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || !strings.Contains(string(body), `"installation_preference":"available"`) {
			t.Errorf("installation setting request body = %q, %v", body, err)
		}
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		first.Do(func() { close(firstStarted) })
		<-release
		active.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"installation_preference":"available","target":{"type":"organization"}}`)
	}))
	defer server.Close()

	client := testClient(t, server, "key")
	var wg sync.WaitGroup
	targets := make(chan Owner, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			setting, err := client.SetInstallationSetting(context.Background(), "plugin_one", "available")
			if err != nil {
				t.Errorf("SetInstallationSetting: %v", err)
				return
			}
			targets <- setting.Target
		}()
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("first installation-setting request did not start")
	}
	time.Sleep(20 * time.Millisecond)
	if got := maximum.Load(); got != 1 {
		close(release)
		t.Fatalf("concurrent installation-setting writes = %d", got)
	}
	close(release)
	wg.Wait()
	close(targets)
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent installation-setting writes = %d", got)
	}
	for target := range targets {
		if target.Type != "organization" {
			t.Fatalf("decoded installation setting target = %+v, want organization", target)
		}
	}
}

func TestListMarketplacesPreservesFiltersAndOpaqueSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organizations/plugin_marketplaces" ||
			r.URL.Query().Get("owner_type") != "organization" ||
			r.URL.Query().Get("source") != "future_source" {
			t.Errorf("marketplace request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"marketplace_1","source":"future_source"}],"next_page":null}`)
	}))
	defer server.Close()
	client := testClient(t, server, "key")
	marketplaces, err := client.ListMarketplaces(context.Background(), MarketplaceListQuery{
		OwnerType: "organization",
		Source:    "future_source",
	})
	if err != nil {
		t.Fatalf("ListMarketplaces: %v", err)
	}
	if len(marketplaces) != 1 || marketplaces[0].Source != "future_source" {
		t.Fatalf("marketplaces = %+v", marketplaces)
	}
}

func TestVersionAndMarketplaceReadsAndRemainingWrites(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/organizations/plugins/plugin_one/versions/pluginver_one":
			_, _ = io.WriteString(w, `{"id":"pluginver_one","plugin_id":"plugin_one"}`)
		case "GET /v1/organizations/plugin_marketplaces/marketplace_one":
			_, _ = io.WriteString(w, `{"id":"marketplace_one","source":"manual","owner":{"type":"organization"}}`)
		case "POST /v1/organizations/plugins/plugin_one/versions":
			_, _ = io.ReadAll(r.Body)
			_, _ = io.WriteString(w, `{"id":"pluginver_two","plugin_id":"plugin_one"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := testClient(t, server, "key")

	version, err := client.GetVersion(context.Background(), "plugin_one", "pluginver_one")
	if err != nil || version.ID != "pluginver_one" {
		t.Fatalf("GetVersion = %+v, %v", version, err)
	}
	marketplace, err := client.GetMarketplace(context.Background(), "marketplace_one")
	if err != nil || marketplace.Source != "manual" || marketplace.Owner.Type != "organization" {
		t.Fatalf("GetMarketplace = %+v, %v", marketplace, err)
	}
	version, err = client.CreateVersion(context.Background(), "plugin_one", []byte("archive"), "release note")
	if err != nil || version.ID != "pluginver_two" {
		t.Fatalf("CreateVersion = %+v, %v", version, err)
	}
	want := []string{
		"GET /v1/organizations/plugins/plugin_one/versions/pluginver_one",
		"GET /v1/organizations/plugin_marketplaces/marketplace_one",
		"POST /v1/organizations/plugins/plugin_one/versions",
	}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("requests = %v, want %v", calls, want)
	}
}

func TestRequestHonorsContextDeadlineWithoutExposingKey(t *testing.T) {
	const key = "deadline-test-admin-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	client, err := NewClient(Options{
		BaseURL:        server.URL,
		APIKey:         NewAPIKey(key),
		HTTPClient:     server.Client(),
		RequestTimeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.GetPlugin(context.Background(), "plugin_timeout")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetPlugin error = %v, want context deadline", err)
	}
	if strings.Contains(fmt.Sprintf("%v %#v", err, err), key) {
		t.Fatalf("deadline error disclosed the API key: %v", err)
	}
}

func TestClientDoesNotForwardAPIKeyAcrossRedirects(t *testing.T) {
	var redirectedRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedRequests.Add(1)
		if r.Header.Get("x-api-key") != "" {
			t.Error("API key was forwarded to a redirected host")
		}
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	_, err := testClient(t, source, "redirect-secret").GetPlugin(context.Background(), "plugin_redirect")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("redirect response error = %v", err)
	}
	if redirectedRequests.Load() != 0 {
		t.Fatalf("redirected server received %d request(s)", redirectedRequests.Load())
	}
}
