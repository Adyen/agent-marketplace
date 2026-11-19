package claudedelivery

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

type ResourceIdentity struct {
	RegistryID   string
	RegistrySlug string
	Kind         string
	Name         string
}

type BuildInput struct {
	CanonicalArtifact []byte
	Identity          ResourceIdentity
	PluginNamePrefix  string
	PluginName        string
	PublicationID     string
	RegistryCommit    string
	PublisherName     string
	PublisherURL      string
	ContentDigest     string
}

type BuildResult struct {
	Archive      []byte
	Digest       string
	PluginName   string
	ReleaseNotes string
}

func DerivePluginName(prefix string, identity ResourceIdentity) (string, error) {
	if len(prefix) < 1 || len(prefix) > 16 || !validPluginPrefix(prefix) ||
		identity.RegistrySlug == "" || identity.Kind == "" || identity.Name == "" {
		return "", errors.New("claude plugin identity is incomplete")
	}
	segments := []string{prefix, identity.RegistrySlug, identity.Kind, identity.Name}
	parts := make([]string, len(segments))
	for i, segment := range segments {
		if i == 0 {
			parts[i] = segment
		} else {
			parts[i] = foldNameSegment(segment)
		}
	}
	name := strings.Trim(strings.Join(parts, "-"), "-")
	if len(name) <= 64 {
		return name, nil
	}
	key, _ := json.Marshal(segments)
	sum := sha256.Sum256(key)
	return name[:55] + "-" + hex.EncodeToString(sum[:])[:8], nil
}

func validPluginPrefix(value string) bool {
	if value[0] < 'a' || value[0] > 'z' {
		return false
	}
	hyphen := false
	for i := 1; i < len(value); i++ {
		char := value[i]
		if char == '-' {
			if hyphen || i == len(value)-1 {
				return false
			}
			hyphen = true
			continue
		}
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
		hyphen = false
	}
	return true
}

func foldNameSegment(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	lastHyphen := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			result.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.Trim(result.String(), "-")
}

func BuildArchive(input BuildInput) (BuildResult, error) {
	if len(input.CanonicalArtifact) == 0 {
		return BuildResult{}, errors.New("canonical artifact is required")
	}
	identity := input.Identity
	if identity.Kind == "" || identity.Name == "" || identity.RegistrySlug == "" {
		return BuildResult{}, errors.New("claude plugin identity is incomplete")
	}
	canonicalArtifact, _, err := artifact.Decode(bytes.NewReader(input.CanonicalArtifact))
	if err != nil {
		return BuildResult{}, errors.New("decode canonical Claude delivery artifact")
	}
	if canonicalArtifact.Manifest.Kind != identity.Kind || canonicalArtifact.Manifest.Name != identity.Name {
		return BuildResult{}, errors.New("claude delivery identity does not match canonical artifact")
	}
	resource, err := artifact.DecodeResource(identity.Kind, canonicalArtifact.Payload)
	if err != nil {
		return BuildResult{}, errors.New("decode canonical Claude delivery resource")
	}
	if _, _, err := ir.ValidateResource(resource); err != nil {
		return BuildResult{}, errors.New("canonical Claude delivery resource is invalid")
	}
	pluginName := input.PluginName
	if pluginName == "" {
		pluginName, err = DerivePluginName(input.PluginNamePrefix, identity)
		if err != nil {
			return BuildResult{}, err
		}
	}
	files, err := pluginFiles(resource, canonicalArtifact)
	if err != nil {
		return BuildResult{}, err
	}
	manifest := map[string]any{
		"name":        pluginName,
		"displayName": truncateRunes(resourceDisplayName(resource), 64),
		"description": truncateRunes(resourceDescription(resource), 500),
	}
	if version := resourceVersion(resource); version != "" {
		manifest["version"] = version
	}
	if input.PublisherName != "" {
		manifest["author"] = map[string]string{"name": input.PublisherName}
	}
	if input.PublisherURL != "" {
		manifest["homepage"] = input.PublisherURL
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return BuildResult{}, errors.New("encode Claude plugin manifest")
	}
	files[".claude-plugin/plugin.json"] = pluginFile{data: manifestBytes}
	archiveBytes, err := encodePluginArchive(files)
	if err != nil {
		return BuildResult{}, err
	}
	digest := sha256.Sum256(archiveBytes)
	contentDigest := input.ContentDigest
	if contentDigest == "" {
		contentDigest = canonicalArtifact.Manifest.ContentDigest
	}
	archiveDigest := "sha256:" + hex.EncodeToString(digest[:])
	releaseNotes := fmt.Sprintf("publication=%s; registry_commit=%s; content_digest=%s; archive_digest=%s",
		input.PublicationID, input.RegistryCommit, contentDigest, archiveDigest)
	return BuildResult{
		Archive:      archiveBytes,
		Digest:       archiveDigest,
		PluginName:   pluginName,
		ReleaseNotes: releaseNotes,
	}, nil
}

type pluginFile struct {
	data       []byte
	executable bool
}

func pluginFiles(resource ir.Resource, canonicalArtifact artifact.CanonicalArtifact) (map[string]pluginFile, error) {
	files := make(map[string]pluginFile, len(canonicalArtifact.Assets)+len(canonicalArtifact.Licenses)+len(canonicalArtifact.Support)+2)
	switch value := resource.(type) {
	case *ir.Skill:
		frontmatter := map[string]any{
			"name":        value.Name(),
			"description": value.Description,
		}
		if value.ArgumentHint != "" {
			frontmatter["argument-hint"] = value.ArgumentHint
		}
		if value.Model != "" {
			frontmatter["model"] = value.Model
		}
		if len(value.AllowedTools) > 0 {
			frontmatter["allowed-tools"] = value.AllowedTools
		}
		for key, extra := range value.Extra {
			if _, exists := frontmatter[key]; !exists {
				frontmatter[key] = extra
			}
		}
		data, err := markdownWithFrontmatter(frontmatter, value.Body)
		if err != nil {
			return nil, err
		}
		files[path.Join("skills", value.Name(), "SKILL.md")] = pluginFile{data: data}
		for name, content := range canonicalArtifact.Assets {
			file, err := containedPluginPath(path.Join("skills", value.Name()), name)
			if err != nil {
				return nil, err
			}
			files[file] = pluginFile{data: bytes.Clone(content)}
		}
	case *ir.Command:
		frontmatter := map[string]any{"name": value.Name(), "description": value.Description}
		for key, extra := range value.Extra {
			if _, exists := frontmatter[key]; !exists {
				frontmatter[key] = extra
			}
		}
		data, err := markdownWithFrontmatter(frontmatter, value.Body)
		if err != nil {
			return nil, err
		}
		files[path.Join("commands", value.Name()+".md")] = pluginFile{data: data}
	case *ir.Subagent:
		frontmatter := map[string]any{"name": value.Name(), "description": value.Description}
		if value.Model != "" {
			frontmatter["model"] = value.Model
		}
		if len(value.Tools) > 0 {
			frontmatter["tools"] = value.Tools
		}
		if len(value.DisallowedTools) > 0 {
			frontmatter["disallowedTools"] = value.DisallowedTools
		}
		if value.Effort != "" {
			frontmatter["effort"] = value.Effort
		}
		if value.Isolation != "" {
			frontmatter["isolation"] = value.Isolation
		}
		if value.MaxNesting != nil {
			frontmatter["maxNesting"] = *value.MaxNesting
		}
		for key, extra := range value.Extra {
			if _, exists := frontmatter[key]; !exists {
				frontmatter[key] = extra
			}
		}
		data, err := markdownWithFrontmatter(frontmatter, value.Body)
		if err != nil {
			return nil, err
		}
		files[path.Join("agents", value.Name()+".md")] = pluginFile{data: data}
	case *ir.Hook:
		entry := map[string]any{"type": value.Type}
		if entry["type"] == "" {
			entry["type"] = "command"
		}
		if value.Command != "" {
			entry["command"] = value.Command
		}
		if value.Prompt != "" {
			entry["prompt"] = value.Prompt
		}
		if value.Timeout != nil {
			entry["timeout"] = *value.Timeout
		}
		for key, extra := range value.Extra {
			if _, exists := entry[key]; !exists {
				entry[key] = extra
			}
		}
		group := map[string]any{"hooks": []any{entry}}
		if value.Matcher != "" {
			group["matcher"] = value.Matcher
		}
		data, err := json.Marshal(map[string]any{
			"hooks": map[string]any{value.Event: []any{group}},
		})
		if err != nil {
			return nil, errors.New("encode Claude plugin hooks")
		}
		files["hooks/hooks.json"] = pluginFile{data: data}
		for name, support := range canonicalArtifact.Support {
			file, err := containedPluginPath(path.Join("hooks", value.Name()), name)
			if err != nil {
				return nil, err
			}
			files[file] = pluginFile{data: bytes.Clone(support.Data), executable: support.Executable}
		}
	case *ir.McpServer:
		server := map[string]any{}
		if value.Command != "" {
			server["command"] = value.Command
		}
		if len(value.Args) > 0 {
			server["args"] = value.Args
		}
		if len(value.Env) > 0 {
			server["env"] = value.Env
		}
		if value.Cwd != "" {
			server["cwd"] = value.Cwd
		}
		if value.URL != "" {
			server["url"] = value.URL
		}
		if value.Transport != "" {
			server["type"] = value.Transport
		}
		if len(value.Headers) > 0 {
			server["headers"] = value.Headers
		}
		if value.DisabledSet || value.Disabled {
			server["disabled"] = value.Disabled
		}
		if value.OAuthClientID != "" {
			server["oauthClientId"] = value.OAuthClientID
		}
		if value.OAuthClientSecret != "" {
			server["oauthClientSecret"] = value.OAuthClientSecret
		}
		if value.OAuthResource != "" {
			server["oauthResource"] = value.OAuthResource
		}
		for key, extra := range value.Extra {
			if key == "type" || key == "transport" {
				continue
			}
			if _, exists := server[key]; !exists {
				server[key] = extra
			}
		}
		data, err := json.Marshal(map[string]any{
			"mcpServers": map[string]any{value.Name(): server},
		})
		if err != nil {
			return nil, errors.New("encode Claude plugin MCP configuration")
		}
		files[".mcp.json"] = pluginFile{data: data}
	default:
		return nil, errors.New("unsupported Claude delivery resource kind")
	}
	for name, content := range canonicalArtifact.Licenses {
		file, err := containedPluginPath("licenses", name)
		if err != nil {
			return nil, err
		}
		files[file] = pluginFile{data: bytes.Clone(content)}
	}
	return files, nil
}

func markdownWithFrontmatter(frontmatter map[string]any, body string) ([]byte, error) {
	metadata, err := yaml.Marshal(frontmatter)
	if err != nil {
		return nil, errors.New("encode Claude plugin resource frontmatter")
	}
	var result bytes.Buffer
	result.WriteString("---\n")
	result.Write(metadata)
	result.WriteString("---\n")
	result.WriteString(body)
	return result.Bytes(), nil
}

func resourceDescription(resource ir.Resource) string {
	switch value := resource.(type) {
	case *ir.Skill:
		return value.Description
	case *ir.Command:
		return value.Description
	case *ir.Subagent:
		return value.Description
	case *ir.Hook:
		return resourceStringExtra(value.Extra, "description")
	case *ir.McpServer:
		return resourceStringExtra(value.Extra, "description")
	default:
		return ""
	}
}

func resourceDisplayName(resource ir.Resource) string {
	extra := resourceExtra(resource)
	if name := resourceStringExtra(extra, "displayName"); name != "" {
		return name
	}
	return resource.Name()
}

func resourceVersion(resource ir.Resource) string {
	return resourceStringExtra(resourceExtra(resource), "version")
}

func resourceExtra(resource ir.Resource) map[string]any {
	switch value := resource.(type) {
	case *ir.Skill:
		return value.Extra
	case *ir.Command:
		return value.Extra
	case *ir.Subagent:
		return value.Extra
	case *ir.Hook:
		return value.Extra
	case *ir.McpServer:
		return value.Extra
	default:
		return nil
	}
}

func resourceStringExtra(extra map[string]any, key string) string {
	value, _ := extra[key].(string)
	return value
}

func containedPluginPath(base, relative string) (string, error) {
	if relative == "" || path.IsAbs(relative) {
		return "", errors.New("canonical artifact contains an invalid support path")
	}
	for _, component := range strings.Split(relative, "/") {
		if component == "" || component == "." || component == ".." {
			return "", errors.New("canonical artifact contains an invalid support path")
		}
	}
	return path.Join(base, relative), nil
}

func encodePluginArchive(files map[string]pluginFile) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	fixedTime := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.Modified = fixedTime
		mode := fs.FileMode(0o644)
		if files[name].executable {
			mode = 0o755
		}
		header.SetMode(mode)
		file, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return nil, errors.New("encode Claude plugin archive")
		}
		if _, err := file.Write(files[name].data); err != nil {
			_ = writer.Close()
			return nil, errors.New("encode Claude plugin archive")
		}
	}
	if err := writer.Close(); err != nil {
		return nil, errors.New("finish Claude plugin archive")
	}
	return buffer.Bytes(), nil
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) > maximum {
		return string(runes[:maximum])
	}
	return value
}
