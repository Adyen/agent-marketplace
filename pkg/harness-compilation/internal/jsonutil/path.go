package jsonutil

import (
	"strconv"
	"strings"
)

type pathSegment struct {
	key   string
	index int
	array bool
}

// JSONPath retains diagnostic segments without copying every ancestor key.
// Child paths share traversal-local scratch storage; render errors before
// visiting a sibling rather than retaining an unfinished path.
type JSONPath struct {
	segments *[]pathSegment
	depth    int
}

// RootPath creates one reusable scratch stack for a single parser invocation.
func RootPath() JSONPath {
	return JSONPath{segments: new([]pathSegment)}
}

func (p JSONPath) Key(key string) JSONPath {
	return p.child(pathSegment{key: key})
}

func (p JSONPath) Index(index int) JSONPath {
	return p.child(pathSegment{index: index, array: true})
}

func (p JSONPath) child(segment pathSegment) JSONPath {
	if p.segments == nil {
		p = RootPath()
	}
	*p.segments = append((*p.segments)[:p.depth], segment)
	p.depth++
	return p
}

func (p JSONPath) String() string {
	if p.depth == 0 {
		return "$"
	}
	var out strings.Builder
	out.WriteByte('$')
	for _, segment := range (*p.segments)[:p.depth] {
		if segment.array {
			out.WriteByte('[')
			out.WriteString(strconv.Itoa(segment.index))
			out.WriteByte(']')
		} else {
			out.WriteByte('.')
			out.WriteString(segment.key)
		}
	}
	return out.String()
}
