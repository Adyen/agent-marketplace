package target

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// NativeHookProjectionFile locates the native hook file a hook-only projection writes within a single scope.
type NativeHookProjectionFile struct {
	Path     string
	Member   string
	Envelope map[string]any
}

// NativeHookProjection declares the hook-only native topology a target compiler uses.
type NativeHookProjection struct {
	Project    NativeHookProjectionFile
	User       NativeHookProjectionFile
	Shape      HookShape
	SupportDir string
}

// File returns the native hook file declared for the selected scope.
func (p NativeHookProjection) File(machineScope bool) NativeHookProjectionFile {
	file := p.Project
	if machineScope {
		file = p.User
	}
	file.Envelope = maps.Clone(file.Envelope)
	return file
}

// LookupHookProjection returns the target's derived hook projection.
func LookupHookProjection(t Target) (NativeHookProjection, bool) {
	definition, ok := Lookup(string(t))
	if !ok || definition.HookProjection == nil {
		return NativeHookProjection{}, false
	}
	return *definition.HookProjection, true
}

func nativeHookProjectionForLayout(t Target) *NativeHookProjection {
	partition, aggregate, ok := nativeAggregate(t, ir.KindHook)
	if !ok {
		return nil
	}
	project, projectOK := NativeAggregateFileForScope(t, ir.KindHook, false)
	user, userOK := NativeAggregateFileForScope(t, ir.KindHook, true)
	if !projectOK || !userOK {
		return nil
	}
	return &NativeHookProjection{
		Project:    NativeHookProjectionFile{Path: project.Path, Member: project.Member, Envelope: maps.Clone(project.Envelope)},
		User:       NativeHookProjectionFile{Path: user.Path, Member: user.Member, Envelope: maps.Clone(user.Envelope)},
		Shape:      aggregate.Shape,
		SupportDir: path.Join(partition.Prefix, aggregate.SupportDir),
	}
}

// ValidateHookProjections validates every declared hook projection.
func ValidateHookProjections() error {
	var errs []error
	for _, definition := range derivedDefinitions() {
		if definition.HookProjection == nil {
			continue
		}
		if err := ValidateHookProjection(definition.Target, *definition.HookProjection); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ValidateHookProjection rejects a target hook projection with invalid paths, shapes, or envelopes.
func ValidateHookProjection(t Target, p NativeHookProjection) error {
	var errs []error
	switch p.Shape {
	case HookShapeMatcherGroups, HookShapeEventHandlers:
	default:
		errs = append(errs, fmt.Errorf("target %q hook projection declares unknown shape %q", t, p.Shape))
	}
	if !fs.ValidPath(p.SupportDir) || p.SupportDir == "." || strings.ContainsAny(p.SupportDir, "\\\x00") {
		errs = append(errs, fmt.Errorf("target %q hook projection has invalid support directory %q", t, p.SupportDir))
	}
	for _, scope := range []struct {
		name string
		file NativeHookProjectionFile
	}{{"project", p.Project}, {"user", p.User}} {
		if !fs.ValidPath(scope.file.Path) || scope.file.Path == "." || strings.ContainsAny(scope.file.Path, "\\\x00") {
			errs = append(errs, fmt.Errorf("target %q hook projection %s-scope file has invalid path %q", t, scope.name, scope.file.Path))
		}
		if member := scope.file.Member; member != "" && (strings.ContainsAny(member, "/\\\x00") || member == "." || member == "..") {
			errs = append(errs, fmt.Errorf("target %q hook projection %s-scope file has invalid member %q", t, scope.name, member))
		}
		for _, key := range slices.Sorted(maps.Keys(scope.file.Envelope)) {
			if key == "" || strings.ContainsAny(key, "/\\\x00") || key == "." || key == ".." {
				errs = append(errs, fmt.Errorf("target %q hook projection %s-scope file declares an invalid envelope member name %q", t, scope.name, key))
				continue
			}
			if scope.file.Member != "" && strings.EqualFold(key, scope.file.Member) {
				errs = append(errs, fmt.Errorf("target %q hook projection %s-scope file declares envelope member %q colliding with the entry member %q", t, scope.name, key, scope.file.Member))
				continue
			}
			if !validEnvelopeScalar(scope.file.Envelope[key]) {
				errs = append(errs, fmt.Errorf("target %q hook projection %s-scope file declares envelope member %q with a non-scalar or unserializable value", t, scope.name, key))
			}
		}
	}
	return errors.Join(errs...)
}

func validEnvelopeScalar(value any) bool {
	if _, err := json.Marshal(value); err != nil {
		return false
	}
	switch value.(type) {
	case nil, string, bool,
		float32, float64, json.Number,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	}
	return false
}
