package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestMatrixIncludesEveryTarget(t *testing.T) {
	for _, native := range []bool{false, true} {
		got, err := makeMatrix(target.Definitions(), "Example/Marketplace", native)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"claude": "claude", "factory": "droid", "cursor": "agent", "windsurf": "", "gemini": "gemini", "devin": "devin", "codex": "codex"}
		if native {
			delete(want, "windsurf")
		}
		if len(got.Include) != len(want) {
			t.Fatalf("matrix has %d rows, want %d", len(got.Include), len(want))
		}
		for _, row := range got.Include {
			cli, exists := want[row.Target]
			if !exists || row.CLI != cli || row.Image != "ghcr.io/example/marketplace/harness-"+row.Target+":ci" {
				t.Fatalf("unexpected matrix row: %+v", row)
			}
			if cli == "" && (row.Mode != "unavailable" || row.Reason == "") || cli != "" && row.Mode != "probe" {
				t.Fatalf("capability misrepresented: %+v", row)
			}
			delete(want, row.Target)
		}
	}
	definitions := append(target.Definitions(), target.Definition{Target: "new-target", Compiler: true})
	if _, err := makeMatrix(definitions, "owner/repo", true); err == nil {
		t.Fatal("new registry target was silently omitted")
	}
	for _, repository := range []string{"", "owner", "owner/repo/extra", "owner/repo:tag", "owner/repo\nmalicious", "owner/../repo"} {
		if _, err := makeMatrix(target.Definitions(), repository, false); err == nil {
			t.Errorf("unsafe repository accepted: %q", repository)
		}
	}
}

func TestMatrixCommandJSON(t *testing.T) {
	var out strings.Builder
	if err := run([]string{"matrix", "--native", "--repository", "Owner/Repo"}, &out); err != nil {
		t.Fatal(err)
	}
	var document struct {
		Include []map[string]string `json:"include"`
	}
	if err := json.Unmarshal([]byte(out.String()), &document); err != nil || len(document.Include) != 6 {
		t.Fatalf("invalid matrix JSON: %s (%v)", out.String(), err)
	}
	for _, args := range [][]string{{}, {"unknown"}, {"matrix"}, {"matrix", "--repository", "owner/repo", "extra"}, {"matrix", "--bad-flag"}} {
		if err := run(args, &out); err == nil {
			t.Errorf("invalid command accepted: %v", args)
		}
	}
}
