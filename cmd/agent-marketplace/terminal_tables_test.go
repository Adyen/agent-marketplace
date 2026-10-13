package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func TestCLITableAndInstallDataIsTerminalSafe(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	bad := "CONTROL\x1b[31mVALUE\x1b[0m\t\n\r\u202e"
	for _, tc := range []struct {
		name   string
		write  func(io.Writer) error
		copies int
	}{
		{"list", func(w io.Writer) error {
			return writeListTable(w, []listRow{{Registry: bad, Kind: bad, Name: bad, Tags: bad, Status: bad, Relation: bad}}, "VIA")
		}, 6},
		{"status", func(w io.Writer) error {
			return writeStatusTable(w, "CONTROL TABLE", []string{"ONE", "TWO"}, [][]string{{bad, bad}})
		}, 2},
		{"collection", func(w io.Writer) error {
			return writeInstallResult(w, &pipeline.InstallResult{IsCollection: true, CollectionLabel: bad}, pipeline.ScopeRepo, false, false)
		}, 1},
		{"resource", func(w io.Writer) error {
			return writeInstallResult(w, &pipeline.InstallResult{Installed: []string{bad}}, pipeline.ScopeRepo, false, false)
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := tc.write(&out); err != nil {
				t.Fatal(err)
			}
			got := out.String()
			if strings.ContainsAny(got, "\x1b\r\u202e") {
				t.Fatalf("unsafe display data: %q", got)
			}
			want := `CONTROL\x1b[31mVALUE\x1b[0m\t\n\r\u202e`
			if strings.Count(got, want) != tc.copies {
				t.Fatalf("data not faithfully escaped: %q", got)
			}
		})
	}
}

func TestCLIDisplayLeavesSourceRowsUnchanged(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	rows := [][]string{{"name\x1b", "ref\n"}}
	var out bytes.Buffer
	if err := writeStatusTable(&out, "TITLE", []string{"NAME", "REF"}, rows); err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "name\x1b" || rows[0][1] != "ref\n" {
		t.Fatal("display changed source data")
	}
	out.Reset()
	if err := writeListTable(&out, []listRow{{Registry: "registry", Kind: "skill", Name: "control", Tags: "safe", Status: "selected"}}, "VIA"); err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"REGISTRY", "registry", "control", "selected"} {
		if !strings.Contains(out.String(), plain) {
			t.Fatalf("ordinary output lost: %q", out.String())
		}
	}
}

func TestTerminalSafeTextPreservesPrintableSyntax(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct{ input, want string }{
		{`agent-marketplace install "fixture" --path "a\b"`, `agent-marketplace install "fixture" --path "a\b"`},
		{"\"quoted\"\\literal\x1b[31m\r\n\u202e", `"quoted"\literal\x1b[31m\r\n\u202e`},
		{"bad\xff\x85\u0085", `bad\xff\x85\u0085`},
	} {
		if got := terminalSafeText(tc.input); got != tc.want {
			t.Fatalf("terminalSafeText(%q)=%q want %q", tc.input, got, tc.want)
		}
	}
}
