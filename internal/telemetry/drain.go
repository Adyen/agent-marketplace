package telemetry

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Adyen/agent-marketplace/internal/logging/logvalue"
	"github.com/Adyen/agent-marketplace/internal/redact"
)

// drain attempts to send queued payloads oldest-first. It respects the
// supplied context deadline.
func (t *Telemetry) drain(ctx context.Context) error {
	if t.queue == nil || len(t.sendClients) == 0 {
		return nil
	}

	slog.Debug("draining queued telemetry payloads", "senders", len(t.sendClients))
	return t.queue.Drain(ctx, t.send)
}

// send replays a queued Envelope to the route's endpoint.
func (t *Telemetry) send(ctx context.Context, payload []byte) error {
	var env Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		slog.Error("discarding malformed queued envelope", "error", err)
		return nil
	}

	client, ok := t.sendClients[env.Route]
	if !ok {
		return fmt.Errorf("no send client for route %q", env.Route)
	}

	body, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return fmt.Errorf("decode queued payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, env.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build replay request: %w", err)
	}
	req = req.WithContext(ctx)

	for k, vv := range env.Headers {
		if k == "Content-Length" || k == "Host" {
			continue
		}
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	req.ContentLength = int64(len(body))

	resp, err := client.Do(req)
	if err != nil {
		err = redact.Error(err)
		slog.Error("send queued payload failed", "url", logURL(env.URL), "route", env.Route, "error", logvalue.Safe(err))
		return fmt.Errorf("send queued payload: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Debug("close response body", "err", cerr)
		}
	}()

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		slog.Debug("queued payload delivered", "url", logURL(env.URL), "route", env.Route)
		return nil
	}

	slog.Error("queued payload delivery rejected", "url", logURL(env.URL), "route", env.Route, "status", resp.Status)
	return fmt.Errorf("queued send to %s failed: %s", redact.URL(env.URL), resp.Status)
}
