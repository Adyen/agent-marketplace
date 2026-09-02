package lockfile

import "strings"

type GitOID struct {
	algorithm string
	hex       string
}

func ParseGitOID(value string) (GitOID, error) {
	algorithm, hex, ok := strings.Cut(value, ":")
	if !ok {
		return GitOID{}, coded(CodeInvalidGitOID, "Git OID must be algorithm-qualified")
	}
	var length int
	switch algorithm {
	case "sha1":
		length = 40
	case "sha256":
		length = 64
	default:
		return GitOID{}, coded(CodeUnsupportedGitOID, "unsupported Git object algorithm %q", algorithm)
	}
	if len(hex) != length || !lowerHex(hex) {
		return GitOID{}, coded(CodeInvalidGitOID, "invalid full %s Git OID", algorithm)
	}
	return GitOID{algorithm: algorithm, hex: hex}, nil
}

func validHexIdentifier(value, algorithm string, length int) bool {
	prefix, hex, ok := strings.Cut(value, ":")
	return ok && prefix == algorithm && len(hex) == length && lowerHex(hex)
}

func lowerHex(value string) bool {
	for _, c := range []byte(value) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (o GitOID) Algorithm() string { return o.algorithm }
func (o GitOID) Hex() string       { return o.hex }
func (o GitOID) String() string {
	if o.algorithm == "" {
		return ""
	}
	return o.algorithm + ":" + o.hex
}
func (o GitOID) IsZero() bool { return o.algorithm == "" && o.hex == "" }
func (o GitOID) MarshalText() ([]byte, error) {
	if o.IsZero() {
		return nil, nil
	}
	if _, err := ParseGitOID(o.String()); err != nil {
		return nil, err
	}
	return []byte(o.String()), nil
}
func (o *GitOID) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*o = GitOID{}
		return nil
	}
	parsed, err := ParseGitOID(string(text))
	if err != nil {
		return err
	}
	*o = parsed
	return nil
}
