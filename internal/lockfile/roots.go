package lockfile

func ProjectGitRoots(roots []GitRoot, requirements []GitRootRequirement) ([]GitRoot, bool) {
	if len(requirements) == 0 {
		return []GitRoot{}, true
	}
	type pathMatch struct {
		last, firstSelected int // firstSelected is one-based; zero means none.
	}
	var byPath map[string]pathMatch
	var firstExplicit map[GitRootRequirement]int
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
			// ponytail: bound either scan dimension to 32; index larger products.
			if byPath == nil && len(roots) > 32 && len(requirements) > 32 {
				byPath = make(map[string]pathMatch, len(roots))
				firstExplicit = make(map[GitRootRequirement]int, len(roots))
				for i, root := range roots {
					key := GitRootRequirement{Path: root.Path, SourceHarness: root.SourceHarness}
					if _, exists := firstExplicit[key]; !exists {
						firstExplicit[key] = i
					}
					matches := byPath[root.Path]
					matches.last = i
					byPath[root.Path] = matches
				}
			}
			match := -1
			if byPath == nil {
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
			} else {
				matches, exists := byPath[want.Path]
				if !exists {
					return nil, false
				}
				match = matches.last
				if explicit {
					match, exists = firstExplicit[want]
					if !exists {
						return nil, false
					}
				} else if matches.firstSelected > 0 {
					match = matches.firstSelected - 1
				}
				if matches.firstSelected == 0 || match+1 < matches.firstSelected {
					matches.firstSelected = match + 1
					byPath[want.Path] = matches
				}
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
