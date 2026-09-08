package catalog

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestHookSupportFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z.sh", "a.sh", "hooks.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := HookSupportFiles(filepath.Join(root, "hooks.json"))
	if err != nil || !reflect.DeepEqual(got, []string{"a.sh", "z.sh"}) {
		t.Fatalf("support source set = %v, %v", got, err)
	}
	if got, err := HookSupportFiles(""); err != nil || len(got) != 0 {
		t.Fatalf("empty source = %v, %v", got, err)
	}
	if _, err := HookSupportFiles(filepath.Join(root, "absent", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("missing source directory = %v", err)
	}
}
