package lockfile

func ProjectGitRoots(roots []GitRoot, requirements []GitRootRequirement) ([]GitRoot, bool) {
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
