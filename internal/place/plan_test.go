package place

import (
	"io/fs"
	"reflect"
	"testing"
)

func planFile(path, digest string, mode fs.FileMode) File {
	return File{Path: path, Digest: digest, Mode: mode}
}
func planPackage(id, revision string, files ...File) Package {
	return Package{ID: id, Revision: revision, Files: files}
}
func observations(files ...File) map[string]Observation {
	out := make(map[string]Observation, len(files))
	for _, file := range files {
		out[file.Path] = Observation{Mode: file.Mode, Digest: file.Digest}
	}
	return out
}
func mustPlan(t *testing.T, installed, desired []Package, seen map[string]Observation) Plan {
	t.Helper()
	plan, err := BuildPlan(installed, desired, seen)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func refusalPairs(t *testing.T, got []Refusal, wantIDs, wantPaths []string) {
	t.Helper()
	if len(got) != len(wantIDs) {
		t.Fatalf("refusals = %#v, want %d", got, len(wantIDs))
	}
	for i, wantID := range wantIDs {
		if got[i].PackageID != wantID || got[i].Path != wantPaths[i] || got[i].Reason == "" {
			t.Fatalf("refusal[%d] = %#v, want package %q at %q with a reason", i, got[i], wantID, wantPaths[i])
		}
	}
}
func writePairs(writes []Write) [][2]string {
	out := make([][2]string, len(writes))
	for i, write := range writes {
		out[i] = [2]string{write.PackageID, write.File.Path}
	}
	return out
}
func removalPairs(removes []Removal) [][2]string {
	out := make([][2]string, len(removes))
	for i, removal := range removes {
		out[i] = [2]string{removal.PackageID, removal.File.Path}
	}
	return out
}
func assertPairs(t *testing.T, kind string, got, want [][2]string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %v, want %v", kind, got, want)
	}
}

func TestPlacePlan(t *testing.T) {
	t.Run("identical unowned file refuses the whole package", func(t *testing.T) {
		newPackage := planPackage("alpha", "r2",
			planFile("mods/new", "new", 0o644),
			planFile("mods/shared", "same", 0o644))
		seen := observations(planFile("mods/shared", "same", 0o644))
		seen["mods"] = Observation{Mode: fs.ModeDir | 0o755}
		plan := mustPlan(t, nil, []Package{newPackage}, seen)
		refusalPairs(t, plan.Refused, []string{"alpha"}, []string{"mods/shared"})
		if len(plan.Next) != 0 || len(plan.Writes) != 0 {
			t.Fatalf("unowned collision admitted a partial package: next=%#v writes=%#v", plan.Next, plan.Writes)
		}
		directory := mustPlan(t, nil, []Package{newPackage}, map[string]Observation{"mods": {Mode: fs.ModeDir | 0o755}})
		if len(directory.Refused) != 0 || len(directory.Writes) != 2 {
			t.Fatalf("ordinary directory ancestor blocked publication: %#v", directory)
		}
		blocked := mustPlan(t, nil, []Package{newPackage}, map[string]Observation{"mods": {Mode: 0o644}})
		refusalPairs(t, blocked.Refused, []string{"alpha"}, []string{"mods"})
		if len(blocked.Writes) != 0 {
			t.Fatalf("non-directory ancestor allowed publication: %#v", blocked.Writes)
		}
	})
	t.Run("normalized and prefix conflicts refuse both packages in either input order", func(t *testing.T) {
		tests := []struct {
			name  string
			left  string
			right string
		}{
			{name: "case fold", left: "mods/ß", right: "mods/SS"},
			{name: "NFC and NFD", left: "mods/Café", right: "mods/cafe\u0301"},
			{name: "file and directory prefix", left: "mods/item", right: "mods/item/child"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				alpha := planPackage("alpha", "r1", planFile(test.left, "a", 0o644))
				beta := planPackage("beta", "r1", planFile(test.right, "b", 0o644))
				var first Plan
				for i, desired := range [][]Package{{alpha, beta}, {beta, alpha}} {
					plan := mustPlan(t, nil, desired, nil)
					refusalPairs(t, plan.Refused,
						[]string{"alpha", "beta"},
						[]string{test.left, test.right})
					if len(plan.Next) != 0 || len(plan.Writes) != 0 {
						t.Fatalf("conflicting snapshots were partly admitted: next=%#v writes=%#v", plan.Next, plan.Writes)
					}
					if i == 0 {
						first = plan
					} else if !reflect.DeepEqual(first.Refused, plan.Refused) {
						t.Fatalf("input order changed refusals: first=%#v reverse=%#v", first.Refused, plan.Refused)
					}
				}
			})
		}
	})
	t.Run("installed package keeps its entire old projection after a desired conflict", func(t *testing.T) {
		old := planPackage("alpha", "r1",
			planFile("mods/base", "base", 0o644),
			planFile("mods/old", "old", 0o644))
		other := planPackage("beta", "r1",
			planFile("mods/shared", "beta-owned", 0o644),
			planFile("mods/beta-old", "beta-old", 0o644))
		desired := planPackage("alpha", "r2",
			planFile("mods/base", "new-base", 0o644),
			planFile("mods/shared", "alpha", 0o644))
		seen := observations(old.Files[0], old.Files[1], other.Files[0], other.Files[1])
		plan := mustPlan(t, []Package{old, other}, []Package{desired}, seen)
		if !reflect.DeepEqual(plan.Next, []Package{old}) {
			t.Fatalf("next = %#v, want the complete old snapshot %#v", plan.Next, old)
		}
		assertPairs(t, "removes", removalPairs(plan.Removes), [][2]string{
			{"beta", "mods/beta-old"},
			{"beta", "mods/shared"},
		})
		refusalPairs(t, plan.Refused, []string{"alpha"}, []string{"mods/shared"})
		if len(plan.Writes) != 0 {
			t.Fatalf("conflicting upgrade published files: %#v", plan.Writes)
		}
	})
	t.Run("edited, deleted, or mode-changed members refuse an upgrade as a unit", func(t *testing.T) {
		old := planPackage("alpha", "r1",
			planFile("mods/a", "old-a", 0o644),
			planFile("mods/b", "old-b", 0o644))
		desired := planPackage("alpha", "r2",
			planFile("mods/a", "new-a", 0o644),
			planFile("mods/b", "new-b", 0o644),
			planFile("mods/c", "new-c", 0o644))
		cases := []struct {
			name string
			seen map[string]Observation
			path string
		}{
			{
				name: "edited",
				seen: observations(planFile("mods/a", "user-edit", 0o644), old.Files[1]),
				path: "mods/a",
			},
			{
				name: "deleted",
				seen: observations(old.Files[1]),
				path: "mods/a",
			},
			{
				name: "permission bits changed",
				seen: observations(planFile("mods/a", "old-a", 0o600), old.Files[1]),
				path: "mods/a",
			},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				plan := mustPlan(t, []Package{old}, []Package{desired}, test.seen)
				if !reflect.DeepEqual(plan.Next, []Package{old}) {
					t.Fatalf("next = %#v, want complete old snapshot %#v", plan.Next, old)
				}
				refusalPairs(t, plan.Refused, []string{"alpha"}, []string{test.path})
				if len(plan.Writes) != 0 || len(plan.Removes) != 0 {
					t.Fatalf("drifted package was partly changed: writes=%#v removes=%#v", plan.Writes, plan.Removes)
				}
			})
		}
	})
	t.Run("deselection removes unchanged and cedes edited and missing paths", func(t *testing.T) {
		old := planPackage("alpha", "r1",
			planFile("mods/unchanged", "same", 0o644),
			planFile("mods/edited", "owned", 0o644),
			planFile("mods/missing", "gone", 0o644))
		seen := observations(
			old.Files[0],
			planFile("mods/edited", "user-edit", 0o644),
		)
		plan := mustPlan(t, []Package{old}, nil, seen)
		if len(plan.Next) != 0 || len(plan.Refused) != 0 {
			t.Fatalf("deselection retained or refused a package: next=%#v refused=%#v", plan.Next, plan.Refused)
		}
		assertPairs(t, "removes", removalPairs(plan.Removes), [][2]string{{"alpha", "mods/unchanged"}})
		assertPairs(t, "ceded", removalPairs(plan.Ceded), [][2]string{{"alpha", "mods/edited"}, {"alpha", "mods/missing"}})
	})
	t.Run("a deselected path blocks a new claimant until the next plan", func(t *testing.T) {
		old := planPackage("alpha", "r1", planFile("mods/reclaim", "old", 0o644))
		claimant := planPackage("beta", "r1", planFile("mods/reclaim", "new", 0o644))
		first := mustPlan(t, []Package{old}, []Package{claimant}, observations(old.Files...))
		refusalPairs(t, first.Refused, []string{"beta"}, []string{"mods/reclaim"})
		if len(first.Writes) != 0 {
			t.Fatalf("reserved path was published before prune: %#v", first.Writes)
		}
		assertPairs(t, "first plan removes", removalPairs(first.Removes), [][2]string{{"alpha", "mods/reclaim"}})
		next := mustPlan(t, nil, []Package{claimant}, nil)
		if len(next.Refused) != 0 || len(next.Next) != 1 || next.Next[0].ID != "beta" {
			t.Fatalf("claim after prune was not admitted: next=%#v refused=%#v", next.Next, next.Refused)
		}
		assertPairs(t, "writes after prune", writePairs(next.Writes), [][2]string{{"beta", "mods/reclaim"}})
	})
	t.Run("an upgrade's stale path stays reserved until prune", func(t *testing.T) {
		old := planPackage("alpha", "r1", planFile("mods/stale", "old", 0o644))
		upgrade := planPackage("alpha", "r2", planFile("mods/current", "new", 0o644))
		claimant := planPackage("beta", "r1", planFile("mods/stale", "claim", 0o644))
		first := mustPlan(t,
			[]Package{old},
			[]Package{upgrade, claimant},
			observations(old.Files...),
		)
		if !reflect.DeepEqual(first.Next, []Package{upgrade}) {
			t.Fatalf("next = %#v, want admitted upgrade %#v", first.Next, upgrade)
		}
		refusalPairs(t, first.Refused, []string{"beta"}, []string{"mods/stale"})
		assertPairs(t, "writes", writePairs(first.Writes), [][2]string{{"alpha", "mods/current"}})
		assertPairs(t, "removes", removalPairs(first.Removes), [][2]string{{"alpha", "mods/stale"}})
	})
	t.Run("dependency publication and reverse prune order are deterministic", func(t *testing.T) {
		oldMod := planPackage("z-mod", "r1",
			planFile("mods/a", "mod-old-a", 0o644),
			planFile("mods/retained", "same", 0o644),
			planFile("mods/old", "mod-old", 0o644))
		oldLayout := planPackage("a-layout", "r1", planFile("layouts/old", "layout-old", 0o644))
		newMod := planPackage("z-mod", "r2",
			planFile("mods/a", "mod-a", 0o644),
			planFile("mods/retained", "same", 0o644),
			planFile("mods/z", "mod-z", 0o644))
		newLayout := planPackage("a-layout", "r2",
			planFile("layouts/a", "layout-a", 0o644),
			planFile("layouts/z", "layout-z", 0o644))
		newLayout.Requires = []string{"z-mod"}
		seen := observations(oldMod.Files[0], oldMod.Files[1], oldMod.Files[2], oldLayout.Files[0])
		plan := mustPlan(t, []Package{oldLayout, oldMod}, []Package{newLayout, newMod}, seen)
		wantWrites := [][2]string{
			{"z-mod", "mods/a"},
			{"z-mod", "mods/z"},
			{"a-layout", "layouts/a"},
			{"a-layout", "layouts/z"},
		}
		assertPairs(t, "writes", writePairs(plan.Writes), wantWrites)
		if plan.Writes[0].Prior == nil || !reflect.DeepEqual(*plan.Writes[0].Prior, oldMod.Files[0]) {
			t.Fatalf("replacement prior = %#v, want %#v", plan.Writes[0].Prior, oldMod.Files[0])
		}
		if plan.Writes[1].Prior != nil {
			t.Fatalf("added file has prior ownership: %#v", plan.Writes[1].Prior)
		}
		assertPairs(t, "removes", removalPairs(plan.Removes), [][2]string{
			{"a-layout", "layouts/old"},
			{"z-mod", "mods/old"},
		})
		if len(plan.Refused) != 0 {
			t.Fatalf("nonconflicting dependency set refused: %#v", plan.Refused)
		}
	})
	t.Run("pruning follows reverse dependencies independent of lexical IDs", func(t *testing.T) {
		oldMod := planPackage("a-mod", "r1", planFile("mods/a", "mod", 0o644))
		oldLayout := planPackage("z-layout", "r1", planFile("layouts/z", "layout", 0o644))
		oldLayout.Requires = []string{"a-mod"}

		plan := mustPlan(t, []Package{oldMod, oldLayout}, nil, observations(oldMod.Files[0], oldLayout.Files[0]))
		assertPairs(t, "removes", removalPairs(plan.Removes), [][2]string{
			{"z-layout", "layouts/z"},
			{"a-mod", "mods/a"},
		})
	})
	t.Run("dependency cycles and corrupt prior snapshots return errors without a plan", func(t *testing.T) {
		cases := []struct {
			name      string
			installed []Package
			desired   []Package
		}{
			{
				name: "desired dependency cycle",
				desired: []Package{
					{ID: "alpha", Revision: "r1", Requires: []string{"beta"}},
					{ID: "beta", Revision: "r1", Requires: []string{"alpha"}},
				},
			},
			{
				name: "duplicate prior path",
				installed: []Package{
					planPackage("alpha", "r1",
						planFile("mods/a", "one", 0o644),
						planFile("mods/a", "two", 0o644)),
				},
			},
			{
				name: "invalid desired path",
				desired: []Package{
					planPackage("alpha", "r1", planFile("../escape", "new", 0o644)),
				},
			},
			{
				name: "control character in prior path",
				installed: []Package{
					planPackage("alpha", "r1", planFile("mods/bad\x00path", "old", 0o644)),
				},
			},
			{
				name: "duplicate prior package ID",
				installed: []Package{
					planPackage("alpha", "r1"),
					planPackage("alpha", "r2"),
				},
			},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				plan, err := BuildPlan(test.installed, test.desired, nil)
				if err == nil {
					t.Fatal("BuildPlan succeeded with invalid input")
				}
				if !reflect.DeepEqual(plan, Plan{}) {
					t.Fatalf("invalid input returned a partial plan: %#v", plan)
				}
			})
		}
	})
}
