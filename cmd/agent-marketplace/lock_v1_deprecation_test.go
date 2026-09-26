package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestLoadLayerLockWarnsOnV1(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	path := filepath.Join(t.TempDir(), "lock.yaml")
	writeFile(t, path, "apiVersion: agent-marketplace-lock/v1\nscope: user\nregistries: []\n")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	doc, _, exists, err := loadLayerLock(path, lockfile.ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || doc == nil || doc.APIVersion != lockfile.APIVersionV1 {
		t.Fatalf("doc = %#v exists=%v, want v1 lock", doc, exists)
	}
	var warnings []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("slog record is not JSON: %q: %v", line, err)
		}
		if record["code"] == "lock.version-deprecated" {
			warnings = append(warnings, record)
		}
	}
	if len(warnings) != 1 {
		t.Fatalf("v1 warnings = %d, want 1: %s", len(warnings), logs.String())
	}
	if warnings[0]["path"] != path || warnings[0]["scope"] != "user" {
		t.Fatalf("warning attrs = %#v, want path %q scope user", warnings[0], path)
	}
}

func TestLoadLayerLockSkipsWarningOnV2(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	path := filepath.Join(t.TempDir(), "lock.yaml")
	writeFile(t, path, "apiVersion: agent-marketplace-lock/v2\nscope: user\nregistries: []\n")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	if _, _, _, err := loadLayerLock(path, lockfile.ScopeUser); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "lock.version-deprecated") {
		t.Fatalf("unexpected deprecation warning: %s", logs.String())
	}
}
