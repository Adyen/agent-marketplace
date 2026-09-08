package tui

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
}

func bundleRootForRow(r row) (root, onlyFile string, err error) {
	if r.Artifact.Path == "" {
		return "", "", fmt.Errorf("resource has no source path")
	}
	info, err := os.Lstat(r.Artifact.Path)
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
		root = filepath.Dir(r.Artifact.Path)
		onlyFile = filepath.Base(r.Artifact.Path)
	} else if info.IsDir() {
		root = r.Artifact.Path
	} else if info.Mode().IsRegular() {
		root = filepath.Dir(r.Artifact.Path)
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
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].isDir != children[j].isDir {
			return children[i].isDir
		}
		return children[i].name < children[j].name
	})
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
func (t *bundleTree) top() {
	if len(t.visible) > 0 {
		t.cursor = 0
		if !t.visible[0].IsDir {
			t.selected = t.visible[0].RelPath
		}
	}
}
func (t *bundleTree) bottom() {
	if len(t.visible) > 0 {
		t.cursor = len(t.visible) - 1
		if !t.visible[t.cursor].IsDir {
			t.selected = t.visible[t.cursor].RelPath
		}
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

func bundleTreeForRow(r row) (*bundleTree, error) {
	root, only, err := bundleRootForRow(r)
	if err != nil {
		return nil, err
	}
	return newBundleTree(root, only), nil
}

func bundleTreeRootLabel(t *bundleTree) string {
	if t == nil || t.rootNode == nil {
		return "bundle"
	}
	return sanitizeTerminalData(t.rootNode.name+"/", terminalSingleLine)
}
