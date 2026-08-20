package nameutil

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidateSafeFilename validates a platform-independent filename component.
// It keeps ordinary Unicode names and punctuation usable while rejecting the
// characters and names that are unsafe on either Unix or Windows.
func ValidateSafeFilename(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("name must not have leading or trailing whitespace")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name must not be a dot path component")
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("name must not end with a dot or space")
	}
	if strings.ContainsAny(name, `/\\<>:"|?*`) {
		return fmt.Errorf("name contains a character unsafe in filenames")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("name contains a control character")
		}
	}
	if isReservedDOSName(name) {
		return fmt.Errorf("name is reserved on Windows")
	}
	return nil
}

func isReservedDOSName(name string) bool {
	base := name
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	base = strings.ToUpper(base)
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CLOCK$" {
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return base[3] >= '1' && base[3] <= '9'
	}
	return false
}
