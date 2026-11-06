package main

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"
)

func documentText(body []byte) (string, error) {
	text := string(body)
	if strings.Contains(strings.ToLower(text), "<html") || strings.Contains(strings.ToLower(text), "<!doctype") {
		doc, err := html.Parse(strings.NewReader(text))
		if err != nil {
			return "", err
		}
		var articles, mains []*html.Node
		var collect func(*html.Node)
		collect = func(n *html.Node) {
			if n.Type == html.ElementNode {
				switch n.Data {
				case "article":
					articles = append(articles, n)
				case "main":
					mains = append(mains, n)
				}
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				collect(child)
			}
		}
		collect(doc)
		selected := articles
		if len(selected) == 0 {
			selected = mains
		}
		ancestors := map[*html.Node]bool{}
		for _, candidate := range selected {
			for parent := candidate.Parent; parent != nil; parent = parent.Parent {
				ancestors[parent] = true
			}
		}
		innermost := selected[:0]
		for _, candidate := range selected {
			if !ancestors[candidate] {
				innermost = append(innermost, candidate)
			}
		}
		selected = innermost
		if len(selected) == 0 {
			return "", fmt.Errorf("documentation article or main is missing")
		}
		var output strings.Builder
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				switch n.Data {
				case "script", "style", "nav", "footer", "header", "svg":
					return
				}
				for _, a := range n.Attr {
					if a.Key == "hidden" || a.Key == "aria-hidden" && a.Val == "true" {
						return
					}
				}
			}
			if n.Type == html.TextNode {
				output.WriteString(n.Data)
				output.WriteByte(' ')
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		var chosen string
		for _, candidate := range selected {
			output.Reset()
			walk(candidate)
			current := strings.Join(strings.Fields(output.String()), " ")
			if chosen != "" && chosen != current {
				return "", fmt.Errorf("ambiguous documentation content")
			}
			chosen = current
		}
		text = chosen
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "", fmt.Errorf("empty documentation text")
	}
	return text, nil
}

func auditDocumentSource(ctx context.Context, files fs.FS, s source, r *sourceReport, fetch fetchFunc) {
	if s.Domain == "pluginpolicy" {
		auditPluginDocumentSource(ctx, files, s, r, fetch)
		return
	}
	if s.Domain != "hookpolicy" && s.Domain != "toolpolicy" {
		r.add("unknown", "catalog.invalid", "", "Unsupported catalog domain; no evidence conclusion.")
		return
	}
	body, err := readBounded(files, "pkg/harness-compilation/"+s.Domain+"/catalogs/"+s.Harness+".yaml")
	var c struct {
		Schema   int    `yaml:"schema"`
		Harness  string `yaml:"harness"`
		Evidence struct {
			Refs []string `yaml:"refs"`
		} `yaml:"evidence"`
		Events   map[string]any `yaml:"events"`
		Tools    map[string]any `yaml:"tools"`
		Surfaces []string       `yaml:"surfaces"`
	}
	if err != nil || yaml.Unmarshal(body, &c) != nil || c.Schema != 1 || c.Harness != s.Harness || !slices.Contains(c.Evidence.Refs, s.URL) ||
		s.Domain == "hookpolicy" && len(c.Events) == 0 || s.Domain == "toolpolicy" && (c.Tools == nil || c.Surfaces == nil) {
		r.add("unknown", "catalog.invalid", "", "Cannot read the declared hook or tool catalog with matching first-party evidence.")
		return
	}
	r.CatalogSHA256 = digest(body)
	auditSource(ctx, s, catalog{}, r, fetch)
}

// auditPluginDocumentSource verifies a pluginpolicy evidence source against
// the shipped catalog it belongs to: a harness catalog when the source names
// a harness, a format catalog when it names Format.
func auditPluginDocumentSource(ctx context.Context, files fs.FS, s source, r *sourceReport, fetch fetchFunc) {
	var c struct {
		Schema   int    `yaml:"schema"`
		Harness  string `yaml:"harness"`
		Format   string `yaml:"format"`
		Evidence struct {
			Refs []string `yaml:"refs"`
		}
	}
	var body []byte
	var err error
	ok := false
	if s.Format != "" {
		body, err = readBounded(files, "pkg/harness-compilation/pluginpolicy/formats/"+s.Format+".yaml")
		ok = err == nil && yaml.Unmarshal(body, &c) == nil && c.Schema == 1 && c.Format == s.Format
	} else {
		body, err = readBounded(files, "pkg/harness-compilation/pluginpolicy/catalogs/"+s.Harness+".yaml")
		ok = err == nil && yaml.Unmarshal(body, &c) == nil && c.Schema == 1 && c.Harness == s.Harness
	}
	if !ok || !slices.Contains(c.Evidence.Refs, s.URL) {
		r.add("unknown", "catalog.invalid", "", "Cannot read the declared plugin catalog with matching first-party evidence.")
		return
	}
	r.CatalogSHA256 = digest(body)
	auditSource(ctx, s, catalog{}, r, fetch)
}
