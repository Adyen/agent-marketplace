package lockfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

func RepairCandidates(data []byte, scope Scope) ([]Document, error) {
	if int64(len(data)) > MaxFileSize {
		return nil, fmt.Errorf("%w: lock input exceeds %d bytes", ErrFileTooLarge, MaxFileSize)
	}
	views, err := splitRepairConflictViews(data)
	if err != nil {
		return nil, err
	}
	documents := make([]Document, 0, len(views)*2)
	seen := make(map[string]struct{}, len(views)*2)
	for _, view := range views {
		roots, err := repairYAMLViews(view)
		if err != nil {
			return nil, err
		}
		for _, root := range roots {
			encoded, err := encodeRepairYAML(root)
			if err != nil {
				return nil, err
			}
			document, err := Decode(encoded)
			if err != nil {
				return nil, err
			}
			if err := ValidateScope(document, scope); err != nil {
				return nil, err
			}
			identity, err := Encode(document)
			if err != nil {
				return nil, err
			}
			if _, duplicate := seen[string(identity)]; duplicate {
				continue
			}
			seen[string(identity)] = struct{}{}
			documents = append(documents, document)
		}
	}
	if len(documents) == 0 {
		return nil, coded(CodeInvalidDocument, "lock recovery produced no valid document")
	}
	return documents, nil
}

func splitRepairConflictViews(data []byte) ([][]byte, error) {
	var ours, theirs bytes.Buffer
	state := byte(0)
	found := false
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		marker := repairConflictMarker(line)
		switch state {
		case 0:
			switch marker {
			case '<':
				found = true
				state = '<'
			case '|', '=', '>':
				return nil, coded(CodeConflictMarkers, "unmatched Git conflict marker")
			default:
				ours.Write(line)
				theirs.Write(line)
			}
		case '<':
			switch marker {
			case '<':
				return nil, coded(CodeConflictMarkers, "nested Git conflict marker")
			case '|':
				state = '|'
			case '=':
				state = '>'
			case '>':
				return nil, coded(CodeConflictMarkers, "Git conflict marker is missing its separator")
			default:
				ours.Write(line)
			}
		case '|':
			switch marker {
			case '=':
				state = '>'
			case '<', '|', '>':
				return nil, coded(CodeConflictMarkers, "malformed diff3 Git conflict markers")
			}
		case '>':
			switch marker {
			case '>':
				state = 0
			case '<', '|', '=':
				return nil, coded(CodeConflictMarkers, "malformed Git conflict markers")
			default:
				theirs.Write(line)
			}
		}
	}
	if state != 0 {
		return nil, coded(CodeConflictMarkers, "incomplete Git conflict markers")
	}
	if !found {
		return [][]byte{data}, nil
	}
	return [][]byte{ours.Bytes(), theirs.Bytes()}, nil
}

func repairConflictMarker(line []byte) byte {
	line = bytes.TrimSpace(line)
	switch {
	case bytes.HasPrefix(line, []byte("<<<<<<<")):
		return '<'
	case bytes.HasPrefix(line, []byte("|||||||")):
		return '|'
	case bytes.Equal(line, []byte("=======")):
		return '='
	case bytes.HasPrefix(line, []byte(">>>>>>>")):
		return '>'
	default:
		return 0
	}
}

func repairYAMLViews(data []byte) ([]*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil {
		code := CodeMalformedYAML
		if strings.Contains(strings.ToLower(err.Error()), "anchor") || strings.Contains(strings.ToLower(err.Error()), "alias") {
			code = CodeYAMLAlias
		}
		return nil, coded(code, "decode recovery view: %v", err)
	}
	if len(root.Content) == 0 {
		return nil, coded(CodeMalformedYAML, "empty recovery view")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, coded(CodeMultipleDocuments, "exactly one YAML document is required in each recovery view")
	}
	views := make([]*yaml.Node, 0, 2)
	for _, chooseLast := range []bool{false, true} {
		expander := repairYAMLExpander{
			chooseLast: chooseLast,
			active:     make(map[*yaml.Node]bool),
		}
		expanded, err := expander.expand(&root, 1)
		if err != nil {
			return nil, err
		}
		views = append(views, expanded)
	}
	return views, nil
}

type repairYAMLPair struct {
	key   *yaml.Node
	value *yaml.Node
}

type repairYAMLExpander struct {
	chooseLast bool
	nodes      int
	active     map[*yaml.Node]bool
}

func (e *repairYAMLExpander) expand(node *yaml.Node, depth int) (*yaml.Node, error) {
	if node == nil {
		return nil, coded(CodeInvalidDocument, "nil node in lock recovery view")
	}
	e.nodes++
	if depth > maxYAMLDepth || e.nodes > maxYAMLNodes {
		return nil, coded(CodeInvalidDocument, "expanded lock YAML exceeds complexity limits")
	}
	if e.active[node] {
		return nil, coded(CodeYAMLAlias, "cyclic YAML alias in lock recovery view")
	}
	e.active[node] = true
	defer delete(e.active, node)
	if node.Kind == yaml.AliasNode {
		if node.Alias == nil {
			return nil, coded(CodeYAMLAlias, "unresolved YAML alias in lock recovery view")
		}
		return e.expand(node.Alias, depth+1)
	}
	copy := *node
	copy.Anchor = ""
	copy.Alias = nil
	copy.Content = nil
	switch node.Kind {
	case yaml.MappingNode:
		return e.expandMapping(node, &copy, depth)
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, child := range node.Content {
			expanded, err := e.expand(child, depth+1)
			if err != nil {
				return nil, err
			}
			copy.Content = append(copy.Content, expanded)
		}
	}
	return &copy, nil
}

func (e *repairYAMLExpander) expandMapping(node, copy *yaml.Node, depth int) (*yaml.Node, error) {
	var inherited []repairYAMLPair
	inheritedKeys := make(map[string]bool)
	var explicit []repairYAMLPair
	explicitIndexes := make(map[string]int, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		expandedKey, err := e.expand(key, depth+1)
		if err != nil {
			return nil, err
		}
		if isRepairMergeKey(key) {
			expandedValue, err := e.expand(value, depth+1)
			if err != nil {
				return nil, err
			}
			maps, err := repairMergeMappings(expandedValue)
			if err != nil {
				return nil, err
			}
			for _, mapping := range maps {
				for j := 0; j+1 < len(mapping.Content); j += 2 {
					mergeKey, mergeValue := mapping.Content[j], mapping.Content[j+1]
					signature := repairYAMLKey(mergeKey)
					if !inheritedKeys[signature] {
						inheritedKeys[signature] = true
						inherited = append(inherited, repairYAMLPair{key: mergeKey, value: mergeValue})
					}
				}
			}
			continue
		}
		expandedValue, err := e.expand(value, depth+1)
		if err != nil {
			return nil, err
		}
		signature := repairYAMLKey(expandedKey)
		if index, duplicate := explicitIndexes[signature]; duplicate {
			if e.chooseLast {
				explicit[index].value = expandedValue
			}
			continue
		}
		explicitIndexes[signature] = len(explicit)
		explicit = append(explicit, repairYAMLPair{key: expandedKey, value: expandedValue})
	}
	for _, pair := range explicit {
		copy.Content = append(copy.Content, pair.key, pair.value)
	}
	for _, pair := range inherited {
		if _, exists := explicitIndexes[repairYAMLKey(pair.key)]; !exists {
			copy.Content = append(copy.Content, pair.key, pair.value)
		}
	}
	return copy, nil
}

func repairMergeMappings(node *yaml.Node) ([]*yaml.Node, error) {
	switch node.Kind {
	case yaml.MappingNode:
		return []*yaml.Node{node}, nil
	case yaml.SequenceNode:
		mappings := make([]*yaml.Node, 0, len(node.Content))
		for _, child := range node.Content {
			if child.Kind != yaml.MappingNode {
				return nil, coded(CodeYAMLMerge, "YAML merge sequence contains a non-mapping")
			}
			mappings = append(mappings, child)
		}
		return mappings, nil
	default:
		return nil, coded(CodeYAMLMerge, "YAML merge value is not a mapping")
	}
}

func isRepairMergeKey(key *yaml.Node) bool {
	return key.Value == "<<" || key.Tag == "!!merge"
}

func repairYAMLKey(key *yaml.Node) string {
	return fmt.Sprintf("%d\x00%s\x00%s", key.Kind, key.Tag, key.Value)
}

func encodeRepairYAML(root *yaml.Node) ([]byte, error) {
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return nil, coded(CodeMalformedYAML, "encode recovery view: %v", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, coded(CodeMalformedYAML, "finish recovery view: %v", err)
	}
	if int64(output.Len()) > MaxFileSize {
		return nil, fmt.Errorf("%w: expanded lock input exceeds %d bytes", ErrFileTooLarge, MaxFileSize)
	}
	return output.Bytes(), nil
}
