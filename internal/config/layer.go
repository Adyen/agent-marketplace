package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

type layerKind int

const (
	organizationLayer layerKind = iota
	userLayer
	repoLayer
	localLayer
)

func (k layerKind) name() string {
	switch k {
	case organizationLayer:
		return "organization"
	case userLayer:
		return "user personal"
	case repoLayer:
		return "repository committed"
	case localLayer:
		return "repository personal"
	default:
		return "unknown"
	}
}

func layerDirectoryBase(configPath string, kind layerKind) string {
	if kind == userLayer {
		return filepath.Dir(configPath)
	}
	return filepath.Dir(filepath.Dir(configPath))
}

func registrySourceName(sources map[string]struct{}, registryURL string) string {
	name := NormalizeGitURL(registryURL)
	if separator := strings.LastIndexAny(name, "/:"); separator >= 0 {
		name = name[separator+1:]
	}
	if !v4SourceNameRE.MatchString(name) {
		name = "registry"
	}
	return uniqueSourceName(sources, name)
}

func uniqueSourceName(sources map[string]struct{}, base string) string {
	for index := 1; ; index++ {
		name := base
		if index > 1 {
			name = fmt.Sprintf("%s-%d", base, index)
		}
		if _, taken := sources[name]; !taken {
			return name
		}
	}
}

func marketplaceKindName(kind string) string {
	if kind == api.KindMcpServer {
		return "mcp"
	}
	return kind
}

func v4ResourceKindName(kind ir.ResourceKind) string {
	switch kind {
	case ir.KindSkill:
		return "skill"
	case ir.KindCommand:
		return "command"
	case ir.KindSubagent:
		return "subagent"
	case ir.KindHook:
		return "hook"
	case ir.KindMcpServer:
		return "mcp"
	case ir.KindCollection:
		return "collection"
	case ir.KindPlugin:
		return "plugin"
	default:
		return "unknown"
	}
}
