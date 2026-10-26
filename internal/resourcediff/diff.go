package resourcediff

import (
	"fmt"
	"strings"

	mpinspection "github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
)

// The diff search is bounded: beyond these limits the renderer degrades to a
// single deterministic whole-file replacement hunk, which still represents
// every input line rather than truncating or failing.
const (
	maxDiffLines    = 50000 // total input lines before degrading
	maxDiffDistance = 1024  // edit distance before degrading
	diffContext     = 3     // context lines around each hunk
)

// RenderableText reports whether data is diffable review text: valid UTF-8
// without terminal-executable control bytes, matching the Marketplace
// inspection text gate.
func RenderableText(data []byte) bool {
	return mpinspection.IsValidText(data)
}

// SanitizeReviewLine neutralizes terminal control characters in one review
// line while keeping the line readable: carriage returns, vertical tabs, and
// form feeds become literal escapes, and C1/bidi/zero-width runes become
// \u escapes. Plain text passes through unchanged, so rendering stays
// deterministic for identical input.
func SanitizeReviewLine(line string) string {
	if line == "" {
		return line
	}
	needs := false
	for _, r := range line {
		if needsEscape(r) {
			needs = true
			break
		}
	}
	if !needs {
		return line
	}
	var b strings.Builder
	b.Grow(len(line))
	for _, r := range line {
		switch {
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\v':
			b.WriteString(`\v`)
		case r == '\f':
			b.WriteString(`\f`)
		case needsEscape(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func needsEscape(r rune) bool {
	switch {
	case r == '\r' || r == '\v' || r == '\f':
		return true
	case r >= 0x80 && r <= 0x9f:
		return true
	case r >= 0x200b && r <= 0x200f:
		return true
	case r >= 0x202a && r <= 0x202e:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	default:
		return false
	}
}

// Unified renders a deterministic unified text diff between two review
// documents. It returns the empty string when the texts are identical.
// Every emitted line is sanitized for terminal safety.
func Unified(oldPath, newPath string, oldData, newData []byte) string {
	if string(oldData) == string(newData) {
		return ""
	}
	oldLines, newLines := splitLines(string(oldData)), splitLines(string(newData))
	matches, ok := myersMatches(oldLines, newLines)
	if !ok {
		return wholeFileHunk(oldPath, newPath, oldLines, newLines)
	}
	ops := opsFromMatches(oldLines, newLines, matches)
	hunks := groupHunks(ops)
	if len(hunks) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n", sanitizePath(oldPath))
	fmt.Fprintf(&b, "+++ b/%s\n", sanitizePath(newPath))
	for _, hunk := range hunks {
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", sideStart(hunk.oldStart, hunk.oldCount), hunk.oldCount, sideStart(hunk.newStart, hunk.newCount), hunk.newCount)
		for _, op := range hunk.ops {
			switch op.kind {
			case opContext:
				writeDiffLine(&b, ' ', op.line)
			case opDelete:
				writeDiffLine(&b, '-', op.line)
			case opInsert:
				writeDiffLine(&b, '+', op.line)
			}
		}
	}
	return b.String()
}

// sideStart renders a hunk's start line: 1-based normally, 0 for a side the
// hunk does not touch.
func sideStart(start, count int) int {
	if count == 0 {
		return 0
	}
	return start + 1
}

// splitLines splits text into lines that keep their trailing newline, so a
// missing final newline is a visible content difference.
func splitLines(text string) []string {
	var lines []string
	for len(text) > 0 {
		index := strings.IndexByte(text, '\n')
		if index < 0 {
			lines = append(lines, text)
			break
		}
		lines = append(lines, text[:index+1])
		text = text[index+1:]
	}
	return lines
}

func displayLine(line string) string {
	return SanitizeReviewLine(strings.TrimSuffix(line, "\n"))
}

func writeDiffLine(out *strings.Builder, prefix byte, line string) {
	out.WriteByte(prefix)
	out.WriteString(displayLine(line))
	out.WriteByte('\n')
	if !strings.HasSuffix(line, "\n") {
		out.WriteString("\\ No newline at end of file\n")
	}
}

func sanitizePath(path string) string {
	return SanitizeReviewLine(strings.ReplaceAll(path, "\n", `\n`))
}

type opKind int

const (
	opContext opKind = iota
	opDelete
	opInsert
)

type diffOp struct {
	kind               opKind
	line               string
	oldIndex, newIndex int // 0-based absolute line positions
}

type hunk struct {
	oldStart, oldCount int
	newStart, newCount int
	ops                []diffOp
}

func myersMatches(a, b []string) ([][2]int, bool) {
	n, m := len(a), len(b)
	if n+m > maxDiffLines {
		return nil, false
	}
	if n == 0 && m == 0 {
		return nil, true
	}
	// trace[d][j] is the furthest x reached on diagonal k=2j-d after d edits.
	var trace [][]int
	finalD, finalJ := -1, -1
	round := make([]int, 1)
	// Round 0: follow the initial diagonal.
	{
		x := 0
		for x < n && x < m && a[x] == b[x] {
			x++
		}
		round[0] = x
		trace = append(trace, round)
		if x == n && x == m {
			finalD, finalJ = 0, 0
		}
	}
	if finalD < 0 {
		for d := 1; d <= n+m; d++ {
			if d > maxDiffDistance {
				return nil, false
			}
			prev := trace[d-1]
			curr := make([]int, d+1)
			for j := 0; j <= d; j++ {
				k := 2*j - d
				var x int
				switch {
				case j == 0:
					x = prev[j]
				case j == d:
					x = prev[j-1] + 1
				case prev[j-1] < prev[j]:
					x = prev[j]
				default:
					x = prev[j-1] + 1
				}
				y := x - k
				for x < n && y < m && a[x] == b[y] {
					x++
					y++
				}
				curr[j] = x
			}
			trace = append(trace, curr)
			if (n-m+d)%2 == 0 {
				j := (n - m + d) / 2
				if j >= 0 && j <= d && curr[j] == n {
					finalD, finalJ = d, j
					break
				}
			}
		}
	}
	if finalD < 0 {
		return nil, false
	}
	// Backtrack the matched pairs.
	var pairs [][2]int
	d, j := finalD, finalJ
	x, y := trace[d][j], trace[d][j]-(2*j-d)
	for d > 0 {
		prev := trace[d-1]
		var prevX, prevY int
		if j == 0 || (j != d && prev[j-1] < prev[j]) {
			prevX = prev[j]
			prevY = prevX - (2*j - d + 1)
		} else {
			prevX = prev[j-1]
			prevY = prevX - (2*j - d - 1)
		}
		for x > prevX && y > prevY {
			x--
			y--
			pairs = append(pairs, [2]int{x, y})
		}
		if j == 0 || (j != d && prev[j-1] < prev[j]) {
			// insertion: j stays
		} else {
			j--
		}
		d--
		x, y = prevX, prevY
	}
	for x > 0 && y > 0 {
		x--
		y--
		pairs = append(pairs, [2]int{x, y})
	}
	for i, j := 0, len(pairs)-1; i < j; i, j = i+1, j-1 {
		pairs[i], pairs[j] = pairs[j], pairs[i]
	}
	return pairs, true
}

func opsFromMatches(a, b []string, matches [][2]int) []diffOp {
	ops := make([]diffOp, 0, len(a)+len(b))
	ai, bi := 0, 0
	for _, pair := range matches {
		for ai < pair[0] {
			ops = append(ops, diffOp{kind: opDelete, line: a[ai], oldIndex: ai, newIndex: -1})
			ai++
		}
		for bi < pair[1] {
			ops = append(ops, diffOp{kind: opInsert, line: b[bi], oldIndex: -1, newIndex: bi})
			bi++
		}
		ops = append(ops, diffOp{kind: opContext, line: a[ai], oldIndex: ai, newIndex: bi})
		ai++
		bi++
	}
	for ai < len(a) {
		ops = append(ops, diffOp{kind: opDelete, line: a[ai], oldIndex: ai, newIndex: -1})
		ai++
	}
	for bi < len(b) {
		ops = append(ops, diffOp{kind: opInsert, line: b[bi], oldIndex: -1, newIndex: bi})
		bi++
	}
	return ops
}

// groupHunks slices the op stream into hunks with diffContext lines of context
// around each change, merging changes separated by at most 2*diffContext
// context lines into one hunk.
func groupHunks(ops []diffOp) []hunk {
	var hunks []hunk
	i := 0
	for i < len(ops) {
		if ops[i].kind == opContext {
			i++
			continue
		}
		end := i
		gap := 0
		for j := i + 1; j < len(ops); j++ {
			if ops[j].kind != opContext {
				end = j
				gap = 0
				continue
			}
			gap++
			if gap > 2*diffContext {
				break
			}
		}
		start := max(i-diffContext, 0)
		stop := min(end+diffContext, len(ops)-1)
		h := hunk{ops: ops[start : stop+1]}
		for _, op := range h.ops {
			if op.kind == opContext || op.kind == opDelete {
				h.oldCount++
			}
			if op.kind == opContext || op.kind == opInsert {
				h.newCount++
			}
		}
		h.oldStart = firstLineIndex(h.ops, true)
		h.newStart = firstLineIndex(h.ops, false)
		hunks = append(hunks, h)
		i = stop + 1
	}
	return hunks
}

// firstLineIndex finds the first line present on the requested side of a hunk.
// A leading insertion has no old index; a leading deletion has no new index.
func firstLineIndex(ops []diffOp, old bool) int {
	for _, op := range ops {
		if old && op.oldIndex >= 0 {
			return op.oldIndex
		}
		if !old && op.newIndex >= 0 {
			return op.newIndex
		}
	}
	return 0
}

func wholeFileHunk(oldPath, newPath string, oldLines, newLines []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n", sanitizePath(oldPath))
	fmt.Fprintf(&b, "+++ b/%s\n", sanitizePath(newPath))
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", sideStart(0, len(oldLines)), len(oldLines), sideStart(0, len(newLines)), len(newLines))
	for _, line := range oldLines {
		writeDiffLine(&b, '-', line)
	}
	for _, line := range newLines {
		writeDiffLine(&b, '+', line)
	}
	return b.String()
}
