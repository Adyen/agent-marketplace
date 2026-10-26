package resourcediff

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// The benchmarks below measure pure in-memory comparison work with no disk or
// network I/O: callers hand resourcediff already-read bytes.

func BenchmarkUnifiedDiffLongLines(b *testing.B) {
	var oldDoc, newDoc strings.Builder
	for i := 0; i < 200; i++ {
		oldDoc.WriteString(strings.Repeat("old content ", 40) + strconv.Itoa(i) + "\n")
		newDoc.WriteString(strings.Repeat("new content ", 40) + strconv.Itoa(i) + "\n")
	}
	oldData, newData := []byte(oldDoc.String()), []byte(newDoc.String())
	b.ReportAllocs()
	for b.Loop() {
		_ = Unified("SKILL.md", "SKILL.md", oldData, newData)
	}
}

func BenchmarkCompareManySupportFiles(b *testing.B) {
	defn := ir.NewSkill("runner", "Runs", "Run the tool.", []string{"runner"}, nil, ir.SourceRef{})
	id := Identity{Scope: "repo", Kind: "skill", Name: "runner", SourceURL: "https://git.example/reg.git", Ref: "main"}
	oldSupport := make(map[string]SupportFile, 64)
	newSupport := make(map[string]SupportFile, 64)
	for i := 0; i < 64; i++ {
		name := "scripts/file" + strconv.Itoa(i) + ".sh"
		oldSupport[name] = SupportFile{Data: []byte("#!/bin/sh\necho v1 " + strconv.Itoa(i) + "\n"), Executable: true}
		newSupport[name] = SupportFile{Data: []byte("#!/bin/sh\necho v2 " + strconv.Itoa(i) + "\n"), Executable: true}
	}
	prior := []Side{{Identity: id, Revision: "r1", Resource: defn, Support: oldSupport}}
	candidate := []Side{{Identity: id, Revision: "r2", Resource: defn, Support: newSupport}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Compare(prior, candidate); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDefinitionRenderSkill(b *testing.B) {
	skill := ir.NewSkillWithOptions("deploy", "Deploy", strings.Repeat("body line\n", 50), []string{"user"}, map[string]any{"x-org": "adyen"}, ir.SourceRef{}, ir.WithSkillAllowedTools([]string{"bash", "git"}), ir.WithSkillModel("sonnet"))
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := Definition(skill); err != nil {
			b.Fatal(err)
		}
	}
}
