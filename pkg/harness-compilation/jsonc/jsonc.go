// Package jsonc is the single concrete-syntax JSON/JSONC layer for harness
// aggregate files.
//
// Shared Devin JSON files (hooks.v1.json, config.json, mcp_config.json) may
// contain comments, and the promoted plan requires that "Structural patching
// must preserve unmanaged comments and key order, including comment
// association with the same unmanaged key or array element"
// (docs/plans/2026-08-03-devin-first-class-harness.md, "JSON preservation").
//
// It is deliberately ONE parse pass. Bead agent-marketplace-5exj.16 flagged
// that a comment/order-preserving parser must ABSORB duplicate-key detection
// rather than layering a third pass on top of stripJSONComments +
// jsonutil.Unmarshal, so Parse rejects duplicate object keys itself with the
// same stable coded diagnostic (json.duplicate-key / json.malformed,
// *diag.CodedError) that internal/jsonutil produces. Unmarshal is the strict
// decode entry point for JSONC bytes: comments are ignored by the tokenizer,
// so they can never hide a duplicate key from the scan, and the scan always
// runs before encoding/json sees anything.
//
// Two deliberate strictness choices:
//
//   - Trailing commas are rejected (json.malformed). Devin's own parser
//     behaviour for them is unverified (docs/devin-cli-harness-spec-3000.3.md
//     lists comment handling as "Inferred"), and emitting a document we cannot
//     prove Devin reads would be worse than refusing to touch the file.
//   - No value is ever re-serialized on a patch. Untouched members keep their
//     exact original bytes, so handwritten formatting inside an unmanaged entry
//     survives byte-for-byte.
package jsonc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
)

// Kind classifies a concrete-syntax node.
type Kind int

const (
	// KindLiteral is any non-container value (string, number, bool, null) and
	// is also used for raw JSON injected by a patch.
	KindLiteral Kind = iota
	// KindObject is a JSON object with ordered members.
	KindObject
	// KindArray is a JSON array with ordered elements.
	KindArray
)

// Node is one concrete-syntax value. Trivia (whitespace and comments) is
// stored on the surrounding structure, never inside a value, so replacing a
// value cannot disturb a neighbour's comments.
type Node struct {
	kind          Kind
	trailingComma bool

	// raw holds the exact source bytes for KindLiteral.
	raw []byte

	members  []*member  // KindObject
	elements []*element // KindArray

	// tail is the trivia between the last member/element (or the opening
	// delimiter, when the container is empty) and the closing delimiter.
	tail []byte
}

// member is one object member plus the trivia around it.
type member struct {
	lead   []byte // trivia after '{' or ',' and before the key: comments belong to THIS member
	keyRaw []byte // the quoted key, exactly as written
	key    string // the decoded key
	sep    []byte // trivia and ':' between key and value
	value  *Node
	trail  []byte // trivia after the value, before ','
}

// element is one array element plus the trivia around it.
type element struct {
	lead  []byte
	value *Node
	trail []byte
}

// Document is a parsed JSONC document.
type Document struct {
	file    string
	lead    []byte // trivia before the top-level value
	root    *Node
	trail   []byte // trivia after the top-level value
	changed bool
}

// Parse parses data as JSONC. file is used only for diagnostics. Failures are
// *diag.CodedError values carrying json.duplicate-key or json.malformed plus
// the 1-based line the failure was detected at.
func Parse(file string, data []byte) (*Document, error) {
	return ParseWithOptions(file, data, ParseOptions{})
}

// ParseOptions enables syntax extensions required by a particular native format.
type ParseOptions struct{ AllowTrailingCommas bool }

// ParseWithOptions preserves native syntax while retaining duplicate-key checks.
func ParseWithOptions(file string, data []byte, options ParseOptions) (*Document, error) {
	p := &parser{data: data, allowTrailingCommas: options.AllowTrailingCommas}
	doc := &Document{file: file}
	doc.lead = p.trivia()
	root, err := p.value("$")
	if err != nil {
		return nil, jsonutil.Coded(file, data, err)
	}
	doc.root = root
	doc.trail = p.trivia()
	if p.pos != len(p.data) {
		return nil, jsonutil.Coded(file, data, fmt.Errorf("trailing JSON data at byte %d", p.pos))
	}
	return doc, nil
}

// Bytes renders the document, preserving comments, key order, and the exact
// bytes of every value that was not patched.
func (d *Document) Bytes() []byte {
	var buf bytes.Buffer
	buf.Write(d.lead)
	d.root.write(&buf)
	buf.Write(d.trail)
	return buf.Bytes()
}

// Changed reports whether any patch actually modified the document. Callers use
// it to skip a write (and therefore a safety backup and a journal generation)
// when reconciliation is a no-op.
func (d *Document) Changed() bool { return d.changed }

// Root returns the top-level node.
func (d *Document) Root() *Node { return d.root }

// ReplaceRoot replaces the whole top-level value with raw JSON, keeping the
// trivia before and after the document (e.g. a file-header comment) intact.
//
// It exists for the one case a preserving patch cannot improve on: a
// destination whose root carries nothing to preserve (an empty object, or a
// non-object such as null) but which must come out as a conventionally
// formatted document. Patching member-by-member into an empty container would
// emit the closing brace on the last member's line, because the trivia that
// should precede it never existed in the source.
func (d *Document) ReplaceRoot(raw []byte) {
	if bytes.Equal(d.root.Raw(), raw) {
		return
	}
	d.root = &Node{kind: KindLiteral, raw: append([]byte(nil), raw...)}
	d.changed = true
}

// Decode decodes the already-parsed document into v. It is the zero-extra-parse
// counterpart of Unmarshal for callers that also need to patch the document:
// duplicate keys were already rejected by Parse, so the canonical rendering is
// safe to hand to encoding/json.
func (d *Document) Decode(v any) error {
	return jsonutil.Decode(d.file, d.root.Canonical(), v)
}

// Canonical returns strict comment-free JSON for the node, preserving the
// source order of object members. Use it to hand a sub-tree to encoding/json;
// Raw returns concrete syntax and may contain comments.
func (n *Node) Canonical() []byte {
	var buf bytes.Buffer
	n.writeCanonical(&buf)
	return buf.Bytes()
}

// AppendIndent returns the indentation (no leading newline) that a member or
// element appended to n will start its line with, derived from the node's
// existing shape. Callers use it as the json.MarshalIndent prefix so an
// inserted value lines up with the document it is inserted into.
func (n *Node) AppendIndent() string {
	lead, _ := n.appendShape()
	return string(bytes.TrimLeft(lead, "\r\n"))
}

// Kind returns the node kind.
func (n *Node) Kind() Kind { return n.kind }

// Raw returns the exact source bytes of the node.
func (n *Node) Raw() []byte {
	var buf bytes.Buffer
	n.write(&buf)
	return buf.Bytes()
}

// Keys returns the object's member keys in source order.
func (n *Node) Keys() []string {
	if n.kind != KindObject {
		return nil
	}
	keys := make([]string, 0, len(n.members))
	for _, m := range n.members {
		keys = append(keys, m.key)
	}
	return keys
}

// Member returns the value stored under key.
func (n *Node) Member(key string) (*Node, bool) {
	if n.kind != KindObject {
		return nil, false
	}
	for _, m := range n.members {
		if m.key == key {
			return m.value, true
		}
	}
	return nil, false
}

// Len returns the number of array elements.
func (n *Node) Len() int {
	if n.kind != KindArray {
		return 0
	}
	return len(n.elements)
}

// Element returns the array element at index.
func (n *Node) Element(i int) (*Node, bool) {
	if n.kind != KindArray || i < 0 || i >= len(n.elements) {
		return nil, false
	}
	return n.elements[i].value, true
}

// SetMember sets key to raw JSON. An existing key keeps its position and its
// associated comments; a new key is appended after the last member and reuses
// the last member's indentation so the file keeps its shape. Returns false when
// the node is not an object.
//
// raw must be valid JSON; callers build it with encoding/json.Marshal on data
// they own, never on user bytes.
func (d *Document) SetMember(n *Node, key string, raw []byte) bool {
	if n == nil || n.kind != KindObject {
		return false
	}
	for _, m := range n.members {
		if m.key != key {
			continue
		}
		if bytes.Equal(m.value.Raw(), raw) {
			return true
		}
		m.value = &Node{kind: KindLiteral, raw: append([]byte(nil), raw...)}
		d.changed = true
		return true
	}
	keyRaw, err := json.Marshal(key)
	if err != nil {
		return false
	}
	lead, sep := n.appendShape()
	n.members = append(n.members, &member{
		lead:   lead,
		keyRaw: keyRaw,
		key:    key,
		sep:    sep,
		value:  &Node{kind: KindLiteral, raw: append([]byte(nil), raw...)},
	})
	d.changed = true
	return true
}

// DeleteMember removes key together with the trivia that belongs to it (its own
// leading comments), leaving every other member's comments attached to the same
// member. Returns true when a member was removed.
func (d *Document) DeleteMember(n *Node, key string) bool {
	if n == nil || n.kind != KindObject {
		return false
	}
	for i, m := range n.members {
		if m.key != key {
			continue
		}
		if i == len(n.members)-1 && len(n.members) > 1 {
			// The removed member owned the pre-close trivia position; keep the
			// previous member's trail as the new pre-close trivia so the closing
			// brace does not end up on the value's line.
			n.tail = slices.Concat(n.members[i-1].trail, n.tail)
			n.members[i-1].trail = nil
		}
		n.members = append(n.members[:i], n.members[i+1:]...)
		if len(n.members) == 0 && len(bytes.TrimSpace(n.tail)) == 0 {
			n.tail = nil
		}
		d.changed = true
		return true
	}
	return false
}

// SetElements replaces the array's elements with the given raw JSON values.
// Elements are matched positionally against the previous contents first, so an
// element whose bytes are unchanged keeps its exact source bytes and its
// comments. This is how "handwritten hooks keep their relative order" is
// implemented: the caller passes the preserved handwritten elements first, in
// their original order, followed by the generated ones.
//
// An element whose bytes reappear at a DIFFERENT index is carried over whole,
// its own leading trivia included, rather than inheriting whatever trivia the
// new index used to hold. A comment belongs to the element that FOLLOWS it, so
// when the element in front of it is deleted the comment must travel with its
// element instead of being left on the vacated slot and dropped. Positional
// matching still wins, so nothing moves that did not have to.
func (d *Document) SetElements(n *Node, raws [][]byte) bool {
	if n == nil || n.kind != KindArray {
		return false
	}
	resolved := make([]*element, len(raws))
	used := make([]bool, len(n.elements))
	// Rendering an element's source bytes walks its whole subtree, so each one
	// is rendered at most once even though the positional pass, the move pass,
	// and a large array's every comparison all want it.
	rendered := make([][]byte, len(n.elements))
	rawAt := func(j int) []byte {
		if rendered[j] == nil {
			rendered[j] = n.elements[j].value.Raw()
		}
		return rendered[j]
	}
	same := len(raws) == len(n.elements)
	unresolved := 0
	for i, raw := range raws {
		if i < len(n.elements) && bytes.Equal(rawAt(i), raw) {
			resolved[i] = n.elements[i]
			used[i] = true
			continue
		}
		same = false
		unresolved++
	}
	if same {
		return true
	}
	// An element that survived at a different index is carried over whole so
	// its comment travels with it. Only elements nothing matched positionally
	// are searched for, and only when there is something to search for.
	//
	// The index is keyed on the WANTED bytes, of which there are only as many
	// as the caller could not place positionally, and the scan stops the moment
	// they are all accounted for. An element is rendered only if the scan
	// actually reaches it.
	if unresolved > 0 {
		wanted := make(map[string][]int, unresolved)
		for i, raw := range raws {
			if resolved[i] != nil {
				continue
			}
			wanted[string(raw)] = append(wanted[string(raw)], i)
		}
		for j := range n.elements {
			if unresolved == 0 {
				break
			}
			if used[j] {
				continue
			}
			queue := wanted[string(rawAt(j))]
			if len(queue) == 0 {
				continue
			}
			wanted[string(rawAt(j))] = queue[1:]
			resolved[queue[0]] = n.elements[j]
			used[j] = true
			unresolved--
		}
	}
	next := make([]*element, 0, len(raws))
	for i, raw := range raws {
		if resolved[i] != nil {
			next = append(next, resolved[i])
			continue
		}
		lead, _ := n.appendShape()
		if i < len(n.elements) && !used[i] {
			lead = n.elements[i].lead
		}
		next = append(next, &element{lead: lead, value: &Node{kind: KindLiteral, raw: append([]byte(nil), raw...)}})
	}
	if len(next) > 0 {
		// The last element must not keep a trailing-comma trivia slot.
		n.tail = slices.Concat(next[len(next)-1].trail, n.tail)
		next[len(next)-1].trail = nil
	}
	n.elements = next
	d.changed = true
	return true
}

// appendShape derives the lead trivia and key/value separator used for a newly
// appended member or element, copying the shape of the existing contents.
func (n *Node) appendShape() (lead, sep []byte) {
	sep = []byte(": ")
	switch {
	case n.kind == KindObject && len(n.members) > 0:
		last := n.members[len(n.members)-1]
		return indentOnly(last.lead), append([]byte(nil), last.sep...)
	case n.kind == KindArray && len(n.elements) > 0:
		return indentOnly(n.elements[len(n.elements)-1].lead), sep
	default:
		return indentOnly(n.tail), sep
	}
}

// indentOnly returns the newline-plus-indentation suffix of trivia, dropping
// any comments: a comment belongs to the member it was written above and must
// never be duplicated onto a generated member.
func indentOnly(trivia []byte) []byte {
	idx := bytes.LastIndexByte(trivia, '\n')
	if idx < 0 {
		return []byte("\n  ")
	}
	rest := trivia[idx:]
	for _, b := range rest {
		if b != '\n' && b != ' ' && b != '\t' && b != '\r' {
			return []byte("\n  ")
		}
	}
	return append([]byte(nil), rest...)
}

func (n *Node) write(buf *bytes.Buffer) {
	switch n.kind {
	case KindObject:
		buf.WriteByte('{')
		for i, m := range n.members {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.Write(m.lead)
			buf.Write(m.keyRaw)
			buf.Write(m.sep)
			m.value.write(buf)
			buf.Write(m.trail)
		}
		if n.trailingComma && len(n.members) > 0 {
			buf.WriteByte(',')
		}
		buf.Write(n.tail)
		buf.WriteByte('}')
	case KindArray:
		buf.WriteByte('[')
		for i, e := range n.elements {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.Write(e.lead)
			e.value.write(buf)
			buf.Write(e.trail)
		}
		if n.trailingComma && len(n.elements) > 0 {
			buf.WriteByte(',')
		}
		buf.Write(n.tail)
		buf.WriteByte(']')
	default:
		buf.Write(n.raw)
	}
}

// Canonicalize returns strict comment-free JSON for data, rejecting duplicate
// object keys in the same pass. It is the replacement for
// stripJSONComments-then-validate: comments are removed by the tokenizer, so
// a commented-out duplicate is not a duplicate and a real duplicate cannot be
// hidden behind a comment.
func Canonicalize(file string, data []byte) ([]byte, error) {
	doc, err := Parse(file, data)
	if err != nil {
		return nil, err
	}
	return doc.root.Canonical(), nil
}

// Unmarshal canonicalizes JSONC bytes and decodes them into v. It is the only
// sanctioned decode path for harness files that may contain comments.
func Unmarshal(file string, data []byte, v any) error {
	canonical, err := Canonicalize(file, data)
	if err != nil {
		return err
	}
	return jsonutil.Decode(file, canonical, v)
}

func (n *Node) writeCanonical(buf *bytes.Buffer) {
	switch n.kind {
	case KindObject:
		buf.WriteByte('{')
		for i, m := range n.members {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.Write(m.keyRaw)
			buf.WriteByte(':')
			m.value.writeCanonical(buf)
		}
		buf.WriteByte('}')
	case KindArray:
		buf.WriteByte('[')
		for i, e := range n.elements {
			if i > 0 {
				buf.WriteByte(',')
			}
			e.value.writeCanonical(buf)
		}
		buf.WriteByte(']')
	default:
		buf.Write(n.raw)
	}
}
