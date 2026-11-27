package rootkey

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

const keyDomain = "agent-marketplace/ownership/root/v1"

// ForCanonicalRoot returns the domain-separated key for a canonical root path.
func ForCanonicalRoot(canonicalRoot string) string {
	h := sha256.New()
	h.Write([]byte(keyDomain))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(len(canonicalRoot))))
	h.Write([]byte{0})
	h.Write([]byte(canonicalRoot))
	return hex.EncodeToString(h.Sum(nil))
}
