package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/Adyen/agent-marketplace/internal/localapi"
	"github.com/Adyen/agent-marketplace/internal/session"
)

const singleInstanceID = "ef334250-9d55-4fdc-8c94-a6b48b8a3c27"

const launchRequestEvent = "desktop:launch-request"

const closeRequestEvent = "desktop:close-request"

var version = "dev"

type Bootstrap struct {
	Credential string `json:"credential"`
	Version    string `json:"version"`
}

type Window interface {
	Unminimise()
	Show()
	Emit(event string, args ...string)
}

type App struct {
	api           *localapi.API
	version       string
	quit          func()
	closeApproved atomic.Bool
}

func NewApp(ctx context.Context, lifecycle session.DependencyLifecycle) (*App, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	api, err := localapi.NewUser(ctx, homeDir, lifecycle, nil, localapi.WithVersion(version))
	if err != nil {
		return nil, err
	}
	return &App{api: api, version: version}, nil
}

func (a *App) Bootstrap() Bootstrap {
	return Bootstrap{Credential: a.api.Credential(), Version: a.version}
}

func (a *App) handler() http.Handler { return a.api.Handler() }

func (a *App) close() { a.api.Close() }

func (a *App) secondLaunch(window Window, args []string) {
	window.Unminimise()
	window.Show()
	window.Emit(launchRequestEvent, args...)
}

func (a *App) Quit() {
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
