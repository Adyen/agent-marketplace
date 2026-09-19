package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func TestTelemetryEndpointOverrideCredentials(t *testing.T) {
	for _, mode := range []string{"changed", "same", "organization-locked"} {
		t.Run(mode, func(t *testing.T) {
			setStateRoot(t)
			t.Setenv("TELEMETRY_FIXTURE_SECRET", "synthetic-env-key")
			secretDir := t.TempDir()
			if err := os.Chmod(secretDir, 0o700); err != nil {
				t.Fatal(err)
			}
			secretFile := filepath.Join(secretDir, "secret")
			if err := os.WriteFile(secretFile, []byte("synthetic-file-key"), 0o600); err != nil {
				t.Fatal(err)
			}
			headers := make(chan http.Header, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case headers <- r.Header.Clone():
				default:
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			user := &amconfig.Config{Telemetry: &tmcfg.TelemetryConfig{
				Level: tmcfg.LevelBasic, Endpoint: server.URL,
				Headers: map[string]tmcfg.HeaderSource{"X-API-Key": {Env: "TELEMETRY_FIXTURE_SECRET"}, "X-Tenant": {File: secretFile}},
			}}
			var org *amconfig.Config
			t.Setenv(EnvEndpoint, server.URL)
			if mode == "changed" {
				user.Telemetry.Endpoint = "https://configured.example.invalid"
			}
			if mode == "organization-locked" {
				org = &amconfig.Config{Telemetry: cloneTelemetryConfig(user.Telemetry)}
				t.Setenv(EnvEndpoint, "https://ignored.example.invalid")
			}
			state, err := ResolveTelemetryConfig(org, user, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(user.Telemetry.Headers) != 2 {
				t.Fatal("resolution changed authored headers")
			}
			if mode == "changed" && len(state.PrimaryHeaders) != 0 {
				t.Error("endpoint override retained configured header sources")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			tel, err := New(ctx, state, nil)
			if err != nil {
				t.Fatal(err)
			}
			tel.CommandCounter().Add(ctx, 1, nil)
			if err := tel.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-headers:
				if mode == "changed" {
					if got.Get("X-API-Key") != "" || got.Get("X-Tenant") != "" {
						t.Error("override recipient received configured credentials")
					}
				} else if got.Get("X-API-Key") != "synthetic-env-key" || got.Get("X-Tenant") != "synthetic-file-key" {
					t.Error("unchanged/managed collector lost its authentication")
				}
			case <-ctx.Done():
				t.Fatal("collector did not receive the positive-control export")
			}
		})
	}
}

func TestTelemetryReplayRejectsRedirect(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
		for _, cross := range []bool{false, true} {
			t.Run(fmt.Sprintf("status=%d/cross=%t", status, cross), func(t *testing.T) {
				var received, original atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					received.Add(1)
					w.WriteHeader(http.StatusOK)
				}))
				defer target.Close()
				var redirect atomic.Bool
				redirect.Store(true)
				source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/target" {
						received.Add(1)
						w.WriteHeader(http.StatusOK)
						return
					}
					original.Add(1)
					if redirect.Load() {
						destination := "/target"
						if cross {
							destination = target.URL
						}
						http.Redirect(w, r, destination, status)
						return
					}
					if r.Header.Get("X-API-Key") != "synthetic-header" {
						t.Error("replay lost the original collector credential")
					}
					w.WriteHeader(http.StatusOK)
				}))
				defer source.Close()
				q := NewQueue(t.TempDir())
				live, sender, err := buildRouteClients(source.URL, nil, "", false, "primary", queueSubdirMetrics, nil, q)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, source.URL, strings.NewReader("payload"))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("X-API-Key", "synthetic-header")
				resp, liveErr := live.Do(req)
				if resp != nil {
					_ = resp.Body.Close()
				}
				if liveErr == nil || received.Load() != 0 {
					t.Error("live export followed a redirect")
				}
				payload, err := json.Marshal(Envelope{Route: "primary", Signal: queueSubdirMetrics, URL: source.URL, Headers: map[string][]string{"X-API-Key": {"synthetic-header"}}, Payload: base64.StdEncoding.EncodeToString([]byte("payload"))})
				if err != nil {
					t.Fatal(err)
				}
				if err := q.Push(ctx, Route{Name: "primary", Signal: queueSubdirMetrics}, payload); err != nil {
					t.Fatal(err)
				}
				tel := &Telemetry{queue: q, sendClients: map[string]*http.Client{"primary": sender}}
				_ = tel.drain(ctx)
				if received.Load() != 0 || q.Count() != 1 {
					t.Errorf("redirect reached receiver=%d or discarded queued payload (remaining=%d)", received.Load(), q.Count())
				}
				redirect.Store(false)
				if err := tel.drain(ctx); err != nil {
					t.Fatal(err)
				}
				if q.Count() != 0 || original.Load() < 3 {
					t.Errorf("original collector recovery failed: queued=%d requests=%d", q.Count(), original.Load())
				}
			})
		}
	}
}
