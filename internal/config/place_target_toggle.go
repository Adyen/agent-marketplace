package config

import (
	"bytes"
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

// RenderPlaceTargetToggle edits one target in a root config list, preserving
// the authored key spelling and starting from baseline when no list is present.
func RenderPlaceTargetToggle(path string, data []byte, baseline []string, targetID string, enabled bool) ([]byte, bool, error) {
	if !nameutil.IsTargetIdentifier(targetID) || target.IsValid(targetID) {
		return nil, false, fmt.Errorf("target.approval.target-invalid: %q is not a place target identifier", targetID)
	}
	doc, err := parseConfigDocument(path, data)
	if err != nil {
		return nil, false, err
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return nil, false, err
	}
	targetsIndex, targetsSequence := findTopLevelSequence(root, "targets")
	harnessesIndex, harnessesSequence := findTopLevelSequence(root, "harnesses")
	if targetsIndex >= 0 && harnessesIndex >= 0 {
		return nil, false, fmt.Errorf("v4.targets-alias: targets and harnesses cannot both be declared")
	}
	key, sequence := "harnesses", (*yaml.Node)(nil)
	if targetsSequence != nil {
		key, sequence = "targets", targetsSequence
	} else if harnessesSequence != nil {
		sequence = harnessesSequence
	}
	values := slices.Clone(baseline)
	if sequence != nil {
		if sequence.Kind != yaml.SequenceNode {
			return nil, false, fmt.Errorf("target.config.invalid: %s must be a sequence", key)
		}
		if key == "targets" || len(sequence.Content) > 0 {
			values = make([]string, 0, len(sequence.Content))
			for _, item := range sequence.Content {
				if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
					return nil, false, fmt.Errorf("target.config.invalid: %s must contain strings", key)
				}
				values = append(values, item.Value)
			}
		}
	}
	currentlyEnabled := slices.Contains(values, targetID)
	if currentlyEnabled == enabled {
		return append([]byte(nil), data...), false, nil
	}
	if enabled {
		values = append(values, targetID)
	} else {
		values = slices.DeleteFunc(values, func(value string) bool { return value == targetID })
	}
	if !enabled && len(values) == 0 && key != "targets" {
		return nil, false, fmt.Errorf("target.config.last-target: upgrade every reader of this file and rename the key to targets: before removing the last target")
	}
	if sequence == nil {
		sequence, err = ensureTopLevelSequence(root, key)
		if err != nil {
			return nil, false, err
		}
		for _, value := range values {
			sequence.Content = append(sequence.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
		}
	} else if key == "harnesses" && len(sequence.Content) == 0 {
		for _, value := range values {
			sequence.Content = append(sequence.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
		}
	} else if enabled {
		sequence.Content = append(sequence.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: targetID})
	} else {
		sequence.Content = slices.DeleteFunc(sequence.Content, func(node *yaml.Node) bool {
			return node.Value == targetID
		})
	}
	rendered, err := encodeConfigDocument(path, doc)
	if err != nil {
		return nil, false, err
	}
	return rendered, !bytes.Equal(rendered, data), nil
}
