package session

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func LocalSkillFiles(row Row) bool {
	skill, isSkill := row.Artifact.Resource.(*ir.Skill)
	return !row.Artifact.IsMarketplace &&
		row.Artifact.Kind == ir.KindSkill &&
		isSkill && skill != nil &&
		row.Artifact.SourceRoot != "" &&
		row.MarketplaceDiscovery == nil &&
		!row.IsMalformed
}

func OpenLocalSkillBundle(row Row) (*InstalledSkillBundle, error) {
	if !LocalSkillFiles(row) {
		return nil, errors.New("resource is not a local skill with a captured source root")
	}
	skill, _ := row.Artifact.Resource.(*ir.Skill)
	root, err := filepath.Abs(row.Artifact.SourceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve local skill source root: %w", err)
	}
	sourcePath := catalog.SourcePath(row.Artifact.Entry)
	if sourcePath == "" {
		return nil, errors.New("local skill has no source path")
	}
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(root, sourcePath)
	}
	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("resolve local skill primary: %w", err)
	}
	relativePrimary, err := filepath.Rel(root, sourcePath)
	if err != nil {
		return nil, fmt.Errorf("locate local skill primary: %w", err)
	}
	if relativePrimary == "." || relativePrimary == ".." || filepath.IsAbs(relativePrimary) || strings.HasPrefix(relativePrimary, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: local skill directory is outside its source root", fsboundary.ErrUnsafePath)
	}
	relativePrimary = filepath.ToSlash(relativePrimary)
	if !fs.ValidPath(relativePrimary) || path.Base(relativePrimary) != installedSkillPrimaryPath {
		return nil, errors.New("local skill source path is not SKILL.md")
	}

	source, err := fsboundary.OpenSourceFS(root)
	if err != nil {
		return nil, err
	}
	bundle := newInstalledSkillBundle()
	bundle.source = source
	fail := func(err error) (*InstalledSkillBundle, error) {
		_ = source.Close()
		return nil, err
	}
	support, err := installedSkillSupportPaths(skill)
	if err != nil {
		return fail(err)
	}
	for _, supportPath := range support {
		if supportPath == installedSkillPrimaryPath {
			return fail(errors.New("local skill support duplicates the canonical skill primary"))
		}
	}

	skillDirectory := path.Dir(relativePrimary)
	members := append([]string{installedSkillPrimaryPath}, support...)
	for _, memberPath := range members {
		sourceMemberPath := path.Join(skillDirectory, memberPath)
		if !fs.ValidPath(sourceMemberPath) {
			return fail(fmt.Errorf("%w: invalid local skill member path", fsboundary.ErrUnsafePath))
		}
		info, err := source.Stat(sourceMemberPath)
		if err != nil {
			return fail(fmt.Errorf("stat local skill member: %w", err))
		}
		identity, err := fsboundary.FileInfoIdentity(info)
		if err != nil {
			return fail(fmt.Errorf("identify local skill member: %w", err))
		}
		if !info.Mode().IsRegular() || identity.Links != 1 {
			return fail(fmt.Errorf("%w: local skill member is not a singly linked regular file", fsboundary.ErrUnsafeFile))
		}
		file, err := installedStageFile(memberPath, identity)
		if err != nil {
			return fail(err)
		}
		bundle.members[memberPath] = installedSkillMember{
			file:       file,
			staged:     true,
			sourcePath: sourceMemberPath,
			state:      identity,
		}
		bundle.files = append(bundle.files, file)
	}
	bundle.sortFiles()
	return bundle, nil
}
