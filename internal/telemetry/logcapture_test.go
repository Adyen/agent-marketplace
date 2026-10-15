package telemetry

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/logging"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	"google.golang.org/protobuf/proto"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// otlpRequests records the bodies of every request a fake OTLP receiver gets.
type otlpRequests struct {
	mu     sync.Mutex
	bodies map[string][][]byte
}

func (r *otlpRequests) count(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies[path])
}

func (r *otlpRequests) last(path string) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	bodies := r.bodies[path]
	if len(bodies) == 0 {
		return nil
	}
	return bodies[len(bodies)-1]
}

// newOTLPReceiver answers every signal with the status returned by statusFor.
func newOTLPReceiver(t *testing.T, statusFor func(path string) int) (*httptest.Server, *otlpRequests) {
	t.Helper()
	reqs := &otlpRequests{bodies: make(map[string][][]byte)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqs.mu.Lock()
		reqs.bodies[r.URL.Path] = append(reqs.bodies[r.URL.Path], body)
		reqs.mu.Unlock()
		w.WriteHeader(statusFor(r.URL.Path))
	}))
	t.Cleanup(srv.Close)
	return srv, reqs
}

func acceptAllSignals(string) int { return http.StatusOK }

func decodeExportedLogs(t *testing.T, body []byte) []*logspb.LogRecord {
	t.Helper()
	var req collogspb.ExportLogsServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode logs request: %v", err)
	}
	var out []*logspb.LogRecord
	for _, rl := range req.GetResourceLogs() {
		for _, sl := range rl.GetScopeLogs() {
			out = append(out, sl.GetLogRecords()...)
		}
	}
	return out
}

// readRetainedPayload returns the single payload kept for review after a failed export.
func readRetainedPayload(t *testing.T, state TelemetryState) DiagnosticPayload {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(state.StateRoot(), "diagnostic", "payload-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one retained payload, got %v", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var payload DiagnosticPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

func findPayloadSpan(t *testing.T, payload DiagnosticPayload, name string) DiagnosticSpan {
	t.Helper()
	for _, s := range payload.Spans {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("payload has no span %q", name)
	return DiagnosticSpan{}
}

// newLogCaptureTelemetry starts diagnostic telemetry with a capture whose
// records come only from the returned logger, so tests control exactly what is
// captured.
func newLogCaptureTelemetry(t *testing.T, endpoint string) (*Telemetry, TelemetryState, *LogCapture, *slog.Logger) {
	t.Helper()
	capture := NewLogCapture()
	tel, state := newDiagnosticTelemetry(t, endpoint, WithLogCapture(capture))
	t.Cleanup(func() { _ = tel.Shutdown(context.Background()) })
	return tel, state, capture, slog.New(capture)
}

func TestDiagnosticLogsCapturedDuringTUISession(t *testing.T) {
	// spec: diagnostic-capture/records-logged-during-a-tui-session-are-captured
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	enabled := true
	capture := NewLogCapture()
	_, _, err := logging.Init(logging.Options{
		FilePath: filepath.Join(t.TempDir(), "am.log"),
		Enabled:  &enabled,
		Stderr:   io.Discard,
		Command:  "test",
		Version:  "test",
		Capture:  capture,
	})
	if err != nil {
		t.Fatalf("init logging: %v", err)
	}

	slog.Info("before terminal session")
	restore := logging.BeginTerminalSession()
	slog.Info("during terminal session", "registry", "example")
	restore()

	records, _ := capture.snapshot()
	if !hasCapturedMessage(records, "before terminal session") {
		t.Fatal("control: record logged before the session was not captured")
	}
	if !hasCapturedMessage(records, "during terminal session") {
		t.Fatalf("record logged during the terminal session was not captured: %v", capturedMessages(records))
	}
}

func TestDiagnosticLogsCapturedDuringTUISessionWithoutLogFile(t *testing.T) {
	// spec: diagnostic-capture/records-logged-during-a-tui-session-are-captured
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	disabled := false
	capture := NewLogCapture()
	_, _, err := logging.Init(logging.Options{Enabled: &disabled, Stderr: io.Discard, Capture: capture})
	if err != nil {
		t.Fatalf("init logging: %v", err)
	}
	restore := logging.BeginTerminalSession()
	slog.Info("during terminal session without file")
	restore()

	records, _ := capture.snapshot()
	if !hasCapturedMessage(records, "during terminal session without file") {
		t.Fatalf("record logged during a session without a log file was not captured: %v", capturedMessages(records))
	}
}

func TestDiagnosticLogRecordWithSpanContextCarriesThatSpan(t *testing.T) {
	// spec: diagnostic-capture/a-record-logged-with-a-span-context-carries-that-span
	_ = setupDiagnosticTest(t)
	srv, _ := newOTLPReceiver(t, func(string) int { return http.StatusServiceUnavailable })

	tel, state, _, logger := newLogCaptureTelemetry(t, srv.URL)
	rootCtx, root := tel.StartSpan(context.Background(), "agent_marketplace.command", map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})
	childCtx, child := tel.StartSpan(rootCtx, "child.work", nil)
	logger.InfoContext(childCtx, "inside child span")
	child.End()
	root.End()

	var out bytes.Buffer
	_ = tel.FinishDiagnostic(context.Background(), &out, strings.NewReader(""), false, true)

	payload := readRetainedPayload(t, state)
	childSpan := findPayloadSpan(t, payload, "child.work")
	rootSpan := findPayloadSpan(t, payload, "agent_marketplace.command")
	logs := findPayloadLog(t, payload, "inside child span")
	if logs.TraceID != rootSpan.TraceID {
		t.Fatalf("record trace id = %q, want root trace %q", logs.TraceID, rootSpan.TraceID)
	}
	if logs.SpanID != childSpan.SpanID {
		t.Fatalf("record span id = %q, want child span %q", logs.SpanID, childSpan.SpanID)
	}
}

func TestDiagnosticLogRecordWithoutSpanCarriesRootSpan(t *testing.T) {
	// spec: diagnostic-capture/a-record-logged-without-a-span-carries-the-root-span
	_ = setupDiagnosticTest(t)
	srv, _ := newOTLPReceiver(t, func(string) int { return http.StatusServiceUnavailable })

	tel, state, _, logger := newLogCaptureTelemetry(t, srv.URL)
	logger.Info("logged before root span")

	ctx, root := tel.StartSpan(context.Background(), "agent_marketplace.command", map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})
	logger.InfoContext(ctx, "logged inside root span")
	logger.Info("logged without context")
	root.End()

	var out bytes.Buffer
	_ = tel.FinishDiagnostic(context.Background(), &out, strings.NewReader(""), false, true)

	payload := readRetainedPayload(t, state)
	rootSpan := findPayloadSpan(t, payload, "agent_marketplace.command")
	for _, msg := range []string{"logged before root span", "logged without context"} {
		entry := findPayloadLog(t, payload, msg)
		if entry.TraceID != rootSpan.TraceID || entry.SpanID != rootSpan.SpanID {
			t.Fatalf("record %q = trace %q span %q, want root trace %q span %q",
				msg, entry.TraceID, entry.SpanID, rootSpan.TraceID, rootSpan.SpanID)
		}
	}
}

func TestDiagnosticLogsAreBounded(t *testing.T) {
	// spec: diagnostic-capture/excess-and-oversized-records-are-bounded
	_ = setupDiagnosticTest(t)
	srv, _ := newOTLPReceiver(t, func(string) int { return http.StatusServiceUnavailable })

	tel, state, _, logger := newLogCaptureTelemetry(t, srv.URL)

	attrs := []any{"huge", strings.Repeat("é", 3000), "Bad Key", "dropped"}
	for i := range 40 {
		attrs = append(attrs, fmt.Sprintf("k%02d", i), "v")
	}
	attrs = append(attrs, "UPPER", "dropped")
	longMessage := strings.Repeat("é", 6000)
	logger.Info(longMessage, attrs...)
	for range maxCapturedLogRecords {
		logger.Info("bulk")
	}

	var out bytes.Buffer
	_ = tel.FinishDiagnostic(context.Background(), &out, strings.NewReader(""), false, true)

	payload := readRetainedPayload(t, state)
	if len(payload.Logs) != maxCapturedLogRecords {
		t.Fatalf("payload logs = %d, want %d", len(payload.Logs), maxCapturedLogRecords)
	}
	if payload.LogsDropped != 1 {
		t.Fatalf("logs_dropped = %d, want 1", payload.LogsDropped)
	}

	first := payload.Logs[0]
	if len(first.Message) > 8<<10 || !strings.HasSuffix(first.Message, "…[truncated]") || !utf8.ValidString(first.Message) {
		t.Fatalf("message not truncated within 8 KiB: len=%d", len(first.Message))
	}
	if len(first.Attributes) != maxCapturedAttrs {
		t.Fatalf("attributes = %d, want %d", len(first.Attributes), maxCapturedAttrs)
	}
	for _, key := range []string{"huge", "k00", "k30"} {
		if _, ok := first.Attributes[key]; !ok {
			t.Fatalf("expected first attributes to be kept, missing %q", key)
		}
	}
	for _, key := range []string{"k31", "Bad Key", "UPPER"} {
		if _, ok := first.Attributes[key]; ok {
			t.Fatalf("attribute %q should not be in the payload", key)
		}
	}
	if huge := first.Attributes["huge"]; len(huge) > 2<<10 || !strings.HasSuffix(huge, "…[truncated]") {
		t.Fatalf("attribute value not truncated within 2 KiB: len=%d", len(huge))
	}
}

func TestDiagnosticLogsStayWithinSizeBudget(t *testing.T) {
	// spec: diagnostic-capture/captured-records-stay-within-the-total-size-budget
	_ = setupDiagnosticTest(t)
	srv, _ := newOTLPReceiver(t, func(string) int { return http.StatusServiceUnavailable })

	tel, state, _, logger := newLogCaptureTelemetry(t, srv.URL)

	const fullSize = 8 << 10
	padded := func(i, size int) string {
		return fmt.Sprintf("%04d", i) + strings.Repeat("x", size-4)
	}
	// 511 full-size records use 4 MiB less one record slot.
	for i := range 511 {
		logger.Info(padded(i, fullSize))
	}
	// This record needs more room than the 8 KiB slot left, so it overflows the budget.
	logger.Info(padded(511, fullSize), "pad", strings.Repeat("y", 100))
	// A tiny record that would fit on its own is still dropped, since it follows an overflow.
	logger.Info("tiny")
	for i := 513; i < 600; i++ {
		logger.Info(padded(i, fullSize))
	}

	var out bytes.Buffer
	_ = tel.FinishDiagnostic(context.Background(), &out, strings.NewReader(""), false, true)

	payload := readRetainedPayload(t, state)
	if len(payload.Logs) != 511 {
		t.Fatalf("payload logs = %d, want 511", len(payload.Logs))
	}
	if payload.LogsDropped != 89 {
		t.Fatalf("logs_dropped = %d, want 89", payload.LogsDropped)
	}
	total := 0
	for i, entry := range payload.Logs {
		if !strings.HasPrefix(entry.Message, fmt.Sprintf("%04d", i)) {
			t.Fatalf("record %d is not the emission-order prefix: %.8q", i, entry.Message)
		}
		total += len(entry.Message)
		for k, v := range entry.Attributes {
			total += len(k) + len(v)
		}
	}
	if total > 4<<20 {
		t.Fatalf("kept records total %d bytes, want at most 4 MiB", total)
	}
}

func TestDiagnosticLogsRemoveURLCredentials(t *testing.T) {
	// spec: diagnostic-capture/url-credentials-are-removed-from-log-records
	_ = setupDiagnosticTest(t)
	srv, reqs := newOTLPReceiver(t, func(path string) int {
		if path == "/v1/logs" {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	})

	tel, state, _, logger := newLogCaptureTelemetry(t, srv.URL)
	_, root := tel.StartSpan(context.Background(), "agent_marketplace.command", nil)
	logger.Info("cloning https://user:token@example.com/repo.git", "url", "https://user:token@example.com/other.git")
	logger.WithGroup("registry").Info("synced", "source", "https://user:token@example.com/src.git")
	root.End()

	var out bytes.Buffer
	_ = tel.FinishDiagnostic(context.Background(), &out, strings.NewReader(""), false, true)

	payload := readRetainedPayload(t, state)
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "token@") {
		t.Fatalf("payload should not contain URL credentials: %s", data)
	}
	entry := findPayloadLog(t, payload, "cloning https://***@example.com/repo.git")
	if entry.Attributes["url"] != "https://***@example.com/other.git" {
		t.Fatalf("attribute url = %q, want redacted", entry.Attributes["url"])
	}
	grouped := findPayloadLog(t, payload, "synced")
	if grouped.Attributes["registry.source"] != "https://***@example.com/src.git" {
		t.Fatalf("grouped attribute = %+v, want flattened redacted key", grouped.Attributes)
	}

	exported := reqs.last("/v1/logs")
	if exported == nil {
		t.Fatal("expected a /v1/logs request")
	}
	if bytes.Contains(exported, []byte("token@")) {
		t.Fatal("exported logs should not contain URL credentials")
	}
	if !bytes.Contains(exported, []byte("https://***@example.com/repo.git")) {
		t.Fatal("exported logs should contain the redacted URL")
	}
}

func TestDiagnosticConsentedExportSendsLogsNextToTraces(t *testing.T) {
	// spec: diagnostic-capture/consented-export-sends-logs-next-to-traces
	_ = setupDiagnosticTest(t)
	srv, reqs := newOTLPReceiver(t, acceptAllSignals)

	tel, _, _, logger := newLogCaptureTelemetry(t, srv.URL)
	rootCtx, root := tel.StartSpan(context.Background(), "agent_marketplace.command", nil)
	childCtx, child := tel.StartSpan(rootCtx, "child.work", nil)
	logger.InfoContext(childCtx, "inside child")
	child.End()
	logger.InfoContext(rootCtx, "inside root")
	root.End()

	var out bytes.Buffer
	if err := tel.FinishDiagnostic(context.Background(), &out, strings.NewReader(""), false, true); err != nil {
		t.Fatalf("finish diagnostic: %v\n%s", err, out.String())
	}

	if reqs.count("/v1/logs") != 1 {
		t.Fatalf("expected one /v1/logs request, got %d", reqs.count("/v1/logs"))
	}
	if reqs.count("/v1/traces") == 0 {
		t.Fatal("expected a traces request alongside logs")
	}
	records := decodeExportedLogs(t, reqs.last("/v1/logs"))
	if len(records) != 2 {
		t.Fatalf("exported %d records, want 2", len(records))
	}
	for _, rec := range records {
		if rec.GetSeverityText() != "INFO" {
			t.Fatalf("severity = %q, want INFO", rec.GetSeverityText())
		}
		if rec.GetSeverityNumber() == 0 {
			t.Fatal("severity number not set")
		}
	}
	if got := records[0].GetBody().GetStringValue(); got != "inside child" {
		t.Fatalf("first record body = %q", got)
	}
	if got := records[1].GetBody().GetStringValue(); got != "inside root" {
		t.Fatalf("second record body = %q", got)
	}
	rootTrace := root.TraceID()
	for _, rec := range records {
		if hex.EncodeToString(rec.GetTraceId()) != rootTrace {
			t.Fatalf("exported trace id = %x, want %s", rec.GetTraceId(), rootTrace)
		}
	}
}

func TestDiagnosticRejectedLogsRequestFailsExport(t *testing.T) {
	// spec: diagnostic-capture/a-rejected-logs-request-fails-the-export
	_ = setupDiagnosticTest(t)
	srv, reqs := newOTLPReceiver(t, func(path string) int {
		if path == "/v1/logs" {
			return http.StatusNotFound
		}
		return http.StatusOK
	})

	tel, state, _, logger := newLogCaptureTelemetry(t, srv.URL)
	ctx, root := tel.StartSpan(context.Background(), "agent_marketplace.command", nil)
	logger.InfoContext(ctx, "a record")
	root.End()

	var out bytes.Buffer
	err := tel.FinishDiagnostic(context.Background(), &out, strings.NewReader(""), false, true)
	if err == nil {
		t.Fatal("expected diagnostic export to fail when /v1/logs is rejected")
	}
	if reqs.count("/v1/logs") == 0 {
		t.Fatal("expected a /v1/logs request")
	}
	if !strings.Contains(out.String(), "Diagnostic export failed") {
		t.Fatalf("expected failure output, got:\n%s", out.String())
	}
	payload := readRetainedPayload(t, state)
	findPayloadLog(t, payload, "a record")
}

func hasCapturedMessage(records []capturedLog, message string) bool {
	for _, r := range records {
		if r.message == message {
			return true
		}
	}
	return false
}

func capturedMessages(records []capturedLog) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.message)
	}
	return out
}

func findPayloadLog(t *testing.T, payload DiagnosticPayload, message string) DiagnosticLog {
	t.Helper()
	for _, entry := range payload.Logs {
		if entry.Message == message {
			return entry
		}
	}
	t.Fatalf("payload has no log record %q", message)
	return DiagnosticLog{}
}
