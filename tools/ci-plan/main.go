package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

var categories = []string{
	"go_build",
	"go_test",
	"benchmark",
	"harness_compatibility",
	"marketplace_e2e",
	"tui_qa",
	"container_config",
	"container_marketplace",
	"container_web",
	"container_site",
	"helm",
}

var benchmarkRoots = []string{
	"internal/resolver",
	"internal/materialize",
	"internal/tui",
	"internal/catalog",
	"internal/config",
	"cmd/agent-marketplace",
	"internal/cache",
	"pkg/harness-compilation",
	"internal/dependencytxn",
	"internal/lockfile",
	"internal/sourcerevision",
	"internal/pipeline",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ci-plan:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 2 {
		return fmt.Errorf("unexpected positional argument %q", os.Args[1])
	}
	all := false
	if len(os.Args) == 2 {
		if os.Args[1] != "--all" {
			return fmt.Errorf("unknown argument %q", os.Args[1])
		}
		all = true
	}

	var paths []string
	if !all {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		paths, err = parseInput(data)
		if err != nil {
			return err
		}
	}

	result, err := planCategories(paths, all)
	if err != nil {
		return err
	}
	for _, name := range categories {
		if result[name] {
			fmt.Printf("%s=true\n", name)
		} else {
			fmt.Printf("%s=false\n", name)
		}
	}
	return nil
}

func parseInput(data []byte) ([]string, error) {
	if len(data) == 0 {
		return []string{}, nil
	}
	if data[len(data)-1] != 0 {
		return nil, fmt.Errorf("input does not end with a NUL byte")
	}
	parts := bytes.Split(data[:len(data)-1], []byte{0})
	var paths []string
	for _, p := range parts {
		if len(p) == 0 {
			return nil, fmt.Errorf("empty path record in input stream")
		}
		s := string(p)
		if err := validatePath(s); err != nil {
			return nil, err
		}
		paths = append(paths, s)
	}
	return paths, nil
}

func validatePath(p string) error {
	if p == "" {
		return fmt.Errorf("empty path record")
	}
	if path.IsAbs(p) {
		return fmt.Errorf("absolute path %q is not allowed", p)
	}
	if strings.ContainsRune(p, '\\') {
		return fmt.Errorf("backslash in path %q is not allowed", p)
	}
	if path.Clean(p) != p {
		return fmt.Errorf("path %q is not clean", p)
	}
	for _, elem := range strings.Split(p, "/") {
		if elem == "." || elem == ".." {
			return fmt.Errorf("path %q contains %q", p, elem)
		}
	}
	return nil
}

func planCategories(paths []string, all bool) (map[string]bool, error) {
	result := make(map[string]bool, len(categories))
	for _, c := range categories {
		result[c] = false
	}
	if all {
		enableAll(result)
		return result, nil
	}
	for _, p := range paths {
		c, err := classifyPath(p)
		if err != nil {
			return nil, err
		}
		for name, value := range c {
			if value {
				result[name] = true
			}
		}
	}
	return result, nil
}

func classifyPath(p string) (map[string]bool, error) {
	c := make(map[string]bool, len(categories))
	for _, name := range categories {
		c[name] = false
	}

	switch p {
	case ".github/workflows/ci.yml", ".github/workflows/harness-compatibility.yml":
		enableAll(c)
		return c, nil
	case "Makefile":
		enableAll(c)
		return c, nil
	}

	if strings.HasPrefix(p, "tools/ci-plan/") {
		enableAll(c)
		return c, nil
	}

	isE2E := strings.HasPrefix(p, "e2e/")
	isRootModuleInput := p == "go.mod" || p == "go.sum" || p == "go.work" || p == "go.work.sum"
	isE2EModuleInput := p == "e2e/go.mod" || p == "e2e/go.sum"

	if isRootModuleInput {
		c["go_build"] = true
		c["go_test"] = true
		c["benchmark"] = true
		c["harness_compatibility"] = true
		c["marketplace_e2e"] = true
		c["tui_qa"] = true
		c["container_marketplace"] = true
	}
	if isE2E {
		c["marketplace_e2e"] = true
	}
	if isE2EModuleInput || (strings.HasPrefix(p, "e2e/harness_compatibility_") && strings.HasSuffix(p, "_test.go")) {
		c["harness_compatibility"] = true
	}

	isGo := strings.HasSuffix(p, ".go")
	if isGo && !isE2E {
		c["go_test"] = true
	}

	if isGo && (strings.HasPrefix(p, "cmd/") || strings.HasPrefix(p, "internal/") || strings.HasPrefix(p, "pkg/")) {
		c["go_build"] = true
		c["harness_compatibility"] = true
		c["marketplace_e2e"] = true
		c["tui_qa"] = true
		c["container_marketplace"] = true
	}

	if isGo && !isE2E && underAnyPrefix(p, benchmarkRoots) {
		c["benchmark"] = true
	}

	if strings.HasPrefix(p, "tools/harness-compat/") {
		c["harness_compatibility"] = true
	}

	if strings.HasPrefix(p, "tools/tui-qa/") {
		c["tui_qa"] = true
	}

	switch {
	case p == "Dockerfile.marketplace" || p == ".dockerignore":
		c["container_config"] = true
		c["container_marketplace"] = true
	case p == "compose.yaml" ||
		strings.HasPrefix(p, "deploy/compose/") ||
		p == "web/Dockerfile" || p == "site/Dockerfile":
		c["container_config"] = true
	}

	if strings.HasPrefix(p, "web/") {
		c["container_web"] = true
	}
	if strings.HasPrefix(p, "site/") {
		c["container_site"] = true
	}
	if strings.HasPrefix(p, "design/") {
		c["container_config"] = true
		c["container_web"] = true
		c["container_site"] = true
	}

	if strings.HasPrefix(p, "deploy/helm/") {
		c["container_config"] = true
		c["helm"] = true
	}

	if !anyEnabled(c) && !proseOnly(p) {
		enableAll(c)
	}

	return c, nil
}

func underAnyPrefix(p string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

func enableAll(result map[string]bool) {
	for _, name := range categories {
		result[name] = true
	}
}

func anyEnabled(result map[string]bool) bool {
	for _, enabled := range result {
		if enabled {
			return true
		}
	}
	return false
}

func proseOnly(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasSuffix(lower, ".md") || p == "LICENSE" || p == "NOTICE"
}
