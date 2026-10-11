//go:build cgo && (darwin || linux)

package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/session"
)

func TestNativeAppOptionsWiring(t *testing.T) {
	newHostFixture(t)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)

	var startupCalls, domReadyCalls int
	var startupCtx, domReadyCtx context.Context
	opts := app.appOptions(lifecycleHooks{
		onStartup: func(ctx context.Context) {
			startupCalls++
			startupCtx = ctx
		},
		onDomReady: func(ctx context.Context) {
			domReadyCalls++
			domReadyCtx = ctx
		},
	})
	if opts.AssetServer == nil || opts.AssetServer.Handler == nil {
		t.Fatal("asset server handler missing from production options")
	}
	if opts.SingleInstanceLock == nil || opts.SingleInstanceLock.UniqueId != singleInstanceID {
		t.Fatalf("single-instance lock misconfigured: %+v", opts.SingleInstanceLock)
	}
	if opts.SingleInstanceLock.OnSecondInstanceLaunch == nil {
		t.Fatal("second-launch callback missing")
	}
	if opts.OnStartup == nil || opts.OnDomReady == nil {
		t.Fatal("lifecycle hooks not wired")
	}
	if opts.OnBeforeClose == nil {
		t.Fatal("before-close hook missing")
	}
	if app.quit == nil {
		t.Fatal("native quit callback not installed")
	}
	startupSentinel := context.WithValue(context.Background(), struct{ k string }{"startup"}, 1)
	domReadySentinel := context.WithValue(context.Background(), struct{ k string }{"domReady"}, 1)
	opts.OnStartup(startupSentinel)
	opts.OnDomReady(domReadySentinel)
	if startupCalls != 1 || startupCtx != startupSentinel {
		t.Fatalf("startup hook calls = %d ctx = %v, want 1 invocation with the supplied context", startupCalls, startupCtx)
	}
	if domReadyCalls != 1 || domReadyCtx != domReadySentinel {
		t.Fatalf("dom-ready hook calls = %d ctx = %v, want 1 invocation with the supplied context", domReadyCalls, domReadyCtx)
	}
	if len(opts.Bind) != 1 {
		t.Fatalf("bound objects = %d, want 1", len(opts.Bind))
	}
	bound := reflect.TypeOf(opts.Bind[0])
	var methods []string
	for i := 0; i < bound.NumMethod(); i++ {
		methods = append(methods, bound.Method(i).Name)
	}
	if len(methods) != 2 || methods[0] != "Bootstrap" || methods[1] != "Quit" {
		t.Fatalf("bound method set = %v, want [Bootstrap Quit]", methods)
	}
}
