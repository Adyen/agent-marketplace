package config

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"gopkg.in/yaml.v3"
)

// PlacePreapproval is an exact organization-authorized target binding.
type PlacePreapproval struct {
	OS                 string   `yaml:"os"`
	TargetID           string   `yaml:"target_id"`
	ProfileDigest      string   `yaml:"profile_digest"`
	CanonicalRoot      string   `yaml:"canonical_root"`
	CanonicalStateRoot string   `yaml:"canonical_state_root"`
	CommandRoles       []string `yaml:"command_roles"`
	executable         targetprofile.ExecutableBinding
}

type rawPlacePreapproval struct {
	PlacePreapproval `yaml:",inline"`
	Executable       yaml.Node `yaml:"executable"`
}

func decodeV4PlacePreapprovals(node *yaml.Node) ([]PlacePreapproval, error) {
	if node == nil || node.Kind != yaml.SequenceNode || node.Tag != "!!seq" {
		return nil, fmt.Errorf("target_preapprovals must be a sequence")
	}
	entries := make([]PlacePreapproval, 0, len(node.Content))
	for i, item := range node.Content {
		var raw rawPlacePreapproval
		if err := decodeV4PlacePreapprovalNode(item, &raw); err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		entry := raw.PlacePreapproval
		if entry.OS != "linux" && entry.OS != "macos" {
			return nil, fmt.Errorf("entry %d: os must be linux or macos", i)
		}
		if !nameutil.IsTargetIdentifier(entry.TargetID) || target.IsValid(entry.TargetID) {
			return nil, fmt.Errorf("entry %d: target_id must name a non-compiled target profile", i)
		}
		if !nameutil.IsCanonicalSHA256Digest(entry.ProfileDigest) {
			return nil, fmt.Errorf("entry %d: profile_digest must be a canonical SHA-256 digest", i)
		}
		if !validPlacePreapprovalPath(raw.CanonicalRoot) {
			return nil, fmt.Errorf("entry %d: canonical_root must be a canonical absolute path", i)
		}
		if !validPlacePreapprovalPath(raw.CanonicalStateRoot) {
			return nil, fmt.Errorf("entry %d: canonical_state_root must be a canonical absolute path", i)
		}

		roles := slices.Clone(entry.CommandRoles)
		seenRoles := make(map[string]struct{}, len(roles))
		for _, role := range roles {
			if strings.TrimSpace(role) == "" || role != strings.TrimSpace(role) {
				return nil, fmt.Errorf("entry %d: command_roles must not contain empty role names", i)
			}
			if _, exists := seenRoles[role]; exists {
				return nil, fmt.Errorf("entry %d: command_roles contains duplicate role %q", i, role)
			}
			seenRoles[role] = struct{}{}
		}
		sort.Strings(roles)
		entry.CommandRoles = roles

		switch raw.Executable.Kind {
		case yaml.ScalarNode:
			if raw.Executable.Tag != "!!str" || raw.Executable.Value != "no-execution" || len(roles) != 0 {
				return nil, fmt.Errorf("entry %d: executable no-execution requires no command roles", i)
			}
		case yaml.MappingNode:
			if len(roles) == 0 {
				return nil, fmt.Errorf("entry %d: executable binding requires at least one command role", i)
			}
			var executable targetprofile.ExecutableBinding
			if err := decodeV4PlacePreapprovalNode(&raw.Executable, &executable); err != nil {
				return nil, fmt.Errorf("entry %d: executable: %w", i, err)
			}
			if !validPlacePreapprovalPath(executable.CanonicalPath) {
				return nil, fmt.Errorf("entry %d: executable canonical_path must be a canonical absolute path", i)
			}
			if !nameutil.IsCanonicalSHA256Digest(executable.SHA256) {
				return nil, fmt.Errorf("entry %d: executable sha256 must be a canonical SHA-256 digest", i)
			}
			entry.executable = executable
		default:
			return nil, fmt.Errorf("entry %d: executable must be a binding or no-execution", i)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func decodeV4PlacePreapprovalNode(node *yaml.Node, dst any) error {
	data, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(dst)
}

func validPlacePreapprovalPath(path string) bool {
	return filepath.IsAbs(path) && path == filepath.Clean(path) &&
		path != string(filepath.Separator) && !strings.ContainsAny(path, "*?[]$\\\x00")
}

// PlacePreapprovalMatch distinguishes exact authority from a diagnostic-only near-match.
type PlacePreapprovalMatch uint8

const (
	PlacePreapprovalUnrelated PlacePreapprovalMatch = iota
	PlacePreapprovalStateRootMismatch
	PlacePreapprovalExact
)

func (p PlacePreapproval) Match(binding targetprofile.ApprovalBinding) PlacePreapprovalMatch {
	roles := slices.Clone(binding.CommandRoles)
	sort.Strings(roles)
	if p.OS != binding.OS || p.TargetID != binding.Profile.TargetID ||
		p.ProfileDigest != binding.Profile.RevisionDigest || p.CanonicalRoot != binding.CanonicalRoot ||
		p.executable != binding.Executable || !slices.Equal(p.CommandRoles, roles) {
		return PlacePreapprovalUnrelated
	}
	if p.CanonicalStateRoot != binding.CanonicalStateRoot {
		return PlacePreapprovalStateRootMismatch
	}
	return PlacePreapprovalExact
}

func clonePlacePreapprovals(entries []PlacePreapproval) []PlacePreapproval {
	if entries == nil {
		return nil
	}
	out := make([]PlacePreapproval, len(entries))
	for i, entry := range entries {
		entry.CommandRoles = slices.Clone(entry.CommandRoles)
		out[i] = entry
	}
	return out
}
