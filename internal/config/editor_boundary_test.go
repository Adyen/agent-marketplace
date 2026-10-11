package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"gopkg.in/yaml.v3"
)

// boundaryLayer builds a repository layer fixture and returns the repo root
// and its .agents directory.
func boundaryLayer(t *testing.T) (string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	agents := filepath.Join(repo, ".agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	return repo, agents
}

// boundaryUserLayer builds the user layer fixture inside a sandboxed HOME.
func boundaryUserLayer(t *testing.T) (string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	return home, filepath.Join(home, ".agents")
}

func boundaryDoc(t *testing.T, configPath string) *yaml.Node {
	t.Helper()
	doc, _, err := readOrInitConfig(configPath)
	if err != nil {
		t.Fatalf("readOrInitConfig: %v", err)
	}
	return doc
}

func assertNoConfigTempResidue(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("config temp residue in %s: %s", dir, entry.Name())
		}
	}
}

// spec: config-mutation/approved-layer-links-receive-real-updates
func TestWriteConfigDocumentPreservesUserDotfileLink(t *testing.T) {
	home, agents := boundaryUserLayer(t)
	target := filepath.Join(home, "dotfiles", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("apiVersion: agent-marketplace/v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}

	_, mode, err := readOrInitConfig(link)
	if err != nil {
		t.Fatalf("readOrInitConfig through dotfile link: %v", err)
	}
	if mode != 0o600 {
		t.Fatalf("mode = %o, want 600", mode)
	}
	if err := SetHarnessesInConfig(link, []string{"factory", "windsurf"}); err != nil {
		t.Fatalf("SetHarnessesInConfig through dotfile link: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced: %v, %v", info, err)
	}
	if text, err := os.Readlink(link); err != nil || text != target {
		t.Fatalf("link text = %q, %v", text, err)
	}
	if os.SameFile(linkInfo, info) != true {
		t.Fatal("link identity changed")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "factory") {
		t.Fatalf("target content = %q", data)
	}
	assertNoConfigTempResidue(t, agents)
	assertNoConfigTempResidue(t, filepath.Dir(target))
}

// spec: config-mutation/approved-layer-links-receive-real-updates
func TestWriteConfigDocumentPreservesContainedRepositoryLink(t *testing.T) {
	_, agents := boundaryLayer(t)
	target := filepath.Join(agents, "shared", "base.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("apiVersion: agent-marketplace/v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(filepath.Join("shared", "base.yaml"), link); err != nil {
		t.Fatal(err)
	}

	if _, _, err := readOrInitConfig(link); err != nil {
		t.Fatalf("readOrInitConfig through contained link: %v", err)
	}
	if err := SetHarnessesInConfig(link, []string{"factory"}); err != nil {
		t.Fatalf("SetHarnessesInConfig through contained link: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced: %v, %v", info, err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "factory") {
		t.Fatalf("target content = %q", data)
	}
	assertNoConfigTempResidue(t, agents)
	assertNoConfigTempResidue(t, filepath.Dir(target))
}

func TestWriteConfigDocumentRejectsEscapingRepositoryLink(t *testing.T) {
	_, agents := boundaryLayer(t)
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("untouched\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	doc, mode, err := readOrInitConfig(link)
	if err == nil {
		if writeErr := writeConfigDocument(link, mode, doc); !errors.Is(writeErr, dependencytxn.ErrUnsafePath) {
			t.Fatalf("writeConfigDocument error = %v, want ErrUnsafePath", writeErr)
		}
	} else if !errors.Is(err, dependencytxn.ErrUnsafePath) {
		t.Fatalf("readOrInitConfig error = %v, want ErrUnsafePath", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "untouched\n" {
		t.Fatalf("outside file changed: %q, %v", data, err)
	}
	assertNoConfigTempResidue(t, agents)
}

// TestConfigDescriptorParentReplacement pins the deterministic replacement
// barriers of the managed config write path: swapping the .agents parent or
// the destination leaf at capture, staging open, or publication must fail
// with a stable conflict, remove the staged temp, and leave the replacement
// tree and the captured tree untouched.
// spec: config-mutation/replaced-publication-parents-and-leaves-are-refused
func TestConfigDescriptorParentReplacement(t *testing.T) {
	const oldContent = "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n"

	swapAgents := func(t *testing.T, repo string) (live, aside string) {
		t.Helper()
		live = filepath.Join(repo, ".agents")
		aside = filepath.Join(repo, ".agents-aside")
		if err := os.Rename(live, aside); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(live, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(live, "config.yaml"), []byte("replacement tree\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return live, aside
	}

	for _, tc := range []struct {
		name  string
		stage dependencytxn.FaultStage
	}{
		{name: "capture", stage: dependencytxn.FaultAfterCAS},
		{name: "open", stage: dependencytxn.FaultAfterStage},
		{name: "publication", stage: dependencytxn.FaultBeforePublish},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, agents := boundaryLayer(t)
			configPath := filepath.Join(agents, "config.yaml")
			if err := os.WriteFile(configPath, []byte(oldContent), 0o644); err != nil {
				t.Fatal(err)
			}
			doc := boundaryDoc(t, configPath)

			var live, aside string
			swapped := false
			err := writeConfigDocumentWithOptions(configPath, 0o644, doc, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
				if point.Stage == tc.stage && !swapped {
					swapped = true
					live, aside = swapAgents(t, repo)
				}
				return nil
			}))
			if !swapped {
				t.Fatalf("fault hook did not fire at %s", tc.stage)
			}
			if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
				t.Fatalf("error = %v, want ErrGenerationConflict", err)
			}
			data, readErr := os.ReadFile(filepath.Join(live, "config.yaml"))
			if readErr != nil || string(data) != "replacement tree\n" {
				t.Fatalf("replacement tree changed: %q, %v", data, readErr)
			}
			data, readErr = os.ReadFile(filepath.Join(aside, "config.yaml"))
			if readErr != nil || string(data) != oldContent {
				t.Fatalf("captured tree changed: %q, %v", data, readErr)
			}
			assertNoConfigTempResidue(t, live)
			assertNoConfigTempResidue(t, aside)
		})
	}

	t.Run("leaf", func(t *testing.T) {
		_, agents := boundaryLayer(t)
		configPath := filepath.Join(agents, "config.yaml")
		if err := os.WriteFile(configPath, []byte(oldContent), 0o644); err != nil {
			t.Fatal(err)
		}
		doc := boundaryDoc(t, configPath)

		replaced := false
		intruder := filepath.Join(agents, "intruder.yaml")
		err := writeConfigDocumentWithOptions(configPath, 0o644, doc, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
			if point.Stage == dependencytxn.FaultBeforePublish && !replaced {
				replaced = true
				if err := os.WriteFile(intruder, []byte("intruder\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(intruder, configPath); err != nil {
					t.Fatal(err)
				}
			}
			return nil
		}))
		if !replaced {
			t.Fatal("fault hook did not fire")
		}
		if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
			t.Fatalf("error = %v, want ErrGenerationConflict", err)
		}
		data, readErr := os.ReadFile(configPath)
		if readErr != nil || string(data) != "intruder\n" {
			t.Fatalf("intruder changed: %q, %v", data, readErr)
		}
		assertNoConfigTempResidue(t, agents)
	})
}
