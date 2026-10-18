package ingest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func FuzzStoreFindingOffsetsRetainLegacyLocations(f *testing.F) {
	f.Add([]byte("a\n\u00e9x\nz"), 2, 2)
	f.Add([]byte("\xff\n\x80"), 1, 5)
	f.Add([]byte{}, -1, -1)
	f.Fuzz(func(t *testing.T, data []byte, line, column int) {
		if len(data) > 4096 {
			return
		}
		support := append([]byte("prefix\n"), data...)
		var findings []analyze.Finding
		for _, position := range [][2]int{
			{line, column}, {1, 1}, {2, int(^uint(0) >> 1)}, {2, 1}, {3, column}, {0, 0},
		} {
			for _, file := range []string{"fixture/body", "support/aux"} {
				findings = append(findings, analyze.Finding{
					Message:     fmt.Sprint(len(findings)),
					SourceRange: &analyze.SourceRange{File: file, Line: position[0], Column: position[1]},
				})
			}
		}
		stored := toStoreFindings(findings, "SKILL.md", data, analyze.FileSet{"aux": support})
		for i, finding := range findings {
			content := data
			if finding.SourceRange.File == "support/aux" {
				content = support
			}
			start, end := legacyFindingByteOffsets(content, finding.SourceRange.Line, finding.SourceRange.Column)
			if stored[i].Message != finding.Message || stored[i].Location.StartByte != int64(start) || stored[i].Location.EndByte != int64(end) {
				t.Fatalf("finding %d: got %+v, want byte range %d:%d", i, stored[i].Location, start, end)
			}
		}
	})
}

// Frozen pre-optimization behavior is the compatibility oracle, including
// out-of-line columns, invalid UTF-8 and ranges beyond EOF.
func legacyFindingByteOffsets(content []byte, line, column int) (int, int) {
	if line < 1 {
		return 0, 0
	}
	start := 0
	for current := 1; current < line; current++ {
		n := bytes.IndexByte(content[start:], '\n')
		if n < 0 {
			return len(content), len(content)
		}
		start += n + 1
	}
	for current := 1; start < len(content) && current < column; current++ {
		_, size := utf8.DecodeRune(content[start:])
		start += size
	}
	if start >= len(content) {
		return start, start
	}
	_, size := utf8.DecodeRune(content[start:])
	return start, start + size
}

func TestStoreFindingOffsetsPreserveSourceRoutingAndOrder(t *testing.T) {
	body := []byte("a\n\u00e9x\nz")
	support := analyze.FileSet{
		"aux":                []byte("\xff\nq\u00e9"),
		"source/body":        []byte("not the body"),
		"source/command":     []byte("not the body"),
		"source/Description": []byte("not the body"),
	}
	for _, request := range []struct {
		file               string
		line, column       int
		path               string
		startByte, endByte int64
	}{
		{"source/body", 3, 1, "SKILL.md", 6, 7},
		{"support/aux", 2, 2, "support/aux", 3, 5},
		{"source/body", 2, 2, "SKILL.md", 4, 5},
		{"source/body", 1, 3, "SKILL.md", 2, 4},
		{"support\\aux", 1, 1, "support/aux", 0, 1},
		{"source/command", 2, 1, "SKILL.md", 2, 4},
		{"source/Description", 2, 1, "SKILL.md", 2, 4},
		{"unknown", 2, 3, "SKILL.md", 5, 6},
		{"source/body", 2, 4, "SKILL.md", 6, 7},
		{"source/body", 2, 5, "SKILL.md", 7, 7},
		{"source/body", 4, 1, "SKILL.md", 7, 7},
		{"source/body", 2, -1, "SKILL.md", 2, 4},
		{"source/body", -1, 2, "SKILL.md", 0, 0},
		{"source/body", 1, int(^uint(0) >> 1), "SKILL.md", 7, 7},
		{"aux", 2, 1, "support/aux", 2, 3},
		{"aux", 2, 3, "support/aux", 5, 5},
		{"aux", 0, 0, "support/aux", 0, 0},
		{"source/body", 2, 2, "SKILL.md", 4, 5},
	} {
		t.Run(fmt.Sprintf("%s/%d/%d", request.file, request.line, request.column), func(t *testing.T) {
			first := analyze.Finding{
				Code: analyze.FindingHTMLComment, Severity: "warn", PolicyOutcome: "allow", Message: "first",
				SourceRange: &analyze.SourceRange{File: request.file, Line: request.line, Column: request.column},
			}
			last := analyze.Finding{Code: analyze.FindingTerminalControl, Message: "no source"}
			got := toStoreFindings([]analyze.Finding{first, last, first}, "SKILL.md", body, support)
			wantFinding := store.Finding{
				Code: first.Code, Severity: first.Severity, Policy: first.PolicyOutcome, Message: first.Message,
				Location: &store.FindingLocation{
					Path: request.path, Line: request.line, Column: request.column,
					StartByte: request.startByte, EndByte: request.endByte,
				},
			}
			want := []store.Finding{wantFinding, {Code: last.Code, Message: last.Message}, wantFinding}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("findings = %+v, want %+v", got, want)
			}
		})
	}

	findings := []analyze.Finding{
		{Message: "later body", SourceRange: &analyze.SourceRange{File: "source/body", Line: 3, Column: 1}},
		{Message: "support", SourceRange: &analyze.SourceRange{File: "aux", Line: 2, Column: 2}},
		{Message: "early body", SourceRange: &analyze.SourceRange{File: "source/body", Line: 1, Column: 1}},
	}
	got := toStoreFindings(findings, "SKILL.md", body, support)
	for i, start := range []int64{6, 3, 0} {
		if got[i].Message != findings[i].Message || got[i].Location.StartByte != start {
			t.Fatalf("finding %d was reordered or used another source: %+v", i, got[i])
		}
	}
}

func TestBuildRegistryRevisionRetainsDenseFindingByteRanges(t *testing.T) {
	const count = 64
	repo := createIngestRepo(t)
	content := "---\nname: review\ndescription: Review code\nlicense: MIT\n---\n" +
		strings.Repeat("\u00e9<!-- inert note -->\n", count)
	if err := os.WriteFile(filepath.Join(repo, ".agents", "skills", "review", "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAllIngestRepo(t, repo, "dense inert findings")
	commit, err := repoCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	source := store.Registry{
		ID: "src-test", Slug: "example-source", Ref: "main", Origins: []string{"file://" + repo},
		Acquisition: store.AcquisitionMirror, License: "MIT",
		Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}},
	}
	revision, err := BuildRegistryRevision(context.Background(), testConfig(t, repo), "mp-test", source, repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	var body []byte
	for _, blob := range revision.InspectionBlobs {
		if bytes.Contains(blob.Data, []byte("<!-- inert note -->")) {
			body = blob.Data
		}
	}
	if len(body) == 0 {
		t.Fatal("missing primary inspection text")
	}
	seen := 0
	for _, assessment := range revision.Assessments {
		if assessment.Coordinate.Kind != "skill" || assessment.Coordinate.Name != "review" {
			continue
		}
		for _, finding := range assessment.Findings {
			if finding.Code != analyze.FindingHTMLComment {
				continue
			}
			location := finding.Location
			if location == nil || location.Path != inspection.PrimarySkillPath ||
				location.StartByte < 0 || location.EndByte != location.StartByte+1 || location.EndByte > int64(len(body)) {
				t.Fatalf("invalid finding location: %+v", location)
			}
			if body[location.StartByte] != '<' || location.Column != 2 ||
				location.Line != bytes.Count(body[:location.StartByte], []byte{'\n'})+1 {
				t.Fatalf("finding does not address its comment: %+v", location)
			}
			seen++
		}
	}
	if seen != count {
		t.Fatalf("stored comment findings = %d, want %d", seen, count)
	}
}
