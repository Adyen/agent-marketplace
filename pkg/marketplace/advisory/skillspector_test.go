package advisory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type scannerFunc func(context.Context, map[string][]byte) (Result, error)

func (f scannerFunc) Scan(ctx context.Context, files map[string][]byte) (Result, error) {
	return f(ctx, files)
}

func TestNewScannerEndpointValidation(t *testing.T) {
	if scanner, err := NewScanner(""); err != nil || scanner != nil {
		t.Fatalf("NewScanner(empty) = %v, %v, want nil, nil", scanner, err)
	}
	socket := filepath.Join(t.TempDir(), "runner.sock")
	for _, endpoint := range []string{
		"unix://" + socket,
		"http://127.0.0.1:8080",
	} {
		if scanner, err := NewScanner(endpoint); err != nil || scanner == nil {
			t.Fatalf("NewScanner(%q) = %v, %v", endpoint, scanner, err)
		}
	}
	for _, endpoint := range []string{
		"unix://relative/runner.sock",
		"unix:///tmp/runner.sock?token=secret",
		"https://127.0.0.1:8080",
		"http://127.0.0.1",
		"http://127.0.0.1:8080/other",
		"http://127.0.0.1:8080?token=secret",
		"http://127.0.0.1:8080#fragment",
		"http://user:secret@127.0.0.1:8080",
	} {
		if scanner, err := NewScanner(endpoint); err == nil || scanner != nil {
			t.Fatalf("NewScanner(%q) = %v, %v, want refusal", endpoint, scanner, err)
		}
	}
}

func TestHTTPScannerPostsBoundedDetachedFilesWithoutProxy(t *testing.T) {
	authored := []byte("---\nname: example\n---\nInert skill body.\n")
	support := []byte("inert support")
	wantResult := Result{
		Version: "2.13.6",
		Status:  store.AdvisoryPartial,
		Findings: []Finding{{
			Code: "SC4", Severity: "HIGH", Path: "references/guide.txt",
		}},
	}
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/scan" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		var body struct {
			Files map[string]string `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if string(decodeBase64(t, body.Files["SKILL.md"])) != string(authored) ||
			string(decodeBase64(t, body.Files["references/guide.txt"])) != string(support) {
			t.Errorf("submitted files = %#v", body.Files)
		}
		_ = json.NewEncoder(w).Encode(wantResult)
	}))
	defer server.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	scanner, err := NewScanner(server.URL)
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}
	got, err := scanner.Scan(context.Background(), map[string][]byte{
		"SKILL.md":             authored,
		"references/guide.txt": support,
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if calls != 1 || got.Version != wantResult.Version || got.Status != wantResult.Status || len(got.Findings) != 1 ||
		got.Findings[0] != wantResult.Findings[0] {
		t.Fatalf("Scan result = %#v, calls = %d", got, calls)
	}
}

// spec: marketplace-analysis/overlapping-indexes-share-one-worker-scanner
func TestSkillSpectorConcurrentReportsQueueForSingleWorker(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	firstResult := Result{Version: skillSpectorVersion, Status: store.AdvisoryComplete, Findings: []Finding{{Code: "SC4"}}}
	waitingResult := Result{Version: skillSpectorVersion, Status: store.AdvisoryPartial, Findings: []Finding{{Code: "AE1"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			entered <- struct{}{}
			<-release
			_ = json.NewEncoder(w).Encode(firstResult)
			return
		}
		select {
		case <-release:
			_ = json.NewEncoder(w).Encode(waitingResult)
		default:
			http.Error(w, "worker busy", http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		server.Close()
	})
	scanner, err := NewScanner(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	firstReport := make(chan store.AdvisoryReport, 1)
	go func() {
		firstReport <- (SkillSpector{Scanner: scanner, Timeout: time.Second}).Report(t.Context(), map[string][]byte{"SKILL.md": []byte("first")}, nil)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first request did not enter worker")
	}
	expired := (SkillSpector{Scanner: scanner, Timeout: 100 * time.Millisecond}).Report(
		t.Context(), map[string][]byte{"SKILL.md": []byte("expired")}, nil)
	if expired.Status != store.AdvisoryFailed || expired.Reason != "timeout" || calls.Load() != 1 {
		t.Fatalf("expired report = %#v, runner calls = %d", expired, calls.Load())
	}
	getConn := make(chan struct{}, 1)
	ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{GetConn: func(string) {
		select {
		case getConn <- struct{}{}:
		default:
		}
	}})
	waitingReport := make(chan store.AdvisoryReport, 1)
	go func() {
		waitingReport <- (SkillSpector{Scanner: scanner, Timeout: time.Second}).Report(ctx, map[string][]byte{"SKILL.md": []byte("waiting")}, nil)
	}()
	select {
	case <-getConn:
	case <-time.After(time.Second):
		t.Fatal("waiting request did not reach the HTTP transport")
	}
	close(release)
	first, waiting := <-firstReport, <-waitingReport
	if first.Status != store.AdvisoryComplete || len(first.Findings) != 1 || first.Findings[0].Code != "SC4" {
		t.Fatalf("first report = %#v", first)
	}
	if waiting.Status != store.AdvisoryPartial || len(waiting.Findings) != 1 || waiting.Findings[0].Code != "AE1" {
		t.Fatalf("waiting report = %#v", waiting)
	}
	if calls.Load() != 2 {
		t.Fatalf("runner calls = %d, want 2", calls.Load())
	}
}

func TestHTTPScannerRequiresExactUniqueResultFields(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want bool
	}{
		{
			name: "required exact fields",
			body: `{"version":"2.13.6","status":"complete","findings":[],"ignored":{"safe":true}}`,
			want: true,
		},
		{
			name: "duplicate field",
			body: `{"version":"2.13.6","version":"2.13.6","status":"complete","findings":[]}`,
		},
		{
			name: "case variant is not a required field",
			body: `{"Version":"2.13.6","status":"complete","findings":[]}`,
		},
		{
			name: "missing findings",
			body: `{"version":"2.13.6","status":"complete"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			scanner, err := NewScanner(server.URL)
			if err != nil {
				t.Fatalf("NewScanner: %v", err)
			}
			_, err = scanner.Scan(context.Background(), map[string][]byte{"SKILL.md": []byte("inert")})
			if (err == nil) != test.want {
				t.Fatalf("Scan error = %v, want success %t", err, test.want)
			}
		})
	}
}

func TestScannerRefusesRedirectsAndBoundViolations(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		var targetCalls int
		target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			targetCalls++
		}))
		defer target.Close()
		redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", target.URL+"/v1/scan")
			w.WriteHeader(http.StatusFound)
		}))
		defer redirect.Close()
		scanner, err := NewScanner(redirect.URL)
		if err != nil {
			t.Fatalf("NewScanner: %v", err)
		}
		if _, err := scanner.Scan(context.Background(), map[string][]byte{"SKILL.md": []byte("inert")}); err == nil {
			t.Fatal("Scan followed a redirect")
		}
		if targetCalls != 0 {
			t.Fatalf("redirect target calls = %d, want 0", targetCalls)
		}
	})

	t.Run("input bounds", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
		defer server.Close()
		scanner, err := NewScanner(server.URL)
		if err != nil {
			t.Fatalf("NewScanner: %v", err)
		}
		files := map[string][]byte{"SKILL.md": []byte("inert")}
		for i := 0; i < 128; i++ {
			files[fmt.Sprintf("support/%03d.txt", i)] = []byte("x")
		}
		for _, invalid := range []map[string][]byte{
			files,
			{"SKILL.md": make([]byte, 1024*1024+1)},
			{
				"SKILL.md": []byte("x"),
				"a.txt":    make([]byte, 1024*1024),
				"b.txt":    make([]byte, 1024*1024),
				"c.txt":    make([]byte, 1024*1024),
				"d.txt":    make([]byte, 1024*1024),
			},
			{"SKILL.md": []byte("x"), "../escape.txt": []byte("x")},
		} {
			if _, err := scanner.Scan(context.Background(), invalid); err == nil {
				t.Fatalf("Scan accepted %d files", len(invalid))
			}
		}
		if calls != 0 {
			t.Fatalf("runner calls = %d, want 0", calls)
		}
	})

	t.Run("response byte and finding bounds", func(t *testing.T) {
		for _, body := range [][]byte{
			bytesOfSize(128*1024 + 1),
			tooManyFindingsResponse(t),
		} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(body)
			}))
			scanner, err := NewScanner(server.URL)
			if err != nil {
				t.Fatalf("NewScanner: %v", err)
			}
			if _, err := scanner.Scan(context.Background(), map[string][]byte{"SKILL.md": []byte("inert")}); err == nil {
				t.Fatal("Scan accepted an oversized response")
			}
			server.Close()
		}
	})
}

func TestUnixScannerUsesPrivateSocketTransport(t *testing.T) {
	socketDir, err := os.MkdirTemp("/tmp", "ss-")
	if err != nil {
		t.Fatalf("create socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "scanner.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/scan" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"version":"2.13.6","status":"complete","findings":[]}`))
	})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		<-done
	})
	scanner, err := NewScanner("unix://" + socket)
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}
	if got, err := scanner.Scan(context.Background(), map[string][]byte{"SKILL.md": []byte("inert")}); err != nil ||
		got.Version != "2.13.6" || got.Status != store.AdvisoryComplete {
		t.Fatalf("Scan = %#v, %v", got, err)
	}
}

func decodeBase64(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("decode base64 input: %v", err)
	}
	return decoded
}

func bytesOfSize(size int) []byte {
	return []byte(strings.Repeat("x", size))
}

func tooManyFindingsResponse(t *testing.T) []byte {
	t.Helper()
	findings := make([]Finding, 129)
	for i := range findings {
		findings[i] = Finding{Code: fmt.Sprintf("SS%04d", i), Severity: "low"}
	}
	encoded, err := json.Marshal(Result{Version: "2.13.6", Status: store.AdvisoryComplete, Findings: findings})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return encoded
}

func TestScannerBoundsAcceptTimeoutContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(time.Second)
	}))
	defer server.Close()
	scanner, err := NewScanner(server.URL)
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := scanner.Scan(ctx, map[string][]byte{"SKILL.md": []byte("inert")}); err == nil {
		t.Fatal("Scan ignored context deadline")
	}
}

func TestSkillSpectorValidatesVersionAndCoverage(t *testing.T) {
	for _, test := range []struct {
		name    string
		result  Result
		status  store.AdvisoryStatus
		reason  string
		version string
	}{
		{"complete", Result{Version: "2.13.6", Status: store.AdvisoryComplete}, store.AdvisoryComplete, "", "2.13.6"},
		{"offline limitations", Result{Version: "2.13.6", Status: store.AdvisoryCompleteWithCaveats}, store.AdvisoryCompleteWithCaveats, "offline_limitations", "2.13.6"},
		{"coverage gaps", Result{Version: "2.13.6", Status: store.AdvisoryPartial}, store.AdvisoryPartial, "coverage_gaps", "2.13.6"},
		{"wrong version", Result{Version: "2.13.7", Status: store.AdvisoryComplete}, store.AdvisoryFailed, "invalid_response", ""},
		{"invalid status", Result{Version: "2.13.6", Status: "unknown"}, store.AdvisoryFailed, "invalid_response", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			scanner := scannerFunc(func(context.Context, map[string][]byte) (Result, error) {
				return test.result, nil
			})
			report := (SkillSpector{Scanner: scanner}).Report(
				context.Background(), map[string][]byte{"SKILL.md": []byte("inert")}, nil,
			)
			if report.Status != test.status || report.Reason != test.reason || report.Version != test.version {
				t.Fatalf("Report = %#v", report)
			}
		})
	}
}
