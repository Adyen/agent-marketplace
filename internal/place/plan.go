package place

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// File is one package-owned path and its expected contents and permissions.
type File struct {
	Path   string
	Digest string
	Mode   fs.FileMode
}

// Package is an immutable package snapshot and its package dependencies.
type Package struct {
	ID       string
	Revision string
	Files    []File
	Requires []string
}

// Observation is the current state of a path in the bound target root.
type Observation struct {
	Mode   fs.FileMode
	Digest string
}

// Write publishes one changed or added package file.
type Write struct {
	PackageID string
	File      File
	Prior     *File
}

// Removal prunes one unchanged stale package file.
type Removal struct {
	PackageID string
	File      File
}

// Refusal identifies a package path that prevents the package from being admitted.
type Refusal struct {
	PackageID string
	Path      string
	Reason    string
}

// Plan is a deterministic set of admitted snapshots and their file operations.
type Plan struct {
	Next    []Package
	Writes  []Write
	Removes []Removal
	Ceded   []Removal
	Refused []Refusal
}
type fileClaim struct {
	packageID string
	file      File
	desired   bool
}
type pathNode struct {
	children map[string]*pathNode
	files    []fileClaim
}
type pathTrie struct {
	root pathNode
}
type refusalSet map[string]Refusal

func (r refusalSet) add(packageID, filePath, reason string) {
	current, exists := r[packageID]
	if !exists || filePath < current.Path || (filePath == current.Path && reason < current.Reason) {
		r[packageID] = Refusal{PackageID: packageID, Path: filePath, Reason: reason}
	}
}

// BuildPlan plans whole-package publication and pruning without filesystem I/O.
func BuildPlan(installed, desired []Package, observed map[string]Observation) (Plan, error) {
	oldByID, err := validatePackages(installed, "installed")
	if err != nil {
		return Plan{}, err
	}
	desiredByID, err := validatePackages(desired, "desired")
	if err != nil {
		return Plan{}, err
	}

	desiredOrder, err := dependencyOrder(desiredByID, false)
	if err != nil {
		return Plan{}, err
	}
	pruneOrder, err := dependencyOrder(oldByID, true)
	if err != nil {
		return Plan{}, err
	}

	oldTrie := &pathTrie{}
	oldFiles := make(map[string]fileClaim)
	for _, packageID := range sortedPackageIDs(oldByID) {
		for _, file := range sortedFiles(oldByID[packageID].Files) {
			if claims := oldTrie.conflicts(file.Path); len(claims) != 0 {
				priorPath := claims[0].file.Path
				for _, claim := range claims[1:] {
					if claim.file.Path < priorPath {
						priorPath = claim.file.Path
					}
				}
				return Plan{}, fmt.Errorf("place: corrupt installed ownership at %q and %q", priorPath, file.Path)
			}
			oldTrie.insert(fileClaim{packageID: packageID, file: file})
			oldFiles[file.Path] = fileClaim{packageID: packageID, file: file}
		}
	}

	refused := make(refusalSet)
	claims := oldTrie
	for _, packageID := range sortedPackageIDs(desiredByID) {
		for _, file := range sortedFiles(desiredByID[packageID].Files) {
			for _, prior := range claims.conflicts(file.Path) {
				if prior.packageID == packageID && prior.file.Path == file.Path {
					continue
				}
				if prior.desired {
					refused.add(packageID, file.Path, "desired package path conflict")
					refused.add(prior.packageID, prior.file.Path, "desired package path conflict")
				} else {
					refused.add(packageID, file.Path, "path is reserved by installed ownership")
				}
			}

			checkObservedPath(packageID, file, oldFiles, observed, refused)
			claims.insert(fileClaim{packageID: packageID, file: file, desired: true})
		}
	}
	for _, packageID := range sortedPackageIDs(oldByID) {
		if _, selected := desiredByID[packageID]; !selected {
			continue
		}
		for _, file := range sortedFiles(oldByID[packageID].Files) {
			if !matchesObservation(file, observed) {
				reason := "owned file drifted"
				if _, exists := observed[file.Path]; !exists {
					reason = "owned file is missing"
				}
				refused.add(packageID, file.Path, reason)
			}
		}
	}
	admitted := make(map[string]bool, len(desiredByID))
	nextByID := make(map[string]Package, len(desiredByID))
	for _, packageID := range sortedPackageIDs(desiredByID) {
		if _, blocked := refused[packageID]; blocked {
			if old, exists := oldByID[packageID]; exists {
				nextByID[packageID] = old
			}
			continue
		}
		admitted[packageID] = true
		nextByID[packageID] = desiredByID[packageID]
	}
	next := make([]Package, 0, len(nextByID))
	for _, packageID := range sortedPackageIDs(nextByID) {
		next = append(next, nextByID[packageID])
	}
	plan := Plan{Next: next}
	for _, packageID := range desiredOrder {
		if !admitted[packageID] {
			continue
		}
		for _, file := range sortedFiles(desiredByID[packageID].Files) {
			prior, isPrior := oldFiles[file.Path]
			if isPrior && prior.packageID != packageID {
				continue
			}
			if isPrior && sameFile(prior.file, file) {
				continue
			}
			write := Write{PackageID: packageID, File: file}
			if isPrior {
				priorFile := prior.file
				write.Prior = &priorFile
			}
			plan.Writes = append(plan.Writes, write)
		}
	}
	for _, packageID := range pruneOrder {
		old := oldByID[packageID]
		desiredPackage, selected := desiredByID[packageID]
		if selected && !admitted[packageID] {
			continue
		}
		desiredPaths := make(map[string]struct{}, len(desiredPackage.Files))
		if selected {
			for _, file := range desiredPackage.Files {
				desiredPaths[file.Path] = struct{}{}
			}
		}
		for _, file := range sortedFiles(old.Files) {
			if _, remains := desiredPaths[file.Path]; remains {
				continue
			}
			if matchesObservation(file, observed) {
				plan.Removes = append(plan.Removes, Removal{PackageID: packageID, File: file})
			} else if !selected {
				plan.Ceded = append(plan.Ceded, Removal{PackageID: packageID, File: file})
			}
		}
	}
	plan.Refused = make([]Refusal, 0, len(refused))
	for _, refusal := range refused {
		plan.Refused = append(plan.Refused, refusal)
	}
	sort.Slice(plan.Refused, func(i, j int) bool { return plan.Refused[i].PackageID < plan.Refused[j].PackageID })
	return plan, nil
}

func validatePackages(packages []Package, label string) (map[string]Package, error) {
	out := make(map[string]Package, len(packages))
	for _, packageSnapshot := range packages {
		if packageSnapshot.ID == "" {
			return nil, fmt.Errorf("place: %s package has an empty ID", label)
		}
		if _, exists := out[packageSnapshot.ID]; exists {
			return nil, fmt.Errorf("place: duplicate %s package ID %q", label, packageSnapshot.ID)
		}
		seenPaths := make(map[string]struct{}, len(packageSnapshot.Files))
		for _, file := range packageSnapshot.Files {
			if !cleanPackagePath(file.Path) {
				return nil, fmt.Errorf("place: invalid %s path %q in package %q", label, file.Path, packageSnapshot.ID)
			}
			if _, exists := seenPaths[file.Path]; exists {
				return nil, fmt.Errorf("place: duplicate %s path %q in package %q", label, file.Path, packageSnapshot.ID)
			}
			seenPaths[file.Path] = struct{}{}
		}
		out[packageSnapshot.ID] = packageSnapshot
	}
	return out, nil
}
func cleanPackagePath(filePath string) bool {
	return filePath != "" && !strings.Contains(filePath, "\\") && !strings.ContainsFunc(filePath, unicode.IsControl) && path.Clean(filePath) == filePath && fs.ValidPath(filePath) && filePath != "."
}
func dependencyOrder(packages map[string]Package, reverse bool) ([]string, error) {
	indegree := make(map[string]int, len(packages))
	edges := make(map[string][]string, len(packages))
	for packageID := range packages {
		indegree[packageID] = 0
	}
	for packageID, packageSnapshot := range packages {
		for _, dependencyID := range packageSnapshot.Requires {
			if _, exists := packages[dependencyID]; !exists {
				continue
			}
			from, to := dependencyID, packageID
			if reverse {
				from, to = packageID, dependencyID
			}
			edges[from] = append(edges[from], to)
			indegree[to]++
		}
	}

	ready := make([]string, 0, len(packages))
	for packageID, degree := range indegree {
		if degree == 0 {
			ready = append(ready, packageID)
		}
	}
	sort.Strings(ready)
	ordered := make([]string, 0, len(packages))
	for len(ready) > 0 {
		packageID := ready[0]
		ready = ready[1:]
		ordered = append(ordered, packageID)
		for _, dependentID := range edges[packageID] {
			indegree[dependentID]--
			if indegree[dependentID] == 0 {
				ready = append(ready, dependentID)
				sort.Strings(ready)
			}
		}
	}
	if len(ordered) != len(packages) {
		return nil, fmt.Errorf("place: package dependency cycle")
	}
	return ordered, nil
}
func (t *pathTrie) conflicts(filePath string) []fileClaim {
	components := strings.Split(filePath, "/")
	node := &t.root
	var conflicts []fileClaim
	for i, component := range components {
		if i > 0 && len(node.files) != 0 {
			conflicts = append(conflicts, node.files...)
		}
		node = node.children[componentKey(component)]
		if node == nil {
			return conflicts
		}
	}
	return collectClaims(node, conflicts)
}
func (t *pathTrie) insert(claim fileClaim) {
	node := &t.root
	for _, component := range strings.Split(claim.file.Path, "/") {
		key := componentKey(component)
		if node.children == nil {
			node.children = make(map[string]*pathNode)
		}
		if node.children[key] == nil {
			node.children[key] = &pathNode{}
		}
		node = node.children[key]
	}
	node.files = append(node.files, claim)
}
func collectClaims(node *pathNode, claims []fileClaim) []fileClaim {
	claims = append(claims, node.files...)
	for _, child := range node.children {
		claims = collectClaims(child, claims)
	}
	return claims
}
func componentKey(component string) string {
	return norm.NFC.String(cases.Fold().String(norm.NFD.String(component)))
}
func checkObservedPath(packageID string, file File, oldFiles map[string]fileClaim, observed map[string]Observation, refused refusalSet) {
	components := strings.Split(file.Path, "/")
	var prefix strings.Builder
	for i, component := range components {
		if i > 0 {
			prefix.WriteByte('/')
		}
		prefix.WriteString(component)
		currentPath := prefix.String()
		current, exists := observed[currentPath]
		if !exists {
			continue
		}
		if i < len(components)-1 {
			if !current.Mode.IsDir() {
				refused.add(packageID, currentPath, "non-directory ancestor is occupied")
			}
			continue
		}
		prior, ownedHere := oldFiles[currentPath]
		if ownedHere && prior.packageID == packageID {
			continue
		}
		refused.add(packageID, currentPath, "unowned path is occupied")
	}
}
func matchesObservation(file File, observed map[string]Observation) bool {
	current, exists := observed[file.Path]
	return exists && current.Mode.IsRegular() && current.Mode.Perm() == file.Mode.Perm() && current.Digest == file.Digest
}
func sameFile(left, right File) bool {
	return left.Digest == right.Digest && left.Mode.Perm() == right.Mode.Perm()
}
func sortedPackageIDs(packages map[string]Package) []string {
	ids := make([]string, 0, len(packages))
	for packageID := range packages {
		ids = append(ids, packageID)
	}
	sort.Strings(ids)
	return ids
}
func sortedFiles(files []File) []File {
	out := append([]File(nil), files...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
