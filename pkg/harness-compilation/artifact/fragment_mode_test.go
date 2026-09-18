package artifact

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func TestMergeFragmentsPropagatesExplicitMode(t *testing.T) {
	for _, format := range []struct {
		name   string
		one    []byte
		two    []byte
		first  string
		second string
	}{
		{name: "json", one: []byte(`{"a":1}`), two: []byte(`{"b":2}`), first: `"a": 1`, second: `"b": 2`},
		{name: "text", one: []byte("first\n"), two: []byte("second\n"), first: "first", second: "second"},
	} {
		for _, tc := range []struct {
			name string
			one  fs.FileMode
			two  fs.FileMode
			want fs.FileMode
		}{
			{name: "first", one: 0, two: 0o600, want: 0o600},
			{name: "second", one: 0o600, two: 0, want: 0o600},
			{name: "both", one: 0o600, two: 0o600, want: 0o600},
			{name: "unspecified", one: 0, two: 0, want: 0},
		} {
			t.Run(format.name+"/"+tc.name, func(t *testing.T) {
				merged, err := MergeFragments([]Artifact{{Path: "settings.json", Bytes: format.one, Fragment: true, FragmentKey: "settings", Mode: tc.one}, {Path: "settings.json", Bytes: format.two, Fragment: true, FragmentKey: "settings", Mode: tc.two}})
				if err != nil {
					t.Fatal(err)
				}
				if len(merged) != 1 || merged[0].Mode != tc.want {
					t.Fatalf("merged mode = %#v, want %o", merged, tc.want)
				}
				text := string(merged[0].Bytes)
				if !strings.Contains(text, format.first) || !strings.Contains(text, format.second) {
					t.Fatalf("merged bytes = %s", text)
				}
			})
		}
	}
}

func TestMergeFragmentsRejectsConflictingModes(t *testing.T) {
	for _, data := range []struct {
		name string
		one  []byte
		two  []byte
	}{
		{name: "json", one: []byte(`{"a":1}`), two: []byte(`{"b":2}`)},
		{name: "text", one: []byte("first\n"), two: []byte("second\n")},
	} {
		t.Run(data.name, func(t *testing.T) {
			_, err := MergeFragments([]Artifact{{Path: "settings", Bytes: data.one, Fragment: true, FragmentKey: "settings", Mode: 0o600}, {Path: "settings", Bytes: data.two, Fragment: true, FragmentKey: "settings", Mode: 0o640}})
			if !errors.Is(err, ErrFragmentModeConflict) {
				t.Fatalf("error = %v, want %v", err, ErrFragmentModeConflict)
			}
		})
	}
}
