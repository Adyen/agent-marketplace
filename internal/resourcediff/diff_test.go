package resourcediff

import (
	"strings"
	"testing"
)

// spec: update-output/supporting-file-changes-are-not-hidden-by-an-unchanged-definition
func TestUnifiedDiffRendersDeterministicHunks(t *testing.T) {
	oldText := "line one\nline two\nline three\nline four\n"
	newText := "line one\nline two changed\nline three\nline four\nline five\n"
	out := Unified("SKILL.md", "SKILL.md", []byte(oldText), []byte(newText))
	for _, want := range []string{
		"--- a/SKILL.md",
		"+++ b/SKILL.md",
		"@@ -1,4 +1,5 @@",
		"-line two",
		"+line two changed",
		"+line five",
		" line one",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("unified diff missing %q:\n%s", want, out)
		}
	}
	if out != Unified("SKILL.md", "SKILL.md", []byte(oldText), []byte(newText)) {
		t.Fatalf("unified diff must be deterministic for identical input")
	}
}

func TestUnifiedDiffAdditionsAndRemovalsOnly(t *testing.T) {
	added := Unified("run.sh", "run.sh", nil, []byte("#!/bin/sh\necho hi\n"))
	if !strings.Contains(added, "@@ -0,0 +1,2 @@") || !strings.Contains(added, "+#!/bin/sh") {
		t.Fatalf("pure addition diff malformed:\n%s", added)
	}
	removed := Unified("run.sh", "run.sh", []byte("old content\n"), nil)
	if !strings.Contains(removed, "@@ -1,1 +0,0 @@") || !strings.Contains(removed, "-old content") {
		t.Fatalf("pure removal diff malformed:\n%s", removed)
	}
}

func TestRenderableTextClassification(t *testing.T) {
	if !RenderableText([]byte("plain text\nwith lines\n")) {
		t.Fatalf("valid UTF-8 text must classify as renderable")
	}
	if RenderableText([]byte{0x1b, '[', '3', '1', 'm'}) {
		t.Fatalf("ANSI escape bytes must not classify as renderable text")
	}
	if RenderableText([]byte{0x00, 0x01}) {
		t.Fatalf("binary bytes must not classify as renderable text")
	}
	if RenderableText([]byte{0xff, 0xfe, 0xfd}) {
		t.Fatalf("invalid UTF-8 must not classify as renderable text")
	}
}

// spec: update-output/evidence-only-updates-do-not-invent-content-changes
func TestUnifiedDiffIdenticalInputIsEmpty(t *testing.T) {
	out := Unified("SKILL.md", "SKILL.md", []byte("same\ncontent\n"), []byte("same\ncontent\n"))
	if out != "" {
		t.Fatalf("identical content must produce no diff, got:\n%s", out)
	}
}

// A bounded diff must degrade to a deterministic whole-file replacement for
// pathological inputs rather than looping or truncating silently.
func TestUnifiedDiffDegradesToWholeFileReplacement(t *testing.T) {
	var oldB, newB strings.Builder
	for i := 0; i < 3000; i++ {
		oldB.WriteString("old line that is quite long and repeats a lot\n")
		newB.WriteString("different line that is also quite long and differs\n")
	}
	out := Unified("big.md", "big.md", []byte(oldB.String()), []byte(newB.String()))
	if !strings.Contains(out, "--- a/big.md") || !strings.Contains(out, "+++ b/big.md") {
		t.Fatalf("degraded diff must still carry review path headers:\n%.200s", out)
	}
	if !strings.Contains(out, "-old line that is quite long and repeats a lot") || !strings.Contains(out, "+different line that is also quite long and differs") {
		t.Fatalf("degraded diff must represent every old and new line:\n%.200s", out)
	}
}

// Independently authored expectation for an interleaved change: the diff must
// keep unchanged interior lines as context and place additions and deletions
// in the right places, exercising the Myers backtracking path.
func TestUnifiedDiffInterleavedChanges(t *testing.T) {
	oldText := "alpha\nbeta\ngamma\ndelta\nepsilon\n"
	newText := "alpha\nBETA\ngamma\ndelta\nEPSILON\nzeta\n"
	out := Unified("f", "f", []byte(oldText), []byte(newText))
	want := `--- a/f
+++ b/f
@@ -1,5 +1,6 @@
 alpha
-beta
+BETA
 gamma
 delta
-epsilon
+EPSILON
+zeta
`
	if out != want {
		t.Fatalf("interleaved diff mismatch:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

// A change that only removes the final trailing newline must stay visible.
func TestUnifiedDiffFinalNewlineChangeIsVisible(t *testing.T) {
	out := Unified("f", "f", []byte("same\n"), []byte("same"))
	want := "--- a/f\n+++ b/f\n@@ -1,1 +1,1 @@\n-same\n+same\n\\ No newline at end of file\n"
	if out != want {
		t.Fatalf("trailing-newline diff = %q, want %q", out, want)
	}
}

func TestUnifiedDiffLeadingChangesHaveValidLineNumbers(t *testing.T) {
	for _, tt := range []struct {
		name, old, candidate, header string
	}{
		{"insertion", "a\nb\n", "first\na\nb\n", "@@ -1,2 +1,3 @@\n"},
		{"deletion", "first\na\nb\n", "a\nb\n", "@@ -1,3 +1,2 @@\n"},
		{"replacement", "old\n", "new\n", "@@ -1,1 +1,1 @@\n"},
		{"add final newline", "same", "same\n", "@@ -1,1 +1,1 @@\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := Unified("f", "f", []byte(tt.old), []byte(tt.candidate))
			if !strings.Contains(out, tt.header) {
				t.Errorf("leading change missing header %q:\n%s", tt.header, out)
			}
		})
	}
}

func TestUnifiedDiffFallbackPreservesMissingNewlineMarker(t *testing.T) {
	old := strings.Repeat("old\n", maxDiffLines) + "last"
	out := Unified("f", "f", []byte(old), []byte("new\n"))
	if !strings.Contains(out, "-last\n\\ No newline at end of file\n+new\n") {
		t.Error("whole-file fallback omitted the missing-newline marker")
	}
}
