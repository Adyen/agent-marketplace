// Package main is a NON-PRODUCTION, throwaway, scratch-only trial; it is not a target-profile v1 implementation.
package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

//go:embed profiles/*.yaml imports/*.yaml fixtures/**
var trialFiles embed.FS

type profile struct {
	ID      string                 `yaml:"id"`
	Roots   map[string]rootProfile `yaml:"roots"`
	Package packageProfile         `yaml:"package"`
	Kinds   map[string]kindProfile `yaml:"kinds"`
}

type rootProfile struct {
	Override string                 `yaml:"override"`
	Defaults map[string]rootDefault `yaml:"defaults"`
}

type rootDefault struct {
	Base     string `yaml:"base"`
	Fallback string `yaml:"fallback"`
	Path     string `yaml:"path"`
}

type packageProfile struct {
	Metadata string   `yaml:"metadata"`
	Display  []string `yaml:"display"`
}

type kindProfile struct {
	Into      []string      `yaml:"into"`
	Directory string        `yaml:"directory"`
	Identity  *identityRule `yaml:"identity"`
}

type identityRule struct {
	File   string `yaml:"file"`
	Format string `yaml:"format"`
	Field  string `yaml:"field"`
}

type importDeclaration struct {
	SourceRoot string          `yaml:"sourceRoot"`
	Package    importedPackage `yaml:"package"`
	Files      []fileMapping   `yaml:"files"`
}

type importedPackage struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Kind string `yaml:"kind"`
}

type fileMapping struct {
	Source      string `yaml:"source"`
	Destination string `yaml:"destination"`
}

type plannedFile struct {
	PackageID string
	Dest      string
	Data      []byte
}

type packagePlan struct {
	Files []plannedFile
}

type refusalError string

func (e refusalError) Error() string {
	return string(e)
}

func main() {
	profiles, imports, err := loadInputs()
	if err != nil {
		fmt.Printf("FAIL load-inputs: %v\n", err)
		os.Exit(1)
	}

	checks := []struct {
		name string
		run  func(string) error
	}{
		{"roots-macos-default", func(scratch string) error {
			return checkRootMatrix(profiles, "macos", nil, filepath.Join(scratch, "home"), map[string]string{
				"herdr-app": filepath.Join(scratch, "home", ".config", "herdr-app"),
				"tern":      filepath.Join(scratch, "home", "Library", "Application Support", "Tern"),
			})
		}},
		{"roots-linux-default", func(scratch string) error {
			return checkRootMatrix(profiles, "linux", nil, filepath.Join(scratch, "home"), map[string]string{
				"herdr-app": filepath.Join(scratch, "home", ".config", "herdr-app"),
				"tern":      filepath.Join(scratch, "home", ".config", "tern"),
			})
		}},
		{"roots-linux-xdg", func(scratch string) error {
			env := map[string]string{"XDG_CONFIG_HOME": filepath.Join(scratch, "xdg config")}
			return checkRootMatrix(profiles, "linux", env, filepath.Join(scratch, "home"), map[string]string{
				"herdr-app": filepath.Join(scratch, "xdg config", "herdr-app"),
				"tern":      filepath.Join(scratch, "xdg config", "tern"),
			})
		}},
		{"herdr-override-with-spaces", func(scratch string) error {
			root := filepath.Join(scratch, "Herdr Root With Spaces")
			return checkPlacement(scratch, profiles["herdr-app"], imports["herdr-app"], "linux",
				map[string]string{"HERDR_APP_HOME": root}, filepath.Join(scratch, "home"), root,
				[]string{"mods", "mods/night-light", "mods/night-light/main.js", "mods/night-light/mod.json"})
		}},
		{"tern-override-with-spaces", func(scratch string) error {
			root := filepath.Join(scratch, "Tern Config With Spaces")
			return checkPlacement(scratch, profiles["tern"], imports["tern"], "macos",
				map[string]string{"TERN_CONFIG_DIR": root}, filepath.Join(scratch, "home"), root,
				[]string{"plugins", "plugins/sample-tools", "plugins/sample-tools/host.luau", "plugins/sample-tools/plugin.toml", "plugins/sample-tools/window.luau"})
		}},
		{"refuse-dot-prefixed-destination", func(scratch string) error {
			declaration := imports["tern"]
			declaration.Files = append([]fileMapping(nil), declaration.Files...)
			declaration.Files[0].Destination = "plugins/.sample-tools/plugin.toml"
			return checkRefusal(scratch, profiles["tern"], declaration, "linux", filepath.Join(scratch, "home"),
				"plugins/.sample-tools/plugin.toml")
		}},
		{"refuse-source-traversal", func(scratch string) error {
			declaration := imports["herdr-app"]
			declaration.Files = append([]fileMapping(nil), declaration.Files...)
			declaration.Files[0].Source = "../outside/mod.json"
			return checkRefusal(scratch, profiles["herdr-app"], declaration, "linux", filepath.Join(scratch, "home"),
				"../outside/mod.json")
		}},
		{"refuse-source-symlink-escape", func(scratch string) error {
			return checkSymlinkRefusal(scratch, profiles["herdr-app"], imports["herdr-app"])
		}},
		{"refuse-outside-into", func(scratch string) error {
			declaration := imports["herdr-app"]
			declaration.Files = append([]fileMapping(nil), declaration.Files...)
			declaration.Files[0].Destination = "themes/night.json"
			return checkRefusal(scratch, profiles["herdr-app"], declaration, "linux", filepath.Join(scratch, "home"),
				"themes/night.json")
		}},
		{"refuse-outside-package-directory", func(scratch string) error {
			declaration := imports["tern"]
			declaration.Files = append([]fileMapping(nil), declaration.Files...)
			declaration.Files[0].Destination = "plugins/name-other-than-manifest-id/plugin.toml"
			return checkRefusal(scratch, profiles["tern"], declaration, "linux", filepath.Join(scratch, "home"),
				"plugins/name-other-than-manifest-id/plugin.toml")
		}},
		{"refuse-collision-order-forward", func(scratch string) error {
			return checkCollisionOrder(scratch, profiles["tern"], imports["tern"],
				[]string{"new-package-a", "new-package-b"})
		}},
		{"refuse-collision-order-reverse", func(scratch string) error {
			return checkCollisionOrder(scratch, profiles["tern"], imports["tern"],
				[]string{"new-package-b", "new-package-a"})
		}},
	}

	failures := 0
	for _, check := range checks {
		scratch, err := os.MkdirTemp("", "target-profile-trial-")
		if err == nil {
			err = check.run(scratch)
			if cleanupErr := os.RemoveAll(scratch); cleanupErr != nil {
				err = fmt.Errorf("remove scratch directory %s: %v (case result: %v)", scratch, cleanupErr, err)
			}
		}
		if err != nil {
			var refusal refusalError
			if errors.As(err, &refusal) {
				fmt.Printf("PASS %s: %s\n", check.name, refusal)
			} else {
				failures++
				fmt.Printf("FAIL %s: %v\n", check.name, err)
			}
		} else {
			fmt.Printf("PASS %s\n", check.name)
		}
	}
	if failures > 0 {
		os.Exit(1)
	}
}

func loadInputs() (map[string]profile, map[string]importDeclaration, error) {
	profiles := make(map[string]profile)
	imports := make(map[string]importDeclaration)
	for _, name := range []string{"herdr-app", "tern"} {
		profileFile := "profiles/" + name + ".yaml"
		data, err := trialFiles.ReadFile(profileFile)
		if err != nil {
			return nil, nil, fmt.Errorf("required fixture %s: %w", profileFile, err)
		}
		var p profile
		if err := yaml.Unmarshal(data, &p); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", profileFile, err)
		}
		profiles[name] = p

		importFile := "imports/" + name + ".yaml"
		data, err = trialFiles.ReadFile(importFile)
		if err != nil {
			return nil, nil, fmt.Errorf("required fixture %s: %w", importFile, err)
		}
		var declaration importDeclaration
		if err := yaml.Unmarshal(data, &declaration); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", importFile, err)
		}
		imports[name] = declaration
	}
	return profiles, imports, nil
}

func checkRootMatrix(profiles map[string]profile, osName string, env map[string]string, home string, expected map[string]string) error {
	for _, name := range []string{"herdr-app", "tern"} {
		root, err := resolveRoot(profiles[name], osName, env, home)
		if err != nil {
			return err
		}
		if root != expected[name] {
			return fmt.Errorf("%s root: got %q, want %q", name, root, expected[name])
		}
	}
	return nil
}

func checkPlacement(scratch string, p profile, declaration importDeclaration, osName string, env map[string]string, home, root string, expected []string) error {
	resolved, err := resolveRoot(p, osName, env, home)
	if err != nil {
		return err
	}
	if resolved != root {
		return fmt.Errorf("%s root: got %q, want %q", p.ID, resolved, root)
	}
	source, err := copyFixture(scratch, declaration.SourceRoot)
	if err != nil {
		return err
	}
	plan, err := planPackage(p, declaration, source, root)
	if err != nil {
		return err
	}
	if err := applyPlans(scratch, root, []packagePlan{plan}); err != nil {
		return err
	}
	return compareTree(root, expected)
}

func checkRefusal(scratch string, p profile, declaration importDeclaration, osName string, home, offending string) error {
	source, err := copyFixture(scratch, declaration.SourceRoot)
	if err != nil {
		return err
	}
	resolved, err := resolveRoot(p, osName, nil, home)
	if err != nil {
		return err
	}
	_, err = planPackage(p, declaration, source, resolved)
	return expectRefusal(err, offending, resolved)
}

func checkSymlinkRefusal(scratch string, p profile, declaration importDeclaration) error {
	source, err := copyFixture(scratch, declaration.SourceRoot)
	if err != nil {
		return err
	}
	escape := filepath.Join(scratch, "outside-main.js")
	if err := os.WriteFile(escape, []byte("not executed"), 0o600); err != nil {
		return err
	}
	link := filepath.Join(source, filepath.FromSlash(declaration.Files[0].Source))
	if err := os.Remove(link); err != nil {
		return err
	}
	if err := os.Symlink(escape, link); err != nil {
		return err
	}
	root := filepath.Join(scratch, "target")
	_, err = planPackage(p, declaration, source, root)
	offending := declaration.Files[0].Source
	return expectRefusal(err, offending, root)
}

func checkCollisionOrder(scratch string, p profile, declaration importDeclaration, order []string) error {
	root := filepath.Join(scratch, "target")
	plans := make([]packagePlan, 0, len(order))
	for _, packageID := range order {
		candidate := declaration
		candidate.Package.ID = packageID
		source, err := copyFixture(scratch, candidate.SourceRoot)
		if err != nil {
			return err
		}
		plan, err := planPackage(p, candidate, source, root)
		if err != nil {
			return err
		}
		plans = append(plans, plan)
	}
	refusals := applyPlans(scratch, root, plans)
	want := []string{"new-package-a", "new-package-b", "plugins/sample-tools/plugin.toml"}
	for _, text := range want {
		if refusals == nil || !strings.Contains(refusals.Error(), text) {
			return fmt.Errorf("collision refusal missing %q: %w", text, refusals)
		}
	}
	return expectRefusal(refusals, "plugins/sample-tools/plugin.toml", root)
}

func expectRefusal(err error, offending, targetRoot string) error {
	if err == nil {
		return fmt.Errorf("%s was accepted", offending)
	}
	if !strings.Contains(err.Error(), offending) {
		return fmt.Errorf("refusal did not name %q: %w", offending, err)
	}
	if _, statErr := os.Lstat(targetRoot); !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("refusal wrote target root %q: %v", targetRoot, statErr)
	}
	return refusalError(fmt.Sprintf("%s refused: %v", offending, err))
}

func planPackage(p profile, declaration importDeclaration, sourceRoot, targetRoot string) (packagePlan, error) {
	kind, ok := p.Kinds[declaration.Package.Kind]
	if !ok {
		return packagePlan{}, fmt.Errorf("package %s has unknown kind %q", declaration.Package.ID, declaration.Package.Kind)
	}
	values := map[string]string{"package": declaration.Package.Name}
	if kind.Identity != nil {
		identity, err := readIdentity(sourceRoot, *kind.Identity)
		if err != nil {
			return packagePlan{}, err
		}
		values["identity."+kind.Identity.Field] = identity
	}
	directory, err := expand(kind.Directory, values)
	if err != nil {
		return packagePlan{}, err
	}
	if err := validRelative(directory); err != nil {
		return packagePlan{}, fmt.Errorf("package %s directory %q: %w", declaration.Package.ID, directory, err)
	}

	excluded := make(map[string]bool)
	excluded[p.Package.Metadata] = true
	for _, display := range p.Package.Display {
		excluded[display] = true
	}
	mappings := make(map[string]string, len(declaration.Files))
	for _, mapping := range declaration.Files {
		if err := validRelative(mapping.Source); err != nil {
			return packagePlan{}, fmt.Errorf("source %q: %w", mapping.Source, err)
		}
		if err := validRelative(mapping.Destination); err != nil {
			return packagePlan{}, fmt.Errorf("destination for %s: %w", mapping.Source, err)
		}
		mappings[mapping.Source] = mapping.Destination
	}

	found := make(map[string]bool, len(mappings))
	plan := packagePlan{}
	err = filepath.WalkDir(sourceRoot, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, full)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if excluded[relative] {
			return nil
		}
		destination, ok := mappings[relative]
		if !ok {
			return fmt.Errorf("unmapped source file %s", relative)
		}
		found[relative] = true
		data, err := readContained(sourceRoot, full)
		if err != nil {
			return err
		}
		if !withinInto(destination, kind.Into) {
			return fmt.Errorf("destination %s for %s is outside allowed into %v", destination, relative, kind.Into)
		}
		if hasDotPrefixedComponent(destination) {
			return fmt.Errorf("destination %s for %s has a dot-prefixed component", destination, relative)
		}
		if !withinDirectory(destination, directory) {
			return fmt.Errorf("destination %s for %s is outside package directory %s", destination, relative, directory)
		}
		absolute := filepath.Join(targetRoot, filepath.FromSlash(destination))
		if err := requireInside(targetRoot, absolute); err != nil {
			return fmt.Errorf("%s -> %s: %w", relative, destination, err)
		}
		plan.Files = append(plan.Files, plannedFile{
			PackageID: declaration.Package.ID,
			Dest:      destination,
			Data:      data,
		})
		return nil
	})
	if err != nil {
		return packagePlan{}, err
	}
	for source := range mappings {
		if !found[source] {
			return packagePlan{}, fmt.Errorf("missing mapped source file %s", source)
		}
	}
	sort.Slice(plan.Files, func(i, j int) bool { return plan.Files[i].Dest < plan.Files[j].Dest })
	return plan, nil
}

func applyPlans(scratch, targetRoot string, plans []packagePlan) error {
	if err := requireInside(scratch, targetRoot); err != nil {
		return err
	}
	owners := make(map[string]plannedFile)
	var conflicts []string
	for _, plan := range plans {
		for _, file := range plan.Files {
			if prior, exists := owners[file.Dest]; exists {
				claimants := []string{prior.PackageID, file.PackageID}
				sort.Strings(claimants)
				conflicts = append(conflicts, fmt.Sprintf("%s and %s both claim %s", claimants[0], claimants[1], file.Dest))
			} else {
				owners[file.Dest] = file
			}
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return errors.New(strings.Join(conflicts, "; "))
	}
	for _, plan := range plans {
		for _, file := range plan.Files {
			destination := filepath.Join(targetRoot, filepath.FromSlash(file.Dest))
			if err := requireInside(targetRoot, destination); err != nil {
				return fmt.Errorf("%s: %w", file.Dest, err)
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return fmt.Errorf("create parent for %s: %w", file.Dest, err)
			}
			if err := os.WriteFile(destination, file.Data, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", file.Dest, err)
			}
		}
	}
	return nil
}

func resolveRoot(p profile, osName string, env map[string]string, home string) (string, error) {
	root, ok := p.Roots["user"]
	if !ok {
		return "", fmt.Errorf("profile %s has no user root", p.ID)
	}
	if override := env[root.Override]; override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("%s override must be absolute: %q", root.Override, override)
		}
		return filepath.Clean(override), nil
	}
	defaultRoot, ok := root.Defaults[osName]
	if !ok {
		return "", fmt.Errorf("profile %s has no %s root default", p.ID, osName)
	}
	var base string
	switch defaultRoot.Base {
	case "home":
		base = home
	case "xdg-config":
		base = env["XDG_CONFIG_HOME"]
		if base == "" {
			base = filepath.Join(home, filepath.FromSlash(defaultRoot.Fallback))
		}
	case "appdata":
		base = env["APPDATA"]
	default:
		return "", fmt.Errorf("profile %s has unsupported root base %q", p.ID, defaultRoot.Base)
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("%s root base must be absolute: %q", p.ID, base)
	}
	return filepath.Clean(filepath.Join(base, filepath.FromSlash(defaultRoot.Path))), nil
}

func readIdentity(sourceRoot string, rule identityRule) (string, error) {
	file := filepath.Join(sourceRoot, filepath.FromSlash(rule.File))
	data, err := readContained(sourceRoot, file)
	if err != nil {
		return "", err
	}
	var values map[string]any
	switch rule.Format {
	case "toml":
		err = toml.Unmarshal(data, &values)
	case "yaml":
		err = yaml.Unmarshal(data, &values)
	default:
		return "", fmt.Errorf("identity file %s uses unsupported format %q", rule.File, rule.Format)
	}
	if err != nil {
		return "", fmt.Errorf("identity file %s: %w", rule.File, err)
	}
	value, ok := values[rule.Field]
	if !ok {
		return "", fmt.Errorf("identity file %s has no field %s", rule.File, rule.Field)
	}
	identity, ok := value.(string)
	if !ok || identity == "" {
		return "", fmt.Errorf("identity file %s field %s is not a string", rule.File, rule.Field)
	}
	return identity, nil
}

func readContained(root, file string) ([]byte, error) {
	relative, err := filepath.Rel(root, file)
	if err != nil {
		return nil, fmt.Errorf("source path %s: %w", file, err)
	}
	relative = filepath.ToSlash(relative)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve source root %s: %w", root, err)
	}
	resolvedFile, err := filepath.EvalSymlinks(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", relative, err)
	}
	if err := requireInside(resolvedRoot, resolvedFile); err != nil {
		return nil, fmt.Errorf("%s: source escapes package root: %w", relative, err)
	}
	info, err := os.Stat(resolvedFile)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", relative, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: source is not a regular file", relative)
	}
	data, err := os.ReadFile(resolvedFile)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", relative, err)
	}
	return data, nil
}

func copyFixture(scratch, fixture string) (string, error) {
	destination := filepath.Join(scratch, "source", filepath.Base(fixture))
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return "", err
	}
	err := fs.WalkDir(trialFiles, fixture, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("%s: %w", name, walkErr)
		}
		relative, err := filepath.Rel(fixture, name)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		output := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(output, 0o755)
		}
		data, err := trialFiles.ReadFile(name)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(output, data, 0o600); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("fixture %s: %w", fixture, err)
	}
	return destination, nil
}

func compareTree(root string, expected []string) error {
	got := make([]string, 0)
	err := filepath.WalkDir(root, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if full == root {
			return nil
		}
		relative, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if hasDotPrefixedComponent(relative) {
			return fmt.Errorf("dot-prefixed tree entry %s", relative)
		}
		got = append(got, relative)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(got)
	sort.Strings(expected)
	if strings.Join(got, "\n") != strings.Join(expected, "\n") {
		return fmt.Errorf("tree mismatch: got %v, want %v", got, expected)
	}
	return nil
}

func validRelative(value string) error {
	if value == "" || path.IsAbs(value) || path.Clean(value) != value || value == "." {
		return fmt.Errorf("invalid relative path %q", value)
	}
	for _, component := range strings.Split(value, "/") {
		if component == ".." || component == "." || component == "" {
			return fmt.Errorf("invalid relative path %q", value)
		}
	}
	return nil
}

func withinInto(destination string, allowlist []string) bool {
	for _, allowed := range allowlist {
		allowed = strings.TrimSuffix(path.Clean(allowed), "/")
		if destination == allowed || strings.HasPrefix(destination, allowed+"/") {
			return true
		}
	}
	return false
}

func withinDirectory(destination, directory string) bool {
	return strings.HasPrefix(destination, directory+"/")
}

func hasDotPrefixedComponent(value string) bool {
	for _, component := range strings.Split(value, "/") {
		if strings.HasPrefix(component, ".") {
			return true
		}
	}
	return false
}

func expand(template string, values map[string]string) (string, error) {
	for key, value := range values {
		template = strings.ReplaceAll(template, "{"+key+"}", value)
	}
	if strings.ContainsAny(template, "{}") {
		return "", fmt.Errorf("unresolved path template %q", template)
	}
	return template, nil
}

func requireInside(root, candidate string) error {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("%q is outside %q", candidate, root)
	}
	return nil
}
