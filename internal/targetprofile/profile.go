package targetprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxProfileBytes = 4 << 20
const profileInvalidCode = "marketplace.profile.invalid"
const profileDigestMismatchCode = "marketplace.profile.digest-mismatch"

type (
	Profile struct {
		APIVersion string           `yaml:"apiVersion"`
		ID         string           `yaml:"id"`
		Label      string           `yaml:"label"`
		Platforms  []string         `yaml:"platforms"`
		Roots      Roots            `yaml:"roots"`
		Host       Host             `yaml:"host"`
		Checks     map[string]Check `yaml:"checks"`
		Kinds      map[string]Kind  `yaml:"kinds"`
	}
	Roots struct {
		User UserRoot `yaml:"user"`
	}
	UserRoot struct {
		Override     string                 `yaml:"override"`
		Default      map[string]RootDefault `yaml:"default"`
		AllowedBases []string               `yaml:"allowed_bases"`
	}
	RootDefault struct {
		Base string `yaml:"base"`
		Path string `yaml:"path"`
	}
	Host struct {
		Executable   string              `yaml:"executable"`
		VersionProbe *VersionProbe       `yaml:"version_probe"`
		Verified     []VerifiedRange     `yaml:"verified"`
		Shipped      map[string][]string `yaml:"shipped"`
	}
	VersionProbe struct {
		Argv    []string `yaml:"argv"`
		Pattern string   `yaml:"pattern"`
	}
	VerifiedRange struct {
		Range    string `yaml:"range"`
		Date     string `yaml:"date"`
		Evidence string `yaml:"evidence"`
	}
	Check struct {
		Scope   string       `yaml:"scope"`
		Argv    []string     `yaml:"argv"`
		Subject CheckSubject `yaml:"subject"`
	}
	CheckSubject struct {
		Under     string `yaml:"under"`
		Component string `yaml:"component"`
	}
	Kind struct {
		Deliver      string   `yaml:"deliver"`
		Into         []string `yaml:"into"`
		SupportFiles []string `yaml:"support_files"`
		Verify       string   `yaml:"verify"`
	}
)

var (
	packageNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	envNamePattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func Parse(data []byte, expected Identity) (Profile, error) {
	if int64(len(data)) > maxProfileBytes {
		return Profile{}, fmt.Errorf("%s: profile exceeds %d bytes", profileInvalidCode, maxProfileBytes)
	}
	if err := expected.Validate(); err != nil {
		return Profile{}, err
	}
	digest := sha256.Sum256(data)
	if expected.RevisionDigest != "sha256:"+hex.EncodeToString(digest[:]) {
		return Profile{}, fmt.Errorf("%s: profile bytes do not match the expected revision", profileDigestMismatchCode)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return Profile{}, fmt.Errorf("%s: decode profile: %w", profileInvalidCode, err)
	}
	if len(document.Content) != 1 {
		return Profile{}, fmt.Errorf("%s: profile must contain one mapping document", profileInvalidCode)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Profile{}, fmt.Errorf("%s: profile must contain one YAML document", profileInvalidCode)
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return Profile{}, fmt.Errorf("%s: profile must be a mapping", profileInvalidCode)
	}
	if err := inspectProfileYAML(root); err != nil {
		return Profile{}, err
	}
	if err := inspectProfileStringScalars(root); err != nil {
		return Profile{}, err
	}
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	var profile Profile
	if err := strict.Decode(&profile); err != nil {
		return Profile{}, fmt.Errorf("%s: decode profile: %w", profileInvalidCode, err)
	}
	if err := validateProfile(profile, expected.TargetID); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func inspectProfileStringScalars(root *yaml.Node) error {
	host := mappingValue(root, "host")
	if executable := mappingValue(host, "executable"); executable != nil && !isYAMLStringScalar(executable) {
		return fmt.Errorf("%s: host executable must be a string scalar", profileInvalidCode)
	}
	probe := mappingValue(host, "version_probe")
	if pattern := mappingValue(probe, "pattern"); pattern != nil && !isYAMLStringScalar(pattern) {
		return fmt.Errorf("%s: version probe pattern must be a string scalar", profileInvalidCode)
	}
	if err := validateYAMLStringSequence(mappingValue(probe, "argv")); err != nil {
		return err
	}
	checks := mappingValue(root, "checks")
	if checks != nil && checks.Kind == yaml.MappingNode {
		for index := 1; index < len(checks.Content); index += 2 {
			if err := validateYAMLStringSequence(mappingValue(checks.Content[index], "argv")); err != nil {
				return err
			}
		}
	}
	return nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

func isYAMLStringScalar(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!str"
}

func validateYAMLStringSequence(node *yaml.Node) error {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	for _, value := range node.Content {
		if !isYAMLStringScalar(value) {
			return fmt.Errorf("%s: command argv entries must be string scalars", profileInvalidCode)
		}
	}
	return nil
}

func inspectProfileYAML(node *yaml.Node) error {
	if node.Alias != nil || node.Tag == "!!null" {
		return fmt.Errorf("%s: YAML aliases and nulls are forbidden", profileInvalidCode)
	}
	switch node.Kind {
	case yaml.MappingNode:
		seen := make(map[string]struct{}, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Value == "<<" || key.Tag == "!!merge" {
				return fmt.Errorf("%s: YAML merges are forbidden", profileInvalidCode)
			}
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return fmt.Errorf("%s: mapping keys must be strings", profileInvalidCode)
			}
			if _, ok := seen[key.Value]; ok {
				return fmt.Errorf("%s: duplicate YAML key %q", profileInvalidCode, key.Value)
			}
			seen[key.Value] = struct{}{}
			if err := inspectProfileYAML(node.Content[index+1]); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			if err := inspectProfileYAML(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateProfile(profile Profile, expectedTargetID string) error {
	if profile.ID != expectedTargetID {
		return fmt.Errorf("%s: profile target ID does not match its identity", profileInvalidCode)
	}
	if profile.APIVersion != "agent-marketplace.io/target-profile/v1" {
		return fmt.Errorf("%s: unsupported apiVersion", profileInvalidCode)
	}
	if strings.TrimSpace(profile.Label) == "" || hasControl(profile.Label) {
		return fmt.Errorf("%s: label must be non-empty text", profileInvalidCode)
	}
	if err := validatePlatforms(profile.Platforms); err != nil {
		return err
	}
	if err := validateUserRoot(profile.Roots.User); err != nil {
		return err
	}
	if err := validateHost(profile.Host, len(profile.Checks) != 0); err != nil {
		return err
	}
	if err := validateChecks(profile.Checks); err != nil {
		return err
	}
	return validateKinds(profile.Kinds, profile.Checks)
}

func validatePlatforms(platforms []string) error {
	if len(platforms) == 0 {
		return fmt.Errorf("%s: at least one platform must be declared", profileInvalidCode)
	}
	seen := make(map[string]bool, len(platforms))
	for _, platform := range platforms {
		if platform != "macos" && platform != "linux" || seen[platform] {
			return fmt.Errorf("%s: unsupported or duplicate platform", profileInvalidCode)
		}
		seen[platform] = true
	}
	return nil
}

func validateUserRoot(root UserRoot) error {
	if root.Override != "" && !envNamePattern.MatchString(root.Override) {
		return fmt.Errorf("%s: invalid root override variable", profileInvalidCode)
	}
	if len(root.Default) != 2 || len(root.AllowedBases) == 0 {
		return fmt.Errorf("%s: user root requires macos and linux defaults and allowed bases", profileInvalidCode)
	}
	allowed := make(map[string]bool, len(root.AllowedBases))
	for _, base := range root.AllowedBases {
		if base != "home" && base != "xdg-config" || allowed[base] {
			return fmt.Errorf("%s: unsupported or duplicate allowed base", profileInvalidCode)
		}
		allowed[base] = true
	}
	for _, platform := range []string{"macos", "linux"} {
		defaultRoot, ok := root.Default[platform]
		if !ok || !allowed[defaultRoot.Base] || !validProfileRelativePath(defaultRoot.Path) {
			return fmt.Errorf("%s: invalid or unadmitted %s root default", profileInvalidCode, platform)
		}
	}
	return nil
}

func validateHost(host Host, hasChecks bool) error {
	if host.Executable != "" || host.VersionProbe != nil || hasChecks {
		if strings.TrimSpace(host.Executable) == "" || hasControl(host.Executable) || strings.Contains(host.Executable, "{subject}") {
			return fmt.Errorf("%s: host executable must be a literal command", profileInvalidCode)
		}
	}
	if probe := host.VersionProbe; probe != nil {
		if err := validateLiteralArgv(probe.Argv, false); err != nil || strings.TrimSpace(probe.Pattern) == "" {
			return fmt.Errorf("%s: invalid version probe", profileInvalidCode)
		}
		if _, err := regexp.Compile(probe.Pattern); err != nil {
			return fmt.Errorf("%s: invalid version probe pattern", profileInvalidCode)
		}
	}
	for _, verified := range host.Verified {
		date, err := time.Parse("2006-01-02", verified.Date)
		if strings.TrimSpace(verified.Range) == "" || err != nil || date.Format("2006-01-02") != verified.Date ||
			strings.TrimSpace(verified.Evidence) == "" || hasControl(verified.Evidence) {
			return fmt.Errorf("%s: invalid verified range", profileInvalidCode)
		}
	}
	for kind, names := range host.Shipped {
		if !validTargetID.MatchString(kind) {
			return fmt.Errorf("%s: invalid shipped kind", profileInvalidCode)
		}
		seen := map[string]bool{}
		for _, name := range names {
			if !packageNamePattern.MatchString(name) || seen[name] {
				return fmt.Errorf("%s: invalid or duplicate shipped package name", profileInvalidCode)
			}
			seen[name] = true
		}
	}
	return nil
}

func validateChecks(checks map[string]Check) error {
	for name, check := range checks {
		if !validTargetID.MatchString(name) || check.Scope != "eager-package-load" ||
			!validProfileRelativePath(check.Subject.Under) || check.Subject.Component != "package-name" {
			return fmt.Errorf("%s: invalid package check", profileInvalidCode)
		}
		if err := validateLiteralArgv(check.Argv, true); err != nil {
			return err
		}
	}
	return nil
}

func validateKinds(kinds map[string]Kind, checks map[string]Check) error {
	if len(kinds) == 0 {
		return fmt.Errorf("%s: at least one place kind is required", profileInvalidCode)
	}
	for name, kind := range kinds {
		if !validTargetID.MatchString(name) || kind.Deliver != "place" || len(kind.Into) == 0 {
			return fmt.Errorf("%s: invalid local place kind", profileInvalidCode)
		}
		seen := map[string]bool{}
		for _, prefix := range kind.Into {
			if !strings.HasSuffix(prefix, "/") || !validProfileRelativePath(strings.TrimSuffix(prefix, "/")) || seen[prefix] {
				return fmt.Errorf("%s: invalid or duplicate destination prefix", profileInvalidCode)
			}
			seen[prefix] = true
		}
		seen = map[string]bool{}
		for _, file := range kind.SupportFiles {
			if !validProfileRelativePath(file) || seen[file] {
				return fmt.Errorf("%s: invalid or duplicate support file", profileInvalidCode)
			}
			seen[file] = true
		}
		if kind.Verify != "" {
			if _, ok := checks[kind.Verify]; !ok {
				return fmt.Errorf("%s: kind references an undefined check", profileInvalidCode)
			}
		}
	}
	return nil
}

func validateLiteralArgv(argv []string, requireSubject bool) error {
	if len(argv) == 0 {
		return fmt.Errorf("%s: command argv requires a literal command role", profileInvalidCode)
	}
	subjects := 0
	for index, arg := range argv {
		if strings.TrimSpace(arg) == "" || hasControl(arg) {
			return fmt.Errorf("%s: command argv contains an invalid argument", profileInvalidCode)
		}
		if strings.Contains(arg, "{subject}") {
			if arg != "{subject}" || index == 0 {
				return fmt.Errorf("%s: subject must be one standalone non-command argument", profileInvalidCode)
			}
			subjects++
		}
	}
	if requireSubject && subjects != 1 || !requireSubject && subjects != 0 {
		return fmt.Errorf("%s: command argv has an invalid subject argument count", profileInvalidCode)
	}
	return nil
}

func validProfileRelativePath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || hasControl(value) {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}
