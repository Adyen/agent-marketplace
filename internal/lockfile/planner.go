package lockfile

type Operation uint8

const (
	OperationSync Operation = iota
	OperationUpgrade
)

type GitRequirement struct {
	URL, Ref string
	Roots    []GitRootRequirement
}

type GitRootRequirement struct{ Path, SourceHarness string }

type UpgradeSelection struct {
	All bool
	Git []GitUpgradeSelector
}

type GitUpgradeSelector struct{ URL, Ref, Path string }

func (s UpgradeSelection) SelectsGit(r GitRequirement) bool {
	if s.All || len(s.Git) == 0 {
		return true
	}
	for _, target := range s.Git {
		url, err := CanonicalGitURL(target.URL)
		if err != nil || url != r.URL || target.Ref != "" && target.Ref != r.Ref {
			continue
		}
		if target.Path == "" {
			return true
		}
		for _, root := range r.Roots {
			if root.Path == target.Path {
				return true
			}
		}
	}
	return false
}
