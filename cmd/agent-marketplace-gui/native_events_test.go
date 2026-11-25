//go:build cgo && (darwin || linux)

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/session"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const nativeCompletionProbeJS = `(async () => {
  const report = { actions: [] }
  try {
    const app = window.go && window.go.main && window.go.main.App
    if (!app || typeof app.Bootstrap !== 'function') throw new Error('bootstrap binding missing')
    const bootstrap = await app.Bootstrap()
    const deadline = Date.now() + 45000
    for (;;) {
      const solo = [...document.querySelectorAll('.workspace__name')].some((name) => name.textContent.trim() === 'solo')
      if (solo) break
      if (Date.now() > deadline) throw new Error('fixture row solo did not appear')
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    const notices = () => [...document.querySelectorAll('[aria-label="Notifications"] > li')]
    for (const action of ['sync', 'update', 'sync', 'update']) {
      const before = new Set(notices())
      const response = await fetch('/api/actions', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Agent-Marketplace-Token': bootstrap.credential,
        },
        body: JSON.stringify({ action }),
      })
      if (response.status !== 202) throw new Error(action + ' status ' + response.status)
      const message = action === 'sync' ? 'Sync complete' : 'Update complete'
      const toastDeadline = Date.now() + 30000
      let toast
      while (Date.now() <= toastDeadline) {
        toast = notices().find((item) => !before.has(item) && item.textContent.includes(message))
        if (toast) break
        await new Promise((resolve) => setTimeout(resolve, 20))
      }
      if (!toast) throw new Error(message + ' toast did not appear')
      report.actions.push({ action, message, toastAt: Date.now() })
      await new Promise((resolve) => setTimeout(resolve, 3000))
    }
  } catch (error) {
    report.error = String(error && error.message ? error.message : error)
  }
  window.runtime.EventsEmit('am:gui-probe', report)
})()`

type nativeControlNotice struct {
	message string
	at      int64
}

func startNativeControlSubscriber(t *testing.T, app *App) <-chan nativeControlNotice {
	t.Helper()
	server := httptest.NewServer(app.handler())
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Agent-Marketplace-Token", app.Bootstrap().Credential)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("control event stream status = %d, want 200", response.StatusCode)
	}
	notices := make(chan nativeControlNotice, 16)
	go func() {
		reader := bufio.NewReader(response.Body)
		var data strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\n" || line == "\r\n" {
				if data.Len() == 0 {
					continue
				}
				var event struct {
					Type    string `json:"type"`
					Payload struct {
						Message string `json:"message"`
					} `json:"payload"`
				}
				if json.Unmarshal([]byte(data.String()), &event) == nil && event.Type == "notification" {
					select {
					case notices <- nativeControlNotice{message: event.Payload.Message, at: time.Now().UnixMilli()}:
					default:
					}
				}
				data.Reset()
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
	}()
	return notices
}

// spec: gui-local-api/completion-notification-renders-without-further-activity
func TestNativeCompletionNotificationPrompt(t *testing.T) {
	if os.Getenv("AM_GUI_NATIVE_SMOKE") != "1" {
		t.Skip("native completion regression; run with AM_GUI_NATIVE_SMOKE=1 and built frontend assets")
	}
	newHostFixture(t)
	app, err := NewApp(context.Background(), session.RuntimeLifecycle())
	if err != nil {
		t.Fatal(err)
	}
	defer app.close()
	controlNotices := startNativeControlSubscriber(t, app)
	nativeSmokeApp <- app

	deadline := time.After(120 * time.Second)
	var windowCtx context.Context
	select {
	case windowCtx = <-nativeSmokeWindow:
	case err := <-nativeSmokeRunDone:
		t.Fatalf("host exited before startup: %v", err)
	case <-deadline:
		t.Fatal("native window startup deadline exceeded")
	}
	t.Cleanup(app.Quit)
	select {
	case <-nativeSmokeDomReady:
	case err := <-nativeSmokeRunDone:
		t.Fatalf("host exited before DOM ready: %v", err)
	case <-deadline:
		t.Fatal("native window DOM readiness deadline exceeded")
	}

	wailsruntime.WindowExecJS(windowCtx, nativeCompletionProbeJS)
	var report map[string]any
	select {
	case report = <-nativeSmokeProbe:
	case err := <-nativeSmokeRunDone:
		t.Fatalf("host exited before completion probe: %v", err)
	case <-deadline:
		t.Fatal("native completion probe deadline exceeded")
	}
	if message, _ := report["error"].(string); message != "" {
		t.Fatalf("native completion probe failed: %s", message)
	}
	actions, ok := report["actions"].([]any)
	if !ok || len(actions) != 4 {
		t.Fatalf("native completion actions = %#v, want four results", report["actions"])
	}
	for index, action := range actions {
		result, ok := action.(map[string]any)
		if !ok {
			t.Fatalf("action %d result = %#v", index, action)
		}
		message, _ := result["message"].(string)
		toastAt, _ := result["toastAt"].(float64)
		var notice nativeControlNotice
		select {
		case notice = <-controlNotices:
		case <-time.After(30 * time.Second):
			t.Fatalf("no loopback notification frame for %s", message)
		}
		if notice.message != message {
			t.Fatalf("loopback notification %d = %q, want %q", index, notice.message, message)
		}
		delay := int64(toastAt) - notice.at
		if delay > 1000 {
			t.Fatalf("%s toast rendered %d ms after the loopback notification frame, want at most 1000 ms", message, delay)
		}
	}

	app.Quit()
	select {
	case err := <-nativeSmokeRunDone:
		if err != nil {
			t.Fatalf("wails.Run returned error: %v", err)
		}
	case <-deadline:
		t.Fatal("host did not exit after quit")
	}
}
