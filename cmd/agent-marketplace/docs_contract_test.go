package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

var docsHeadingRe = regexp.MustCompile("(?m)^#{2,3} `([a-z][a-z -]*)`$")

// TestDocsContract keeps site/docs/reference in sync with the binary: every
// command, flag, harness identifier, environment variable, and resource kind the
// CLI exposes must be documented, and documented commands must still exist.
func TestDocsContract(t *testing.T) {
	root := newRootCmd()

	t.Run("cli", func(t *testing.T) {
		doc := readDoc(t, "site/docs/reference/cli.md")
		global := section(doc, "## Global flags")
		checkFlags := func(t *testing.T, where string, text string, flags *pflag.FlagSet) {
			flags.VisitAll(func(f *pflag.Flag) {
				if f.Hidden || f.Name == "help" {
					return
				}
				if !strings.Contains(text, "`--"+f.Name+"`") {
					t.Errorf("%s: flag `--%s` not documented", where, f.Name)
				}
			})
		}
		checkFlags(t, "## Global flags", global, root.PersistentFlags())
		checkFlags(t, "## Global flags", global, root.LocalFlags())

		var walk func(cmd *cobra.Command)
		walk = func(cmd *cobra.Command) {
			for _, sub := range cmd.Commands() {
				if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
					continue
				}
				path := strings.TrimPrefix(sub.CommandPath(), root.Name()+" ")
				heading := "`" + path + "`"
				var sec string
				if sub.HasParent() && sub.Parent() != root {
					sec = section(doc, "### "+heading)
				} else {
					sec = section(doc, "## "+heading)
				}
				if sec == "" {
					t.Errorf("cli.md: no heading for command %q", path)
				} else {
					checkFlags(t, path, sec, sub.LocalFlags())
				}
				walk(sub)
			}
		}
		walk(root)

		for _, m := range docsHeadingRe.FindAllStringSubmatch(doc, -1) {
			path := m[1]
			if path == "completion" {
				continue
			}
			found, _, err := root.Find(strings.Fields(path))
			if err != nil || found == nil || found == root || strings.TrimPrefix(found.CommandPath(), root.Name()+" ") != path {
				t.Errorf("cli.md: heading `%s` does not match an existing command", path)
			}
		}
	})

	t.Run("harnesses", func(t *testing.T) {
		harnessDoc := readDoc(t, "site/docs/reference/harnesses-and-output-paths.md")
		configDoc := readDoc(t, "site/docs/reference/configuration.md")
		names := hcc.TargetNames()
		for _, name := range names {
			for file, doc := range map[string]string{"harnesses-and-output-paths.md": harnessDoc, "configuration.md": configDoc} {
				if !strings.Contains(doc, "`"+name+"`") {
					t.Errorf("%s: harness `%s` not documented", file, name)
				}
			}
		}
		rows := 0
		for _, line := range strings.Split(section(harnessDoc, "## Supported harness identifiers"), "\n") {
			if strings.HasPrefix(line, "|") && strings.Trim(line, "|-: ") != "" {
				rows++
			}
		}
		if got := rows - 1; got != len(names) {
			t.Errorf("harnesses-and-output-paths.md: identifier table has %d rows, binary has %d targets %v", got, len(names), names)
		}
	})

	t.Run("environment", func(t *testing.T) {
		doc := readDoc(t, "site/docs/reference/environment-variables.md")
		want := map[string]bool{
			"AGENT_MARKETPLACE_NO_TUI":           true,
			"AGENT_MARKETPLACE_DEBUG":            true,
			"AGENT_MARKETPLACE_LOG_ENABLED":      true,
			"AGENT_MARKETPLACE_LOG_LEVEL":        true,
			"AGENT_MARKETPLACE_LOG_FILE":         true,
			"AGENT_MARKETPLACE_LOG_MAX_SIZE_MB":  true,
			"AGENT_MARKETPLACE_LOG_MAX_BACKUPS":  true,
			"AGENT_MARKETPLACE_LOG_MAX_AGE_DAYS": true,
			"AGENT_MARKETPLACE_LOG_COMPRESS":     true,
			telemetry.EnvLevel:                   true,
			telemetry.EnvEndpoint:                true,
			telemetry.EnvCommunityReporting:      true,
			ownership.LockTimeoutEnvVar:          true,
			"CODEX_HOME":                         true,
			"XDG_STATE_HOME":                     true,
		}
		envRe := regexp.MustCompile(`"(AGENT_MARKETPLACE_[A-Z_]+)"`)
		for _, dir := range []string{"internal", "cmd"} {
			err := filepath.WalkDir(filepath.Join(repoRoot(t), dir), func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				for _, m := range envRe.FindAllStringSubmatch(string(data), -1) {
					want[m[1]] = true
				}
				return nil
			})
			if err != nil {
				t.Fatalf("scan %s: %v", dir, err)
			}
		}
		for name := range want {
			if !strings.Contains(doc, "`"+name+"`") {
				t.Errorf("environment-variables.md: `%s` not documented", name)
			}
		}
	})

	t.Run("kinds", func(t *testing.T) {
		kinds := section(readDoc(t, "site/docs/reference/cli.md"), "## Resource kinds")
		for _, k := range []string{"skill", "command", "subagent", "hook", "mcp", "collection"} {
			if !strings.Contains(kinds, "`"+k+"`") {
				t.Errorf("cli.md Resource kinds: `%s` not documented", k)
			}
		}
		if doc := readDoc(t, "site/docs/reference/marketplace-resource-locators.md"); !strings.Contains(doc, "`mcpserver`") {
			t.Errorf("marketplace-resource-locators.md: `mcpserver` not documented")
		}
	})

	t.Run("section_helper", func(t *testing.T) {
		docAtStart := "## Heading At Start\ncontent line 1\n# Next Top Heading\ncontent line 2"
		if got := section(docAtStart, "## Heading At Start"); !strings.Contains(got, "content line 1") || strings.Contains(got, "content line 2") {
			t.Errorf("expected heading at start to be parsed and stop at top heading, got %q", got)
		}

		docSub := "## Parent\n### Subsection\nsub content\n# Top Heading\ntop content"
		if got := section(docSub, "### Subsection"); !strings.Contains(got, "sub content") || strings.Contains(got, "top content") {
			t.Errorf("expected subsection to stop at top heading, got %q", got)
		}
	})
}

func TestPublicCommandPathsHaveBehaviorTests(t *testing.T) {
	witnesses := map[string]func(*testing.T){
		"init":                  TestInitCreatesV4UserPersonalConfig,
		"list":                  TestListCommandPrintsCatalogWithStatuses,
		"search":                TestSearchCommandJSONOutput,
		"install":               TestInstallCommandAddsSkillToRepoConfigAndSyncsIt,
		"remove":                TestInstallThenRemove_RepoScope,
		"lock":                  TestLockUpgradeGitNameExpandsContainingSource,
		"sync":                  TestSyncCommandMaterializesSkillsAndPrintsSummary,
		"update":                TestUpdateCommandRefreshesPinnedRefsAndSyncsWithoutChangingConfig,
		"status":                TestStatusCommandPrintsSyncedSkillsOrphansAndCollisions,
		"migrate":               TestMigrateCommandDefaultsToV4ForUserAndRepo,
		"convert":               TestConvertCommandConvertsClaudeSkillToFactory,
		"doctor":                TestDoctorCommandReportsDetectedHarnessAndInstallRoots,
		"backup":                TestBackupListCommand,
		"backup create":         TestBackupCreateOnDemand,
		"backup list":           TestBackupListCommand,
		"backup restore":        TestBackupRestoreRoundTrip,
		"gc":                    TestGcCommandRemovesInactiveHashDirectoriesAndIsIdempotent,
		"serve":                 TestServeRequiresConfig,
		"serve reindex":         TestServeReindexRequiresConfig,
		"serve adopt":           TestServeAdoptMaintenance,
		"deinit":                TestDeinitTable,
		"telemetry":             TestTelemetryStatus,
		"telemetry status":      TestTelemetryStatus,
		"telemetry enable":      TestTelemetryEnableAndDisable,
		"telemetry disable":     TestTelemetryEnableAndDisable,
		"telemetry preview":     TestTelemetryPreview,
		"telemetry purge":       TestTelemetryPurge,
		"telemetry reset-id":    TestTelemetryResetID,
		"tui":                   TestSyncCLIAndTUIParity,
		"completion":            TestCompletionCommandGeneratesEachShellScript,
		"completion bash":       TestCompletionCommandGeneratesEachShellScript,
		"completion zsh":        TestCompletionCommandGeneratesEachShellScript,
		"completion fish":       TestCompletionCommandGeneratesEachShellScript,
		"completion powershell": TestCompletionCommandGeneratesEachShellScript,
		"help":                  TestHelpListsPlaceholderSubcommands,
	}
	missing, stale := missingCommandWitnesses(newRootCmd(), witnesses)
	for _, path := range missing {
		t.Errorf("public command %q has no behavior-test witness", path)
	}
	for _, path := range stale {
		t.Errorf("behavior-test witness %q no longer has a public command", path)
	}
	root := newRootCmd()
	root.AddCommand(&cobra.Command{Use: "untested-command"})
	missing, _ = missingCommandWitnesses(root, witnesses)
	if !slices.Contains(missing, "untested-command") {
		t.Fatal("a newly added public command did not fail the coverage guard")
	}
	flagWitnesses := map[string]func(*testing.T){
		"backup restore --target":                 TestBackupRestoreRoundTrip,
		"completion bash --no-descriptions":       TestCompletionCommandGeneratesEachShellScript,
		"completion fish --no-descriptions":       TestCompletionCommandGeneratesEachShellScript,
		"completion powershell --no-descriptions": TestCompletionCommandGeneratesEachShellScript,
		"completion zsh --no-descriptions":        TestCompletionCommandGeneratesEachShellScript,
		"convert --autodetect":                    TestConvertCommandAutodetectsSourceHarness,
		"convert --dry-run":                       TestConvertCommandToCodexDryRunDoesNotWrite,
		"convert --from":                          TestConvertCommandConvertsClaudeSkillToFactory,
		"convert --out":                           TestConvertCommandConvertsClaudeSkillToFactory,
		"convert --sync":                          TestConvertCommandToCodexSyncPrunesStaleOutput,
		"convert --to":                            TestConvertCommandConvertsClaudeSkillToFactory,
		"deinit --dry-run":                        TestDeinitTable,
		"deinit --purge-config":                   TestDeinitTable,
		"deinit --scope":                          TestDeinitTable,
		"doctor --repair":                         TestDoctorRepairYesAdoptsWithoutPrompting,
		"doctor --yes":                            TestDoctorRepairYesAdoptsWithoutPrompting,
		"gc --dry-run":                            TestGcCommandDryRunReportsOrphanedCacheDirectoriesWithoutDeleting,
		"init --harnesses":                        TestInitCreatesV4UserPersonalConfig,
		"install --accept-risk":                   TestInstallValidationAllowsSelectorAndBareCoordinateWithMarketplaceFlags,
		"install --all":                           TestInstallCommandAddsAllDiscoveredSkillsToRepoConfigAndSyncsThem,
		"install --as":                            TestInstallValidationAllowsSelectorAndBareCoordinateWithMarketplaceFlags,
		"install --from":                          TestInstallFromFlagInstallsNamedResourceFromAdHocRegistry,
		"install --kind":                          TestInstallFromFlagWithKindFilter,
		"install --marketplace":                   TestInstallValidationAllowsSelectorAndBareCoordinateWithMarketplaceFlags,
		"install --path":                          TestInstallFromFlagWithWebURLInfersResourceName,
		"install --ref":                           TestInstallFromFlagInstallsNamedResourceFromAdHocRegistry,
		"install --registry":                      TestInstallCommandAddsAllDiscoveredSkillsForSingleRegistryWhenRegistryFlagIsSet,
		"install --scope":                         TestInstallThenRemove_UserScope,
		"install --tag":                           TestInstallCommandAddsTagToRepoConfigAndSyncsMatchingSkills,
		"install --verbose":                       TestMarketplaceInstallOutputShowsCompactVerifiedDetails,
		"install --yes":                           TestInstallValidationAllowsSelectorAndBareCoordinateWithMarketplaceFlags,
		"list --from":                             TestListFromFlagListsResourcesAtAdHocRegistry,
		"list --kind":                             TestListFromFlagWithKindFilter,
		"list --path":                             TestListFromFlagRefAndPathRequireFrom,
		"list --ref":                              TestListFromFlagListsResourcesAtAdHocRegistry,
		"lock --check":                            TestLockCheckDoesNotAcquireMissingOrganizationMarketplaceLock,
		"lock --kind":                             TestLockUpgradeGitNameExpandsContainingSource,
		"lock --path":                             TestLockUpgradeTargetsExactRefAndPath,
		"lock --ref":                              TestLockUpgradeTargetsExactRefAndPath,
		"lock --registry":                         TestLockUpgradeGitNameExpandsContainingSource,
		"lock --scope":                            TestLockUpgradeScopeLeavesOtherLayerAtExactCommit,
		"lock --upgrade":                          TestLockUpgradeGitNameExpandsContainingSource,
		"migrate --dry-run":                       TestMigrateCommandToV4IsExplicitAndSupportsDryRun,
		"migrate --harnesses":                     TestMigrateCommandUsesExplicitHarnesses,
		"migrate --scope":                         TestMigrateCommandDefaultsToV4ForUserAndRepo,
		"migrate --to":                            TestMigrateCommandToV4IsExplicitAndSupportsDryRun,
		"remove --from":                           TestRemoveFromFlagRemovesNamedResourceFromAdHocRegistry,
		"remove --kind":                           TestRemoveFromFlagUsesResolvedKindNotFlagDefault,
		"remove --path":                           TestRemoveFromFlagRefAndPathRequireFrom,
		"remove --ref":                            TestRemoveFromFlagRefAndPathRequireFrom,
		"remove --scope":                          TestInstallThenRemove_UserScope,
		"root --debug":                            TestDebugFlagEnablesStructuredDebugLogs,
		"root --diagnostic":                       TestDiagnosticFlagExportsPayload,
		"root --diagnostic-accept-sensitive-data": TestDiagnosticFlagExportsPayload,
		"root --version":                          TestVersionFlagPrintsVersion,
		"search --kind":                           TestSearchCommandWithoutKindReturnsEveryResourceKind,
		"search --limit":                          TestSearchCommandLimitOffsetBounds,
		"search --marketplace":                    TestSearchCommandJSONOutput,
		"search --offset":                         TestSearchCommandLimitOffsetBounds,
		"search --output":                         TestSearchCommandJSONOutput,
		"serve --config":                          TestServeRequiresConfig,
		"serve --role":                            TestServeRejectsUnsupportedRole,
		"serve adopt --actor":                     TestServeAdoptMaintenance,
		"serve adopt --config":                    TestServeAdoptMaintenance,
		"serve adopt --confirm-maintenance":       TestServeAdoptMaintenance,
		"serve adopt --idempotency-key":           TestServeAdoptMaintenance,
		"serve adopt --reason":                    TestServeAdoptMaintenance,
		"serve reindex --config":                  TestServeReindexRequiresConfig,
		"sync --allow-revoked":                    TestSyncTransientOrganizationLockHonorsRevocationOverride,
		"sync --check":                            TestSyncCheckExitsZeroWhenInSync,
		"sync --locked":                           TestSyncCLIAndTUIParity,
		"sync --no-backup":                        TestSyncNoBackupFlag,
		"sync --offline":                          TestSyncCLIAndTUIParity,
		"sync --prune-stale":                      TestSyncPruneStaleRemovesStaleSkillSelection,
		"sync --strict":                           TestSyncPruneStaleStrictModePrunesTheSelectionsItPromoted,
		"sync --verbose":                          TestSyncCommandOutputDirsShowCountsAndVerboseDetail,
		"telemetry enable --endpoint":             TestTelemetryEnableEndpoint,
		"telemetry enable --level":                TestTelemetryEnableAndDisable,
		"telemetry preview --level":               TestTelemetryPreview,
		"update --bump":                           TestUpdateCommandWithBumpUpdatesRepoConfigRefAndSyncsNewCache,
		"update --kind":                           TestUpdateBumpHonorsScopeAndDirectTarget,
		"update --path":                           TestUpdateBumpHonorsScopeAndDirectTarget,
		"update --ref":                            TestUpdateBumpHonorsScopeAndDirectTarget,
		"update --registry":                       TestUpdateBumpHonorsScopeAndDirectTarget,
		"update --scope":                          TestUpdateBumpHonorsScopeAndDirectTarget,
	}
	missingFlags, staleFlags := missingFlagWitnesses(newRootCmd(), flagWitnesses)
	for _, path := range missingFlags {
		t.Errorf("public flag %q has no behavior-test witness", path)
	}
	for _, path := range staleFlags {
		t.Errorf("flag behavior-test witness %q no longer has a public flag", path)
	}
	root = newRootCmd()
	root.PersistentFlags().Bool("untested-public-flag", false, "")
	missingFlags, _ = missingFlagWitnesses(root, flagWitnesses)
	if !slices.Contains(missingFlags, "root --untested-public-flag") {
		t.Fatal("a newly added public flag did not fail the coverage guard")
	}
	// tools/witness-check reads these lines from `go test -json` and fails CI
	// unless every witness also ran and passed; mapping alone is not coverage.
	for _, name := range witnessTestNames(witnesses, flagWitnesses) {
		t.Logf("%s%s", behaviorWitnessPrefix, name)
	}
}

const behaviorWitnessPrefix = "behavior-witness: "

func witnessTestNames(maps ...map[string]func(*testing.T)) []string {
	var names []string
	for _, witnesses := range maps {
		for _, fn := range witnesses {
			full := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
			names = append(names, full[strings.LastIndex(full, ".")+1:])
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func missingCommandWitnesses(root *cobra.Command, witnesses map[string]func(*testing.T)) (missing, stale []string) {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	seen := make(map[string]bool, len(witnesses))
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, cmd := range parent.Commands() {
			if cmd.Hidden {
				continue
			}
			path := strings.TrimPrefix(cmd.CommandPath(), root.Name()+" ")
			if witnesses[path] == nil {
				missing = append(missing, path)
			}
			seen[path] = true
			walk(cmd)
		}
	}
	walk(root)
	for path := range witnesses {
		if !seen[path] {
			stale = append(stale, path)
		}
	}
	slices.Sort(missing)
	slices.Sort(stale)
	return missing, stale
}

func missingFlagWitnesses(root *cobra.Command, witnesses map[string]func(*testing.T)) (missing, stale []string) {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	seen := make(map[string]bool, len(witnesses))
	collect := func(path string, flags *pflag.FlagSet) {
		flags.VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden || flag.Name == "help" {
				return
			}
			key := path + " --" + flag.Name
			if seen[key] {
				return
			}
			if witnesses[key] == nil {
				missing = append(missing, key)
			}
			seen[key] = true
		})
	}
	collect("root", root.PersistentFlags())
	collect("root", root.LocalFlags())
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, cmd := range parent.Commands() {
			if cmd.Hidden {
				continue
			}
			path := strings.TrimPrefix(cmd.CommandPath(), root.Name()+" ")
			collect(path, cmd.LocalFlags())
			walk(cmd)
		}
	}
	walk(root)
	for path := range witnesses {
		if !seen[path] {
			stale = append(stale, path)
		}
	}
	slices.Sort(missing)
	slices.Sort(stale)
	return missing, stale
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above working directory")
		}
		dir = parent
	}
}

func readDoc(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func section(doc, heading string) string {
	doc = "\n" + doc
	idx := strings.Index(doc, "\n"+heading+"\n")
	if idx < 0 {
		return ""
	}
	body := doc[idx+1+len(heading)+1:]
	stop := regexp.MustCompile("(?m)^#{1,2} ")
	if strings.HasPrefix(heading, "### ") {
		stop = regexp.MustCompile("(?m)^#{1,3} ")
	}
	if m := stop.FindStringIndex(body); m != nil {
		body = body[:m[0]]
	}
	return body
}
