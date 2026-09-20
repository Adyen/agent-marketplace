package harness

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestDevinRepoDestinationContainment(t *testing.T) {
	for _, name := range []string{"outside", "dangling-outside", "self", "inside", "checkout-alias", "normal"} {
		t.Run(name, func(t *testing.T) {
			repo, outside := t.TempDir(), t.TempDir()
			inside := filepath.Join(repo, "native")
			if err := os.Mkdir(inside, 0755); err != nil {
				t.Fatal(err)
			}
			destination := outside
			switch name {
			case "dangling-outside":
				destination = filepath.Join(outside, "missing")
			case "self":
				destination = repo
			case "inside", "checkout-alias":
				destination = inside
			}
			if name != "normal" {
				if err := os.Symlink(destination, filepath.Join(repo, ".devin")); err != nil {
					t.Fatal(err)
				}
			}
			input := repo
			if name == "checkout-alias" {
				input = filepath.Join(t.TempDir(), "checkout")
				if err := os.Symlink(repo, input); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ResolveDestinations(hcc.TargetDevin, false, input)
			blocked := name == "outside" || name == "dangling-outside" || name == "self"
			if blocked {
				if !errors.Is(err, os.ErrPermission) {
					t.Fatalf("escape not refused with permission identity: %v %+v", err, got)
				}
				return
			}
			want := filepath.Join(input, ".devin")
			if err != nil || len(got) != 1 || got[0].Root != want {
				t.Fatalf("valid root failed: %v %+v; want %s", err, got, want)
			}
		})
	}
}

func TestDevinMachineRootAliasRemainsAllowed(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config", "devin")
	if err := os.MkdirAll(filepath.Dir(root), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveDestinations(hcc.TargetDevin, true, t.TempDir())
	if err != nil || len(got) != 1 || got[0].Root != root {
		t.Fatalf("machine alias changed: %v %+v", err, got)
	}
}
