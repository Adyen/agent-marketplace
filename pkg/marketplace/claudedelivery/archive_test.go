package claudedelivery

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"testing"

	canonical "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var claudePluginName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// spec: claude-enterprise-delivery/a-long-identity-produces-a-bounded-unique-name
func TestDerivePluginNameBoundsLongIdentitiesWithoutCollision(t *testing.T) {
	prefix := "am"
	first := ResourceIdentity{
		RegistryID:   "registry-one",
		RegistrySlug: strings.Repeat("long-registry-segment-", 4) + "one",
		Kind:         "skill",
		Name:         "threat-model",
	}
	second := first
	second.RegistrySlug = strings.Repeat("long-registry-segment-", 4) + "two"

	gotFirst, err := DerivePluginName(prefix, first)
	if err != nil {
		t.Fatalf("DerivePluginName(first): %v", err)
	}
	gotSecond, err := DerivePluginName(prefix, second)
	if err != nil {
		t.Fatalf("DerivePluginName(second): %v", err)
	}
	for _, name := range []string{gotFirst, gotSecond} {
		if len(name) > 64 || !claudePluginName.MatchString(name) {
			t.Errorf("derived name %q violates the plugin name contract", name)
		}
	}
	if gotFirst == gotSecond {
		t.Fatalf("distinct long identities collided: %q", gotFirst)
	}
	if len(gotFirst) != 64 || !strings.HasSuffix(gotFirst, "-9c87619f") {
		t.Fatalf("long derived name = %q, want 55-character prefix and identity digest", gotFirst)
	}
	commonSuffix := strings.Repeat("shared-suffix-", 8)
	ambiguousFirst := ResourceIdentity{
		RegistryID: "registry-one", RegistrySlug: "shared", Kind: "skill", Name: "team-skill-" + commonSuffix,
	}
	ambiguousSecond := ResourceIdentity{
		RegistryID: "registry-two", RegistrySlug: "shared-skill-team", Kind: "skill", Name: commonSuffix,
	}
	firstName, err := DerivePluginName(prefix, ambiguousFirst)
	if err != nil {
		t.Fatalf("DerivePluginName(ambiguous first): %v", err)
	}
	secondName, err := DerivePluginName(prefix, ambiguousSecond)
	if err != nil {
		t.Fatalf("DerivePluginName(ambiguous second): %v", err)
	}
	if len(firstName) != 64 || len(secondName) != 64 ||
		firstName[:55] != secondName[:55] || firstName[55] != '-' || secondName[55] != '-' ||
		firstName[56:] == secondName[56:] {
		t.Fatalf("ambiguous long identities produced names %q and %q, want distinct truncated names", firstName, secondName)
	}
}

// spec: claude-enterprise-delivery/the-mirrored-resource-keeps-its-own-name
// spec: claude-enterprise-delivery/a-lost-version-response-does-not-store-a-duplicate-version
func TestBuildArchiveKeepsResourceNameAndUsesDerivedManifestName(t *testing.T) {
	input := skillBuildInput(t)
	result, err := BuildArchive(input)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	files := readZipFiles(t, result.Archive)
	body, ok := files["skills/threat-model/SKILL.md"]
	if !ok || !bytes.Contains(body, []byte("name: threat-model")) {
		t.Fatalf("skill source layout does not preserve its own name: %q", body)
	}
	var manifest map[string]any
	if err := json.Unmarshal(files[".claude-plugin/plugin.json"], &manifest); err != nil {
		t.Fatalf("decode plugin manifest: %v", err)
	}
	wantName := "am-acme-skill-threat-model"
	if manifest["name"] != wantName || manifest["name"] == "threat-model" {
		t.Fatalf("manifest name = %v, want derived plugin name %q", manifest["name"], wantName)
	}
	if manifest["displayName"] != "threat-model" || manifest["description"] != "Threat model description" {
		t.Fatalf("manifest metadata = %#v", manifest)
	}
	author, ok := manifest["author"].(map[string]any)
	if !ok || author["name"] != "Example Publisher" {
		t.Fatalf("manifest author = %#v, want object with name Example Publisher", manifest["author"])
	}
	if manifest["version"] != "1.2.3" {
		t.Fatalf("manifest version = %v, want declared version 1.2.3", manifest["version"])
	}
	digest := sha256.Sum256(result.Archive)
	if result.Digest != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatalf("archive digest = %q, want sha256:%x", result.Digest, digest)
	}
	if !strings.Contains(result.ReleaseNotes, "publication-42") ||
		!strings.Contains(result.ReleaseNotes, "commit-123") ||
		!strings.Contains(result.ReleaseNotes, input.ContentDigest) ||
		!strings.HasSuffix(result.ReleaseNotes, "; archive_digest="+result.Digest) {
		t.Fatalf("release notes omit provenance: %q", result.ReleaseNotes)
	}
}

// spec: claude-enterprise-delivery/the-mirrored-resource-keeps-its-own-name
func TestBuildArchiveUsesClaudeSourceLayoutForEveryResourceKind(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		path      string
		transport string
	}{
		{name: "skill", kind: "skill", path: "skills/threat-model/SKILL.md"},
		{name: "command", kind: "command", path: "commands/audit.md"},
		{name: "subagent", kind: "subagent", path: "agents/reviewer.md"},
		{name: "hook", kind: "hook", path: "hooks/hooks.json"},
		{name: "mcpserver stdio", kind: "mcpserver", path: ".mcp.json"},
		{name: "mcpserver remote", kind: "mcpserver", path: ".mcp.json", transport: "http"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := BuildArchive(buildInputForKind(t, test.kind, test.transport))
			if err != nil {
				t.Fatalf("BuildArchive: %v", err)
			}
			files := readZipFiles(t, result.Archive)
			if _, ok := files[test.path]; !ok {
				t.Fatalf("archive is missing %q; entries=%v", test.path, sortedKeys(files))
			}
			if _, ok := files[".claude-plugin/plugin.json"]; !ok {
				t.Fatal("generated plugin manifest is missing")
			}
			if _, ok := files["licenses/LICENSE"]; !ok {
				t.Fatal("canonical license file was not included")
			}
			archive, err := zip.NewReader(bytes.NewReader(result.Archive), int64(len(result.Archive)))
			if err != nil {
				t.Fatalf("open built archive: %v", err)
			}
			switch test.kind {
			case "skill":
				if _, ok := files["skills/threat-model/guide.md"]; !ok {
					t.Fatal("skill asset was not included beside SKILL.md")
				}
			case "hook":
				if _, ok := files["hooks/validator/helper.sh"]; !ok {
					t.Fatal("hook support file was not placed beneath hooks/<name>")
				}
				executableSupport, normalSupport := false, false
				for _, file := range archive.File {
					switch file.Name {
					case "hooks/validator/helper.sh":
						executableSupport = file.Mode().Perm() == 0o755
					case "hooks/validator/readme.txt":
						normalSupport = file.Mode().Perm() == 0o644
					}
				}
				if !executableSupport || !normalSupport {
					t.Fatalf("hook support modes executable=%t normal=%t, want 0755 and 0644",
						executableSupport, normalSupport)
				}
			case "mcpserver":
				var config struct {
					MCPServers map[string]map[string]any `json:"mcpServers"`
				}
				if err := json.Unmarshal(files[".mcp.json"], &config); err != nil {
					t.Fatalf("decode MCP config: %v", err)
				}
				server := config.MCPServers["knowledge"]
				if test.transport == "" {
					if server["command"] != "mcp" || server["type"] != nil ||
						server["url"] != nil || server["transport"] != nil {
						t.Fatalf("stdio MCP server = %#v, want stdio fallback fields", server)
					}
				} else {
					if server["type"] != test.transport || server["url"] != "https://example.invalid/mcp" {
						t.Fatalf("remote MCP server = %#v, want documented type and URL", server)
					}
					if _, exists := server["transport"]; exists {
						t.Fatalf("remote MCP server contains obsolete transport field: %#v", server)
					}
				}
			}
			manifestCount := 0
			for _, file := range archive.File {
				if file.Name == ".claude-plugin/plugin.json" {
					manifestCount++
				}
			}
			if manifestCount != 1 {
				t.Fatalf("generated manifest count = %d", manifestCount)
			}
		})
	}
}

// spec: claude-enterprise-delivery/the-mirrored-resource-keeps-its-own-name
func TestMCPPluginFilesReserveTypeAndTransportExtras(t *testing.T) {
	server := ir.NewMcpServer("knowledge", "", nil, nil,
		map[string]any{"type": "extra-type", "transport": "extra-transport"}, ir.SourceRef{})
	server.Transport = "http"
	server.URL = "https://example.invalid/mcp"
	files, err := pluginFiles(server, canonical.CanonicalArtifact{})
	if err != nil {
		t.Fatalf("pluginFiles: %v", err)
	}
	var config struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(files[".mcp.json"].data, &config); err != nil {
		t.Fatalf("decode MCP config: %v", err)
	}
	result := config.MCPServers["knowledge"]
	if result["type"] != "http" {
		t.Fatalf("MCP type = %v, want http", result["type"])
	}
	if _, exists := result["transport"]; exists {
		t.Fatalf("MCP config contains transport field from an extra: %#v", result)
	}
}

// spec: claude-enterprise-delivery/repeated-builds-are-byte-identical
func TestBuildArchiveIsDeterministicAcrossProcesses(t *testing.T) {
	if os.Getenv("CLAUDE_DELIVERY_BUILD_HELPER") == "1" {
		result, err := BuildArchive(skillBuildInput(t))
		if err != nil {
			t.Fatalf("BuildArchive in child: %v", err)
		}
		sum := sha256.Sum256(result.Archive)
		_, _ = fmt.Fprint(os.Stdout, hex.EncodeToString(sum[:]))
		return
	}
	digests := make([]string, 2)
	for i := range digests {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBuildArchiveIsDeterministicAcrossProcesses$")
		cmd.Env = append(os.Environ(), "CLAUDE_DELIVERY_BUILD_HELPER=1")
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("child build %d: %v", i, err)
		}
		digests[i] = strings.TrimSuffix(strings.TrimSpace(string(output)), "PASS")
	}
	if _, err := hex.DecodeString(digests[0]); err != nil {
		t.Fatalf("first child did not return a SHA-256 digest: %q", digests[0])
	}
	if digests[0] == "" || digests[0] != digests[1] {
		t.Fatalf("separate process archive digests = %q and %q", digests[0], digests[1])
	}
}

func TestValidateArchiveReturnsStableCodeForEachUploadRule(t *testing.T) {
	tests := []struct {
		name  string
		files []archiveEntry
		code  string
		edit  func(*testing.T, []byte) []byte
	}{
		{name: "missing manifest", files: []archiveEntry{{"skills/example/SKILL.md", validSkill}}, code: "manifest.missing"},
		{name: "duplicate manifest", files: []archiveEntry{{".claude-plugin/plugin.json", validManifest}, {".claude-plugin/plugin.json", validManifest}}, code: "manifest.duplicate"},
		{name: "invalid name", files: defaultFiles(), code: "manifest.name-invalid", edit: func(t *testing.T, raw []byte) []byte {
			return editManifestInZip(t, raw, `"name":"example-plugin"`, `"name":"Invalid_Name"`)
		}},
		{name: "display name length", files: defaultFiles(), code: "manifest.display-name-too-long", edit: func(t *testing.T, raw []byte) []byte {
			return editManifestInZip(t, raw, `"displayName":"Example"`, `"displayName":"`+strings.Repeat("x", 65)+`"`)
		}},
		{name: "description length", files: defaultFiles(), code: "manifest.description-too-long", edit: func(t *testing.T, raw []byte) []byte {
			return editManifestInZip(t, raw, `"description":"Valid description"`, `"description":"`+strings.Repeat("x", 501)+`"`)
		}},
		{name: "missing skill name", files: []archiveEntry{{".claude-plugin/plugin.json", validManifest}, {"skills/x/SKILL.md", "---\ndescription: good\n---\nbody"}}, code: "skill.frontmatter-invalid"},
		{name: "missing skill description", files: []archiveEntry{{".claude-plugin/plugin.json", validManifest}, {"skills/x/SKILL.md", "---\nname: x\n---\nbody"}}, code: "skill.frontmatter-invalid"},
		{name: "malformed skill frontmatter", files: []archiveEntry{{".claude-plugin/plugin.json", validManifest}, {"skills/x/SKILL.md", "---\nname: [\n---\nbody"}}, code: "skill.frontmatter-invalid"},
		{name: "xml in skill metadata", files: []archiveEntry{{".claude-plugin/plugin.json", validManifest}, {"skills/x/SKILL.md", "---\nname: <tag>\ndescription: good\n---\nbody"}}, code: "skill.frontmatter-xml"},
		{name: "xml in skill description", files: []archiveEntry{{".claude-plugin/plugin.json", validManifest}, {"skills/x/SKILL.md", "---\nname: x\ndescription: <tag>\n---\nbody"}}, code: "skill.frontmatter-xml"},
		{name: "duplicate skills", files: []archiveEntry{{".claude-plugin/plugin.json", validManifest}, {"skills/one/SKILL.md", validSkill}, {"skills/two/SKILL.md", validSkill}}, code: "skill.name-duplicate"},
		{name: "duplicate commands", files: []archiveEntry{{".claude-plugin/plugin.json", validManifest}, {"commands/one.md", "x"}, {"commands/one.md", "y"}}, code: "command.name-duplicate"},
		{name: "top-level bin", files: append(defaultFiles(), archiveEntry{"bin/tool", "x"}), code: "archive.top-level-bin"},
		{name: "nested zip", files: append(defaultFiles(), archiveEntry{"assets/archive.zip", "x"}), code: "archive.nested-zip"},
		{name: "invalid path character", files: append(defaultFiles(), archiveEntry{"assets/invalid@path", "x"}), code: "archive.path-invalid"},
		{name: "path depth", files: append(defaultFiles(), archiveEntry{"a/b/c/d/e/f/g/h/i/j/k/l/m/file", "x"}), code: "archive.path-depth"},
		{name: "path traversal", files: append(defaultFiles(), archiveEntry{"assets/../escape", "x"}), code: "archive.path-invalid"},
		{name: "path length", files: append(defaultFiles(), archiveEntry{"assets/" + strings.Repeat("a", 245) + "/" + strings.Repeat("b", 225), "x"}), code: "archive.path-too-long"},
		{name: "component length", files: append(defaultFiles(), archiveEntry{"assets/" + strings.Repeat("a", 256), "x"}), code: "archive.component-too-long"},
		{name: "file count", files: manyFiles(5001), code: "archive.too-many-files"},
		{name: "uncompressed size", files: defaultFiles(), code: "archive.uncompressed-too-large", edit: func(_ *testing.T, raw []byte) []byte { return oversizedCentralDirectory(raw) }},
		{name: "encrypted member", files: defaultFiles(), code: "archive.encrypted", edit: editEncryptedFlags},
		{name: "symlink member", files: append(defaultFiles(), archiveEntry{"assets/link", "target"}), code: "archive.symlink"},
		{name: "unsupported compression", files: defaultFiles(), code: "archive.compression-unsupported", edit: editUnsupportedMethod},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := makeZip(t, test.files)
			if test.edit != nil {
				raw = test.edit(t, raw)
			}
			diagnostics := ValidateArchive(raw)
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == test.code {
					return
				}
			}
			t.Fatalf("diagnostics = %+v, want stable code %q", diagnostics, test.code)
		})
	}
}

type archiveEntry struct {
	path string
	data string
}

const (
	validManifest = `{"name":"example-plugin","displayName":"Example","description":"Valid description"}`
	validSkill    = "---\nname: example\ndescription: A valid skill\n---\nbody"
)

func defaultFiles() []archiveEntry {
	return []archiveEntry{
		{".claude-plugin/plugin.json", validManifest},
		{"skills/example/SKILL.md", validSkill},
	}
}

func makeZip(t *testing.T, files []archiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.path}
		if file.path == "assets/link" {
			header.SetMode(os.ModeSymlink | 0o777)
		}
		header.Method = zip.Deflate
		part, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create zip member %q: %v", file.path, err)
		}
		if _, err := io.WriteString(part, file.data); err != nil {
			t.Fatalf("write zip member %q: %v", file.path, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

func readZipFiles(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	files := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		r, err := file.Open()
		if err != nil {
			t.Fatalf("open zip member %q: %v", file.Name, err)
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatalf("read zip member %q: %v", file.Name, err)
		}
		files[file.Name] = data
	}
	return files
}

func manyFiles(count int) []archiveEntry {
	files := defaultFiles()
	for len(files) < count {
		files = append(files, archiveEntry{path: fmt.Sprintf("assets/file-%04d.txt", len(files)), data: "x"})
	}
	return files
}

func oversizedCentralDirectory(raw []byte) []byte {
	raw = bytes.Clone(raw)
	offset := bytes.Index(raw, []byte{'P', 'K', 1, 2})
	if offset >= 0 {
		size := uint32(200*1024*1024 + 1)
		raw[offset+24] = byte(size)
		raw[offset+25] = byte(size >> 8)
		raw[offset+26] = byte(size >> 16)
		raw[offset+27] = byte(size >> 24)
	}
	return raw
}

func editManifestInZip(t *testing.T, raw []byte, from, to string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("open zip for manifest edit: %v", err)
	}
	files := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		r, err := file.Open()
		if err != nil {
			t.Fatalf("open zip member %q: %v", file.Name, err)
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatalf("read zip member %q: %v", file.Name, err)
		}
		files[file.Name] = data
	}
	files[".claude-plugin/plugin.json"] = []byte(strings.Replace(string(files[".claude-plugin/plugin.json"]), from, to, 1))
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, data := range files {
		part, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip member %q: %v", name, err)
		}
		_, _ = part.Write(data)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close edited zip: %v", err)
	}
	return buffer.Bytes()
}

func editEncryptedFlags(t *testing.T, raw []byte) []byte {
	t.Helper()
	raw = bytes.Clone(raw)
	if offset := bytes.Index(raw, []byte{'P', 'K', 1, 2}); offset >= 0 {
		raw[offset+8] |= 1
	}
	return raw
}

func editUnsupportedMethod(t *testing.T, raw []byte) []byte {
	t.Helper()
	raw = bytes.Clone(raw)
	if offset := bytes.Index(raw, []byte{'P', 'K', 1, 2}); offset >= 0 {
		raw[offset+10] = 99
		raw[offset+11] = 0
	}
	return raw
}

func skillBuildInput(t *testing.T) BuildInput {
	t.Helper()
	return buildInputForKind(t, "skill", "")
}

func buildInputForKind(t *testing.T, kind, mcpTransport string) BuildInput {
	t.Helper()
	var resource ir.Resource
	switch kind {
	case "skill":
		resource = ir.NewSkill("threat-model", "Threat model description", "Skill body", nil, map[string]any{"version": "1.2.3"}, ir.SourceRef{})
	case "command":
		resource = ir.NewCommand("audit", "Audit description", "Audit body", nil, ir.SourceRef{})
	case "subagent":
		resource = ir.NewSubagent("reviewer", "Reviewer description", "Review body", "sonnet", nil, "", nil, "", nil, ir.SourceRef{})
	case "hook":
		resource = ir.NewHook("validator", "PreToolUse", "echo check", "command", nil, ir.SourceRef{})
	case "mcpserver":
		if mcpTransport == "" {
			resource = ir.NewMcpServer("knowledge", "mcp", []string{"serve"},
				map[string]string{"TOKEN": "private"}, nil, ir.SourceRef{})
		} else {
			server := ir.NewMcpServer("knowledge", "", nil, nil, nil, ir.SourceRef{})
			server.Transport = mcpTransport
			server.URL = "https://example.invalid/mcp"
			resource = server
		}
	default:
		t.Fatalf("unsupported fixture kind %q", kind)
	}
	artifact, err := canonical.FromIR(resource)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	if kind == "skill" {
		artifact.Assets = map[string][]byte{"guide.md": []byte("asset")}
	}
	if kind == "hook" {
		artifact.Support = map[string]canonical.SupportFile{
			"helper.sh":  {Data: []byte("echo helper"), Executable: true},
			"readme.txt": {Data: []byte("helper documentation")},
		}
	}
	artifact.Licenses = map[string][]byte{"LICENSE": []byte("MIT")}
	artifact.Manifest.RegistryID = "registry-1"
	artifact.Manifest.RegistrySlug = "acme"
	artifact.Manifest.Revision = "revision-1"
	artifact.Manifest.ContentDigest = canonical.ContentDigest(artifact.Payload)
	raw, _, err := canonical.Encode(artifact)
	if err != nil {
		t.Fatalf("Encode canonical artifact: %v", err)
	}
	return BuildInput{
		CanonicalArtifact: raw,
		Identity: ResourceIdentity{
			RegistryID:   "registry-1",
			RegistrySlug: "acme",
			Kind:         kind,
			Name:         resource.Name(),
		},
		PluginNamePrefix: "am",
		PublicationID:    "publication-42",
		RegistryCommit:   "commit-123",
		PublisherName:    "Example Publisher",
		PublisherURL:     "https://publisher.example",
		ContentDigest:    artifact.Manifest.ContentDigest,
	}
}

func sortedKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
