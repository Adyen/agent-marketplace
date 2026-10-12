package localapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const fixtureSkillBody = "---\nname: solo\ndescription: Solo skill\n---\n# Solo\n"

func writeFile(t testing.TB, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t testing.TB) (ws pipeline.Workspace, home string) {
	t.Helper()
	ws, home, _ = newFixtureWithSource(t)
	return ws, home
}

func newFixtureWithSource(t testing.TB) (ws pipeline.Workspace, home, source string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("CODEX_HOME", t.TempDir())
	repo := t.TempDir()
	t.Chdir(repo)
	source = t.TempDir()
	writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
	writeFile(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  local: {directory: "+source+"}\nresources:\n  local: {all: true}\n")
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n")
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	workspace, err := pipeline.NewWorkspace(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	return workspace, home, source
}

func newAPI(t *testing.T, lifecycle session.DependencyLifecycle, gate *session.OperationGate) *API {
	t.Helper()
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, lifecycle, gate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	return api
}

func authedRequest(t *testing.T, api *API, path, body string) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, reader)
	req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
	return req
}

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buf
}

func TestCredentialIsRandomPerLaunchAndUnlogged(t *testing.T) {
	logs := captureSlog(t)
	api := newAPI(t, session.DependencyLifecycle{}, nil)
	credential := api.Credential()
	raw, err := hex.DecodeString(credential)
	if err != nil || len(raw) != 32 {
		t.Fatalf("credential is not a 256-bit hex token: len=%d err=%v", len(raw), err)
	}
	other := newAPI(t, session.DependencyLifecycle{}, nil)
	if other.Credential() == credential {
		t.Fatal("two launches produced identical credentials")
	}
	handler := api.Handler()
	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", ""))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/events", "").WithContext(ctx))
	if strings.Contains(logs.String(), credential) {
		t.Fatal("credential appeared in log output")
	}
}

// spec: gui-local-api/requests-without-the-launch-credential-are-rejected
func TestRequestsWithoutCredentialRejected(t *testing.T) {
	var calls atomic.Int32
	var reads atomic.Int32
	lifecycle := session.DependencyLifecycle{
		Sync:   func(context.Context, pipeline.Workspace) ([]error, error) { calls.Add(1); return nil, nil },
		Update: func(context.Context, pipeline.Workspace) ([]error, error) { calls.Add(1); return nil, nil },
	}
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, lifecycle, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	previousRepo := session.LoadRepoConfig
	session.LoadRepoConfig = func() (*config.Config, error) { reads.Add(1); return nil, nil }
	t.Cleanup(func() { session.LoadRepoConfig = previousRepo })
	handler := api.Handler()

	configBefore, err := os.ReadFile(filepath.Join(ws.RepoRoot(), ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	paths := []string{"/api/snapshot", "/api/events", "/api/actions", "/api/nonexistent"}
	for _, path := range paths {
		for name, tokenValues := range map[string][]string{
			"missing":   {},
			"wrong":     {strings.Repeat("00", 32)},
			"duplicate": {api.Credential(), api.Credential()},
		} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"action":"sync"}`))
			for _, value := range tokenValues {
				req.Header.Add("X-Agent-Marketplace-Token", value)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s: status = %d, want 401", name, path, recorder.Code)
			}
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != codeUnauthorized {
				t.Fatalf("%s %s: code = %q, want %q", name, path, body.Code, codeUnauthorized)
			}
			for header := range recorder.Header() {
				if strings.HasPrefix(strings.ToLower(header), "access-control-") {
					t.Fatalf("%s %s: CORS header %q present", name, path, header)
				}
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("rejected requests ran %d lifecycle callbacks", calls.Load())
	}
	if reads.Load() != 0 {
		t.Fatalf("rejected requests triggered %d repo config reads", reads.Load())
	}
	configAfter, err := os.ReadFile(filepath.Join(ws.RepoRoot(), ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(configBefore, configAfter) {
		t.Fatal("rejected requests mutated the committed config")
	}
	if _, err := os.Stat(filepath.Join(ws.RepoRoot(), ".devin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected requests materialized harness output")
	}
}

// spec: gui-local-api/a-credential-from-an-earlier-launch-is-invalid
func TestEarlierLaunchCredentialInvalid(t *testing.T) {
	earlier := newAPI(t, session.DependencyLifecycle{}, nil)
	current := newAPI(t, session.DependencyLifecycle{}, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/snapshot", nil)
	req.Header.Set("X-Agent-Marketplace-Token", earlier.Credential())
	recorder := httptest.NewRecorder()
	current.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != codeUnauthorized {
		t.Fatalf("code = %q, want %q", body.Code, codeUnauthorized)
	}
}

func TestInprocessOriginPolicy(t *testing.T) {
	api := newAPI(t, session.DependencyLifecycle{}, nil)
	handler := api.Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(t, api, "/api/snapshot", ""))
	if recorder.Code == http.StatusForbidden || recorder.Code == http.StatusUnauthorized {
		t.Fatalf("in-process request without Origin rejected: %d", recorder.Code)
	}

	req := authedRequest(t, api, "/api/snapshot", "")
	req.Header.Set("Origin", "wails://wails")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code == http.StatusForbidden || recorder.Code == http.StatusUnauthorized {
		t.Fatalf("GUI origin rejected in-process: %d", recorder.Code)
	}

	req = authedRequest(t, api, "/api/actions", `{"action":"sync"}`)
	req.Header.Set("Origin", "https://evil.example")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin status = %d, want 403", recorder.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != codeForbidden {
		t.Fatalf("code = %q, want %q", body.Code, codeForbidden)
	}
}

// spec: gui-local-api/socket-binds-only-to-loopback-at-an-ephemeral-port
func TestSocketBindsLoopbackEphemeral(t *testing.T) {
	api := newAPI(t, session.DependencyLifecycle{}, nil)
	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(strings.TrimPrefix(loopback.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatalf("listener host %q is not loopback", host)
	}
	if port == "0" {
		t.Fatal("listener did not select an ephemeral port")
	}
	if err := loopback.Close(); err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(loopback.URL, "http://"), 500*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("listener still reachable after Close")
	}
}

// spec: gui-local-api/non-loopback-host-or-foreign-origin-is-rejected
func TestSocketRejectsForeignHostAndOrigin(t *testing.T) {
	var calls atomic.Int32
	var reads atomic.Int32
	lifecycle := session.DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) { calls.Add(1); return nil, nil },
	}
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, lifecycle, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	previousRepo := session.LoadRepoConfig
	session.LoadRepoConfig = func() (*config.Config, error) { reads.Add(1); return nil, nil }
	t.Cleanup(func() { session.LoadRepoConfig = previousRepo })
	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loopback.Close() }()
	origin := loopback.URL

	configBefore, err := os.ReadFile(filepath.Join(ws.RepoRoot(), ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	post := func(host, token, originHeader string, extraOrigin []string) (int, string, http.Header) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, loopback.URL+"/api/actions", strings.NewReader(`{"action":"sync"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		if token != "" {
			req.Header.Set("X-Agent-Marketplace-Token", token)
		}
		if originHeader != "" {
			req.Header.Set("Origin", originHeader)
		}
		for _, extra := range extraOrigin {
			req.Header.Add("Origin", extra)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &body)
		return resp.StatusCode, body.Code, resp.Header
	}

	address := strings.TrimPrefix(loopback.URL, "http://")
	for name, tc := range map[string]struct {
		host        string
		token       string
		origin      string
		extraOrigin []string
		status      int
		code        string
	}{
		"foreign origin":  {address, api.Credential(), "https://evil.example", nil, http.StatusForbidden, codeForbidden},
		"absent origin":   {address, api.Credential(), "", nil, http.StatusForbidden, codeForbidden},
		"null origin":     {address, api.Credential(), "null", nil, http.StatusForbidden, codeForbidden},
		"double origin":   {address, api.Credential(), origin, []string{origin}, http.StatusForbidden, codeForbidden},
		"foreign host":    {"example.com", api.Credential(), origin, nil, http.StatusForbidden, codeForbidden},
		"localhost alias": {"localhost" + address[strings.LastIndex(address, ":"):], api.Credential(), origin, nil, http.StatusForbidden, codeForbidden},
		"bad credential":  {address, strings.Repeat("11", 32), origin, nil, http.StatusUnauthorized, codeUnauthorized},
		"no credential":   {address, "", origin, nil, http.StatusUnauthorized, codeUnauthorized},
	} {
		status, code, headers := post(tc.host, tc.token, tc.origin, tc.extraOrigin)
		if status != tc.status || code != tc.code {
			t.Fatalf("%s: status=%d code=%q, want %d %q", name, status, code, tc.status, tc.code)
		}
		for header := range headers {
			if strings.HasPrefix(strings.ToLower(header), "access-control-") {
				t.Fatalf("%s: CORS header %q present", name, header)
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("rejected requests ran %d lifecycle callbacks", calls.Load())
	}
	if reads.Load() != 0 {
		t.Fatalf("rejected requests triggered %d repo config reads", reads.Load())
	}
	configAfter, err := os.ReadFile(filepath.Join(ws.RepoRoot(), ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(configBefore, configAfter) {
		t.Fatal("rejected requests mutated the committed config")
	}
}

type blockingLifecycle struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingLifecycle(t testing.TB) *blockingLifecycle {
	l := &blockingLifecycle{entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(l.unblock)
	return l
}

func (l *blockingLifecycle) unblock() { l.once.Do(func() { close(l.release) }) }

func (l *blockingLifecycle) lifecycle() session.DependencyLifecycle {
	return session.DependencyLifecycle{
		Sync: func(ctx context.Context, _ pipeline.Workspace) ([]error, error) {
			close(l.entered)
			pipeline.ObservePhase(ctx, pipeline.PhaseResolve)
			pipeline.ObservePhase(ctx, pipeline.PhaseMaterialize)
			<-l.release
			return nil, nil
		},
	}
}

type sseEvent struct {
	Type       string           `json:"type"`
	Sequence   uint64           `json:"sequence"`
	Generation uint64           `json:"generation"`
	Operation  *json.RawMessage `json:"operation"`
	Payload    json.RawMessage  `json:"payload"`
}

func readSSEEvent(reader *bufio.Reader) (sseEvent, error) {
	var data strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return sseEvent{}, err
		}
		if line == "\n" || line == "\r\n" {
			if data.Len() == 0 {
				continue
			}
			var event sseEvent
			if err := json.Unmarshal([]byte(data.String()), &event); err != nil {
				return sseEvent{}, err
			}
			return event, nil
		}
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
		}
	}
}

func readSSE(t *testing.T, reader *bufio.Reader) sseEvent {
	t.Helper()
	type result struct {
		event sseEvent
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		event, err := readSSEEvent(reader)
		ch <- result{event, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.event
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for SSE event")
		return sseEvent{}
	}
}

func subscribeEvents(t *testing.T, api *API, url string) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
	req.Header.Set("Origin", url)
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Body.Close() })
	if ct := stream.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("events Content-Type = %q", ct)
	}
	return stream, bufio.NewReader(stream.Body)
}

func postJSON(t *testing.T, url, token, path, body string) *http.Response {
	t.Helper()
	r, err := http.NewRequest(http.MethodPost, url+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-Agent-Marketplace-Token", token)
	r.Header.Set("Origin", url)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// spec: gui-local-api/event-identity-preserves-order-and-generation
func TestSnapshotStagesThenProgressThenCompletion(t *testing.T) {
	blocked := newBlockingLifecycle(t)
	api := newAPI(t, blocked.lifecycle(), nil)
	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loopback.Close() }()

	_, reader := subscribeEvents(t, api, loopback.URL)

	resp := postJSON(t, loopback.URL, api.Credential(), "/api/snapshot", "")
	_ = resp.Body.Close()

	var lastSequence uint64
	stages := []string{}
	var generation uint64
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		event := readSSE(t, reader)
		if event.Sequence <= lastSequence {
			t.Fatalf("out-of-order stream sequence %d after %d", event.Sequence, lastSequence)
		}
		lastSequence = event.Sequence
		if event.Type != "snapshot" {
			continue
		}
		var payload struct {
			Stage string `json:"stage"`
			Rows  []struct {
				Name                string `json:"name"`
				PendingVerification bool   `json:"pendingVerification"`
			} `json:"rows"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if event.Generation == 0 {
			t.Fatal("snapshot event carried no generation identity")
		}
		if generation != 0 && event.Generation != generation {
			t.Fatalf("staged snapshots carried differing generations %d and %d", generation, event.Generation)
		}
		generation = event.Generation
		stages = append(stages, payload.Stage)
		if payload.Stage == "provisional" || payload.Stage == "config" {
			if len(payload.Rows) != 1 || payload.Rows[0].Name != "solo" || !payload.Rows[0].PendingVerification {
				t.Fatalf("%s stage rows = %+v, want solo pending", payload.Stage, payload.Rows)
			}
		}
		if payload.Stage == "final" {
			found := false
			for _, row := range payload.Rows {
				if row.Name == "solo" {
					found = true
				}
			}
			if !found {
				t.Fatalf("final snapshot lacks the fixture row: %v", payload.Rows)
			}
			break
		}
	}
	want := []string{"provisional", "config", "inventory", "final"}
	if len(stages) != len(want) {
		t.Fatalf("staged sequence = %v, want %v", stages, want)
	}
	for i := range want {
		if stages[i] != want[i] {
			t.Fatalf("staged sequence = %v, want %v", stages, want)
		}
	}

	resp = postJSON(t, loopback.URL, api.Credential(), "/api/actions", `{"action":"sync"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("action status = %d, want 202", resp.StatusCode)
	}
	var accepted struct {
		Operation struct {
			ID   uint64 `json:"id"`
			Kind string `json:"kind"`
		} `json:"operation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}
	<-blocked.entered

	var sawProgress, sawNotification, sawActiveSnapshot bool
	var progressOps []uint64
	for !sawNotification {
		event := readSSE(t, reader)
		if event.Sequence <= lastSequence {
			t.Fatalf("out-of-order stream sequence %d after %d", event.Sequence, lastSequence)
		}
		lastSequence = event.Sequence
		switch event.Type {
		case "snapshot":
			if event.Operation == nil {
				continue
			}
			var ref struct {
				ID uint64 `json:"id"`
			}
			if err := json.Unmarshal(*event.Operation, &ref); err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Operation *struct {
					ID uint64 `json:"id"`
				} `json:"operation"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if ref.ID != accepted.Operation.ID || payload.Operation == nil || payload.Operation.ID != accepted.Operation.ID {
				t.Fatalf("active snapshot carried operation %v/%v, want %d", ref.ID, payload.Operation, accepted.Operation.ID)
			}
			sawActiveSnapshot = true
		case "progress":
			var payload struct {
				ProgressSequence uint64 `json:"progressSequence"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if event.Operation == nil {
				t.Fatal("progress envelope lacks operation identity")
			}
			var ref struct {
				ID uint64 `json:"id"`
			}
			if err := json.Unmarshal(*event.Operation, &ref); err != nil {
				t.Fatal(err)
			}
			progressOps = append(progressOps, ref.ID)
			sawProgress = true
			blocked.unblock()
		case "notification":
			sawNotification = true
		}
	}
	if !sawProgress {
		t.Fatal("no progress event observed during the blocked lifecycle")
	}
	if !sawActiveSnapshot {
		t.Fatal("no snapshot announced the active operation")
	}
	for _, id := range progressOps {
		if id != accepted.Operation.ID {
			t.Fatalf("progress carried operation %d, want %d", id, accepted.Operation.ID)
		}
	}

}

// spec: gui-local-api/event-identity-preserves-order-and-generation
func TestStaleSnapshotDeliveryCannotOverwriteNewer(t *testing.T) {
	api := newAPI(t, session.DependencyLifecycle{}, nil)
	handler := api.Handler()

	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", ""))
	api.mu.Lock()
	current := api.current
	currentG := api.currentG
	api.mu.Unlock()
	if current == nil || currentG == 0 {
		t.Fatal("initial snapshot did not publish state")
	}

	var stale session.Snapshot
	stale.Generation = currentG - 1
	if delivered := api.deliverSnapshot(stale.Generation, stale); delivered != nil {
		t.Fatal("superseded generation overwrote current state")
	}
	api.mu.Lock()
	if api.current != current || api.currentG != currentG {
		api.mu.Unlock()
		t.Fatal("stale delivery replaced current state")
	}
	api.mu.Unlock()

	var foreign session.Snapshot
	if delivered := api.deliverSnapshot(currentG, foreign); delivered != nil {
		t.Fatal("foreign snapshot overwrote current state")
	}
}

// spec: gui-local-api/event-identity-preserves-order-and-generation
func TestSnapshotReturnsCurrentWhileOperationHoldsGate(t *testing.T) {
	blocked := newBlockingLifecycle(t)
	api := newAPI(t, blocked.lifecycle(), nil)
	handler := api.Handler()

	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", ""))
	api.mu.Lock()
	baseG := api.currentG
	api.mu.Unlock()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(t, api, "/api/actions", `{"action":"sync"}`))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("sync status = %d, want 202; body %s", recorder.Code, recorder.Body.String())
	}
	<-blocked.entered

	for i := 0; i < 50; i++ {
		api.mu.Lock()
		current := api.current
		api.mu.Unlock()
		if current != nil && current.Operation != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	api.mu.Lock()
	heldG := api.currentG
	opActive := api.current != nil && api.current.Operation != nil
	api.mu.Unlock()
	if !opActive {
		t.Fatal("active operation was not announced in the current snapshot")
	}
	if heldG < baseG {
		t.Fatalf("generation rolled back: %d < %d", heldG, baseG)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(t, api, "/api/snapshot", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("busy-read status = %d, want 200", recorder.Code)
	}
	var dto snapshotDTO
	if err := json.Unmarshal(recorder.Body.Bytes(), &dto); err != nil {
		t.Fatal(err)
	}
	if dto.Operation == nil {
		t.Fatal("busy-read snapshot lost the active operation")
	}

	blocked.unblock()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		done := api.current != nil && api.current.Operation == nil && api.currentG >= heldG
		g := api.currentG
		api.mu.Unlock()
		if done {
			if g < heldG {
				t.Fatalf("final generation %d rolled back below %d", g, heldG)
			}
			return
		}
		if _, _, active := api.gate.Active(); !active {
			api.mu.Lock()
			g := api.currentG
			api.mu.Unlock()
			if g < heldG {
				t.Fatalf("final generation %d rolled back below %d", g, heldG)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("operation did not publish its final snapshot")
}

// spec: gui-local-api/concurrent-mutation-is-rejected-as-busy
func TestBusySnapshotReportsSharedGateWithoutMutatingCache(t *testing.T) {
	for _, kind := range []session.OperationKind{session.OperationSync, session.OperationMutation} {
		t.Run(string(kind), func(t *testing.T) {
			gate := session.NewOperationGate()
			api := newAPI(t, session.DependencyLifecycle{}, gate)
			handler := api.Handler()

			seed := httptest.NewRecorder()
			handler.ServeHTTP(seed, authedRequest(t, api, "/api/snapshot", ""))
			if seed.Code != http.StatusOK {
				t.Fatalf("initial snapshot status = %d, want 200", seed.Code)
			}

			api.mu.Lock()
			cached := api.current
			if cached == nil {
				api.mu.Unlock()
				t.Fatal("initial snapshot was not cached")
			}
			if cached.Operation != nil {
				api.mu.Unlock()
				t.Fatalf("initial operation = %+v, want nil", cached.Operation)
			}
			cachedJSON, err := json.Marshal(cached)
			if err != nil {
				api.mu.Unlock()
				t.Fatal(err)
			}
			cachedGeneration, cachedSequence := api.currentG, api.sequence
			api.mu.Unlock()

			id, ok := gate.Begin(kind)
			if !ok {
				t.Fatal("could not hold shared operation gate")
			}
			defer gate.Finish(id)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, authedRequest(t, api, "/api/snapshot", ""))
			if recorder.Code != http.StatusOK {
				t.Fatalf("busy snapshot status = %d, want 200", recorder.Code)
			}
			var got snapshotDTO
			if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Operation == nil || got.Operation.ID != uint64(id) || got.Operation.Kind != string(kind) {
				t.Fatalf("busy snapshot operation = %+v, want id %d kind %q", got.Operation, id, kind)
			}

			api.mu.Lock()
			afterJSON, err := json.Marshal(api.current)
			unchanged := api.current == cached && api.currentG == cachedGeneration && api.sequence == cachedSequence && bytes.Equal(cachedJSON, afterJSON)
			api.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if !unchanged {
				t.Fatal("busy snapshot changed cached DTO, generation, or event sequence")
			}
		})
	}
}

// spec: gui-local-api/concurrent-mutation-is-rejected-as-busy
func TestConcurrentMutationRejectedAsBusy(t *testing.T) {
	blocked := newBlockingLifecycle(t)
	ws, home := newFixture(t)
	gate := session.NewOperationGate()
	api, err := New(context.Background(), ws, home, blocked.lifecycle(), gate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	handler := api.Handler()

	held, ok := gate.Begin(session.OperationMutation)
	if !ok {
		t.Fatal("could not hold gate")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(t, api, "/api/actions", `{"action":"sync"}`))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("busy status = %d, want 409", recorder.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "localapi.busy" {
		t.Fatalf("busy code = %q", body.Code)
	}
	gate.Finish(held)

	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", ""))
	configBefore, err := os.ReadFile(filepath.Join(ws.RepoRoot(), ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(t, api, "/api/actions", `{"action":"sync"}`))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("accepted status = %d, want 202; body %s", recorder.Code, recorder.Body.String())
	}
	<-blocked.entered

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, authedRequest(t, api, "/api/actions", `{"action":"update"}`))
	if second.Code != http.StatusConflict {
		t.Fatalf("concurrent action status = %d, want 409", second.Code)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "localapi.busy" {
		t.Fatalf("concurrent code = %q", body.Code)
	}
	blocked.unblock()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, active := api.gate.Active(); !active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	configAfter, err := os.ReadFile(filepath.Join(ws.RepoRoot(), ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(configBefore, configAfter) {
		t.Fatal("busy-rejected action mutated the committed config")
	}
}

func TestActionValidationBeforeGuard(t *testing.T) {
	var calls atomic.Int32
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, session.DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) { calls.Add(1); return nil, nil },
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	handler := api.Handler()

	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", ""))
	configBefore, err := os.ReadFile(filepath.Join(ws.RepoRoot(), ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	for name, body := range map[string]string{
		"unknown action":       `{"action":"install"}`,
		"unknown field":        `{"action":"sync","extra":1}`,
		"trailing json":        `{"action":"sync"} {}`,
		"trailing scalar":      `{"action":"sync"} 1`,
		"not json":             `oops`,
		"empty object":         `{}`,
		"duplicate sync":       `{"action":"sync","action":"sync"}`,
		"duplicate upgraded":   `{"action":"update","action":"sync"}`,
		"duplicate downgraded": `{"action":"sync","action":"update"}`,
		"alias then action":    `{"Action":"update","action":"sync"}`,
		"action then alias":    `{"action":"sync","Action":"update"}`,
		"singleton alias":      `{"Action":"sync"}`,
		"non-string action":    `{"action":1}`,
		"array":                `["sync"]`,
		"unterminated":         `{"action":"sync"`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/actions", strings.NewReader(body))
		req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", name, recorder.Code)
		}
		var parsed struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.Code != codeInvalidRequest {
			t.Fatalf("%s: code = %q, want %q", name, parsed.Code, codeInvalidRequest)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/actions", nil)
	req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", recorder.Code)
	}
	if allow := recorder.Header().Get("Allow"); allow != http.MethodPost {
		t.Fatalf("GET Allow = %q, want %q", allow, http.MethodPost)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid requests ran %d lifecycle callbacks", calls.Load())
	}
	configAfter, err := os.ReadFile(filepath.Join(ws.RepoRoot(), ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(configBefore, configAfter) {
		t.Fatal("invalid requests mutated the committed config")
	}
	if _, err := os.Stat(filepath.Join(ws.RepoRoot(), ".devin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid requests materialized harness output")
	}
}

type inprocessWriter struct {
	mu     sync.Mutex
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *inprocessWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *inprocessWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(b)
}

func (w *inprocessWriter) WriteHeader(status int) { w.status = status }

func (w *inprocessWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func TestInprocessEventsWithoutFlusher(t *testing.T) {
	api := newAPI(t, session.DependencyLifecycle{}, nil)
	handler := api.Handler()

	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", ""))

	writer := &inprocessWriter{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(writer, authedRequest(t, api, "/api/events", "").WithContext(ctx))
	}()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(writer.String(), "data:") {
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("no event written to a writer lacking http.Flusher")
}

func TestEventsEndOnAPIClose(t *testing.T) {
	api := newAPI(t, session.DependencyLifecycle{}, nil)
	handler := api.Handler()

	writer := &inprocessWriter{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(writer, authedRequest(t, api, "/api/events", ""))
	}()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(writer.String(), "data:") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	api.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("event stream did not end on API close")
	}
}

func TestConstructorRejectsInvalidWorkspaceBeforeIO(t *testing.T) {
	if _, err := New(context.Background(), pipeline.Workspace{}, t.TempDir(), session.DependencyLifecycle{}, nil); !errors.Is(err, pipeline.ErrInvalidWorkspace) {
		t.Fatalf("error = %v, want ErrInvalidWorkspace", err)
	}
}

// spec: gui-local-api/event-identity-preserves-order-and-generation

func TestSyncActionMaterializesLocalSkill(t *testing.T) {
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	handler := api.Handler()

	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", ""))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(t, api, "/api/actions", `{"action":"sync"}`))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("sync status = %d, want 202; body %s", recorder.Code, recorder.Body.String())
	}
	deadline := time.Now().Add(120 * time.Second)
	target := filepath.Join(ws.RepoRoot(), ".devin", "skills", "solo", "SKILL.md")
	for time.Now().Before(deadline) {
		if _, _, active := api.gate.Active(); !active {
			data, err := os.ReadFile(target)
			if err == nil && string(data) == fixtureSkillBody {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("materialized skill %q not found after sync", target)
}

// spec: gui-local-api/event-identity-preserves-order-and-generation
func TestFailedOperationPublishesFailureNotification(t *testing.T) {
	lifecycle := session.DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
			return nil, errors.New("injected sync failure")
		},
	}
	api := newAPI(t, lifecycle, nil)
	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loopback.Close() }()

	_, reader := subscribeEvents(t, api, loopback.URL)
	resp := postJSON(t, loopback.URL, api.Credential(), "/api/snapshot", "")
	_ = resp.Body.Close()
	readSSE(t, reader)

	resp = postJSON(t, loopback.URL, api.Credential(), "/api/actions", `{"action":"sync"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("sync status = %d, want 202", resp.StatusCode)
	}
	deadline := time.Now().Add(30 * time.Second)
	var finalSnapshot, notification *sseEvent
	for time.Now().Before(deadline) && notification == nil {
		event := readSSE(t, reader)
		if event.Type == "snapshot" {
			copy := event
			finalSnapshot = &copy
		}
		if event.Type == "notification" {
			copy := event
			notification = &copy
		}
	}
	if notification == nil {
		t.Fatal("no completion notification for the failed operation")
	}
	var payload struct {
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal(notification.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Severity != "error" {
		t.Fatalf("failed sync announced severity %q", payload.Severity)
	}
	if finalSnapshot == nil || finalSnapshot.Operation != nil {
		t.Fatal("final snapshot did not clear the operation")
	}
	if _, _, active := api.gate.Active(); active {
		t.Fatal("gate still held after the failed operation finished")
	}
}

func TestMutationRejectsUnreadySnapshot(t *testing.T) {
	api := newAPI(t, session.DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) { return nil, nil },
	}, nil)
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, authedRequest(t, api, "/api/actions", `{"action":"sync"}`))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d before verified inventory, want 400", recorder.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != codeNotReady {
		t.Fatalf("code = %q, want %q", body.Code, codeNotReady)
	}
}

// spec: gui-local-api/event-identity-preserves-order-and-generation
func TestRefreshPublishesSingleFinalSnapshotUnderNewerGeneration(t *testing.T) {
	ws, home, source := newFixtureWithSource(t)
	api, err := New(context.Background(), ws, home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loopback.Close() }()

	_, reader := subscribeEvents(t, api, loopback.URL)
	resp := postJSON(t, loopback.URL, api.Credential(), "/api/snapshot", "")
	var first snapshotDTO
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if len(first.Rows) != 1 || first.Rows[0].Name != "solo" {
		t.Fatalf("initial rows = %+v, want solo only", first.Rows)
	}
	var firstGeneration uint64
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && firstGeneration == 0 {
		event := readSSE(t, reader)
		if event.Type != "snapshot" {
			continue
		}
		var payload struct {
			Stage string `json:"stage"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Stage == "final" {
			firstGeneration = event.Generation
		}
	}
	if firstGeneration == 0 {
		t.Fatal("no final snapshot event for the initial read")
	}

	writeFile(t, filepath.Join(source, "second", "SKILL.md"), "---\nname: second\ndescription: Second skill\n---\n# Second\n")

	resp = postJSON(t, loopback.URL, api.Credential(), "/api/snapshot", "")
	var refreshed snapshotDTO
	if err := json.NewDecoder(resp.Body).Decode(&refreshed); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	names := map[string]bool{}
	for _, row := range refreshed.Rows {
		names[row.Name] = true
	}
	if !names["solo"] || !names["second"] {
		t.Fatalf("refreshed rows = %+v, want solo and second", refreshed.Rows)
	}
	if refreshed.Stage != "final" {
		t.Fatalf("refresh returned stage %q, want final", refreshed.Stage)
	}

	event := readSSE(t, reader)
	if event.Type != "snapshot" {
		t.Fatalf("refresh emitted %q before its snapshot", event.Type)
	}
	var payload struct {
		Stage string `json:"stage"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Stage != "final" {
		t.Fatalf("refresh emitted staged snapshot %q, want final only", payload.Stage)
	}
	if event.Generation <= firstGeneration {
		t.Fatalf("refresh generation %d did not advance past %d", event.Generation, firstGeneration)
	}
	drain := time.Now().Add(2 * time.Second)
	for time.Now().Before(drain) {
		event, ok := readSSEOptional(reader, 500*time.Millisecond)
		if !ok {
			break
		}
		if event.Type == "snapshot" {
			t.Fatalf("refresh emitted a second snapshot event: %+v", event.Payload)
		}
	}
}

func readSSEOptional(reader *bufio.Reader, timeout time.Duration) (sseEvent, bool) {
	type result struct {
		event sseEvent
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		event, err := readSSEEvent(reader)
		ch <- result{event, err}
	}()
	select {
	case r := <-ch:
		return r.event, r.err == nil
	case <-time.After(timeout):
		return sseEvent{}, false
	}
}

// spec: gui-local-api/event-identity-preserves-order-and-generation
func TestRowIDsBoundToRegistryIdentityAndStable(t *testing.T) {
	row := session.Row{
		Artifact: resolver.ResolvedArtifact{
			Entry: catalog.Entry{Kind: ir.KindSkill, Name: "twin", Description: "Twin skill"},
		},
		RegistryURL:    "https://example.com/first.git",
		RegistryName:   "first",
		RegistryRef:    "main",
		EnabledProject: true,
	}
	first := rowPresentation(row)
	second := row
	second.RegistryURL = "https://example.com/second.git"
	second.RegistryName = "second"
	secondDTO := rowPresentation(second)
	if first.ID == secondDTO.ID {
		t.Fatalf("same-name rows from distinct registries share ID %q", first.ID)
	}
	for _, dto := range []rowDTO{first, secondDTO} {
		parts := strings.SplitN(dto.ID, "#", 2)
		if len(parts) != 2 || len(parts[1]) != 64 {
			t.Fatalf("row ID %q does not carry a full SHA256 identity suffix", dto.ID)
		}
	}
	if again := rowPresentation(row); again.ID != first.ID {
		t.Fatalf("row ID unstable: %q vs %q", first.ID, again.ID)
	}
	scoped := row
	scoped.EnabledProject = false
	scoped.EnabledGlobal = true
	if other := rowPresentation(scoped); other.ID == first.ID {
		t.Fatalf("row ID %q ignores scope identity", first.ID)
	}
}

func TestSnapshotStatusGroupPresentation(t *testing.T) {
	api := &API{}
	rows := []session.Row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "required"}}},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "enabled"}}, EnabledProject: true},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "pending"}}, DriftItem: &materialize.DriftItem{Reason: materialize.DriftMissing}},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "orphaned"}}, DriftItem: &materialize.DriftItem{Reason: materialize.DriftOrphaned}},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "broken"}}, UnavailableReason: "catalog unavailable"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "available"}}, IsAvailable: true},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "malformed"}}, IsMalformed: true},
	}
	rows[0].Artifact.Required = true

	wantGroups := []struct {
		id    string
		title string
	}{
		{"required", "Required"},
		{"enabled", "Enabled"},
		{"pending", "Pending sync"},
		{"stale", "Stale"},
		{"broken", "Broken"},
		{"available", "Available"},
		{"malformed", "Malformed"},
	}
	type rowExpectation struct {
		name  string
		group string
	}
	wantRows := []rowExpectation{
		{"required", "required"},
		{"enabled", "enabled"},
		{"pending", "pending"},
		{"orphaned", "broken"},
		{"broken", "broken"},
		{"available", "available"},
		{"malformed", "malformed"},
	}
	check := func(snapshot session.Snapshot, expectedRows []rowExpectation) {
		t.Helper()
		body, err := json.Marshal(api.snapshotPresentationLocked(snapshot, nil))
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Groups []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"groups"`
			Rows []struct {
				Name  string `json:"name"`
				Group string `json:"group"`
			} `json:"rows"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Groups) != len(wantGroups) {
			t.Fatalf("groups = %v, want all %d status groups", got.Groups, len(wantGroups))
		}
		for i, want := range wantGroups {
			if got.Groups[i].ID != want.id || got.Groups[i].Title != want.title {
				t.Fatalf("groups[%d] = %+v, want id=%q title=%q", i, got.Groups[i], want.id, want.title)
			}
		}
		if len(got.Rows) != len(expectedRows) {
			t.Fatalf("rows = %v, want %d rows", got.Rows, len(expectedRows))
		}
		for i, want := range expectedRows {
			if got.Rows[i].Name != want.name || got.Rows[i].Group != want.group {
				t.Fatalf("rows[%d] = %+v, want name=%q group=%q", i, got.Rows[i], want.name, want.group)
			}
		}
	}

	check(session.Snapshot{Stage: session.SnapshotStageFinal, DriftAssessment: session.DriftReady, Rows: rows}, wantRows)
	check(session.Snapshot{Stage: session.SnapshotStageFinal, DriftAssessment: session.DriftFailed}, nil)
}

// spec: gui-local-api/event-identity-preserves-order-and-generation
func TestSyncWarningsRetainedAcrossRefresh(t *testing.T) {
	lifecycle := session.DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
			return []error{errors.New("partial mirror degraded")}, nil
		},
	}
	api := newAPI(t, lifecycle, nil)
	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loopback.Close() }()

	resp := postJSON(t, loopback.URL, api.Credential(), "/api/snapshot", "")
	_ = resp.Body.Close()
	_, reader := subscribeEvents(t, api, loopback.URL)
	readSSE(t, reader)

	resp = postJSON(t, loopback.URL, api.Credential(), "/api/actions", `{"action":"sync"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("sync status = %d, want 202; body %s", resp.StatusCode, resp.Body)
	}
	var notification *sseEvent
	var finalSnapshot *sseEvent
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && notification == nil {
		event := readSSE(t, reader)
		switch event.Type {
		case "snapshot":
			copy := event
			finalSnapshot = &copy
		case "notification":
			copy := event
			notification = &copy
		}
	}
	if notification == nil {
		t.Fatal("no completion notification for the warnings-only sync")
	}
	var note struct {
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal(notification.Payload, &note); err != nil {
		t.Fatal(err)
	}
	if note.Severity != "warning" {
		t.Fatalf("warnings-only sync announced severity %q", note.Severity)
	}
	if finalSnapshot == nil {
		t.Fatal("no final snapshot after the warnings-only sync")
	}
	var body struct {
		Diagnostics []diagnosticDTO `json:"diagnostics"`
	}
	if err := json.Unmarshal(finalSnapshot.Payload, &body); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range body.Diagnostics {
		if d.Severity == "warning" && d.Category == string(session.DiagnosticSync) {
			found = true
		}
	}
	if !found {
		t.Fatalf("final snapshot lacks the sync warning diagnostic: %+v", body.Diagnostics)
	}

	resp = postJSON(t, loopback.URL, api.Credential(), "/api/snapshot", "")
	var refreshed snapshotDTO
	if err := json.NewDecoder(resp.Body).Decode(&refreshed); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	found = false
	for _, d := range refreshed.Diagnostics {
		if d.Severity == "warning" && d.Category == string(session.DiagnosticSync) {
			found = true
		}
	}
	if !found {
		t.Fatalf("ordinary refresh dropped operation diagnostics: %+v", refreshed.Diagnostics)
	}
}

// spec: gui-local-api/event-identity-preserves-order-and-generation
func TestReloadFailureMarksSyncFailedAndRetainsDiagnostics(t *testing.T) {
	ws, home := newFixture(t)
	configPath := filepath.Join(ws.RepoRoot(), ".agents", "config.yaml")
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(configPath, original, 0o644) })
	lifecycle := session.DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
			if err := os.WriteFile(configPath, []byte("apiVersion: [\n"), 0o644); err != nil {
				return nil, err
			}
			return nil, nil
		},
	}
	api, err := New(context.Background(), ws, home, lifecycle, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loopback.Close() }()

	resp := postJSON(t, loopback.URL, api.Credential(), "/api/snapshot", "")
	_ = resp.Body.Close()
	_, reader := subscribeEvents(t, api, loopback.URL)
	readSSE(t, reader)

	resp = postJSON(t, loopback.URL, api.Credential(), "/api/actions", `{"action":"sync"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("sync status = %d, want 202; body %s", resp.StatusCode, resp.Body)
	}
	var notification *sseEvent
	var finalSnapshot *sseEvent
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && notification == nil {
		event := readSSE(t, reader)
		switch event.Type {
		case "snapshot":
			copy := event
			finalSnapshot = &copy
		case "notification":
			copy := event
			notification = &copy
		}
	}
	if notification == nil {
		t.Fatal("no completion notification for the reload-failed sync")
	}
	var note struct {
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal(notification.Payload, &note); err != nil {
		t.Fatal(err)
	}
	if note.Severity != "error" {
		t.Fatalf("reload-failed sync announced severity %q, want error", note.Severity)
	}
	if finalSnapshot == nil {
		t.Fatal("no final snapshot after the reload-failed sync")
	}
	var body struct {
		Drift       string          `json:"drift"`
		Diagnostics []diagnosticDTO `json:"diagnostics"`
	}
	if err := json.Unmarshal(finalSnapshot.Payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.Drift != "failed" {
		t.Fatalf("reload-failed snapshot drift = %q, want failed", body.Drift)
	}
	if !hasErrorDiagnostic(body.Diagnostics) {
		t.Fatalf("final snapshot lacks error diagnostics: %+v", body.Diagnostics)
	}
	if _, _, active := api.gate.Active(); active {
		t.Fatal("gate still held after the failed reload finished")
	}

	resp = postJSON(t, loopback.URL, api.Credential(), "/api/snapshot", "")
	var refreshed snapshotDTO
	if err := json.NewDecoder(resp.Body).Decode(&refreshed); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !hasErrorDiagnostic(refreshed.Diagnostics) {
		t.Fatalf("ordinary refresh dropped reload diagnostics: %+v", refreshed.Diagnostics)
	}
}

// spec: gui-local-api/event-identity-preserves-order-and-generation
func TestFailedOperationDoesNotBlockHealthyRetry(t *testing.T) {
	for _, action := range []string{"sync", "update"} {
		t.Run(action, func(t *testing.T) {
			var calls atomic.Int32
			flaky := func(context.Context, pipeline.Workspace) ([]error, error) {
				if calls.Add(1) == 1 {
					return nil, errors.New("transient " + action + " failure")
				}
				return nil, nil
			}
			lifecycle := session.DependencyLifecycle{}
			if action == "sync" {
				lifecycle.Sync = flaky
			} else {
				lifecycle.Update = flaky
			}
			api := newAPI(t, lifecycle, nil)
			loopback, err := api.ListenLoopback(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = loopback.Close() }()

			resp := postJSON(t, loopback.URL, api.Credential(), "/api/snapshot", "")
			var ready snapshotDTO
			if err := json.NewDecoder(resp.Body).Decode(&ready); err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if ready.Drift != "ready" {
				t.Fatalf("initial drift = %q, want ready", ready.Drift)
			}
			_, reader := subscribeEvents(t, api, loopback.URL)
			readSSE(t, reader)

			run := func() (int, string) {
				t.Helper()
				resp := postJSON(t, loopback.URL, api.Credential(), "/api/actions", `{"action":"`+action+`"}`)
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusAccepted {
					raw, _ := io.ReadAll(resp.Body)
					return resp.StatusCode, string(raw)
				}
				var severity string
				deadline := time.Now().Add(30 * time.Second)
				for time.Now().Before(deadline) && severity == "" {
					event := readSSE(t, reader)
					if event.Type == "notification" {
						var note struct {
							Severity string `json:"severity"`
						}
						if err := json.Unmarshal(event.Payload, &note); err != nil {
							t.Fatal(err)
						}
						severity = note.Severity
					}
				}
				for i := 0; i < 200; i++ {
					if _, _, active := api.gate.Active(); !active {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				return resp.StatusCode, severity
			}

			status, severity := run()
			if status != http.StatusAccepted || severity != "error" {
				t.Fatalf("first %s: status=%d severity=%q, want 202 error", action, status, severity)
			}
			api.mu.Lock()
			retained := api.current.Diagnostics
			api.mu.Unlock()
			if !hasErrorDiagnostic(retained) {
				t.Fatalf("failed %s left no retained error diagnostic: %+v", action, retained)
			}

			status, severity = run()
			if status != http.StatusAccepted {
				t.Fatalf("retry %s rejected: status=%d body=%s, want 202", action, status, severity)
			}
			if severity != "info" {
				t.Fatalf("retry %s announced severity %q, want info", action, severity)
			}
			api.mu.Lock()
			cleared := api.current.Diagnostics
			api.mu.Unlock()
			for _, d := range cleared {
				if d.Severity == "error" && d.Category == string(session.DiagnosticCategory(action)) {
					t.Fatalf("retry left stale %s error diagnostic: %+v", action, cleared)
				}
			}
		})
	}
}

// spec: gui-local-api/event-identity-preserves-order-and-generation
func TestMutationStillRejectsBrokenSnapshotConfig(t *testing.T) {
	ws, home := newFixture(t)
	configPath := filepath.Join(ws.RepoRoot(), ".agents", "config.yaml")
	broken := []byte("apiVersion: [\n")
	if err := os.WriteFile(configPath, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	api, err := New(context.Background(), ws, home, session.DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) { return nil, nil },
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	handler := api.Handler()

	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", ""))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(t, api, "/api/actions", `{"action":"sync"}`))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("broken-config action status = %d, want 400", recorder.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != codeNotReady {
		t.Fatalf("code = %q, want %q", body.Code, codeNotReady)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, broken) {
		t.Fatal("rejected action mutated the committed config")
	}
}

// spec: gui-local-api/socket-binds-only-to-loopback-at-an-ephemeral-port
func TestLoopbackCloseIsIdempotentAndConcurrent(t *testing.T) {
	api := newAPI(t, session.DependencyLifecycle{}, nil)
	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = loopback.Close()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Close call %d returned %v", i, err)
		}
	}
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(loopback.URL, "http://"), 500*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("listener still reachable after Close")
	}
}

// spec: gui-local-api/requests-without-the-launch-credential-are-rejected
func TestAPICloseStopsListenerAndRejectsRequests(t *testing.T) {
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	handler := api.Handler()
	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", ""))

	api.Close()
	t.Cleanup(api.Close)

	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(loopback.URL, "http://"), 500*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("listener still reachable after API.Close")
	}
	if _, err := api.ListenLoopback(context.Background()); err == nil {
		t.Fatal("ListenLoopback succeeded after API.Close")
	}
	for _, path := range []string{"/api/snapshot", "/api/events", "/api/actions"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, authedRequest(t, api, path, `{"action":"sync"}`))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s after close: status = %d, want 503", path, recorder.Code)
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Code != "localapi.closed" {
			t.Fatalf("%s after close: code = %q, want %q", path, body.Code, "localapi.closed")
		}
	}
}

// spec: gui-local-api/concurrent-mutation-is-rejected-as-busy
func TestCloseRacingBlockedOperationKeepsGuardUntilCallbackReturns(t *testing.T) {
	blocked := newBlockingLifecycle(t)
	api := newAPI(t, blocked.lifecycle(), nil)
	handler := api.Handler()
	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", ""))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(t, api, "/api/actions", `{"action":"sync"}`))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("sync status = %d, want 202; body %s", recorder.Code, recorder.Body.String())
	}
	<-blocked.entered

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		api.Close()
	}()
	select {
	case <-closed:
		t.Fatal("API.Close returned while the admitted callback was still blocked")
	case <-time.After(300 * time.Millisecond):
	}
	if _, _, active := api.gate.Active(); !active {
		t.Fatal("close freed the operation guard before the callback returned")
	}
	blocked.unblock()
	select {
	case <-closed:
	case <-time.After(30 * time.Second):
		t.Fatal("API.Close did not return after the callback finished")
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(t, api, "/api/snapshot", ""))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("post-close snapshot status = %d, want 503", recorder.Code)
	}
}

// spec: gui-local-api/concurrent-mutation-is-rejected-as-busy
func TestCanceledRequestLeavesOperationGuarded(t *testing.T) {
	blocked := newBlockingLifecycle(t)
	api := newAPI(t, blocked.lifecycle(), nil)
	handler := api.Handler()
	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", ""))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(t, api, "/api/actions", `{"action":"sync"}`))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("sync status = %d, want 202; body %s", recorder.Code, recorder.Body.String())
	}
	<-blocked.entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := authedRequest(t, api, "/api/snapshot", "").WithContext(ctx)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, canceled)
	if recorder.Code != http.StatusOK {
		t.Fatalf("canceled busy-read status = %d, want 200", recorder.Code)
	}
	if _, _, active := api.gate.Active(); !active {
		t.Fatal("canceled request released the operation guard")
	}
	blocked.unblock()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, active := api.gate.Active(); !active {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("gate still held after the blocked callback finished")
}
