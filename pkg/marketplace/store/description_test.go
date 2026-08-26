package store_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestBoundDescriptionNormalizesUntrustedText(t *testing.T) {
	for _, test := range []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{
			name: "plain text is preserved",
			in:   "Analyze user retention and engagement patterns by cohort.",
			want: "Analyze user retention and engagement patterns by cohort.",
		},
		{
			name: "newlines cannot forge extra lines",
			in:   "first line\nsecond line\r\nthird",
			want: "first line second line third",
		},
		{
			name: "ansi escape sequences are neutralized",
			in:   "safe\x1b[31mRED\x1b[0m text",
			want: "safe [31mRED [0m text",
		},
		{
			name: "tabs and repeated spaces collapse",
			in:   "  spaced\t\tout   text  ",
			want: "spaced out text",
		},
		{
			name: "zero width and bidi format runes are removed",
			in:   "we\u200bird\u202ereversed",
			want: "we ird reversed",
		},
		{
			name: "nul bytes are removed",
			in:   "before\x00after",
			want: "before after",
		},
		{
			name: "multi-byte text survives",
			in:   "Análisis de retención 日本語 émoji",
			want: "Análisis de retención 日本語 émoji",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := store.BoundDescription(test.in); got != test.want {
				t.Fatalf("BoundDescription(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestBoundDescriptionIsBoundedAtTheLimit(t *testing.T) {
	exact := strings.Repeat("a", store.MaxDescriptionBytes)
	if got := store.BoundDescription(exact); got != exact {
		t.Fatalf("a description of exactly the limit was modified: len %d", len(got))
	}

	over := strings.Repeat("a", store.MaxDescriptionBytes+1)
	got := store.BoundDescription(over)
	if len(got) > store.MaxDescriptionBytes {
		t.Fatalf("bounded length = %d, want <= %d", len(got), store.MaxDescriptionBytes)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("truncated description %q does not signal truncation", got[len(got)-8:])
	}
}

// A naive byte-slice truncation would split a multi-byte rune and produce
// invalid UTF-8, which JSON encoding would then silently corrupt.
func TestBoundDescriptionTruncatesOnRuneBoundary(t *testing.T) {
	// "€" is three bytes, so some limit offsets land mid-rune.
	for _, filler := range []string{"", "a", "aa"} {
		description := filler + strings.Repeat("€", store.MaxDescriptionBytes)
		got := store.BoundDescription(description)

		if len(got) > store.MaxDescriptionBytes {
			t.Fatalf("filler %q: bounded length = %d, want <= %d", filler, len(got), store.MaxDescriptionBytes)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("filler %q: bounded description is not valid UTF-8", filler)
		}
		if strings.ContainsRune(got, utf8.RuneError) {
			t.Fatalf("filler %q: bounded description contains a replacement character", filler)
		}

		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("filler %q: marshal: %v", filler, err)
		}
		var roundTripped string
		if err := json.Unmarshal(encoded, &roundTripped); err != nil {
			t.Fatalf("filler %q: unmarshal: %v", filler, err)
		}
		if roundTripped != got {
			t.Fatalf("filler %q: description did not survive a JSON round trip", filler)
		}
	}
}

// BoundDescription short-circuits on already-normalized input. That fast path
// must never disagree with the normalization it skips, so anything it returns
// unchanged must survive a second, forced normalization identically.
func TestBoundDescriptionFastPathAgreesWithNormalization(t *testing.T) {
	for _, in := range []string{
		"",
		"simple",
		"a normal one-line description.",
		"Análisis de retención 日本語 émoji",
		" leading space",
		"trailing space ",
		"double  space",
		"tab\tseparated",
		"newline\nseparated",
		"non\u00a0breaking space",
		"zero\u200bwidth",
		"escape\x1b[0m",
		"nul\x00byte",
		strings.Repeat("a", store.MaxDescriptionBytes+1),
		strings.Repeat("€", store.MaxDescriptionBytes),
	} {
		once := store.BoundDescription(in)
		// Feeding the result back in exercises the fast path on the exact
		// value the first call produced. Any disagreement shows up as drift.
		if twice := store.BoundDescription(once); twice != once {
			t.Errorf("fast path disagrees with normalization for %q: %q -> %q", in, once, twice)
		}
		if len(once) > store.MaxDescriptionBytes {
			t.Errorf("result for %q exceeds the bound: %d bytes", in, len(once))
		}
	}
}

// The API re-bounds every listed description on every request, so the
// already-clean case must not allocate.
func TestBoundDescriptionDoesNotAllocateForCleanInput(t *testing.T) {
	clean := "Analyze user retention and engagement patterns by cohort."
	allocations := testing.AllocsPerRun(100, func() {
		if got := store.BoundDescription(clean); got != clean {
			t.Fatalf("clean description was modified: %q", got)
		}
	})
	if allocations != 0 {
		t.Fatalf("BoundDescription allocated %.1f times for already-clean input, want 0", allocations)
	}
}

// Bounding must be idempotent: it runs at ingest and again on the way out of
// the API, and the second pass must not shorten or re-mark the value.
func TestBoundDescriptionIsIdempotent(t *testing.T) {
	for _, in := range []string{
		"",
		"a normal description",
		"control\x1b chars\nand   spacing",
		strings.Repeat("a", store.MaxDescriptionBytes*2),
		strings.Repeat("€", store.MaxDescriptionBytes),
	} {
		once := store.BoundDescription(in)
		if twice := store.BoundDescription(once); twice != once {
			t.Fatalf("BoundDescription is not idempotent for %d-byte input: %q -> %q", len(in), once, twice)
		}
	}
}
