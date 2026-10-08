package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemNativeMarkerMergeInputRefusesUnsafeClaims(t *testing.T) {
	for _, shape := range []string{"hardlink", "symlink"} {
		t.Run(shape, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.MustRunAM("init", "--harnesses", "factory")
			source := filepath.Join(env.workspace, "source")
			env.WriteFile(filepath.Join(source, ".claude-plugin", "plugin.json"), `{"name":"merge-probe"}`)
			env.WriteFile(filepath.Join(source, ".mcp.json"), `{"mcpServers":{"managed":{"command":"inert desired"}}}`)
			env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  fixture:\n    directory: ./source\nresources:\n  fixture:\n    all: true\n")
			env.MustRunAM("sync")
			out := filepath.Join(env.workspace, ".factory")
			path := filepath.Join(out, "mcp.json")
			if data, err := os.ReadFile(path); err != nil || !bytes.Contains(data, []byte("inert desired")) || !bytes.Contains(data, []byte("agent-marketplace")) {
				t.Fatalf("actual native marker control missing: %q %v", data, err)
			}
			retained := filepath.Join(env.home, "retained.json")
			if err := os.Rename(path, retained); err != nil {
				t.Fatal(err)
			}
			keep := filepath.Join(out, "keep.txt")
			env.WriteFile(keep, "inert unrelated sibling")
			foreign := filepath.Join(env.home, "foreign.json")
			env.WriteFile(foreign, `{"mcpServers":{"manual":{"command":"inert"}},"ambiguous":1,"ambiguous":2}`)
			if shape == "symlink" {
				if err := os.Symlink(foreign, path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Link(foreign, path); err != nil {
				t.Fatal(err)
			}
			foreignBefore, targetBefore, keepBefore := captureDest(t, foreign), captureDest(t, path), captureDest(t, keep)
			for _, args := range [][]string{{"sync", "--check"}, {"sync"}} {
				result := env.RunAM(args...)
				env.AssertExitCode(result, 1)
				env.AssertOutputContains(result, "materialize.write.error")
				env.AssertOutputNotContains(result, "json.duplicate-key")
				requireDestUntouched(t, foreignBefore, captureDest(t, foreign))
				requireDestUntouched(t, targetBefore, captureDest(t, path))
				requireDestUntouched(t, keepBefore, captureDest(t, keep))
				if stages, err := filepath.Glob(filepath.Join(out, ".tmp-*")); err != nil || len(stages) != 0 {
					t.Fatalf("native marker refusal left staging: %v %v", stages, err)
				}
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			env.WriteFile(path, "{\n // handwritten control\n \"mcpServers\": {\"manual\": {\"command\": \"inert manual\"}},\n \"theme\": \"preserve\"\n}\n")
			env.MustRunAM("sync")
			if data, err := os.ReadFile(path); err != nil || !bytes.Contains(data, []byte("inert desired")) || !bytes.Contains(data, []byte("handwritten control")) || !bytes.Contains(data, []byte("inert manual")) {
				t.Fatalf("regular native control did not merge desired and handwritten content: %q %v", data, err)
			}
			requireDestUntouched(t, keepBefore, captureDest(t, keep))
		})
	}
}
