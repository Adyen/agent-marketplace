package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func writeCaptureFixture(tb testing.TB, root, name string, size int) catalog.Entry {
	tb.Helper()
	path := filepath.Join(root, name+".md")
	content := name + strings.Repeat("x", size-len(name))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		tb.Fatal(err)
	}
	return catalog.Entry{Kind: ir.KindSkill, Name: name, Path: path}
}

func captureTraversalFixture(tb testing.TB, count, size int) ([]resolver.ResolvedArtifact, map[string]catalogInfo) {
	tb.Helper()
	root := tb.TempDir()
	selected := writeCaptureFixture(tb, root, "resolved", size-7)
	resolved := []resolver.ResolvedArtifact{{Entry: selected}, {Entry: selected}}
	catalogs := make(map[string]catalogInfo)
	for _, key := range []string{"b", "a"} {
		loaded := catalogInfo{}
		for i := count - 1; i >= 0; i-- {
			loaded.Entries = append(loaded.Entries, writeCaptureFixture(tb, root, fmt.Sprintf("%s-%02d", key, i), size))
		}
		for i := 1; i >= 0; i-- {
			entry := writeCaptureFixture(tb, root, fmt.Sprintf("%s-malformed-%d", key, i), size)
			loaded.Malformed = append(loaded.Malformed, skill.MalformedSkill{DirName: entry.Name, Path: entry.Path})
		}
		loaded.Entries = append(loaded.Entries, selected, catalog.Entry{})
		catalogs[key] = loaded
	}
	return resolved, catalogs
}

func TestCaptureSnapshotDetailSourcesDeterministicBudgets(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		size  int
	}{
		{"entries", 40, 32},
		{"bytes", 5, 256 << 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved, catalogs := captureTraversalFixture(t, tc.count, tc.size)
			wantCaptured := []string{"resolved", "a-malformed-0", "a-malformed-1"}
			for i := 0; i < tc.count; i++ {
				wantCaptured = append(wantCaptured, fmt.Sprintf("a-%02d", i))
			}
			bCaptured := 21
			if tc.name == "bytes" {
				bCaptured = 1
			}
			for i := 0; i < bCaptured; i++ {
				wantCaptured = append(wantCaptured, fmt.Sprintf("b-%02d", i))
			}
			wantOmitted := []string{"b-malformed-0", "b-malformed-1"}
			for i := bCaptured; i < tc.count; i++ {
				wantOmitted = append(wantOmitted, fmt.Sprintf("b-%02d", i))
			}
			slices.Sort(wantCaptured)
			slices.Sort(wantOmitted)
			for permutation := 0; permutation < 4; permutation++ {
				input := make(map[string]catalogInfo)
				keys := []string{"a", "b"}
				if permutation&1 != 0 {
					slices.Reverse(keys)
				}
				for _, key := range keys {
					loaded := catalogs[key]
					loaded.Entries = slices.Clone(loaded.Entries)
					loaded.Malformed = slices.Clone(loaded.Malformed)
					if permutation&2 != 0 {
						slices.Reverse(loaded.Entries)
						slices.Reverse(loaded.Malformed)
					}
					input[key] = loaded
				}
				before := make(map[string]catalogInfo)
				for key, loaded := range input {
					loaded.Entries = slices.Clone(loaded.Entries)
					loaded.Malformed = slices.Clone(loaded.Malformed)
					before[key] = loaded
				}
				got := captureSnapshotDetailSources(resolved, input)
				if !reflect.DeepEqual(input, before) {
					t.Fatal("capture mutated shared catalog slices")
				}
				var captured, omitted []string
				var bytes int
				for path, source := range got {
					name := strings.TrimSuffix(filepath.Base(path), ".md")
					if source.omitted {
						omitted = append(omitted, name)
						if rendered := renderDetailFullContent(newTheme(true), row{DetailSource: source}, 80); rendered != detailCaptureOmittedMarker {
							t.Fatalf("%s: omitted marker = %q", name, rendered)
						}
						continue
					}
					captured = append(captured, name)
					bytes += len(source.content)
					if source.err != nil || !strings.HasPrefix(source.content, name) {
						t.Fatalf("%s: missing captured source: %#v", name, source)
					}
					wantLength, wantMarker := tc.size, ""
					if name == "resolved" {
						wantLength -= 7
					}
					if tc.name == "bytes" && name == "b-00" {
						wantLength = 7
						wantMarker = detailBudgetTruncationMarker
					}
					if len(source.content) != wantLength || source.truncationMarker != wantMarker {
						t.Errorf("%s: length/marker = %d/%q, want %d/%q", name, len(source.content), source.truncationMarker, wantLength, wantMarker)
					}
				}
				slices.Sort(captured)
				slices.Sort(omitted)
				if !slices.Equal(captured, wantCaptured) || !slices.Equal(omitted, wantOmitted) {
					t.Fatalf("permutation %d: captured %v, omitted %v; want captured %v, omitted %v", permutation, captured, omitted, wantCaptured, wantOmitted)
				}
				if tc.name == "bytes" && bytes != 2<<20 {
					t.Fatalf("captured %d bytes, want 2 MiB", bytes)
				}
			}
		})
	}
}

func TestCaptureSnapshotDetailSourcesTieBreaks(t *testing.T) {
	root := t.TempDir()
	var resolved []resolver.ResolvedArtifact
	for i := 0; i < 63; i++ {
		resolved = append(resolved, resolver.ResolvedArtifact{Entry: writeCaptureFixture(t, root, fmt.Sprintf("selected-%02d", i), 32)})
	}
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprintf("malformed=%t", malformed), func(t *testing.T) {
			loaded := catalogInfo{}
			for _, name := range []string{"d", "c", "b", "a"} {
				entry := writeCaptureFixture(t, root, name, 32)
				entry.Kind, entry.Name = ir.KindCommand, "same"
				if name == "a" {
					entry.Kind = ir.KindSkill
				}
				if name == "b" {
					entry.Name = "zzz"
				}
				if malformed {
					entry.Name = "same"
					if name == "a" {
						entry.Name = "zzz"
					}
					loaded.Malformed = append(loaded.Malformed, skill.MalformedSkill{DirName: entry.Name, Path: entry.Path})
				} else {
					loaded.Entries = append(loaded.Entries, entry)
				}
			}
			beforeEntries, beforeMalformed := slices.Clone(loaded.Entries), slices.Clone(loaded.Malformed)
			got := captureSnapshotDetailSources(resolved, map[string]catalogInfo{"catalog": loaded})
			want := "c"
			if malformed {
				want = "b"
			}
			for _, name := range []string{"a", "b", "c", "d"} {
				source := got[filepath.Join(root, name+".md")]
				if source == nil || source.omitted != (name != want) {
					t.Errorf("%s: capture = %#v, want only %s captured", name, source, want)
				}
			}
			if !reflect.DeepEqual(loaded.Entries, beforeEntries) || !reflect.DeepEqual(loaded.Malformed, beforeMalformed) {
				t.Fatal("capture reordered shared slices")
			}
		})
	}
}
