package lockfile

import (
	"reflect"
	"testing"
)

func TestProjectGitRoots(t *testing.T) {
	factory := GitRoot{Path: "skills", SourceHarness: "factory", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)}
	claude := GitRoot{Path: "skills", SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)}
	for _, tc := range []struct {
		name      string
		wants     []GitRootRequirement
		projected []GitRoot
		match     bool
	}{
		{"auto-first", []GitRootRequirement{{Path: "skills"}, {Path: "skills", SourceHarness: "factory"}}, []GitRoot{factory}, true},
		{"explicit-first", []GitRootRequirement{{Path: "skills", SourceHarness: "factory"}, {Path: "skills"}}, []GitRoot{factory}, true},
		{"distinct-explicit", []GitRootRequirement{{Path: "skills", SourceHarness: "factory"}, {Path: "skills", SourceHarness: "claude"}}, []GitRoot{claude, factory}, true},
		{"duplicate-explicit", []GitRootRequirement{{Path: "skills", SourceHarness: "factory"}, {Path: "skills", SourceHarness: "factory"}}, []GitRoot{factory}, true},
		{"normalized", []GitRootRequirement{{Path: "./skills/", SourceHarness: "factory"}}, []GitRoot{factory}, true},
		{"missing-path", []GitRootRequirement{{Path: "missing"}}, nil, false},
		{"missing-harness", []GitRootRequirement{{Path: "skills"}, {Path: "skills", SourceHarness: "devin"}}, nil, false},
		{"traversal", []GitRootRequirement{{Path: "skills/../skills"}}, nil, false},
		{"absolute", []GitRootRequirement{{Path: "/skills"}}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roots := []GitRoot{claude, factory}
			before := append([]GitRootRequirement(nil), tc.wants...)
			got, match := ProjectGitRoots(roots, tc.wants)
			if match != tc.match || !reflect.DeepEqual(got, tc.projected) {
				t.Fatalf("projection = %#v, %v; want %#v, %v", got, match, tc.projected, tc.match)
			}
			if !reflect.DeepEqual(roots, []GitRoot{claude, factory}) || !reflect.DeepEqual(tc.wants, before) {
				t.Fatal("projection mutated inputs")
			}
		})
	}
}

func TestPlanProjectsEquivalentRoots(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		wanted := []GitRootRequirement{{Path: "skills"}, {Path: "skills", SourceHarness: "factory"}}
		if reverse {
			wanted[0], wanted[1] = wanted[1], wanted[0]
		}
		current := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: []GitRegistry{{URL: "https://example.com/repo", Ref: "main", Commit: mustOID(t, sha1a), Roots: []GitRoot{
			{Path: "skills", SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)},
			{Path: "skills", SourceHarness: "factory", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)},
		}}}}
		plan, err := Plan(Requirements{Scope: ScopeRepo, Git: []GitRequirement{{URL: "https://example.com/repo", Ref: "main", Roots: wanted}}}, &current, OperationSync)
		if err != nil || !plan.Changed() || len(plan.Removed) != 1 || len(plan.Acquisitions) != 0 || len(plan.Desired.Registries) != 1 {
			t.Fatalf("plan = %#v, %v", plan, err)
		}
		if got := plan.Desired.Registries[0].Roots; len(got) != 1 || got[0].SourceHarness != "factory" {
			t.Fatalf("projected roots = %#v", got)
		}
	}
}
