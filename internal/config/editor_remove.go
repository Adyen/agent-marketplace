package config

// ValidateScopeBoundary returns an error if targetScope is repo-local
// (RepoCommitted or RepoPersonal) and skillSource is machine-scope
// (Organization or UserPersonal). Returns nil if the operation is allowed.
//
// This ensures repo/local layers cannot exclude org/user skills — those
// skills are installed at the user-wide scope and the exclude would be
// silently ineffective.
func ValidateScopeBoundary(skillSource Source, targetScope Source) error {
	if !targetScope.IsMachineScope() && skillSource.IsMachineScope() {
		return &ScopeBoundaryError{
			Source:     skillSource,
			Target:     targetScope,
			Suggestion: "Use --scope user to exclude at the user level.",
		}
	}
	return nil
}
