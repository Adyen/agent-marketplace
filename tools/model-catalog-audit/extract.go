package main

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var errSourceFormat = errors.New("source format not recognized")

var inlineCode = regexp.MustCompile("`([^`]+)`")
var modelID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
var modelDetails = regexp.MustCompile(`(?s)<ModelDetails\s[^>]*?\bslug="([^"]+)"`)
var geminiConstant = regexp.MustCompile(`(?m)^export (?:const|let) ([A-Z0-9_]+)\s*=\s*('([^']+)'|[A-Z0-9_]+);`)
var geminiSet = regexp.MustCompile(`(?s)export const VALID_GEMINI_MODELS = new Set\(\[([^]]+)\]\);`)
var markdownLink = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
var heading = regexp.MustCompile(`^(#{1,6}) (.+)$`)

type source struct {
	Name     string
	Harness  string
	URL      string
	Kind     string
	Fields   []string
	Sections []string
}

type observedModel struct {
	ID      string   `json:"id"`
	Efforts []string `json:"efforts,omitempty"`
}

type observation struct {
	Models []observedModel `json:"models,omitempty"`
	Text   string          `json:"text,omitempty"`
}

func extract(s source, body []byte) (observation, error) {
	text := string(body)
	var result observation
	switch s.Kind {
	case "claude":
		for _, line := range strings.Split(text, "\n") {
			cells := tableCells(line)
			if len(cells) > 1 && cells[0] == "Claude API ID" {
				for _, cell := range cells[1:] {
					ids := codeValues(cell)
					if len(ids) != 1 {
						return result, errSourceFormat
					}
					result.Models = append(result.Models, observedModel{ID: ids[0]})
				}
			}
		}
	case "factory", "cursor":
		var err error
		result, err = extractModelTable(s.Kind, text)
		if err != nil {
			return result, err
		}
	case "codex":
		matches := modelDetails.FindAllStringSubmatch(text, -1)
		if len(matches) != strings.Count(text, "<ModelDetails") {
			return result, errSourceFormat
		}
		for _, match := range matches {
			result.Models = append(result.Models, observedModel{ID: match[1]})
		}
	case "gemini":
		var err error
		result.Models, err = extractGemini(text)
		if err != nil {
			return result, err
		}
	case "watch":
		var selected []string
		for _, field := range s.Fields {
			found := false
			for _, line := range strings.Split(text, "\n") {
				cells := tableCells(line)
				if len(cells) > 1 && cells[0] == "`"+field+"`" {
					selected = append(selected, "| "+strings.Join(cells, " | ")+" |")
					found = true
				}
			}
			if !found {
				return result, fmt.Errorf("%w: missing field %s", errSourceFormat, field)
			}
		}
		for _, title := range s.Sections {
			section, err := markdownSection(text, title)
			if err != nil {
				return result, err
			}
			selected = append(selected, section)
		}
		result.Text = normalizeText(strings.Join(selected, "\n"))
	default:
		return result, errSourceFormat
	}
	if len(result.Models) == 0 && result.Text == "" {
		return result, errSourceFormat
	}
	slices.SortFunc(result.Models, func(a, b observedModel) int { return strings.Compare(a.ID, b.ID) })
	for i, m := range result.Models {
		if !modelID.MatchString(m.ID) || i > 0 && result.Models[i-1].ID == m.ID {
			return observation{}, fmt.Errorf("%w: invalid or duplicate model ID", errSourceFormat)
		}
	}
	return result, nil
}

func extractGemini(text string) ([]observedModel, error) {
	set := geminiSet.FindAllStringSubmatch(text, -1)
	if len(set) != 1 {
		return nil, errSourceFormat
	}
	constants := map[string]string{}
	for _, match := range geminiConstant.FindAllStringSubmatch(text, -1) {
		constants[match[1]] = match[2]
	}
	seen := map[string]bool{}
	var models []observedModel
	for _, token := range strings.Split(set[0][1], ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		visited := map[string]bool{}
		for !strings.HasPrefix(token, "'") {
			value, ok := constants[token]
			if !ok || visited[token] {
				return nil, errSourceFormat
			}
			visited[token] = true
			token = value
		}
		id := strings.Trim(token, "'")
		if id != "none" && !seen[id] {
			seen[id] = true
			models = append(models, observedModel{ID: id})
		}
	}
	return models, nil
}

func extractModelTable(kind, text string) (observation, error) {
	var result observation
	var header []string
	var rows []string
	for _, line := range strings.Split(text, "\n") {
		cells := tableCells(line)
		if len(cells) == 0 {
			header = nil
			continue
		}
		if cells[0] == "Model" {
			header = cells
			continue
		}
		if len(header) == 0 || strings.Trim(cells[0], ":- ") == "" {
			continue
		}
		if len(cells) != len(header) {
			return result, errSourceFormat
		}
		column := func(name string) string {
			if i := slices.Index(header, name); i >= 0 {
				return cells[i]
			}
			return ""
		}
		if kind == "factory" {
			ids, efforts := codeValues(column("Model ID")), codeValues(column("Reasoning"))
			if len(ids) != 1 || len(efforts) == 0 {
				return result, errSourceFormat
			}
			slices.Sort(efforts)
			result.Models = append(result.Models, observedModel{ID: ids[0], Efforts: slices.Compact(efforts)})
		} else {
			if column("Provider") == "" || column("Notes") == "" {
				return result, errSourceFormat
			}
			rows = append(rows, markdownLink.ReplaceAllString(cells[0], "$1")+" | "+column("Provider")+" | "+column("Notes"))
		}
	}
	result.Text = normalizeText(strings.Join(rows, "\n"))
	return result, nil
}

func tableCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}
	cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|"), "|")
	for i := range cells {
		cells[i] = strings.Join(strings.Fields(cells[i]), " ")
	}
	return cells
}

func codeValues(text string) []string {
	var values []string
	for _, match := range inlineCode.FindAllStringSubmatch(text, -1) {
		values = append(values, match[1])
	}
	return values
}

func markdownSection(text, title string) (string, error) {
	var lines []string
	level, count := 0, 0
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		}
		if match := heading.FindStringSubmatch(line); match != nil && !fenced {
			if level > 0 && len(match[1]) <= level {
				level = 0
			}
			if match[2] == title {
				count++
				level = len(match[1])
			}
		}
		if level > 0 {
			lines = append(lines, line)
		}
	}
	if count != 1 || len(lines) < 2 {
		return "", fmt.Errorf("%w: missing or ambiguous section %s", errSourceFormat, title)
	}
	return strings.Join(lines, "\n"), nil
}

func normalizeText(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if normalized := strings.Join(strings.Fields(line), " "); normalized != "" {
			lines = append(lines, normalized)
		}
	}
	return strings.Join(lines, "\n")
}
