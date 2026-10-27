package remedy

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

type Remediable interface {
	Remediation() string
}

type codedError interface {
	DiagnosticCode() string
}

type fileContext interface {
	DiagnosticFile() string
	DiagnosticLine() int
}

type scopeContext interface {
	DiagnosticScope() string
}

type pathContext interface {
	DiagnosticPath() string
}

type locatorContext interface {
	DiagnosticLocator() string
}

type variantContext interface {
	DiagnosticVariant() string
}

type lockFileContext interface {
	DiagnosticLockFile() string
}

type hintContext struct {
	File     string
	Line     int
	Scope    string
	Path     string
	Locator  string
	Variant  string
	LockFile string
}

type hintFunc func(hintContext) string

func For(err error, command string) string {
	if err == nil {
		return ""
	}
	if hint := firstRemediation(err); hint != "" {
		return hint
	}
	if code := firstCode(err); code != "" {
		if hint := catalog[code]; hint != nil {
			return hint(contextFrom(err))
		}
	}
	return fallback(command)
}

func firstRemediation(err error) string {
	if remediable, ok := err.(Remediable); ok {
		if hint := strings.TrimSpace(remediable.Remediation()); hint != "" {
			return hint
		}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if hint := firstRemediation(child); hint != "" {
				return hint
			}
		}
	} else if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return firstRemediation(wrapped.Unwrap())
	}
	return ""
}

func firstCode(err error) string {
	if coded, ok := err.(codedError); ok && coded.DiagnosticCode() != "" {
		return coded.DiagnosticCode()
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if code := firstCode(child); code != "" {
				return code
			}
		}
	} else if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return firstCode(wrapped.Unwrap())
	}
	return ""
}

func contextFrom(err error) hintContext {
	ctx := hintContext{}
	var file fileContext
	if errors.As(err, &file) {
		ctx.File, ctx.Line = file.DiagnosticFile(), file.DiagnosticLine()
	}
	var scope scopeContext
	if errors.As(err, &scope) {
		ctx.Scope = scope.DiagnosticScope()
	}
	var path pathContext
	if errors.As(err, &path) {
		ctx.Path = path.DiagnosticPath()
	}
	var locator locatorContext
	if errors.As(err, &locator) {
		ctx.Locator = locator.DiagnosticLocator()
	}
	var variant variantContext
	if errors.As(err, &variant) {
		ctx.Variant = variant.DiagnosticVariant()
	}
	var lockFile lockFileContext
	if errors.As(err, &lockFile) {
		ctx.LockFile = lockFile.DiagnosticLockFile()
	}
	return ctx
}

func fallback(command string) string {
	if command == "" {
		return ""
	}
	parts := strings.Fields(command)
	if len(parts) > 0 {
		switch parts[len(parts)-1] {
		case "sync", "lock":
			return fmt.Sprintf("Run `agent-marketplace %s --check` to inspect the current configuration and acquisition state, then fix the reported cause.", parts[len(parts)-1])
		case "doctor":
			return "Rerun `agent-marketplace doctor --debug` to inspect configuration and environment diagnostics."
		case "list", "status":
			return "Run `agent-marketplace doctor` to inspect configuration and environment diagnostics."
		case "install", "remove", "update":
			return fmt.Sprintf("Run `agent-marketplace doctor` to inspect configuration, then see `agent-marketplace %s --help` for command options.", parts[len(parts)-1])
		}
	}
	return "Run `agent-marketplace doctor` to inspect configuration and environment diagnostics, then rerun the failed command with `--debug`."
}

func staticHint(text string) hintFunc {
	return func(hintContext) string { return text }
}

func configurationHint(action string) hintFunc {
	return func(ctx hintContext) string {
		if ctx.Scope == "organization" {
			return organizationConfigHint(ctx)
		}
		configurationAction := lowerFirst(action)
		prefix := ""
		if location := fileLine(ctx); location != "" {
			prefix = "In " + location + ", "
		}
		return prefix + configurationAction + ", then run `agent-marketplace doctor` to check the fix."
	}
}

func lowerFirst(text string) string {
	if text == "" {
		return text
	}
	first, size := utf8.DecodeRuneInString(text)
	return strings.ToLower(string(first)) + text[size:]
}

func organizationConfigHint(ctx hintContext) string {
	if ctx.File != "" {
		return fmt.Sprintf("Ask your administrator to correct the organization configuration in %s.", ctx.File)
	}
	return "Ask your administrator to correct the organization configuration."
}

func versionRefusalHint(ctx hintContext) string {
	const guide = "https://github.com/Adyen/agent-marketplace/blob/main/site/docs/how-to/migrate-configuration.md"
	if ctx.Scope == "organization" {
		return "Ask your administrator to convert and deploy the organization configuration before upgrading. See " + guide + "."
	}
	return "Use the older released agent-marketplace v4.1.4 migrator for the owning configuration layer. Conversion can refuse include trees or unrepresentable input. See " + guide + "."
}

func fileLine(ctx hintContext) string {
	if ctx.File == "" {
		return ""
	}
	if ctx.Line > 0 {
		return fmt.Sprintf("%s:%d", ctx.File, ctx.Line)
	}
	return ctx.File
}

func lockHint(action string) hintFunc {
	return func(ctx hintContext) string {
		if ctx.Scope == "organization" {
			return organizationLockGuidance
		}
		if ctx.Path != "" {
			return fmt.Sprintf("%s (lock file: %s).", strings.TrimSuffix(action, "."), ctx.Path)
		}
		return action
	}
}

func lockScopeHint(action string) hintFunc {
	return func(ctx hintContext) string {
		if ctx.Scope == "organization" {
			return lockHint(action)(ctx)
		}
		if ctx.Scope == "" {
			unscopedAction := strings.ReplaceAll(action, " --scope <scope>", "")
			if ctx.Path != "" {
				return fmt.Sprintf("%s: %s", unscopedAction, ctx.Path)
			}
			return strings.TrimSuffix(unscopedAction, ".") + "."
		}
		scopedAction := strings.ReplaceAll(action, "<scope>", ctx.Scope)
		if ctx.Path != "" {
			return fmt.Sprintf("%s: %s", scopedAction, ctx.Path)
		}
		return scopedAction + "."
	}
}

func marketplaceLockMismatchHint(ctx hintContext) string {
	if ctx.Scope == "organization" {
		if ctx.Locator != "" {
			return fmt.Sprintf("Ask your administrator to record %q in the organization acquisition lock.", ctx.Locator)
		}
		return "Ask your administrator to reconcile the organization acquisition lock."
	}
	switch ctx.Variant {
	case "configuration", "duplicate-request":
		return marketplaceConfigurationMismatchHint(ctx)
	case "stale-endpoint":
		return marketplaceStaleEndpointHint(ctx)
	case "lock-restore", "lock-read":
		return marketplaceRestoreLockHint(ctx)
	}
	return marketplaceBootstrapHint(ctx.Scope, ctx.Locator, ctx.LockFile)
}

func marketplaceStaleEndpointHint(ctx hintContext) string {
	if ctx.Scope == "organization" {
		return organizationLockGuidance
	}
	lockPath := ctx.LockFile
	if lockPath == "" {
		lockPath = "the acquisition lock"
	}
	return fmt.Sprintf("In %s, remove the stale Marketplace endpoint or add its matching Marketplace source, then run `agent-marketplace doctor` to check the fix.", lockPath)
}

func marketplaceBootstrapHint(scope, locator, lockFile string) string {
	if scope == "organization" {
		return "Ask your administrator to record selected Marketplace resources in the organization acquisition lock."
	}
	lockPath := lockFile
	if lockPath == "" {
		switch scope {
		case "repo":
			lockPath = ".agents/lock.yaml"
		case "user":
			lockPath = "the user acquisition lock file"
		case "local":
			lockPath = ".agents/lock.local.yaml"
		default:
			lockPath = "the acquisition lock file"
		}
	}
	resource := "selected Marketplace resources"
	if locator != "" {
		resource = fmt.Sprintf("%q", locator)
	}
	hint := "Run `agent-marketplace sync` while online (not with `--check`, `--locked`, or `--offline`) to add " + resource + " to " + lockPath
	if scope == "repo" {
		hint += ", then commit " + lockPath
	}
	return hint + "."
}

func marketplaceConfigurationMismatchHint(ctx hintContext) string {
	location := fileLine(ctx)
	if location == "" {
		location = "the named configuration file"
	}
	return fmt.Sprintf("In %s, change the Marketplace requests so each selects a distinct locked resource, then run `agent-marketplace doctor` to check the fix.", location)
}

func marketplaceRestoreLockHint(ctx hintContext) string {
	if ctx.Scope == "organization" {
		if ctx.LockFile != "" {
			return "Ask your administrator to restore the organization lock file " + ctx.LockFile + "."
		}
		return "Ask your administrator to restore the organization acquisition lock."
	}
	if ctx.LockFile != "" {
		return "Restore the acquisition lock file " + ctx.LockFile + " from a known-good copy or correct its permissions, then run `agent-marketplace sync --check` to verify it."
	}
	return restoreLockGuidance
}

func lockStaleHint(ctx hintContext) string {
	if ctx.Scope == "organization" {
		return organizationLockGuidance
	}
	if ctx.Scope == "" {
		if ctx.Path != "" {
			return fmt.Sprintf("Run `agent-marketplace lock` in this workspace to reconcile the acquisition lock file %s without upgrading a current pin. Then run `agent-marketplace sync --check`.", ctx.Path)
		}
		return "Run `agent-marketplace lock` in this workspace to reconcile the acquisition lock without upgrading a current pin, then run `agent-marketplace sync --check`."
	}
	return fmt.Sprintf("Run `agent-marketplace lock --scope %s` in this workspace to create or reconcile this acquisition lock without upgrading a current pin. Then run `agent-marketplace sync --check`.", ctx.Scope)
}

func lockSnapshotHint(ctx hintContext) string {
	if ctx.Scope == "organization" {
		return organizationLockGuidance
	}
	return "Check your network and Git access, then run `agent-marketplace sync --locked` in this workspace to fetch the pinned source without changing acquisition locks. If verification still fails, keep the lock and ask the registry owner to investigate."
}

func lockManifestHint(ctx hintContext) string {
	if ctx.Scope == "organization" {
		return organizationLockGuidance
	}
	return "Run `agent-marketplace sync --locked` in this workspace to recheck the pinned source without changing acquisition locks. If verification still fails, ask the registry owner to investigate. Do not delete the lock or run `agent-marketplace update` to bypass verification."
}

const (
	organizationLockGuidance = "Ask your administrator to fix the organization config or acquisition lock named above. Do not edit or delete organization files to bypass the check. After they fix it, run `agent-marketplace sync --check` in this workspace."
	restoreLockGuidance      = "Restore the acquisition lock named above from a known-good copy, or fix its read permissions. Do not delete the lock or run `update` to bypass verification. Run `agent-marketplace sync --check` in this workspace."
)

var catalog = map[string]hintFunc{
	"config.edit.ambiguous-source":      configurationHint("Use `agent-marketplace install --from <source>` or specify a unique source in the named configuration"),
	"config.edit.include-owned":         configurationHint("Change the selection in the included configuration file that owns this entry"),
	"config.edit.invalid-binding":       configurationHint("Rerun the configuration edit with the source selected from the current configuration"),
	"config.edit.request-missing":       configurationHint("Run `agent-marketplace list` and select a resource that is present in the named source"),
	"config.edit.source-in-use":         configurationHint("Remove the resources from this Marketplace source before removing its registration"),
	"config.edit.unsafe-directory":      configurationHint("Choose a local source directory contained by the configuration layer"),
	"config.edit.unsupported-operation": configurationHint("Use a supported resource command such as `agent-marketplace install --help` or `agent-marketplace remove --help`"),

	"dependency.recovery-required": staticHint("Run `agent-marketplace sync` or `agent-marketplace lock` while online (not with `--check`, `--locked`, or `--offline`) to finish the interrupted write."),

	"git.auth-config-untrusted": staticHint("Move Git authentication settings to trusted system or global configuration, then rerun the command."),
	"git.auth-transport-failed": staticHint("Check your SSH agent or Git credentials and repository access, then rerun the command."),

	"lock.alias-forbidden":          lockHint("Replace YAML aliases in the acquisition lock with explicit values"),
	"lock.change-required":          lockScopeHint("Run `agent-marketplace lock` for the affected scope to write the required acquisition-lock changes"),
	"lock.conflicted":               lockHint("Resolve the Git conflict in the acquisition lock, then run `agent-marketplace sync --check`"),
	"lock.digest-invalid":           lockHint(restoreLockGuidance),
	"lock.duplicate-field":          lockHint("Remove the duplicate field from the acquisition lock, then run `agent-marketplace sync --check`"),
	"lock.git-oid-invalid":          lockHint("Restore a valid Git commit ID in the acquisition lock, then run `agent-marketplace sync --check`"),
	"lock.git-oid-unsupported":      lockHint("Use a supported Git commit ID in the acquisition lock, then run `agent-marketplace sync --check`"),
	"lock.git-url-invalid":          lockHint("Restore the source URL in the acquisition lock from a trusted copy, then run `agent-marketplace sync --check`"),
	"lock.identity-conflict":        lockHint("Resolve the conflicting source identity in the acquisition lock, then run `agent-marketplace sync --check`"),
	"lock.invalid":                  lockHint(restoreLockGuidance),
	"lock.invalid-document":         lockHint("Correct the acquisition-lock document structure, then run `agent-marketplace sync --check`"),
	"lock.invalid-scope":            lockHint("Set the acquisition lock scope to match its configuration layer, then run `agent-marketplace sync --check`"),
	"lock.lookup":                   lockHint(restoreLockGuidance),
	"lock.malformed":                lockHint("Restore valid YAML in the acquisition lock, then run `agent-marketplace sync --check`"),
	"lock.manifest-mismatch":        lockManifestHint,
	"lock.merge-forbidden":          lockHint("Replace YAML merge keys in the acquisition lock with explicit values"),
	"lock.missing":                  lockStaleHint,
	"lock.multiple-documents":       lockHint("Keep one YAML document in the acquisition lock, then run `agent-marketplace sync --check`"),
	"lock.offline-resolution":       lockScopeHint("Connect to the source and run `agent-marketplace lock --scope <scope>` to reconcile the stale lock"),
	"lock.offline-snapshot-missing": lockSnapshotHint,
	"lock.path-invalid":             lockHint("Restore a safe relative source path in the acquisition lock, then run `agent-marketplace sync --check`"),
	"lock.read":                     lockHint(restoreLockGuidance),
	"lock.scope-mismatch":           lockScopeHint("Place the acquisition lock beside its matching configuration layer"),
	"lock.snapshot-unavailable":     lockSnapshotHint,
	"lock.stale":                    lockStaleHint,
	"lock.trust":                    lockHint("Check the acquisition lock's permissions and trusted ownership"),
	"lock.unknown-field":            lockHint("Remove the unsupported field from the acquisition lock, then run `agent-marketplace sync --check`"),
	"lock.unsupported-version":      lockScopeHint("Run `agent-marketplace lock --scope <scope>` to write the supported acquisition-lock format"),
	"lock.version-upgrade-required": lockScopeHint("Run `agent-marketplace lock --scope <scope>` to upgrade the acquisition-lock schema"),
	"lock.version-deprecated":       lockScopeHint("Run `agent-marketplace lock --scope <scope>` to write the supported acquisition-lock format"),

	"marketplace.install-collision":              staticHint("Run `agent-marketplace list` to identify the conflicting resource, then choose a unique installed name or source"),
	"marketplace.remove-ambiguous-source":        staticHint("Use `agent-marketplace remove --scope <scope>` or `agent-marketplace remove --from <source>` to identify the Marketplace resource to remove"),
	"marketplace.remove-not-selected":            staticHint("Run `agent-marketplace list` to confirm the selected Marketplace resource name before removing it"),
	"marketplace.remove-organization-collection": staticHint("Ask your administrator to change the organization-owned collection selection"),
	"marketplace.remove-unsupported-qualifier":   staticHint("Run `agent-marketplace remove --help` to see supported resource qualifiers"),

	"organization.trust.ancestor.inspect":       staticHint("Ask your administrator to inspect the organization configuration authority path"),
	"organization.trust.ancestor.non-directory": staticHint("Ask your administrator to restore the organization configuration authority directory"),
	"organization.trust.ancestor.owner":         staticHint("Ask your administrator to restore trusted ownership of the organization configuration authority path"),
	"organization.trust.ancestor.symlink":       staticHint("Ask your administrator to replace the organization configuration authority symlink with a trusted directory"),
	"organization.trust.ancestor.writable":      staticHint("Ask your administrator to restrict writes to the organization configuration authority path"),
	"organization.trust.config.inspect":         staticHint("Ask your administrator to inspect access to the organization configuration"),
	"organization.trust.config.missing":         staticHint("Ask your administrator to restore the missing organization configuration"),
	"organization.trust.config.non-regular":     staticHint("Ask your administrator to replace the organization configuration with a regular file"),
	"organization.trust.config.owner":           staticHint("Ask your administrator to restore trusted ownership of the organization configuration"),
	"organization.trust.config.symlink":         staticHint("Ask your administrator to replace the organization configuration symlink with a trusted file"),
	"organization.trust.config.writable":        staticHint("Ask your administrator to restrict writes to the organization configuration"),
	"organization.trust.include.inspect":        staticHint("Ask your administrator to inspect access to the organization include"),
	"organization.trust.include.missing":        staticHint("Ask your administrator to restore the missing organization include"),
	"organization.trust.include.non-regular":    staticHint("Ask your administrator to replace the organization include with a regular file"),
	"organization.trust.include.owner":          staticHint("Ask your administrator to restore trusted ownership of the organization include"),
	"organization.trust.include.symlink":        staticHint("Ask your administrator to replace the organization include symlink with a trusted file"),
	"organization.trust.include.writable":       staticHint("Ask your administrator to restrict writes to the organization include"),
	"organization.trust.lock.inspect":           staticHint("Ask your administrator to inspect access to the organization acquisition lock"),
	"organization.trust.lock.malformed":         staticHint("Ask your administrator to restore a valid organization acquisition lock"),
	"organization.trust.lock.missing":           staticHint("Ask your administrator to restore the missing organization acquisition lock"),
	"organization.trust.lock.non-regular":       staticHint("Ask your administrator to replace the organization acquisition lock with a regular file"),
	"organization.trust.lock.owner":             staticHint("Ask your administrator to restore trusted ownership of the organization acquisition lock"),
	"organization.trust.lock.read":              staticHint("Ask your administrator to restore access to the organization acquisition lock"),
	"organization.trust.lock.scope":             staticHint("Ask your administrator to restore the organization acquisition lock with its organization scope"),
	"organization.trust.lock.symlink":           staticHint("Ask your administrator to replace the organization acquisition lock symlink with a trusted file"),
	"organization.trust.lock.writable":          staticHint("Ask your administrator to restrict writes to the organization acquisition lock"),

	"ownership.entry-drift":         staticHint("Edit the Marketplace resource and run `agent-marketplace sync` to regenerate its owned entry"),
	"ownership.foreign-change":      staticHint("Rerun `agent-marketplace sync` to inspect the changed aggregate and retry the generation"),
	"ownership.generation-conflict": staticHint("Rerun the failed command after the concurrent ownership change has finished"),
	"ownership.lock-contended":      staticHint("Wait for the other agent-marketplace process to finish, then retry the command"),
	"ownership.lock-timeout":        staticHint("Wait for other agent-marketplace processes to finish, then retry the command"),
	"ownership.lock-unavailable":    staticHint("Use a filesystem that supports advisory locking before retrying this ownership change"),
	"ownership.recovered":           staticHint("No action is needed; rerun the failed command to inspect the recovered state"),
	"ownership.safety-backup":       staticHint("Check available disk space and write access, then retry the command"),
	"ownership.state-corrupt":       staticHint("Run `agent-marketplace doctor --repair` to inspect ownership state and adopt exact matches"),
	"ownership.state-missing":       staticHint("Run `agent-marketplace doctor --repair` to inspect and adopt exact existing matches"),
	"ownership.state-root-mismatch": staticHint("Run `agent-marketplace doctor` from the correct workspace and check the target root"),
	"ownership.state-unsafe-value":  staticHint("Remove secret or unsafe values from the generated resource and retry the command"),
	"ownership.unmanaged-key":       staticHint("Run `agent-marketplace doctor --repair` to review and resolve the unmanaged entry"),

	"v4.alias-forbidden":           configurationHint("Replace YAML aliases in the v4 configuration with explicit values"),
	"v4.api-version":               versionRefusalHint,
	"v4.api-version-required":      versionRefusalHint,
	"v4.auxiliary":                 configurationHint("Correct the named auxiliary configuration setting"),
	"v4.directory-path":            configurationHint("Choose a local source directory that exists and remains inside the allowed configuration root"),
	"v4.duplicate":                 configurationHint("Remove the duplicate value from the named configuration field"),
	"v4.duplicate-key":             configurationHint("Remove or rename the duplicate key in the v4 configuration"),
	"v4.duplicate-request":         configurationHint("Keep only one request for the resource in the v4 configuration"),
	"v4.dynamic-unbounded":         configurationHint("Choose an explicit Marketplace registry list before using all or tags selection"),
	"v4.edit-exclusion":            configurationHint("Change the exclusion that prevents this resource from being selected"),
	"v4.edit-source":               configurationHint("Correct the source URL, ref, and path in the v4 configuration"),
	"v4.exclusion":                 configurationHint("Use a supported resource-kind exclusion in the v4 configuration"),
	"v4.git-origin":                configurationHint("Use a supported Git repository URL for the source"),
	"v4.git-path":                  configurationHint("Choose a repository-relative source path without traversal"),
	"v4.git-ref":                   configurationHint("Choose a valid Git ref for the source"),
	"v4.git-ref-required":          configurationHint("Add an explicit Git ref to the source"),
	"v4.harness":                   configurationHint("Choose a supported harness name in the v4 configuration"),
	"v4.include":                   configurationHint("Correct the include path and ensure its file is readable"),
	"v4.include-cycle":             configurationHint("Remove the configuration include that creates the cycle"),
	"v4.installed-name":            configurationHint("Choose a safe installed resource name"),
	"v4.internal":                  configurationHint("Correct the resource selection and rerun `agent-marketplace doctor`"),
	"v4.key-type":                  configurationHint("Use string keys in the v4 configuration mapping"),
	"v4.layer-field":               configurationHint("Move this field to a configuration layer that supports it"),
	"v4.legacy-key":                configurationHint("Convert legacy configuration content with the supported v4 migrator"),
	"v4.locator":                   configurationHint("Use a supported resource locator in the v4 configuration"),
	"v4.marketplace-lock-mismatch": marketplaceLockMismatchHint,
	"v4.marketplace-lock-required": func(ctx hintContext) string {
		return marketplaceBootstrapHint(ctx.Scope, ctx.Locator, ctx.LockFile)
	},
	"v4.marketplace-runtime-unsupported": configurationHint("Change the Marketplace selection to use supported explicit resource requests"),
	"v4.marketplace-url":                 configurationHint("Correct the Marketplace endpoint URL in the v4 configuration"),
	"v4.merge-key-forbidden":             configurationHint("Replace the YAML merge key with explicit values"),
	"v4.multiple-documents":              configurationHint("Keep one YAML document in the v4 configuration file"),
	"v4.option-combination":              configurationHint("Remove the incompatible resource option from the v4 configuration"),
	"v4.parse":                           configurationHint("Correct the YAML syntax in the named configuration file"),
	"v4.registry-slug":                   configurationHint("Use a valid Marketplace registry slug"),
	"v4.resource-item":                   configurationHint("Use a locator string or a single locator-to-options mapping"),
	"v4.resource-options":                configurationHint("Correct the resource options to use supported non-empty string values"),
	"v4.revision":                        configurationHint("Use a `sha256:` revision with 64 lowercase hexadecimal characters"),
	"v4.root-type":                       configurationHint("Make the configuration file contain one YAML mapping at its root"),
	"v4.runtime":                         configurationHint("Rerun `agent-marketplace doctor` to inspect configuration loading"),
	"v4.runtime-option":                  configurationHint("Remove the unsupported runtime option from the consumer selection"),
	"v4.source-conflict":                 configurationHint("Make the repeated source declarations identical or give them distinct names"),
	"v4.source-discriminator":            configurationHint("Declare exactly one source type: Git, directory, or Marketplace"),
	"v4.source-field":                    configurationHint("Remove fields that do not apply to this source type"),
	"v4.source-kind":                     configurationHint("Choose a source kind that supports the requested resource"),
	"v4.source-name":                     configurationHint("Rename the source to a name containing only letters, numbers, dots, underscores, or hyphens"),
	"v4.source-type":                     configurationHint("Make the named source a mapping with one supported source type"),
	"v4.source-value":                    configurationHint("Set a non-empty value for the selected source type"),
	"v4.type":                            configurationHint("Change the named configuration field to the required value type"),
	"v4.undefined-source":                configurationHint("Declare the source referenced by this resource selection"),
	"v4.unknown-field":                   configurationHint("Remove or rename the unsupported field in the v4 configuration"),
}

// SampleHints renders one representative, non-organization hint for every code.
func SampleHints() map[string]string {
	ctx := hintContext{
		File: "/workspace/.agents/config.yaml", Line: 7, Scope: "repo",
		Path: "/workspace/.agents/lock.yaml", LockFile: ".agents/lock.yaml",
		Locator: "awesome-ai/collection/coderabbit-skills",
	}
	hints := make(map[string]string, len(catalog))
	for code, entry := range catalog {
		hints[code] = entry(ctx)
	}
	return hints
}
