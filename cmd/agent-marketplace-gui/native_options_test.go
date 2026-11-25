//go:build cgo && (darwin || linux)

package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
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
	type eventRegistration struct {
		ctx  context.Context
		name string
	}
	var registrations []eventRegistration
	opts := app.appOptions(lifecycleHooks{
		eventsOn: func(ctx context.Context, name string, _ func(...any)) func() {
			registrations = append(registrations, eventRegistration{ctx: ctx, name: name})
			return func() {}
		},
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
	if opts.Mac == nil || !reflect.DeepEqual(opts.Mac.TitleBar, mac.TitleBarHiddenInset()) {
		t.Fatalf("mac title bar = %+v, want hidden-inset", opts.Mac)
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
	wantEvents := []string{sessionEventsOpenEvent, sessionEventsCloseEvent}
	if len(registrations) != len(wantEvents) {
		t.Fatalf("event registrations = %+v, want %v", registrations, wantEvents)
	}
	for i, want := range wantEvents {
		if registrations[i].ctx != startupSentinel || registrations[i].name != want {
			t.Fatalf("event registration %d = %+v, want context %v and event %q", i, registrations[i], startupSentinel, want)
		}
	}
	if len(opts.Bind) != 1 {
		t.Fatalf("bound objects = %d, want 1", len(opts.Bind))
	}
	bound := reflect.TypeOf(opts.Bind[0])
	var methods []string
	for i := 0; i < bound.NumMethod(); i++ {
		methods = append(methods, bound.Method(i).Name)
	}
	if len(methods) != 3 || methods[0] != "Bootstrap" || methods[1] != "ChooseDirectory" || methods[2] != "Quit" {
		t.Fatalf("bound method set = %v, want [Bootstrap ChooseDirectory Quit]", methods)
	}
}

func TestChooseDirectoryBeforeStartupReturnsError(t *testing.T) {
	app := &App{}
	_, err := app.ChooseDirectory("")
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("ChooseDirectory error = %v, want an uninitialized dialog error", err)
	}
}

// spec: gui-configuration/the-dialog-starts-at-an-existing-location-or-home
func TestChooseDirectoryUsesExistingLocationOrHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	existing := filepath.Join(t.TempDir(), "registry")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	selected := t.TempDir()
	var dialogStart string
	dialog := directoryDialog(func(defaultPath string) (string, error) {
		dialogStart = defaultPath
		return selected, nil
	})
	app.directoryDialog.Store(&dialog)

	for _, test := range []struct {
		name     string
		location string
		want     string
	}{
		{name: "existing directory", location: existing, want: existing},
		{name: "blank location", location: "", want: home},
		{name: "nonexistent location", location: filepath.Join(t.TempDir(), "missing"), want: home},
		{name: "file location", location: file, want: home},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, err := app.ChooseDirectory(test.location)
			if err != nil {
				t.Fatal(err)
			}
			if path != selected {
				t.Fatalf("selected path = %q, want %q", path, selected)
			}
			if dialogStart != test.want {
				t.Fatalf("dialog start = %q, want %q", dialogStart, test.want)
			}
		})
	}

	dialog = func(defaultPath string) (string, error) {
		dialogStart = defaultPath
		return "", nil
	}
	app.directoryDialog.Store(&dialog)
	path, err := app.ChooseDirectory(existing)
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("cancelled path = %q, want empty", path)
	}
	if dialogStart != existing {
		t.Fatalf("cancelled dialog start = %q, want %q", dialogStart, existing)
	}
}
