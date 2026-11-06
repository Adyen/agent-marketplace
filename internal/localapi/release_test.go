package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
)

type releaseRoundTripFunc func(*http.Request) (*http.Response, error)

func (f releaseRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type releaseStubTransport struct {
	calls       atomic.Int32
	status      int
	body        string
	lastRequest *http.Request
	respond     func(r *http.Request) (*http.Response, error)
}

func (s *releaseStubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	s.calls.Add(1)
	s.lastRequest = r
	if s.respond != nil {
		return s.respond(r)
	}
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d", status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Request:    r,
	}, nil
}

func releaseBody(tag string) string {
	return fmt.Sprintf(`{"tag_name":%q,"draft":false,"prerelease":false}`, tag)
}

func newReleaseAPI(t *testing.T, version string) (*API, *releaseStubTransport, string, string) {
	t.Helper()
	home, repo := newUserAPIFixture(t, true)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil, WithVersion(version))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	stub := &releaseStubTransport{body: releaseBody("v0.0.1")}
	api.releaseClient.Transport = stub
	return api, stub, home, repo
}

func requestRelease(t *testing.T, api *API, mutate func(*http.Request)) (int, map[string]any) {
	t.Helper()
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/release", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(tokenHeader, api.Credential())
	if mutate != nil {
		mutate(req)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("release response is not JSON: %q", body)
		}
	}
	return resp.StatusCode, decoded
}

func postReleaseNotice(t *testing.T, api *API) string {
	t.Helper()
	status, decoded := requestRelease(t, api, nil)
	if status != http.StatusOK {
		t.Fatalf("release status = %d", status)
	}
	version, _ := decoded["latestVersion"].(string)
	return version
}

func setOrgReleaseCheck(t *testing.T, enabled *bool) string {
	t.Helper()
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	if enabled == nil {
		writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\n")
	} else {
		writeFile(t, orgPath, fmt.Sprintf("apiVersion: agent-marketplace/v4\ngui:\n  version_check: %v\n", *enabled))
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	return orgPath
}

func assertReleaseRequestHeaders(t *testing.T, stub *releaseStubTransport, version string) {
	t.Helper()
	r := stub.lastRequest
	if r == nil {
		t.Fatal("no release request observed")
	}
	if r.Method != http.MethodGet {
		t.Fatalf("release request method = %q, want GET", r.Method)
	}
	if r.URL.String() != "https://api.github.com/repos/Adyen/agent-marketplace/releases/latest" {
		t.Fatalf("release request URL = %q", r.URL.String())
	}
	if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
		t.Fatalf("Accept = %q", got)
	}
	wantUA := "agent-marketplace-gui/" + version
	if got := r.Header.Get("User-Agent"); got != wantUA {
		t.Fatalf("User-Agent = %q, want %q", got, wantUA)
	}
	if got := r.Header.Get("Authorization"); got != "" {
		t.Fatalf("unexpected Authorization header %q", got)
	}
}

// spec: gui-shell/new-stable-release-is-available
func TestReleaseNoticeNewerStableRelease(t *testing.T) {
	api, stub, home, repo := newReleaseAPI(t, "1.2.3")
	stub.body = releaseBody("v1.3.0")
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	userBefore, _ := os.ReadFile(userConfig)
	repoBefore, _ := os.ReadFile(repoConfig)
	localBefore, _ := os.ReadFile(repoLocal)
	orgPath := setOrgReleaseCheck(t, nil)
	orgBefore, _ := os.ReadFile(orgPath)

	if got := postReleaseNotice(t, api); got != "v1.3.0" {
		t.Fatalf("latestVersion = %q, want v1.3.0", got)
	}
	assertReleaseRequestHeaders(t, stub, "1.2.3")
	if got := postReleaseNotice(t, api); got != "v1.3.0" {
		t.Fatalf("cached latestVersion = %q, want v1.3.0", got)
	}
	if calls := stub.calls.Load(); calls != 1 {
		t.Fatalf("release requests = %d, want 1", calls)
	}
	if after, _ := os.ReadFile(userConfig); string(after) != string(userBefore) {
		t.Fatal("user root changed")
	}
	if after, _ := os.ReadFile(repoConfig); string(after) != string(repoBefore) {
		t.Fatal("repo root changed")
	}
	if after, _ := os.ReadFile(repoLocal); string(after) != string(localBefore) {
		t.Fatal("personal-local root changed")
	}
	if after, _ := os.ReadFile(orgPath); string(after) != string(orgBefore) {
		t.Fatal("organization root changed")
	}
}

// spec: gui-shell/new-stable-release-is-available
func TestReleaseNoticeVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		name    string
		running string
		latest  string
		want    string
	}{
		{name: "same", running: "1.3.0", latest: "v1.3.0", want: ""},
		{name: "older latest", running: "1.3.0", latest: "v1.2.9", want: ""},
		{name: "numeric ten beats nine", running: "1.9.0", latest: "v1.10.0", want: "v1.10.0"},
		{name: "build metadata ignored", running: "1.2.0", latest: "v1.3.0+build.7", want: "v1.3.0"},
		{name: "running prerelease vs final stable", running: "1.3.0-rc.1", latest: "v1.3.0", want: "v1.3.0"},
		{name: "tag without v prefix", running: "1.2.3", latest: "1.3.0", want: "v1.3.0"},
		{name: "latest prerelease tag rejected", running: "1.2.0", latest: "v1.4.0-beta.1", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, stub, _, _ := newReleaseAPI(t, tc.running)
			stub.body = releaseBody(tc.latest)
			if got := postReleaseNotice(t, api); got != tc.want {
				t.Fatalf("latestVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

// spec: gui-shell/version-check-fails
func TestReleaseNoticeInvalidRunningVersion(t *testing.T) {
	for _, running := range []string{"dev", "", "not-a-version"} {
		api, stub, _, _ := newReleaseAPI(t, running)
		if got := postReleaseNotice(t, api); got != "" {
			t.Fatalf("running %q latestVersion = %q, want empty", running, got)
		}
		if calls := stub.calls.Load(); calls != 0 {
			t.Fatalf("running %q triggered %d requests", running, calls)
		}
	}
}

// spec: gui-shell/version-check-fails
func TestReleaseNoticeBadResponses(t *testing.T) {
	oversized := fmt.Sprintf(`{"tag_name":"v9.9.9","pad":%q}`, strings.Repeat("x", 1<<20))
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "server error", status: 500, body: releaseBody("v9.9.9")},
		{name: "redirect", status: 302, body: releaseBody("v9.9.9")},
		{name: "invalid json", status: 200, body: `{"tag_name":`},
		{name: "missing tag", status: 200, body: `{"draft":false,"prerelease":false}`},
		{name: "wrong typed tag", status: 200, body: `{"tag_name":42,"draft":false,"prerelease":false}`},
		{name: "missing draft", status: 200, body: `{"tag_name":"v9.9.9","prerelease":false}`},
		{name: "null draft", status: 200, body: `{"tag_name":"v9.9.9","draft":null,"prerelease":false}`},
		{name: "wrong typed draft", status: 200, body: `{"tag_name":"v9.9.9","draft":"no","prerelease":false}`},
		{name: "missing prerelease", status: 200, body: `{"tag_name":"v9.9.9","draft":false}`},
		{name: "null prerelease", status: 200, body: `{"tag_name":"v9.9.9","draft":false,"prerelease":null}`},
		{name: "wrong typed prerelease", status: 200, body: `{"tag_name":"v9.9.9","draft":false,"prerelease":"no"}`},
		{name: "draft", status: 200, body: `{"tag_name":"v9.9.9","draft":true,"prerelease":false}`},
		{name: "prerelease", status: 200, body: `{"tag_name":"v9.9.9","draft":false,"prerelease":true}`},
		{name: "oversized", status: 200, body: oversized},
		{name: "trailing data", status: 200, body: releaseBody("v9.9.9") + ` {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, stub, _, _ := newReleaseAPI(t, "1.0.0")
			stub.status = tc.status
			stub.body = tc.body
			for range 2 {
				if got := postReleaseNotice(t, api); got != "" {
					t.Fatalf("latestVersion = %q, want empty", got)
				}
			}
			if calls := stub.calls.Load(); calls != 1 {
				t.Fatalf("cached failure sent %d release requests, want 1", calls)
			}
		})
	}
}

func TestReleaseNoticeRedirectIsNotFollowed(t *testing.T) {
	api, _, _, _ := newReleaseAPI(t, "1.0.0")
	var calls atomic.Int32
	api.releaseClient.Transport = releaseRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			header := make(http.Header)
			header.Set("Location", "https://api.github.com/redirected")
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    r,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(releaseBody("v9.9.9"))),
			Request:    r,
		}, nil
	})
	if got := postReleaseNotice(t, api); got != "" {
		t.Fatalf("redirected latestVersion = %q, want empty", got)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("redirect was followed with %d requests, want 1", got)
	}
}

// spec: gui-shell/version-check-fails
func TestReleaseNoticeTransportFailures(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		api, stub, _, _ := newReleaseAPI(t, "1.0.0")
		stub.respond = func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial failed")
		}
		for range 2 {
			if got := postReleaseNotice(t, api); got != "" {
				t.Fatalf("latestVersion = %q, want empty", got)
			}
		}
		if calls := stub.calls.Load(); calls != 1 {
			t.Fatalf("cached transport failure sent %d release requests, want 1", calls)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		api, stub, _, _ := newReleaseAPI(t, "1.0.0")
		api.releaseClient.Timeout = 50 * time.Millisecond
		stub.respond = func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		for range 2 {
			if got := postReleaseNotice(t, api); got != "" {
				t.Fatalf("latestVersion = %q, want empty", got)
			}
		}
		if calls := stub.calls.Load(); calls != 1 {
			t.Fatalf("cached timeout sent %d release requests, want 1", calls)
		}
	})
	t.Run("request cancellation", func(t *testing.T) {
		api, stub, _, _ := newReleaseAPI(t, "1.0.0")
		started := make(chan struct{})
		canceled := make(chan struct{})
		stub.respond = func(r *http.Request) (*http.Response, error) {
			close(started)
			<-r.Context().Done()
			close(canceled)
			return nil, r.Context().Err()
		}
		server := httptest.NewServer(api.Handler())
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(func() {
			cancel()
			api.Close()
			server.Close()
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/release", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(tokenHeader, api.Credential())
		done := make(chan struct{}, 1)
		go func() {
			resp, err := server.Client().Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
			done <- struct{}{}
		}()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("release transport did not start")
		}
		cancel()
		select {
		case <-canceled:
		case <-time.After(5 * time.Second):
			t.Fatal("release transport did not observe request cancellation")
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("release request did not finish after cancellation")
		}
		for range 2 {
			if got := postReleaseNotice(t, api); got != "" {
				t.Fatalf("latestVersion after cancellation = %q, want empty", got)
			}
		}
		if calls := stub.calls.Load(); calls != 1 {
			t.Fatalf("cached cancellation sent %d release requests, want 1", calls)
		}
	})
}

func TestReleaseAPICloseCancelsInFlightCheck(t *testing.T) {
	api, stub, _, _ := newReleaseAPI(t, "1.0.0")
	setOrgReleaseCheck(t, nil)
	started := make(chan struct{})
	canceled := make(chan struct{})
	stub.respond = func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		close(canceled)
		return nil, r.Context().Err()
	}
	server := httptest.NewServer(api.Handler())
	ctx, cancelRequest := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancelRequest()
		api.Close()
		server.Close()
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/release", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(tokenHeader, api.Credential())
	type result struct {
		status int
		body   []byte
		err    error
	}
	requestDone := make(chan result, 1)
	go func() {
		resp, err := server.Client().Do(req)
		if err != nil {
			requestDone <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		requestDone <- result{status: resp.StatusCode, body: body, err: err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("release transport did not start")
	}
	closeDone := make(chan struct{})
	go func() {
		api.Close()
		close(closeDone)
	}()
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("API close did not cancel the release transport")
	}
	select {
	case <-closeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("API close did not finish after transport cancellation")
	}
	select {
	case got := <-requestDone:
		if got.err != nil || got.status != http.StatusOK {
			t.Fatalf("release response status=%d error=%v", got.status, got.err)
		}
		var body map[string]string
		if err := json.Unmarshal(got.body, &body); err != nil {
			t.Fatalf("release response is not JSON: %v", err)
		}
		if body["latestVersion"] != "" {
			t.Fatalf("latestVersion = %q, want empty", body["latestVersion"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("release request did not finish after API close")
	}
	if calls := stub.calls.Load(); calls != 1 {
		t.Fatalf("API close sent %d release requests, want 1", calls)
	}
}

// spec: gui-shell/organization-disables-release-checking
// spec: config-layering/organization-setting-controls-release-availability-checks
func TestReleaseNoticeOrganizationPolicy(t *testing.T) {
	api, stub, _, _ := newReleaseAPI(t, "1.0.0")
	stub.body = releaseBody("v2.0.0")
	disabled := false
	setOrgReleaseCheck(t, &disabled)

	if got := postReleaseNotice(t, api); got != "" {
		t.Fatalf("disabled latestVersion = %q, want empty", got)
	}
	if calls := stub.calls.Load(); calls != 0 {
		t.Fatalf("disabled policy sent %d release requests", calls)
	}
	enabled := true
	setOrgReleaseCheck(t, &enabled)
	if got := postReleaseNotice(t, api); got != "v2.0.0" {
		t.Fatalf("enabled latestVersion = %q, want v2.0.0", got)
	}
	if calls := stub.calls.Load(); calls != 1 {
		t.Fatalf("enabled control sent %d release requests, want 1", calls)
	}
}

// spec: gui-shell/organization-disables-release-checking
func TestReleaseNoticeDisabledAfterCachedNotice(t *testing.T) {
	api, stub, _, _ := newReleaseAPI(t, "1.0.0")
	stub.body = releaseBody("v2.0.0")
	if got := postReleaseNotice(t, api); got != "v2.0.0" {
		t.Fatalf("latestVersion = %q, want v2.0.0", got)
	}
	disabled := false
	setOrgReleaseCheck(t, &disabled)
	if got := postReleaseNotice(t, api); got != "" {
		t.Fatalf("disabled latestVersion = %q, want empty", got)
	}
	if calls := stub.calls.Load(); calls != 1 {
		t.Fatalf("disabled check sent %d release requests, want 1", calls)
	}
}

// spec: gui-shell/version-check-fails
func TestReleaseNoticePolicyReadFailure(t *testing.T) {
	api, stub, _, _ := newReleaseAPI(t, "1.0.0")
	stub.body = releaseBody("v2.0.0")
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, "apiVersion: [broken\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	if got := postReleaseNotice(t, api); got != "" {
		t.Fatalf("latestVersion = %q, want empty", got)
	}
	if calls := stub.calls.Load(); calls != 0 {
		t.Fatalf("policy read failure sent %d release requests", calls)
	}
}

func TestReleaseRejectsUnauthorizedAndMalformed(t *testing.T) {
	api, stub, _, _ := newReleaseAPI(t, "1.0.0")
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	send := func(mutate func(*http.Request)) int {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/release", nil)
		if err != nil {
			t.Fatal(err)
		}
		if mutate != nil {
			mutate(req)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}

	if status := send(func(r *http.Request) { r.Header.Set(tokenHeader, "wrong") }); status != http.StatusUnauthorized {
		t.Fatalf("wrong credential status = %d, want 401", status)
	}
	if status := send(nil); status != http.StatusUnauthorized {
		t.Fatalf("missing credential status = %d, want 401", status)
	}
	if status := send(func(r *http.Request) {
		r.Header.Set(tokenHeader, api.Credential())
		r.Header.Set("Origin", "https://evil.example")
	}); status != http.StatusForbidden {
		t.Fatalf("foreign origin status = %d, want 403", status)
	}
	if status := send(func(r *http.Request) {
		r.Header.Set(tokenHeader, api.Credential())
		r.Header.Set("Origin", "wails://other")
	}); status != http.StatusForbidden {
		t.Fatalf("wrong GUI origin status = %d, want 403", status)
	}
	if status := send(func(r *http.Request) {
		r.Header.Set(tokenHeader, api.Credential())
		r.Body = io.NopCloser(strings.NewReader(`{"version":"1"}`))
		r.ContentLength = int64(len(`{"version":"1"}`))
	}); status != http.StatusBadRequest {
		t.Fatalf("body status = %d, want 400", status)
	}
	if status := send(func(r *http.Request) {
		r.Header.Set(tokenHeader, api.Credential())
		r.URL.RawQuery = "version=1"
	}); status != http.StatusBadRequest {
		t.Fatalf("query status = %d, want 400", status)
	}
	if calls := stub.calls.Load(); calls != 0 {
		t.Fatalf("rejected requests triggered %d release requests", calls)
	}
}

func TestReleaseUnsupportedSessionMode(t *testing.T) {
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	stub := &releaseStubTransport{body: releaseBody("v9.9.9")}
	api.releaseClient.Transport = stub

	status, decoded := requestRelease(t, api, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("workspace release status = %d, want 400", status)
	}
	if decoded != nil {
		t.Fatalf("unexpected body %v", decoded)
	}
	if calls := stub.calls.Load(); calls != 0 {
		t.Fatalf("unsupported mode triggered %d release requests", calls)
	}
}

func TestReleaseMethodAndPathRejected(t *testing.T) {
	api, stub, _, _ := newReleaseAPI(t, "1.0.0")
	stub.body = releaseBody("v9.9.9")
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/release", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(tokenHeader, api.Credential())
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", resp.StatusCode)
	}
	if calls := stub.calls.Load(); calls != 0 {
		t.Fatalf("GET triggered %d release requests", calls)
	}
}
