package claudedelivery

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	maxUploadFiles      = 5000
	maxUploadBytes      = 200 << 20
	maxUploadPathDepth  = 12
	maxUploadPathLength = 472
	maxUploadNameLength = 255
	pluginManifestPath  = ".claude-plugin/plugin.json"
)

type Diagnostic struct {
	Code string
}

var xmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func ValidateArchive(data []byte) []Diagnostic {
	codes := make(map[string]struct{})
	add := func(code string) { codes[code] = struct{}{} }
	if len(data) > maxUploadBytes {
		add("archive.compressed-too-large")
		return diagnostics(codes)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		add("archive.invalid")
		return diagnostics(codes)
	}
	var manifest *zip.File
	manifestCount := 0
	fileCount := 0
	var uncompressed uint64
	skillNames := make(map[string]struct{})
	commandNames := make(map[string]struct{})
	for _, file := range reader.File {
		name := strings.TrimSuffix(file.Name, "/")
		if err := validateArchivePath(name); err != nil {
			add(err.code)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		fileCount++
		if fileCount > maxUploadFiles {
			add("archive.too-many-files")
		}
		if file.UncompressedSize64 > maxUploadBytes ||
			uncompressed > maxUploadBytes-file.UncompressedSize64 {
			add("archive.uncompressed-too-large")
		} else {
			uncompressed += file.UncompressedSize64
		}
		canRead := file.UncompressedSize64 <= maxUploadBytes &&
			file.Flags&1 == 0 && (file.Method == zip.Deflate || file.Method == zip.Store)
		if topLevelBin(name) {
			add("archive.top-level-bin")
		}
		if strings.EqualFold(path.Ext(name), ".zip") {
			add("archive.nested-zip")
		}
		if file.Flags&1 != 0 {
			add("archive.encrypted")
		}
		if file.Mode()&os.ModeSymlink != 0 {
			add("archive.symlink")
		}
		if file.Method != zip.Deflate && file.Method != zip.Store {
			add("archive.compression-unsupported")
		}
		if name == pluginManifestPath {
			manifestCount++
			if manifest == nil {
				manifest = file
			}
		}
		if canRead && (strings.HasSuffix(name, "/SKILL.md") || name == "SKILL.md") {
			content, readErr := readZipFile(file)
			if readErr != nil {
				add("archive.invalid")
				continue
			}
			skillName, frontmatterCode, xmlMetadata := skillFrontmatter(content)
			if frontmatterCode != "" {
				add(frontmatterCode)
			} else {
				if _, exists := skillNames[skillName]; exists {
					add("skill.name-duplicate")
				}
				skillNames[skillName] = struct{}{}
				if xmlMetadata {
					add("skill.frontmatter-xml")
				}
			}
		}
		if strings.HasPrefix(name, "commands/") && strings.HasSuffix(name, ".md") {
			commandName := strings.TrimSuffix(path.Base(name), ".md")
			if _, exists := commandNames[commandName]; exists {
				add("command.name-duplicate")
			}
			commandNames[commandName] = struct{}{}
		}
	}
	if manifestCount == 0 {
		add("manifest.missing")
	} else if manifestCount > 1 {
		add("manifest.duplicate")
	}
	if manifest != nil && manifest.UncompressedSize64 <= maxUploadBytes &&
		manifest.Flags&1 == 0 && (manifest.Method == zip.Deflate || manifest.Method == zip.Store) {
		content, readErr := readZipFile(manifest)
		if readErr != nil {
			add("manifest.invalid")
		} else {
			validateManifest(content, add)
		}
	}
	return diagnostics(codes)
}

type invalidPath struct{ code string }

func validateArchivePath(value string) *invalidPath {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return &invalidPath{code: "archive.path-invalid"}
	}
	if utf8.RuneCountInString(value) > maxUploadPathLength {
		return &invalidPath{code: "archive.path-too-long"}
	}
	parts := strings.Split(value, "/")
	if len(parts) > maxUploadPathDepth {
		return &invalidPath{code: "archive.path-depth"}
	}
	for _, component := range parts {
		if component == "" || component == "." || component == ".." {
			return &invalidPath{code: "archive.path-invalid"}
		}
		if utf8.RuneCountInString(component) > maxUploadNameLength {
			return &invalidPath{code: "archive.component-too-long"}
		}
		for _, char := range component {
			if !unicode.IsLetter(char) && !unicode.IsDigit(char) &&
				!strings.ContainsRune(" _.-(),", char) {
				return &invalidPath{code: "archive.path-invalid"}
			}
		}
	}
	return nil
}

func topLevelBin(name string) bool {
	return name == "bin" || strings.HasPrefix(name, "bin/")
}

func validateManifest(data []byte, add func(string)) {
	var manifest struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		add("manifest.invalid")
		return
	}
	if manifest.Name == "" || utf8.RuneCountInString(manifest.Name) > 64 {
		add("manifest.name-invalid")
	} else {
		for _, char := range manifest.Name {
			if !unicode.IsLower(char) && !unicode.IsDigit(char) && char != '-' {
				add("manifest.name-invalid")
				break
			}
		}
	}
	if utf8.RuneCountInString(manifest.DisplayName) > 64 {
		add("manifest.display-name-too-long")
	}
	if utf8.RuneCountInString(manifest.Description) > 500 {
		add("manifest.description-too-long")
	}
}

func skillFrontmatter(data []byte) (string, string, bool) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", "skill.frontmatter-invalid", false
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "skill.frontmatter-invalid", false
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(rest[:end]), &fields); err != nil {
		return "", "skill.frontmatter-invalid", false
	}
	name, nameOK := fields["name"].(string)
	description, descriptionOK := fields["description"].(string)
	if !nameOK || strings.TrimSpace(name) == "" || !descriptionOK || strings.TrimSpace(description) == "" {
		return "", "skill.frontmatter-invalid", false
	}
	return name, "", xmlTagPattern.MatchString(name) || xmlTagPattern.MatchString(description)
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, maxUploadBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(data) > maxUploadBytes {
		return nil, io.ErrUnexpectedEOF
	}
	return data, nil
}

func diagnostics(codes map[string]struct{}) []Diagnostic {
	names := make([]string, 0, len(codes))
	for code := range codes {
		names = append(names, code)
	}
	sort.Strings(names)
	result := make([]Diagnostic, len(names))
	for i, code := range names {
		result[i] = Diagnostic{Code: code}
	}
	return result
}
