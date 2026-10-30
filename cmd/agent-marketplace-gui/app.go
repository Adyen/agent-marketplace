package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/localapi"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

const singleInstanceID = "ef334250-9d55-4fdc-8c94-a6b48b8a3c27"

const launchRequestEvent = "desktop:launch-request"

const closeRequestEvent = "desktop:close-request"

var version = "dev"
var packageFormat = ""

type Bootstrap struct {
	Credential    string `json:"credential"`
	PackageFormat string `json:"packageFormat"`
	Version       string `json:"version"`
}

type Window interface {
	Unminimise()
	Show()
	Emit(event string, args ...string)
}

const guiTelemetryUnavailableWarning = "GUI telemetry unavailable; continuing without telemetry"

type App struct {
	api              *localapi.API
	version          string
	telemetry        *telemetry.Telemetry
	telemetryState   telemetry.TelemetryState
	telemetryContext context.Context
	telemetryStarted time.Time
	rootSpan         telemetry.Span
	quit             func()
	closeOnce        sync.Once
	nativeState
}

func NewApp(ctx context.Context, lifecycle session.DependencyLifecycle) (*App, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	startedAt := time.Now()
	state, resolveErr := telemetry.LoadAndResolve(ctx)
	var runtime *telemetry.Telemetry
	rootSpan := telemetry.Span{}
	apiCtx := ctx
	if resolveErr != nil {
		slog.Warn(guiTelemetryUnavailableWarning)
	} else if state.EffectiveLevel == tmcfg.LevelBasic || state.EffectiveLevel == tmcfg.LevelDetailed {
		initialized, telemetryErr := telemetry.New(ctx, state, slog.Default(), telemetry.WithVersion(version))
		if telemetryErr != nil || initialized == nil {
			shutdownTelemetry(initialized)
			slog.Warn(guiTelemetryUnavailableWarning)
		} else {
			runtime = initialized
			if state.EffectiveLevel == tmcfg.LevelDetailed {
				apiCtx, rootSpan = runtime.StartSpan(ctx, "agent_marketplace.command", map[string]string{
					string(tmcfg.ResourceAttrCommandName): "gui",
					"command.surface":                     "gui",
				})
			}
		}
	}
	telemetryCtx := apiCtx
	if runtime != nil {
		telemetryCtx = telemetry.NewContext(apiCtx, runtime, "gui", startedAt, state.HarnessesString(), state.Managed, rootSpan, false)
	}
	api, err := localapi.NewUser(telemetryCtx, homeDir, lifecycle, nil, localapi.WithVersion(version))
	if err != nil {
		rootSpan.End()
		shutdownTelemetry(runtime)
		return nil, err
	}
	return &App{
		api:              api,
		version:          version,
		telemetry:        runtime,
		telemetryState:   state,
		telemetryContext: telemetryCtx,
		telemetryStarted: startedAt,
		rootSpan:         rootSpan,
	}, nil
}

func (a *App) Bootstrap() Bootstrap {
	return Bootstrap{Credential: a.api.Credential(), PackageFormat: packageFormat, Version: a.version}
}

func (a *App) handler() http.Handler { return a.api.Handler() }

func (a *App) close() {
	a.closeOnce.Do(func() {
		a.api.Close()
		if a.telemetry == nil {
			return
		}
		a.rootSpan.End()
		attributes := map[string]string{
			string(tmcfg.ResourceAttrCommandName): "gui",
			string(tmcfg.ResourceAttrHarnesses):   a.telemetryState.HarnessesString(),
			string(tmcfg.ResourceAttrManaged):     strconv.FormatBool(a.telemetryState.Managed),
			string(tmcfg.ResourceAttrOutcomeCode): "success",
		}
		a.telemetry.CommandCounter().Add(a.telemetryContext, 1, attributes)
		a.telemetry.CommandDuration().Record(a.telemetryContext, time.Since(a.telemetryStarted).Milliseconds(), attributes)
		shutdownTelemetry(a.telemetry)
	})
}

func shutdownTelemetry(runtime *telemetry.Telemetry) {
	if runtime == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := runtime.Shutdown(ctx); err != nil {
		slog.Warn("GUI telemetry shutdown failed")
	}
}

func (a *App) secondLaunch(window Window, args []string) {
	window.Unminimise()
	window.Show()
	window.Emit(launchRequestEvent, args...)
}

func (a *App) Quit() {
	a.approveClose()
	if a.quit != nil {
		a.quit()
	}
}

func (a *App) beforeClose(window Window, approved bool) bool {
	if approved {
		return false
	}
	window.Emit(closeRequestEvent)
	return true
}
