package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	metricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	tracespb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// otlpCapture is an OTLP/HTTP endpoint that records every export, so a test
// can first prove that telemetry was exported at all and only then assert
// that a quarantined payload never reached it.
type otlpCapture struct {
	URL string

	mu      sync.Mutex
	metrics []*metricspb.ExportMetricsServiceRequest
	traces  []*tracespb.ExportTraceServiceRequest
	bodies  [][]byte
}

func startOTLPCapture(t *testing.T) *otlpCapture {
	t.Helper()
	capture := &otlpCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			t.Errorf("read telemetry body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("Content-Encoding") == "gzip" {
			reader, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				t.Errorf("telemetry body claims gzip but does not decode: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			plain, err := io.ReadAll(io.LimitReader(reader, 8<<20))
			_ = reader.Close()
			if err != nil {
				t.Errorf("gunzip telemetry body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			body = plain
		}
		if len(body) == 0 {
			t.Errorf("empty telemetry export to %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		capture.mu.Lock()
		capture.bodies = append(capture.bodies, body)
		capture.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		switch r.URL.Path {
		case "/v1/metrics":
			var req metricspb.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Errorf("malformed metrics export: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			capture.mu.Lock()
			capture.metrics = append(capture.metrics, &req)
			capture.mu.Unlock()
			resp, _ := proto.Marshal(&metricspb.ExportMetricsServiceResponse{})
			_, _ = w.Write(resp)
		case "/v1/traces":
			var req tracespb.ExportTraceServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Errorf("malformed trace export: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			capture.mu.Lock()
			capture.traces = append(capture.traces, &req)
			capture.mu.Unlock()
			resp, _ := proto.Marshal(&tracespb.ExportTraceServiceResponse{})
			_, _ = w.Write(resp)
		default:
			t.Errorf("unexpected telemetry path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	capture.URL = srv.URL
	return capture
}

func (c *otlpCapture) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics, c.traces, c.bodies = nil, nil, nil
}

// assertExportedWithout requires a positive metrics data point for command
// plus the command span and a detailed pipeline span, then requires that no
// export body and no pending telemetry file under telemetryRoot contains any
// of payloads.
func (c *otlpCapture) assertExportedWithout(t *testing.T, command, telemetryRoot string, payloads []string) {
	t.Helper()
	c.mu.Lock()
	metrics := append([]*metricspb.ExportMetricsServiceRequest{}, c.metrics...)
	traces := append([]*tracespb.ExportTraceServiceRequest{}, c.traces...)
	bodies := append([][]byte{}, c.bodies...)
	c.mu.Unlock()

	if len(metrics) == 0 {
		t.Fatalf("expected at least one metrics export from %s", command)
	}
	var sawInvocation bool
	for _, req := range metrics {
		for _, rm := range req.ResourceMetrics {
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					for _, dp := range m.GetSum().GetDataPoints() {
						if keyValueMap(dp.GetAttributes())[string(tmcfg.ResourceAttrCommandName)] == command && dp.GetAsInt() > 0 {
							sawInvocation = true
						}
					}
				}
			}
		}
	}
	if !sawInvocation {
		t.Fatalf("no metrics data point carried command.name=%s with a positive value", command)
	}
	if len(traces) == 0 {
		t.Fatalf("expected at least one trace export from the detailed %s", command)
	}
	spanNames := map[string]int{}
	for _, req := range traces {
		for _, rs := range req.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				for _, sp := range ss.Spans {
					spanNames[sp.Name]++
				}
			}
		}
	}
	if spanNames["agent_marketplace.command"] == 0 {
		t.Fatalf("no agent_marketplace.command span exported; spans = %v", spanNames)
	}
	var detailedSpan bool
	for name := range spanNames {
		if strings.HasPrefix(name, "agent_marketplace.pipeline.") {
			detailedSpan = true
		}
	}
	if !detailedSpan {
		t.Fatalf("no detailed pipeline span exported; spans = %v", spanNames)
	}

	for i, body := range bodies {
		decoded := body
		var req metricspb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &req); err == nil {
			if rendered, err := protojson.Marshal(&req); err == nil {
				decoded = rendered
			}
		}
		for _, payload := range payloads {
			if bytes.Contains(decoded, []byte(payload)) || bytes.Contains(body, []byte(payload)) {
				t.Fatalf("telemetry request %d contains the quarantined payload %q", i, payload)
			}
		}
	}
	err := filepath.WalkDir(telemetryRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, payload := range payloads {
			if bytes.Contains(data, []byte(payload)) {
				t.Fatalf("pending telemetry file %s contains the quarantined payload %q", path, payload)
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk telemetry queue: %v", err)
	}
}

// assertLogRecordsWithout requires the log file to hold a record containing
// want, proving the run was logged, and to contain none of payloads.
func assertLogRecordsWithout(t *testing.T, logPath, want string, payloads []string) {
	t.Helper()
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !bytes.Contains(logBytes, []byte(want)) {
		t.Fatalf("log file = %d bytes without %q; expected an actual record of the run", len(logBytes), want)
	}
	for _, payload := range payloads {
		if bytes.Contains(logBytes, []byte(payload)) {
			t.Fatalf("log file contains the quarantined payload %q", payload)
		}
	}
}
