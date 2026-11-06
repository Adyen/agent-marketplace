package plugindecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sync"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// agentPluginSchema is the decoded shape of the bundled 1.0.0 schemas. The
// allowed top-level fields, required fields, and closed per-variant checks
// are derived from this data rather than duplicated in Go literals.
type agentPluginSchema struct {
	required   []string
	properties map[string]json.RawMessage
	cwdPattern *regexp.Regexp
	serverDefs map[string]serverDef
}

type serverDef struct {
	typeConst  string
	required   []string
	properties map[string]json.RawMessage
}

var agentSchemas = sync.OnceValues(func() (agentPluginSchema, error) {
	var plugin struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(pluginSchemaJSON, &plugin); err != nil {
		return agentPluginSchema{}, err
	}
	var mcp struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Const    string   `json:"const"`
				Pattern  string   `json:"pattern"`
				Required []string `json:"-"`
			} `json:"properties"`
			Required []string `json:"required"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(mcpSchemaJSON, &mcp); err != nil {
		return agentPluginSchema{}, err
	}
	out := agentPluginSchema{required: plugin.Required, properties: plugin.Properties, serverDefs: map[string]serverDef{}}
	for _, variant := range []string{"stdioServer", "streamableHttpServer", "sseServer"} {
		def := mcp.Defs[variant]
		sd := serverDef{required: def.Required, properties: map[string]json.RawMessage{}}
		for name, prop := range def.Properties {
			raw, _ := json.Marshal(prop)
			sd.properties[name] = raw
			if prop.Const != "" {
				sd.typeConst = prop.Const
			}
			if name == "cwd" && prop.Pattern != "" {
				re, err := regexp.Compile(prop.Pattern)
				if err != nil {
					return agentPluginSchema{}, err
				}
				out.cwdPattern = re
			}
		}
		out.serverDefs[variant] = sd
	}
	return out, nil
})

// decodeManifest reads, validates, and interprets the package manifest. For
// agent-plugin the closed checks derive from the bundled schema; for the
// native formats they come from the format catalog's field rules.
func (d *decoder) decodeManifest() error {
	obj, err := d.readJSON(d.layout.ManifestPath)
	if err != nil {
		var ce *diag.CodedError
		if errors.As(err, &ce) {
			return err
		}
		return codedErr(diag.CodePluginFormatManifestInvalid, d.layout.ManifestPath, "manifest cannot be read: %v", err)
	}
	d.manifestObj = obj
	if d.format == "agent-plugin" {
		return d.decodeAgentManifest(obj)
	}
	return d.decodeNativeManifest(obj)
}

func codedErr(code, file, format string, args ...any) error {
	return diag.NewCodedError(code, diag.SourceRef{File: file}, fmt.Errorf(format, args...))
}

func (d *decoder) decodeAgentManifest(obj map[string]any) error {
	schema, err := agentSchemas()
	if err != nil {
		return err
	}
	file := d.layout.ManifestPath
	allowed := map[string]bool{}
	for name := range schema.properties {
		allowed[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(obj)) {
		if !allowed[name] {
			d.warn(diag.CodePluginFormatFieldIgnored,
				fmt.Sprintf("manifest declares unknown top-level field %q; it is ignored", name), file)
		}
	}
	schemaID, isString := obj["$schema"].(string)
	if _, present := obj["$schema"]; !present || !isString {
		return codedErr(diag.CodePluginFormatManifestInvalid, file, "manifest lacks a valid required $schema field")
	}
	if schemaID != agentPluginSchemaID {
		return codedErr(diag.CodePluginFormatVersionUnsupported, file,
			"manifest declares an Agent Plugins schema identifier this decoder does not support")
	}
	name, _ := obj["name"].(string)
	if err := ir.ValidatePluginName(d.format, name); err != nil {
		return codedErr(diag.CodePluginFormatManifestInvalid, file, "manifest lacks a valid required name: %v", err)
	}
	// Every remaining permitted field must match its schema type; a schema
	// violation other than an unknown field or a non-object extensions
	// member is fatal before discovery (Agent Plugins §5.2).
	for field, want := range map[string]string{
		"version": "string", "description": "string", "homepage": "string",
		"repository": "string", "license": "string", "author": "object", "keywords": "array",
	} {
		if raw, present := obj[field]; present && jsonType(raw) != want {
			return codedErr(diag.CodePluginFormatManifestInvalid, file,
				"manifest field %q must be a %s", field, want)
		}
	}
	if raw, present := obj["extensions"]; present {
		if ext, ok := raw.(map[string]any); ok {
			for _, v := range ext {
				if _, isObj := v.(map[string]any); !isObj {
					return codedErr(diag.CodePluginFormatManifestInvalid, file,
						"extensions members must be objects")
				}
			}
			d.extensions = ext
		} else {
			d.warn(diag.CodePluginFormatFieldIgnored,
				"manifest extensions is not an object; it is ignored", file)
		}
	}
	d.manifest = ir.Manifest{
		Name:        name,
		Version:     stringField(obj, "version"),
		Description: stringField(obj, "description"),
		Extra:       map[string]any{},
	}
	d.formatVersion = "1.0.0"
	return nil
}

func (d *decoder) decodeNativeManifest(obj map[string]any) error {
	file := d.layout.ManifestPath
	extra := maps.Clone(obj)
	for field, rule := range d.info.ManifestFields {
		raw, present := obj[field]
		delete(extra, field)
		if !present {
			if rule.Required {
				return codedErr(diag.CodePluginFormatManifestInvalid, file,
					"manifest lacks required field %q", field)
			}
			continue
		}
		if jsonType(raw) != rule.Type {
			if rule.Invalid == "refuse-package" || rule.Required {
				return codedErr(diag.CodePluginFormatManifestInvalid, file,
					"manifest field %q must be a %s", field, rule.Type)
			}
			d.warn(diag.CodePluginFormatFieldIgnored,
				fmt.Sprintf("manifest field %q is not a %s; it is ignored", field, rule.Type), file)
		}
	}
	if _, present := obj["dependencies"]; present && d.info.Dependencies == "refuse-package" {
		return codedErr(diag.CodePluginFormatManifestInvalid, file,
			"manifest declares dependencies, which the %s catalog refuses", d.format)
	}
	delete(extra, "dependencies")
	name, _ := obj["name"].(string)
	if err := ir.ValidatePluginName(d.format, name); err != nil {
		return codedErr(diag.CodePluginFormatManifestInvalid, file, "manifest lacks a valid required name: %v", err)
	}
	d.manifest = ir.Manifest{
		Name:        name,
		Version:     stringField(obj, "version"),
		Description: stringField(obj, "description"),
		Extra:       extra,
	}
	return nil
}

func stringField(obj map[string]any, name string) string {
	s, _ := obj[name].(string)
	return s
}

// jsonType reports the catalog field type of a decoded JSON value.
func jsonType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, json.Number:
		return "number"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return "unknown"
	}
}
