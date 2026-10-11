package analyze

import "unicode/utf8"

type sourceRangeCursor struct {
	content string
	offset  int
	line    int
	column  int
}

func (c *sourceRangeCursor) at(file string, offset int) *SourceRange {
	if offset < 0 || offset > len(c.content) {
		return &SourceRange{File: file}
	}
	if c.line == 0 || offset < c.offset {
		c.offset, c.line, c.column = 0, 1, 1
	}
	for c.offset < offset {
		r, size := utf8.DecodeRuneInString(c.content[c.offset:])
		if r == '\n' {
			c.line++
			c.column = 1
		} else {
			c.column++
		}
		c.offset += size
	}
	return &SourceRange{File: file, Line: c.line, Column: c.column}
}
