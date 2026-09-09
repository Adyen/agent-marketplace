package lockfile

import (
	"path/filepath"
)

func LockPathForConfig(configPath string) (string, error) { return PathForConfig(configPath) }

func PathForConfig(configPath string) (string, error) {
	if !filepath.IsAbs(configPath) {
		return "", coded(CodeInvalidPath, "config path must be absolute")
	}
	clean := filepath.Clean(configPath)
	switch filepath.Base(clean) {
	case "config.yaml":
		return filepath.Join(filepath.Dir(clean), "lock.yaml"), nil
	case "config.local.yaml":
		return filepath.Join(filepath.Dir(clean), "lock.local.yaml"), nil
	default:
		return "", coded(CodeInvalidPath, "unsupported config filename %q", filepath.Base(clean))
	}
}

func ConfigPathForLock(lockPath string) (string, error) {
	if !filepath.IsAbs(lockPath) {
		return "", coded(CodeInvalidPath, "lock path must be absolute")
	}
	clean := filepath.Clean(lockPath)
	switch filepath.Base(clean) {
	case "lock.yaml":
		return filepath.Join(filepath.Dir(clean), "config.yaml"), nil
	case "lock.local.yaml":
		return filepath.Join(filepath.Dir(clean), "config.local.yaml"), nil
	default:
		return "", coded(CodeInvalidPath, "unsupported lock filename %q", filepath.Base(clean))
	}
}
