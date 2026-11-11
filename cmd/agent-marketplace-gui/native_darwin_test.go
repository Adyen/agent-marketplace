//go:build cgo && darwin

package main

import (
	"context"
	"debug/macho"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/session"
)

// spec: gui-distribution/packaged-app-and-unbundled-binary-are-named-agent-marketplace
func TestNativeApplicationName(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := macho.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	section := file.Section("__info_plist")
	if section == nil || section.Seg != "__TEXT" {
		t.Fatal("unbundled executable has no __TEXT,__info_plist section")
	}
	embedded, err := section.Data()
	if err != nil {
		t.Fatal(err)
	}
	assertApplicationNamePlist(t, embedded)

	packaged, err := os.ReadFile(filepath.Join("..", "..", "packaging", "gui", "macos", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	assertApplicationNamePlist(t, packaged)

	newHostFixture(t)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)
	if title := app.appOptions(lifecycleHooks{}).Title; title != "Agent Marketplace" {
		t.Fatalf("window title = %q, want Agent Marketplace", title)
	}
}

func assertApplicationNamePlist(t *testing.T, data []byte) {
	t.Helper()
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, key := range []string{"CFBundleName", "CFBundleDisplayName"} {
		if !strings.Contains(text, "<key>"+key+"</key>\n\t<string>Agent Marketplace</string>") {
			t.Fatalf("plist has no %s = Agent Marketplace: %s", key, text)
		}
	}
}
