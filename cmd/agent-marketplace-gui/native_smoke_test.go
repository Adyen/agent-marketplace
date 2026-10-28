//go:build cgo && (darwin || linux)

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/wailsapp/wails/v2"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	nativeSmokeApp      = make(chan *App, 1)
	nativeSmokeWindow   = make(chan context.Context, 1)
	nativeSmokeDomReady = make(chan struct{})
	nativeSmokeProbe    = make(chan map[string]any, 1)
	nativeSmokeRunDone  = make(chan error, 1)
)

const nativeProbeEvent = "am:gui-probe"

func TestMain(m *testing.M) {
	if os.Getenv("AM_GUI_NATIVE_SMOKE") != "1" {
		os.Exit(m.Run())
	}
	result := make(chan int, 1)
	go func() { result <- m.Run() }()

	var app *App
	select {
	case app = <-nativeSmokeApp:
	case code := <-result:
		os.Exit(code)
	}
	nativeSmokeRunDone <- wails.Run(app.appOptions(lifecycleHooks{
		onStartup: func(ctx context.Context) {
			wailsruntime.EventsOn(ctx, nativeProbeEvent, func(optionalData ...any) {
				if len(optionalData) == 0 {
					return
				}
				data, err := json.Marshal(optionalData[0])
				if err != nil {
					return
				}
				var report map[string]any
				if err := json.Unmarshal(data, &report); err != nil {
					return
				}
				select {
				case nativeSmokeProbe <- report:
				default:
				}
			})
			nativeSmokeWindow <- ctx
		},
		onDomReady: func(ctx context.Context) {
			select {
			case <-nativeSmokeDomReady:
			default:
				close(nativeSmokeDomReady)
			}
		},
	}))
	os.Exit(<-result)
}

const probeJS = `(async () => {
	const report = { origin: String(window.location.origin) }
	try {
		const app = window.go && window.go.main && window.go.main.App
		report.boundMethods = app ? Object.keys(app) : []
		if (!app || typeof app.Bootstrap !== 'function') {
			throw new Error('bootstrap binding missing')
		}
		const creds = await app.Bootstrap()
		report.bootstrap = Boolean(creds && creds.credential)
		const deadline = Date.now() + 45000
		for (;;) {
			if (document.body && document.body.innerText.includes('skill/solo')) {
				report.domRow = true
				break
			}
			if (Date.now() > deadline) {
				report.domRow = false
				report.domText = (document.body ? document.body.innerText : '').slice(0, 400)
				break
			}
			await new Promise((resolve) => setTimeout(resolve, 250))
		}
		const response = await fetch('/api/snapshot', {
			method: 'POST',
			headers: { 'X-Agent-Marketplace-Token': creds.credential },
		})
		report.status = response.status
		const snapshot = await response.json()
		report.stage = snapshot.stage
		report.apiRow = (snapshot.rows || []).some((row) => row.name === 'solo' && row.scope === 'user')
	} catch (error) {
		report.error = String(error && error.message ? error.message : error)
	}
	window.runtime.EventsEmit('` + nativeProbeEvent + `', report)
})()`

func TestNativeLaunchSmoke(t *testing.T) {
	if os.Getenv("AM_GUI_NATIVE_SMOKE") != "1" {
		t.Skip("native window smoke; run with AM_GUI_NATIVE_SMOKE=1 and built frontend assets")
	}
	newHostFixture(t)
	org := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, org, "apiVersion: agent-marketplace/v4\ngui:\n  version_check: false\n")
	config.SetTestOrganizationConfigPath(t, org)

	app, err := NewApp(context.Background(), session.RuntimeLifecycle())
	if err != nil {
		t.Fatal(err)
	}
	defer app.close()
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

	wailsruntime.WindowExecJS(windowCtx, probeJS)
	var report map[string]any
	select {
	case report = <-nativeSmokeProbe:
	case err := <-nativeSmokeRunDone:
		t.Fatalf("host exited before probe result: %v", err)
	case <-deadline:
		t.Fatal("native window probe deadline exceeded")
	}
	if message, _ := report["error"].(string); message != "" {
		t.Fatalf("in-window probe failed: %s", message)
	}
	if report["bootstrap"] != true {
		t.Fatalf("bound Bootstrap did not return a credential payload: %v", report)
	}
	if methods, _ := report["boundMethods"].([]any); !slices.Equal(methods, []any{"Bootstrap", "Quit"}) {
		t.Fatalf("unexpected bound method set: %v", methods)
	}
	if report["status"] != float64(200) || report["stage"] != "final" || report["apiRow"] != true {
		t.Fatalf("authenticated in-window API fetch failed: %v", report)
	}
	if report["domRow"] != true {
		t.Fatalf("fixture row missing from native DOM: %v", report["domText"])
	}
	if origin, _ := report["origin"].(string); origin == "" {
		t.Fatalf("native window location origin empty: %v", report)
	} else {
		t.Logf("native window origin: %s", origin)
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
