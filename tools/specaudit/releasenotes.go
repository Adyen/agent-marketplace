package main

import (
	"regexp"
	"strings"
)

const (
	releaseNoteHeading = "## Release note"
	breakingPrefix     = "Breaking:"
)

var noneNote = regexp.MustCompile(`^none(\s|—|-|$)`)

// releaseNote returns the lines of the first "## Release note" section outside
// fenced code, without trailing whitespace or leading and trailing blank lines,
// and whether the section exists at all.
func releaseNote(content string) ([]string, bool) {
	var (
		lines  []string
		found  bool
		fenced bool
	)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		} else if !fenced {
			if !found {
				found = strings.TrimSpace(line) == releaseNoteHeading
				continue
			}
			if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
				break
			}
		}
		if found {
			lines = append(lines, strings.TrimRight(line, " \t"))
		}
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, found
}

// normalizedNote joins note lines into one whitespace-normalized line.
func normalizedNote(lines []string) string {
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

func isNoneNote(text string) bool {
	return noneNote.MatchString(text)
}

// breakingText returns the text after the Breaking: prefix of a normalized note.
func breakingText(text string) (string, bool) {
	body, ok := strings.CutPrefix(text, breakingPrefix)
	return strings.TrimSpace(body), ok
}
