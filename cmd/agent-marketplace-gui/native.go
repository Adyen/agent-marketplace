//go:build cgo && (darwin || linux)

package main

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"

	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend
var assets embed.FS

type nativeWindow struct {
	ctx context.Context
}

type directoryDialog func(string) (string, error)

func (w nativeWindow) Unminimise() { wailsruntime.WindowUnminimise(w.ctx) }
func (w nativeWindow) Show()       { wailsruntime.WindowShow(w.ctx) }
func (w nativeWindow) Emit(event string, args ...string) {
	data := make([]any, len(args))
	for i, arg := range args {
		data[i] = arg
	}
	wailsruntime.EventsEmit(w.ctx, event, data...)
}

type nativeState struct {
	closeApproved   atomic.Bool
	directoryDialog atomic.Pointer[directoryDialog]
}

func (s *nativeState) approveClose() {
	s.closeApproved.Store(true)
}

func (app *App) ChooseDirectory(location string) (string, error) {
	dialog := app.directoryDialog.Load()
	if dialog == nil {
		return "", fmt.Errorf("native directory dialog is not initialized")
	}

	defaultDirectory := location
	if info, err := os.Stat(location); err != nil || !info.IsDir() {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		defaultDirectory = home
	}
	return (*dialog)(defaultDirectory)
}

type lifecycleHooks struct {
	eventsOn   func(context.Context, string, func(...any)) func()
	onStartup  func(context.Context)
	onDomReady func(context.Context)
}

func (app *App) appOptions(hooks lifecycleHooks) *options.App {
	eventsOn := hooks.eventsOn
	if eventsOn == nil {
		eventsOn = wailsruntime.EventsOn
	}

	var windowCtx context.Context
	app.quit = func() {
		if windowCtx == nil {
			return
		}
		wailsruntime.Quit(windowCtx)
	}
	return &options.App{
		Title:     "Agent Marketplace",
		Width:     1280,
		Height:    832,
		MinWidth:  760,
		MinHeight: 520,
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: app.handler(),
		},
		Bind: []any{app},
		OnStartup: func(ctx context.Context) {
			windowCtx = ctx
			dialog := directoryDialog(func(defaultDirectory string) (string, error) {
				return wailsruntime.OpenDirectoryDialog(ctx, wailsruntime.OpenDialogOptions{
					DefaultDirectory: defaultDirectory,
				})
			})
			app.directoryDialog.Store(&dialog)
			eventsOn(ctx, sessionEventsOpenEvent, func(data ...any) {
				if len(data) < 2 {
					return
				}
				id, idOK := data[0].(string)
				credential, credentialOK := data[1].(string)
				if idOK && credentialOK {
					app.openEventStream(nativeWindow{ctx: ctx}, id, credential)
				}
			})
			eventsOn(ctx, sessionEventsCloseEvent, func(data ...any) {
				if len(data) == 0 {
					return
				}
				if id, ok := data[0].(string); ok {
					app.closeEventStream(id)
				}
			})
			controlClose, err := startE2EControl(app)
			if err != nil {
				slog.Error("GUI E2E control server could not start", "err", err)
				wailsruntime.Quit(ctx)
				return
			}
			app.e2eControlClose = controlClose
			if err := app.updater.CleanupBackup(); err != nil {
				slog.Warn("could not remove previous GUI update backup", "err", err)
			}
			if hooks.onStartup != nil {
				hooks.onStartup(ctx)
			}
		},
		OnDomReady: hooks.onDomReady,
		OnBeforeClose: func(ctx context.Context) bool {
			return app.beforeClose(nativeWindow{ctx: ctx}, app.closeApproved.Swap(false))
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: singleInstanceID,
			OnSecondInstanceLaunch: func(second options.SecondInstanceData) {
				if windowCtx == nil {
					return
				}
				app.secondLaunch(nativeWindow{ctx: windowCtx}, second.Args)
			},
		},
	}
}

func run() error {
	ctx := context.Background()
	app, err := NewApp(ctx, session.RuntimeLifecycle())
	if err != nil {
		return err
	}
	defer closeWithin(app.close, shutdownTimeout)
	return wails.Run(app.appOptions(lifecycleHooks{}))
}
