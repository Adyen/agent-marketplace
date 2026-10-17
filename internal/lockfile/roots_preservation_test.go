package lockfile

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

func TestProjectGitRootsSelectionCompatibility(t *testing.T) {
	baseRoots := []GitRoot{
		{Path: "skills", SourceHarness: "claude", RevisionDigest: Digest(sha256a),
			Resources: []GitResource{{Kind: "skill", Name: "review", ContentDigest: Digest(sha256a), SupportFiles: map[string]Digest{"example.txt": Digest(sha256a)}}}},
		{Path: "skills", SourceHarness: "factory", RevisionDigest: Digest(sha256a)},
		{Path: "skills", SourceHarness: "claude", RevisionDigest: Digest(sha256b)},
		{Path: ".", SourceHarness: "factory", RevisionDigest: Digest(sha256a)},
		{Path: "other", SourceHarness: "claude", RevisionDigest: Digest(sha256a)},
	}
	for _, count := range []int{len(baseRoots), 33} {
		roots := slices.Clone(baseRoots)
		for len(roots) < count {
			roots = append(roots, GitRoot{Path: fmt.Sprintf("unused-%d", len(roots)), SourceHarness: "claude"})
		}
		for _, tc := range []struct {
			name    string
			wants   []GitRootRequirement
			indexes []int
			match   bool
		}{
			{"implicit-last", []GitRootRequirement{{Path: "skills"}}, []int{2}, true},
			{"explicit-first-duplicate", []GitRootRequirement{{Path: "skills", SourceHarness: "claude"}}, []int{0}, true},
			{"explicit-before-implicit", []GitRootRequirement{{Path: "skills"}, {Path: "skills", SourceHarness: "factory"}}, []int{1}, true},
			{"lowest-selected-not-selection-order", []GitRootRequirement{{Path: "skills", SourceHarness: "factory"}, {Path: "skills", SourceHarness: "claude"}, {Path: "skills"}}, []int{0, 1}, true},
			{"original-output-order", []GitRootRequirement{{Path: "other"}, {Path: "skills", SourceHarness: "factory"}}, []int{1, 4}, true},
			{"repeated-implicit", []GitRootRequirement{{Path: "skills"}, {Path: "skills"}}, []int{2}, true},
			{"default-root", []GitRootRequirement{{}, {Path: "./"}}, []int{3}, true},
			{"normalized-want", []GitRootRequirement{{Path: "./skills//", SourceHarness: "factory"}}, []int{1}, true},
			{"no-wants", nil, []int{}, true},
			{"missing-path", []GitRootRequirement{{Path: "missing"}}, nil, false},
			{"missing-explicit", []GitRootRequirement{{Path: "skills"}, {Path: "skills", SourceHarness: "devin"}}, nil, false},
			{"invalid-after-match", []GitRootRequirement{{Path: "skills", SourceHarness: "factory"}, {Path: "other/../other"}}, nil, false},
			{"absolute", []GitRootRequirement{{Path: "/skills"}}, nil, false},
			{"backslash", []GitRootRequirement{{Path: `skills\other`}}, nil, false},
			{"control", []GitRootRequirement{{Path: "skills\x00"}}, nil, false},
		} {
			t.Run(fmt.Sprintf("%s/roots=%d", tc.name, count), func(t *testing.T) {
				wants := slices.Clone(tc.wants)
				if count > 32 && len(wants) > 0 {
					for len(wants) <= 32 {
						wants = append(wants, tc.wants...)
					}
				}
				beforeRoots := cloneDocument(Document{Registries: []GitRegistry{{Roots: roots}}}).Registries[0].Roots
				beforeWants := slices.Clone(wants)
				var want []GitRoot
				if tc.match {
					want = make([]GitRoot, 0, len(tc.indexes))
					for _, index := range tc.indexes {
						want = append(want, roots[index])
					}
				}
				got, match := ProjectGitRoots(roots, wants)
				if match != tc.match || !reflect.DeepEqual(got, want) {
					t.Fatalf("projection = %#v, %v; want %#v, %v", got, match, want, tc.match)
				}
				if original, originalMatch := scanProjectGitRoots(roots, wants); originalMatch != tc.match || !reflect.DeepEqual(original, want) {
					t.Fatal("authored expectation disagrees with the original scan")
				}
				if !reflect.DeepEqual(roots, beforeRoots) || !reflect.DeepEqual(wants, beforeWants) {
					t.Fatal("projection mutated its inputs")
				}
			})
		}
	}
}

func FuzzProjectGitRootsMatchesScan(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 1, 17, 1, 33, 17, 1, 1})
	f.Add([]byte{0, 0, 16, 16, 32, 32, 255, 255})
	f.Add(bytes.Repeat([]byte{1, 1}, 64))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 256 {
			return
		}
		paths := []string{".", "skills", "other", "", "./skills/", "a/../b", "/skills", `a\b`}
		harnesses := []string{"", "claude", "factory", "devin"}
		roots := make([]GitRoot, 0, len(data)/2)
		wants := make([]GitRootRequirement, 0, len(data)/2)
		for i, value := range data {
			path, harness := paths[int(value)%len(paths)], harnesses[int(value>>4)%len(harnesses)]
			if i%2 == 0 {
				roots = append(roots, GitRoot{Path: path, SourceHarness: harness, RevisionSchema: strconv.Itoa(i)})
			} else {
				wants = append(wants, GitRootRequirement{Path: path, SourceHarness: harness})
			}
		}
		beforeRoots, beforeWants := slices.Clone(roots), slices.Clone(wants)
		want, wantMatch := scanProjectGitRoots(roots, wants)
		got, match := ProjectGitRoots(roots, wants)
		if match != wantMatch || !reflect.DeepEqual(got, want) {
			t.Fatalf("roots=%#v wants=%#v: got %#v/%v, want %#v/%v", roots, wants, got, match, want, wantMatch)
		}
		if !reflect.DeepEqual(roots, beforeRoots) || !reflect.DeepEqual(wants, beforeWants) {
			t.Fatal("projection mutated its inputs")
		}
	})
}

// scanProjectGitRoots pins the pre-R48 selection algorithm as a test oracle.
func scanProjectGitRoots(roots []GitRoot, requirements []GitRootRequirement) ([]GitRoot, bool) {
	selected := make([]bool, len(roots))
	for _, explicit := range []bool{true, false} {
		for _, want := range requirements {
			if (want.SourceHarness != "") != explicit {
				continue
			}
			if want.Path == "" {
				want.Path = "."
			}
			var err error
			want.Path, err = normalizeArtifactPath(want.Path, true)
			if err != nil {
				return nil, false
			}
			match := -1
			for i, root := range roots {
				if want.Path != root.Path || want.SourceHarness != "" && want.SourceHarness != root.SourceHarness {
					continue
				}
				match = i
				if explicit || selected[i] {
					break
				}
			}
			if match < 0 {
				return nil, false
			}
			selected[match] = true
		}
	}
	projected := make([]GitRoot, 0, len(roots))
	for i, root := range roots {
		if selected[i] {
			projected = append(projected, root)
		}
	}
	return projected, true
}
