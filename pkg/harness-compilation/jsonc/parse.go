package jsonc

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
)

// maxDepth bounds parser recursion. Harness aggregates nest a handful of
// levels; this is insurance against hostile input, mirroring the cap in
// internal/jsonutil.
const maxDepth = 1000

type parser struct {
	data []byte
	pos  int
	err  error
}

// trivia consumes whitespace, // line comments, and /* */ block comments and
// returns the exact bytes consumed.
func (p *parser) trivia() []byte {
	if p.err != nil {
		return nil
	}
	start := p.pos
	for p.pos < len(p.data) {
		switch c := p.data[p.pos]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.pos++
		case c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '/':
			for p.pos < len(p.data) && p.data[p.pos] != '\n' {
				p.pos++
			}
		case c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '*':
			commentStart := p.pos
			p.pos += 2
			closed := false
			for p.pos < len(p.data) {
				if p.data[p.pos] == '*' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '/' {
					p.pos += 2
					closed = true
					break
				}
				p.pos++
			}
			if !closed {
				p.err = fmt.Errorf("unterminated block comment at byte %d", commentStart)
				slog.Error("unterminated JSONC block comment", "offset", commentStart)
				p.pos = commentStart
				return p.data[start:p.pos]
			}
		default:
			return p.data[start:p.pos]
		}
	}
	return p.data[start:p.pos]
}

func (p *parser) value(path string) (*Node, error) {
	return p.valueAt(path, 0)
}

func (p *parser) valueAt(path string, depth int) (*Node, error) {
	if p.err != nil {
		return nil, p.err
	}
	if depth > maxDepth {
		return nil, fmt.Errorf("%w at %s", jsonutil.ErrMaxDepth, path)
	}
	if p.pos >= len(p.data) {
		return nil, fmt.Errorf("unexpected end of JSON input at %s", path)
	}
	switch p.data[p.pos] {
	case '{':
		return p.object(path, depth)
	case '[':
		return p.array(path, depth)
	default:
		return p.literal(path)
	}
}

func (p *parser) object(path string, depth int) (*Node, error) {
	p.pos++ // '{'
	node := &Node{kind: KindObject}
	seen := make(map[string]struct{})
	for {
		lead := p.trivia()
		if p.err != nil {
			return nil, p.err
		}
		if p.pos >= len(p.data) {
			return nil, fmt.Errorf("unexpected end of JSON input in object at %s", path)
		}
		if p.data[p.pos] == '}' {
			if len(node.members) > 0 {
				// A '}' right after a comma is a trailing comma.
				return nil, fmt.Errorf("trailing comma in object at %s", path)
			}
			node.tail = lead
			p.pos++
			return node, nil
		}
		keyStart := p.pos
		if p.data[p.pos] != '"' {
			return nil, fmt.Errorf("%s: object key is not a string", path)
		}
		if err := p.skipString(); err != nil {
			return nil, err
		}
		keyRaw := p.data[keyStart:p.pos]
		var key string
		if err := json.Unmarshal(keyRaw, &key); err != nil {
			return nil, fmt.Errorf("%s: invalid object key: %w", path, err)
		}
		if _, exists := seen[key]; exists {
			return nil, &jsonutil.DuplicateKeyError{Key: key, Path: path, Offset: int64(p.pos)}
		}
		seen[key] = struct{}{}

		sepStart := p.pos
		p.trivia()
		if p.err != nil {
			return nil, p.err
		}
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, fmt.Errorf("%s.%s: expected ':' after object key", path, key)
		}
		p.pos++
		p.trivia()
		if p.err != nil {
			return nil, p.err
		}
		sep := p.data[sepStart:p.pos]

		value, err := p.valueAt(path+"."+key, depth+1)
		if err != nil {
			return nil, err
		}
		m := &member{lead: lead, keyRaw: keyRaw, key: key, sep: sep, value: value}
		node.members = append(node.members, m)

		after := p.trivia()
		if p.err != nil {
			return nil, p.err
		}
		if p.pos >= len(p.data) {
			return nil, fmt.Errorf("unexpected end of JSON input in object at %s", path)
		}
		switch p.data[p.pos] {
		case ',':
			m.trail = after
			p.pos++
		case '}':
			node.tail = after
			p.pos++
			return node, nil
		default:
			return nil, fmt.Errorf("%s: expected ',' or '}' after object member %q", path, key)
		}
	}
}

func (p *parser) array(path string, depth int) (*Node, error) {
	p.pos++ // '['
	node := &Node{kind: KindArray}
	for index := 0; ; index++ {
		lead := p.trivia()
		if p.err != nil {
			return nil, p.err
		}
		if p.pos >= len(p.data) {
			return nil, fmt.Errorf("unexpected end of JSON input in array at %s", path)
		}
		if p.data[p.pos] == ']' {
			if len(node.elements) > 0 {
				return nil, fmt.Errorf("trailing comma in array at %s", path)
			}
			node.tail = lead
			p.pos++
			return node, nil
		}
		value, err := p.valueAt(fmt.Sprintf("%s[%d]", path, index), depth+1)
		if err != nil {
			return nil, err
		}
		e := &element{lead: lead, value: value}
		node.elements = append(node.elements, e)

		after := p.trivia()
		if p.err != nil {
			return nil, p.err
		}
		if p.pos >= len(p.data) {
			return nil, fmt.Errorf("unexpected end of JSON input in array at %s", path)
		}
		switch p.data[p.pos] {
		case ',':
			e.trail = after
			p.pos++
		case ']':
			node.tail = after
			p.pos++
			return node, nil
		default:
			return nil, fmt.Errorf("%s: expected ',' or ']' after array element %d", path, index)
		}
	}
}

// literal consumes a string, number, true, false, or null and validates it with
// encoding/json so no malformed scalar can be preserved and written back.
func (p *parser) literal(path string) (*Node, error) {
	if p.err != nil {
		return nil, p.err
	}
	start := p.pos
	if p.data[p.pos] == '"' {
		if err := p.skipString(); err != nil {
			return nil, err
		}
	} else {
		for p.pos < len(p.data) && !isLiteralTerminator(p.data[p.pos]) {
			p.pos++
		}
	}
	raw := p.data[start:p.pos]
	if len(raw) == 0 {
		return nil, fmt.Errorf("%s: expected a JSON value", path)
	}
	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("%s: invalid JSON value: %w", path, err)
	}
	return &Node{kind: KindLiteral, raw: raw}, nil
}

func isLiteralTerminator(c byte) bool {
	switch c {
	case ',', '}', ']', ' ', '\t', '\n', '\r', '/':
		return true
	}
	return false
}

// skipString advances past a JSON string starting at the current quote.
func (p *parser) skipString() error {
	p.pos++ // opening quote
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case '\\':
			p.pos += 2
			continue
		case '"':
			p.pos++
			return nil
		}
		p.pos++
	}
	return fmt.Errorf("unterminated JSON string at byte %d", p.pos)
}
