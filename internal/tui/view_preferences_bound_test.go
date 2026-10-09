package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// spec: tui-startup/missing-invalid-unreadable-and-failed-save-preference-files
func TestViewPreferencesUseEstablishedReadBound(t *testing.T) {
	home := isolatedPrefsHome(t)
	path := prefsFile(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"version":1,"view":{"groupBy":"source","thenBy":"kind"}}`)
	data := append(body, bytes.Repeat([]byte(" "), (4<<20)-len(body))...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, warning := loadViewPreferences(home)
	if warning != "" || got.GroupBy != groupingAxis("source") || got.ThenBy != groupingAxis("kind") {
		t.Fatalf("valid preferences at established bound: got=%+v warning=%q", got, warning)
	}
	data = append(data, ' ')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, warning = loadViewPreferences(home)
	if warning == "" || got.GroupBy != groupingAxis("status") || got.ThenBy != groupingAxis("none") {
		t.Fatalf("oversized preferences must warn and fall back: got=%+v warning=%q", got, warning)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, data) {
		t.Fatalf("preference read changed oversized state bytes: err=%v", err)
	}
}
