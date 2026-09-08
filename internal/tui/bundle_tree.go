package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// bundleNode is a filesystem entry kept by the bundle browser. Children are
// loaded only when a directory is expanded, so opening a large bundle does not
// require recursively walking it.
type bundleNode struct {
	name     string
	relPath  string
	path     string
	isDir    bool
	parent   *bundleNode
	expanded bool
	loaded   bool
	err      error
	children []*bundleNode
}

// bundleEntry is one visible row in the flattened, depth-first browser tree.
type bundleEntry struct {
	Name     string
	RelPath  string
	Path     string
	Depth    int
	IsDir    bool
	Expanded bool
	HasChild bool
	Err      error
	node     *bundleNode
}

// bundleTree owns filesystem policy and selection state for one resource.
// Paths exposed to the UI are slash-separated relative paths; host operations
// always use filepath paths stored on the nodes.
type bundleTree struct {
	root     string
	rootNode *bundleNode
	onlyFile string
	visible  []bundleEntry
	cursor   int
	selected string
	rootErr  error

	// files is non-nil for archive-backed trees: verified canonical
	// Marketplace artifacts whose members are held as bounded in-memory
	// content keyed by slash-separated relative path instead of a
	// browsable directory.
	files map[string][]byte
}

// marketplaceArtifactName is the canonical archive filename produced by the
// verified Marketplace mirror cache. Its sibling marker records that the
// archive content was cryptographically verified.
const (
	marketplaceArtifactName   = "artifact.tar.gz"
	marketplaceVerifiedMarker = "verified.marker"

	// maxMarketplaceArtifactBytes bounds the compressed archive read for a
	// detail preview. It matches the canonical artifact total-size budget so
	// a corrupt or hostile cache object cannot exhaust TUI memory.
	maxMarketplaceArtifactBytes = 64 << 20
)

// isMarketplaceDetailOnly reports whether the row is Marketplace-owned with
// no local verified content to browse — typically a discovery-only result.
// Such rows open a metadata detail view instead of a filesystem lookup, so a
// registry-relative path is never treated as a consumer-workspace file.
func isMarketplaceDetailOnly(r row) bool {
	return r.Artifact.IsMarketplace && r.Artifact.Path == ""
}

// isMarketplaceArtifactPath reports whether path is a verified canonical
// Marketplace artifact archive rather than a browsable source directory.
// Callers must already have established that the row is Marketplace-owned.
func isMarketplaceArtifactPath(path string) bool {
	return filepath.Base(path) == marketplaceArtifactName
}

func bundleRootForRow(r row) (root, onlyFile string, err error) {
	path := catalog.SourcePath(r.Artifact.Entry)
	if r.Artifact.IsMarketplace {
		// Marketplace rows have no registry-checkout source path; the
		// resolved path is the verified cache artifact or staged compiled
		// content. A registry-relative catalog path must not be treated as
		// a consumer-workspace file.
		path = r.Artifact.Path
	}
	if path == "" {
		path = r.Artifact.Path
	}
	if path == "" {
		return "", "", fmt.Errorf("resource has no source path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("resource source is a symlink")
	}
	if r.Artifact.Kind != ir.KindSkill {
		if !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("resource source is not a regular file")
		}
		root = filepath.Dir(path)
		onlyFile = filepath.Base(path)
	} else if info.IsDir() {
		root = path
	} else if info.Mode().IsRegular() {
		root = filepath.Dir(path)
	} else {
		return "", "", fmt.Errorf("resource source is not a directory or regular file")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve resource root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve resource root: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", "", err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return "", "", fmt.Errorf("resource root is not a directory")
	}
	return root, onlyFile, nil
}

// newBundleTree initializes the root and its first level. onlyFile is used by
// file-backed resources so their browser contains exactly one source entry.
func newBundleTree(root, onlyFile string) *bundleTree {
	t := &bundleTree{root: root, onlyFile: filepath.ToSlash(onlyFile)}
	t.rootNode = &bundleNode{name: filepath.Base(root), path: root, expanded: true}
	if err := t.load(t.rootNode); err != nil {
		t.rootErr = err
		t.rootNode.err = err
		slog.Error("tui bundle root load failed", "error", err)
	} else {
		t.selectInitial()
	}
	t.rebuild()
	return t
}

func (t *bundleTree) load(node *bundleNode) error {
	if node.loaded {
		return node.err
	}
	info, err := os.Lstat(node.path)
	if err != nil {
		node.loaded = true
		node.err = err
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		err = fmt.Errorf("bundle directory is not a directory")
		node.loaded = true
		node.err = err
		return err
	}
	entries, err := os.ReadDir(node.path)
	if err != nil {
		node.loaded = true
		node.err = err
		return err
	}
	children := make([]*bundleNode, 0, len(entries))
	for _, entry := range entries {
		if node == t.rootNode && t.onlyFile != "" && filepath.ToSlash(entry.Name()) != t.onlyFile {
			continue
		}
		path := filepath.Join(node.path, entry.Name())
		info, statErr := os.Lstat(path)
		if statErr != nil {
			// A disappearing entry is not selectable and should not make the
			// remaining directory unusable.
			continue
		}
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 || (!mode.IsDir() && !mode.IsRegular()) {
			continue
		}
		rel, relErr := filepath.Rel(t.root, path)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		children = append(children, &bundleNode{
			name: entry.Name(), relPath: filepath.ToSlash(rel), path: path,
			isDir: mode.IsDir(), parent: node,
		})
	}
	sortBundleChildren(children)
	node.children = children
	node.loaded = true
	return nil
}

func (t *bundleTree) selectInitial() {
	for _, child := range t.rootNode.children {
		if child.isDir {
			continue
		}
		if child.name == "SKILL.md" {
			t.selected = child.relPath
			t.cursor = 0
			return
		}
	}
	for _, child := range t.rootNode.children {
		if !child.isDir {
			t.selected = child.relPath
			return
		}
	}
}

func (t *bundleTree) rebuild() {
	t.visible = t.visible[:0]
	if t.rootNode == nil {
		return
	}
	var visit func(*bundleNode, int)
	visit = func(node *bundleNode, depth int) {
		if node != t.rootNode {
			t.visible = append(t.visible, bundleEntry{
				Name: node.name, RelPath: node.relPath, Path: node.path,
				Depth: depth, IsDir: node.isDir, Expanded: node.expanded,
				HasChild: node.isDir, Err: node.err, node: node,
			})
		}
		if !node.expanded {
			return
		}
		for _, child := range node.children {
			visit(child, depth+1)
		}
	}
	visit(t.rootNode, 0)
	if len(t.visible) == 0 {
		t.cursor = 0
		return
	}
	t.cursor = min(max(t.cursor, 0), len(t.visible)-1)
	if t.selected == "" || t.visible[t.cursor].IsDir {
		for i := range t.visible {
			if t.visible[i].RelPath == t.selected && !t.visible[i].IsDir {
				t.cursor = i
				return
			}
		}
		for i := range t.visible {
			if !t.visible[i].IsDir {
				t.cursor = i
				t.selected = t.visible[i].RelPath
				return
			}
		}
	}
}

func (t *bundleTree) restoreCursor(node *bundleNode) {
	for i, entry := range t.visible {
		if entry.node == node {
			t.cursor = i
			return
		}
	}
	t.cursor = min(max(t.cursor, 0), max(0, len(t.visible)-1))
}

func (t *bundleTree) entries() []bundleEntry { return t.visible }
func (t *bundleTree) selectedPath() string   { return t.selected }
func (t *bundleTree) selectedEntry() (bundleEntry, bool) {
	if t == nil || t.cursor < 0 || t.cursor >= len(t.visible) {
		return bundleEntry{}, false
	}
	return t.visible[t.cursor], true
}
func (t *bundleTree) move(delta int) {
	if len(t.visible) == 0 {
		return
	}
	t.cursor = min(max(t.cursor+delta, 0), len(t.visible)-1)
	entry := t.visible[t.cursor]
	if !entry.IsDir {
		t.selected = entry.RelPath
	}
}

func (t *bundleTree) toggle() {
	entry, ok := t.selectedEntry()
	if !ok || !entry.IsDir {
		return
	}
	if !entry.node.expanded {
		if err := t.load(entry.node); err != nil {
			slog.Error("tui bundle directory expansion failed", "path", entry.RelPath, "error", err)
			t.rebuild()
			return
		}
	}
	node := entry.node
	node.expanded = !node.expanded
	t.rebuild()
	t.restoreCursor(node)
}
func (t *bundleTree) expand() {
	entry, ok := t.selectedEntry()
	if !ok || !entry.IsDir || entry.node.expanded {
		return
	}
	if err := t.load(entry.node); err != nil {
		slog.Error("tui bundle directory expansion failed", "path", entry.RelPath, "error", err)
		t.rebuild()
		return
	}
	node := entry.node
	node.expanded = true
	t.rebuild()
	t.restoreCursor(node)
}
func (t *bundleTree) collapse() {
	entry, ok := t.selectedEntry()
	if !ok {
		return
	}
	if entry.IsDir && entry.node.expanded {
		node := entry.node
		node.expanded = false
		t.rebuild()
		t.restoreCursor(node)
		return
	}
	if entry.node != nil && entry.node.parent != nil && entry.node.parent != t.rootNode {
		parent := entry.node.parent
		for i, visible := range t.visible {
			if visible.node == parent {
				t.cursor = i
				return
			}
		}
	}
}

func (t *bundleTree) restoreSelection(rel string) bool {
	if t == nil || t.rootNode == nil || rel == "" {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.ToSlash(clean) != rel {
		return false
	}
	node := t.rootNode
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if !node.loaded {
			if err := t.load(node); err != nil {
				return false
			}
		}
		var child *bundleNode
		for _, candidate := range node.children {
			if candidate.name == part {
				child = candidate
				break
			}
		}
		if child == nil {
			return false
		}
		node.expanded = true
		node = child
	}
	if node.isDir {
		return false
	}
	t.selected = rel
	t.rebuild()
	for i, entry := range t.visible {
		if entry.RelPath == rel && !entry.IsDir {
			t.cursor = i
			return true
		}
	}
	return false
}

func sortBundleChildren(children []*bundleNode) {
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].isDir != children[j].isDir {
			return children[i].isDir
		}
		return children[i].name < children[j].name
	})
}

func bundleTreeForRow(r row) (*bundleTree, error) {
	if r.Artifact.IsMarketplace {
		if r.Artifact.Path == "" {
			return nil, fmt.Errorf("marketplace resource has no verified local content")
		}
		if isMarketplaceArtifactPath(r.Artifact.Path) {
			return newMarketplaceArtifactTree(r.Artifact.Path)
		}
	}
	root, only, err := bundleRootForRow(r)
	if err != nil {
		return nil, err
	}
	return newBundleTree(root, only), nil
}

// newMarketplaceArtifactTree opens an installed Marketplace resource whose
// verified content is a canonical artifact.tar.gz rather than a browsable
// directory. The sibling verified.marker must be present and the archive is
// decoded, size-bounded, and cryptographically re-verified before any member
// reaches the browser; missing or corrupt content fails closed.
func newMarketplaceArtifactTree(artifactPath string) (*bundleTree, error) {
	info, err := os.Lstat(artifactPath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("marketplace artifact is not a regular file")
	}
	marker, err := os.Lstat(filepath.Join(filepath.Dir(artifactPath), marketplaceVerifiedMarker))
	if err != nil {
		return nil, fmt.Errorf("marketplace artifact is not verified: %w", err)
	}
	if marker.Mode()&os.ModeSymlink != 0 || !marker.Mode().IsRegular() {
		return nil, fmt.Errorf("marketplace artifact verification marker is not a regular file")
	}
	f, err := os.OpenFile(artifactPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("marketplace artifact is not a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxMarketplaceArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxMarketplaceArtifactBytes {
		return nil, fmt.Errorf("marketplace artifact exceeds %d MiB", maxMarketplaceArtifactBytes>>20)
	}
	ca, _, err := hccartifact.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode marketplace artifact: %w", err)
	}
	if err := ca.ValidateArchive(nil); err != nil {
		return nil, fmt.Errorf("validate marketplace artifact: %w", err)
	}
	manifest, err := json.MarshalIndent(ca.Manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode marketplace artifact manifest: %w", err)
	}
	files := map[string][]byte{hccartifact.CanonicalManifestPath: manifest}
	payload := ""
	for _, member := range []struct {
		dir  string
		data map[string][]byte
	}{
		{dir: hccartifact.CanonicalAssetsDir, data: ca.Assets},
		{dir: hccartifact.CanonicalLicensesDir, data: ca.Licenses},
	} {
		for name, data := range member.data {
			files[member.dir+"/"+name] = data
		}
	}
	if len(ca.Payload) > 0 {
		payload = hccartifact.PayloadPath(ca.Manifest.Kind)
		payloadContent := ca.Payload
		if ca.Manifest.Kind == ir.KindSkill.String() {
			payload = "SKILL.md"
			payloadContent, err = marketplaceSkillMarkdown(ca.Payload)
			if err != nil {
				return nil, fmt.Errorf("render marketplace skill: %w", err)
			}
		}
		files[payload] = payloadContent
	}

	t := &bundleTree{files: files}
	t.rootNode = &bundleNode{name: ca.Manifest.Name, expanded: true, loaded: true}
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		t.insertArchivePath(rel)
	}
	t.expandArchiveDirs(t.rootNode)
	if payload != "" {
		t.selected = payload
	} else {
		t.selectInitial()
	}
	t.rebuild()
	return t, nil
}

func marketplaceSkillMarkdown(payload []byte) ([]byte, error) {
	resource, err := hccartifact.DecodeResource(ir.KindSkill.String(), payload)
	if err != nil {
		return nil, err
	}
	result, err := hcc.Compile(resource, hcc.TargetFactory)
	if err != nil {
		return nil, err
	}
	for _, compiled := range result.Artifacts {
		if filepath.Base(compiled.Path) == "SKILL.md" {
			return compiled.Bytes, nil
		}
	}
	return nil, fmt.Errorf("skill compilation produced no Markdown artifact")
}

// insertArchivePath materializes one slash-separated archive member path into
// the node tree, creating intermediate directory nodes as needed.
func (t *bundleTree) insertArchivePath(rel string) {
	node := t.rootNode
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		var child *bundleNode
		for _, candidate := range node.children {
			if candidate.name == part {
				child = candidate
				break
			}
		}
		if child == nil {
			child = &bundleNode{
				name: part, relPath: strings.Join(parts[:i+1], "/"),
				isDir: i < len(parts)-1, parent: node,
			}
			node.children = append(node.children, child)
		}
		node = child
	}
}

// expandArchiveDirs marks decoded archive directories loaded and expanded and
// applies the same deterministic dirs-first ordering as filesystem loads.
// Canonical archives are bounded by the archive limits, so expansion is cheap.
func (t *bundleTree) expandArchiveDirs(node *bundleNode) {
	sortBundleChildren(node.children)
	for _, child := range node.children {
		if child.isDir {
			child.loaded = true
			child.expanded = true
			t.expandArchiveDirs(child)
		}
	}
}

func bundleTreeRootLabel(t *bundleTree) string {
	if t == nil || t.rootNode == nil {
		return "bundle"
	}
	return sanitizeTerminalData(t.rootNode.name+"/", terminalSingleLine)
}
