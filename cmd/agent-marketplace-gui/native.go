//go:build cgo && (darwin || linux)

package main

import (
	"context"
	"embed"

	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

type nativeWindow struct {
	ctx context.Context
}

func (w nativeWindow) Unminimise() { wailsruntime.WindowUnminimise(w.ctx) }
func (w nativeWindow) Show()       { wailsruntime.WindowShow(w.ctx) }
func (w nativeWindow) Emit(event string, args ...string) {
	data := make([]any, len(args))
	for i, arg := range args {
		data[i] = arg
	}
	wailsruntime.EventsEmit(w.ctx, event, data...)
}

type lifecycleHooks struct {
	onStartup  func(context.Context)
	onDomReady func(context.Context)
}

func (app *App) appOptions(hooks lifecycleHooks) *options.App {
	var windowCtx context.Context
	return &options.App{
		Title:  "Agent Marketplace",
		Width:  1280,
		Height: 832,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: app.handler(),
		},
		Bind: []any{app},
		OnStartup: func(ctx context.Context) {
			windowCtx = ctx
			if hooks.onStartup != nil {
				hooks.onStartup(ctx)
			}
		},
		OnDomReady: hooks.onDomReady,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: singleInstanceID,
			OnSecondInstanceLaunch: func(second options.SecondInstanceData) {
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
	defer app.close()
	return wails.Run(app.appOptions(lifecycleHooks{}))
}
