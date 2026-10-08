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

	opts := app.appOptions(lifecycleHooks{})
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
	if len(opts.Bind) != 1 {
		t.Fatalf("bound objects = %d, want 1", len(opts.Bind))
	}
	bound := reflect.TypeOf(opts.Bind[0])
	var methods []string
	for i := 0; i < bound.NumMethod(); i++ {
		methods = append(methods, bound.Method(i).Name)
	}
	if len(methods) != 1 || methods[0] != "Bootstrap" {
		t.Fatalf("bound method set = %v, want [Bootstrap]", methods)
	}
}
