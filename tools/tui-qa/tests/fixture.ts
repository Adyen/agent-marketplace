import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

import { binary, isolatedEnvironment, runAM, write } from "./process-helpers.js";
import { createMarketplaceStub, type MarketplaceStub } from "./marketplace-stub.js";

const alphaSkillContent = "---\nname: alpha-skill\ndescription: Available alpha fixture skill\n---\n\n# Alpha Skill\n\nAlpha fixture body.\n";
const betaSkillContent = "---\nname: beta-skill\ndescription: Synced beta fixture skill\n---\n\n# Beta Skill\n\nBeta fixture body.\n";

export type OrganizationFixtureMode = "missing-lock" | "published-lock";

export interface FixtureOptions {
  directorySource?: boolean;
  overflowStatus?: boolean;
  // Installs a v4 organization layer at <root>/etc/agent-marketplace that
  // declares one exact Marketplace skill:
  //  - "missing-lock" leaves the organization acquisition lock absent, so a
  //    consumer must acquire a verified transient lock without writing policy.
  //    The initial fixture sync is skipped to keep the TUI's first load cold.
  //  - "published-lock" publishes the same exact lock alongside the config.
  organization?: OrganizationFixtureMode;
  userMarketplaceDrift?: boolean;
  // The stub serves placeholder archives that do not hash to their digest.
  mismatchedPlaceholderArchive?: boolean;
  // Leaves ~/.agents/config.yaml absent and skips the fixture sync so the TUI
  // opens first-run harness setup. ~/.claude marks Claude Code as installed.
  firstRun?: boolean;
  // Registers the stub as the user's "market" endpoint without selecting
  // anything, and lets a search find its installable skill. "findings"
  // assesses that skill with a warning so installing it requires consent.
  marketplaceDiscovery?: "plain" | "findings";
  // Adds an unconfigured local Git registry for install-from-source journeys.
  adHocSource?: boolean;
}

export interface Fixture {
  root: string;
  home: string;
  repo: string;
  launcher: string;
  pidFile: string;
  registryURL: string;
  adHocSourceURL?: string;
  env: Record<string, string>;
  cacheDir: string;
  stateDir: string;
  expectedAlphaSkillContent: string;
  expectedBetaSkillContent: string;
  expectedRepoConfig: string;
  expectedLocalConfig: string;
  userLockPath?: string;
  marketplaceDriftedArchiveDigest?: string;
  orgConfigPath?: string;
  orgLockPath?: string;
  expectedOrgConfig?: string;
  expectedOrgSkillContent?: string;
  marketplaceAcquisitionDataRequestCount?: () => number;
  marketplaceAcquisitionDataRequestPaths?: () => string[];
  marketplaceArchiveDigest?: string;
  marketplaceURL?: string;
  marketplaceContentDigest?: string;
  marketplaceArchiveFetches?: () => { placeholder: string[]; final: string[] };
  marketplaceDelayNextAcquisition?: (ms: number) => void;
  marketplaceDelayedAcquisitionCount?: () => number;
  // Commits a file change to the Git registry and pushes it, moving the ref
  // past the commit the repository lock pins. Null content removes the path.
  pushRegistryChange(relativePath: string, content: string | null): void;
  resetPersonalState(): void;
  cleanup(): void;
}

const git = (cwd: string, env: Record<string, string>, args: string[]): void => {
  execFileSync("git", args, { cwd, env, stdio: ["ignore", "pipe", "pipe"], timeout: 30_000 });
};

const shellQuote = (value: string): string => `'${value.replaceAll("'", `'"'"'`)}'`;

const orgConfigContent = (marketplaceURL: string): string =>
  `apiVersion: agent-marketplace/v4\nsources:\n  org-market:\n    marketplace: ${marketplaceURL}\nresources:\n  org-market:\n    include:\n      - tools/skill/org-review\n    required: true\n`;

const orgLockContent = (stub: MarketplaceStub): string =>
  `apiVersion: agent-marketplace-lock/v2\nscope: organization\nmarketplaces:\n  - url: ${stub.url}\n    instance_id: ${stub.instanceID}\n    registry_bindings:\n      - name: ${stub.registrySlug}\n        registry_id: ${stub.registryID}\n    resources:\n      - registry_name: ${stub.registrySlug}\n        registry_id: ${stub.registryID}\n        kind: skill\n        name: ${stub.resourceName}\n        revision: ${stub.contentDigest}\n        content_digest: ${stub.contentDigest}\n        archive_digest: ${stub.archiveDigest}\n        acquisition: mirror\n`;

export const createFixture = async (name: string, options: FixtureOptions = {}): Promise<Fixture> => {
  if (!/^[a-z0-9-]+$/.test(name)) throw new Error(`unsafe fixture name ${JSON.stringify(name)}`);
  if (!fs.existsSync(binary)) throw new Error(`TUI QA binary not found: ${binary}`);
  if (options.firstRun && (options.directorySource || options.userMarketplaceDrift || options.organization)) {
    throw new Error("firstRun fixtures start without a user config and cannot combine with other layers");
  }
  if (options.marketplaceDiscovery && (options.firstRun || options.directorySource || options.userMarketplaceDrift || options.organization)) {
    throw new Error("marketplaceDiscovery fixtures own the user config and cannot combine with other layers");
  }
  const root = fs.mkdtempSync(path.join(os.tmpdir(), `agent-marketplace-tui-${name}-`));
  let stub: MarketplaceStub | undefined;
  try {
    const home = path.join(root, "home");
    const repo = path.join(root, "workspace");
    const registries = path.join(root, "registries");
    const tmp = path.join(root, "tmp");
    const xdg = path.join(root, "xdg");
    for (const directory of [home, repo, registries, tmp, xdg]) fs.mkdirSync(directory, { recursive: true, mode: 0o700 });

    let orgConfigPath: string | undefined;
    let orgLockPath: string | undefined;
    let expectedOrgConfig: string | undefined;
    let expectedOrgSkillContent: string | undefined;
    let marketplaceAcquisitionDataRequestCount: (() => number) | undefined;
    let marketplaceAcquisitionDataRequestPaths: (() => string[]) | undefined;
    let marketplaceDelayNextAcquisition: ((ms: number) => void) | undefined;
    let marketplaceDelayedAcquisitionCount: (() => number) | undefined;
    if (options.organization || options.userMarketplaceDrift || options.marketplaceDiscovery) {
      stub = await createMarketplaceStub({
        mismatchedPlaceholderArchive: options.mismatchedPlaceholderArchive,
        searchableInstall: Boolean(options.marketplaceDiscovery),
        installFindings: options.marketplaceDiscovery === "findings"
      });
    }
    if (options.organization && stub) {
      const configPath = path.join(root, "etc", "agent-marketplace", "config.yaml");
      const config = orgConfigContent(stub.url);
      write(configPath, config);
      // The runtime derives the lock path from the canonical (symlink-free)
      // config path and names that path in its errors, so compute the expected
      // location the same way (macOS keeps the real temp dir behind /var).
      const lockPath = path.join(path.dirname(fs.realpathSync(configPath)), "lock.yaml");
      const lock = orgLockContent(stub);
      if (options.organization === "published-lock") write(lockPath, lock);
      orgConfigPath = configPath;
      orgLockPath = lockPath;
      expectedOrgConfig = config;
      expectedOrgSkillContent = stub.materializedSkillContent;
      marketplaceAcquisitionDataRequestCount = stub.acquisitionDataRequestCount;
      marketplaceAcquisitionDataRequestPaths = stub.acquisitionDataRequestPaths;
      marketplaceDelayNextAcquisition = stub.delayNextAcquisition;
      marketplaceDelayedAcquisitionCount = stub.delayedAcquisitionCount;
    }
    if ((options.userMarketplaceDrift || options.marketplaceDiscovery) && stub) {
      expectedOrgSkillContent = stub.materializedSkillContent;
    }

    const env = isolatedEnvironment(root, home, tmp, xdg, orgConfigPath);
    const source = path.join(registries, "source");
    write(path.join(source, "skills", "alpha-skill", "SKILL.md"), alphaSkillContent);
    write(path.join(source, "skills", "beta-skill", "SKILL.md"), betaSkillContent);
    const overflowSkills = options.overflowStatus ? Array.from({ length: 30 }, (_, index) => `skill-${String(index).padStart(2, "0")}`) : [];
    for (const name of overflowSkills) {
      write(path.join(source, "skills", name, "SKILL.md"), `---\nname: ${name}\ndescription: Status overflow fixture\n---\n\n# ${name}\n`);
    }
    let registryURL: string;
    let expectedRepoConfig: string;
    if (options.directorySource) {
      registryURL = source;
      expectedRepoConfig = "";
      write(path.join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4\nharnesses: [factory, gemini, devin]\nsources:\n  local:\n    directory: ${source}\n`);
    } else {
      git(source, env, ["init", "--quiet", "--initial-branch=main"]);
      git(source, env, ["add", "."]);
      git(source, env, ["-c", "user.name=TUI QA", "-c", "user.email=tui-qa@example.invalid", "commit", "--quiet", "-m", "Seed TUI fixture"]);
      const bareRegistry = path.join(registries, "fixture.git");
      git(registries, env, ["clone", "--quiet", "--bare", source, bareRegistry]);
      registryURL = pathToFileURL(bareRegistry).href;
      expectedRepoConfig = `apiVersion: agent-marketplace/v4\nsources:\n  fixture:\n    git: ${registryURL}\n    ref: main\n    path: skills\nresources:\n  fixture:\n    include:\n      - skill/beta-skill\n${overflowSkills.map((name) => `      - skill/${name}\n`).join("")}`;
      if (options.userMarketplaceDrift && stub) {
        write(path.join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\nsources:\n  user-market:\n    marketplace: ${stub.url}\nresources:\n  user-market:\n    include:\n      - tools/skill/org-review\n    required: true\n`);
      } else if (options.firstRun) {
        fs.mkdirSync(path.join(home, ".claude"), { recursive: true, mode: 0o700 });
      } else if (options.marketplaceDiscovery && stub) {
        write(path.join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\nsources:\n  market:\n    marketplace: ${stub.url}\n`);
      } else {
        write(path.join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\nsources: {}\n");
      }
      write(path.join(repo, ".agents", "config.yaml"), expectedRepoConfig);
    }
    let adHocSourceURL: string | undefined;
    let adHocSource = "";
    let adHocBareRegistry = "";
    if (options.adHocSource) {
      adHocSource = path.join(registries, "ad-hoc-source");
      adHocBareRegistry = path.join(registries, "ad-hoc-source.git");
      write(path.join(adHocSource, "skills", "ad-hoc-alpha", "SKILL.md"), "---\nname: ad-hoc-alpha\ndescription: An unconfigured source skill\n---\n\n# Ad Hoc Alpha\n");
      write(path.join(adHocSource, "skills", "ad-hoc-second", "SKILL.md"), "---\nname: ad-hoc-second\ndescription: A second unconfigured skill\n---\n\n# Ad Hoc Second\n");
      write(path.join(adHocSource, "skills", "ad-hoc-broken", "SKILL.md"), "---\nname: [\n---\nBroken fixture.\n");
      write(path.join(adHocSource, "commands", "ad-hoc-command.md"), "---\nname: ad-hoc-command\ndescription: An unconfigured source command\n---\n\n# Ad Hoc Command\n");
      git(adHocSource, env, ["init", "--quiet", "--initial-branch=main"]);
      git(adHocSource, env, ["add", "."]);
      git(adHocSource, env, ["-c", "user.name=TUI QA", "-c", "user.email=tui-qa@example.invalid", "commit", "--quiet", "-m", "Seed ad-hoc source"]);
      git(registries, env, ["clone", "--quiet", "--bare", adHocSource, adHocBareRegistry]);
      adHocSourceURL = pathToFileURL(adHocBareRegistry).href;
      const fixtureRegistryPattern = `^${registryURL.replace(/\.git$/, "").replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`;
      const adHocRegistryPattern = `^${adHocSourceURL.replace(/\.git$/, "").replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`;
      write(path.join(home, ".agents", "config.yaml"), [
        "apiVersion: agent-marketplace/v4",
        "harnesses: [factory]",
        "allowed_registries:",
        `  - ${JSON.stringify(fixtureRegistryPattern)}`,
        `  - ${JSON.stringify(adHocRegistryPattern)}`,
        "sources: {}",
        ""
      ].join("\n"));
    }
    git(repo, env, ["init", "--quiet", "--initial-branch=main"]);

    // Keep the missing-lock fixture cold so the TUI's first real-terminal load
    // must perform transient acquisition, and the first-run fixture cold so the
    // TUI's first sync follows harness setup. The published-lock counterpart
    // gets the normal preflight sync. It runs without blocking the event loop
    // so the organization Marketplace stub can answer it.
    if (options.organization !== "missing-lock" && !options.firstRun) {
      const initialSync = await runAM({ repo, env }, ["sync"]);
      if (initialSync.status !== 0) {
        throw new Error(`fixture sync failed (exit ${initialSync.status}):\n${initialSync.stdout}\n${initialSync.stderr}`);
      }

      if (options.directorySource) {
        for (const skill of ["alpha-skill", "beta-skill"]) {
          for (const rootPath of [path.join(home, ".factory", "skills"), path.join(home, ".gemini", "skills"), path.join(home, ".config", "devin", "skills")]) {
            if (fs.lstatSync(path.join(rootPath, skill), { throwIfNoEntry: false })) throw new Error(`fixture ${skill} was unexpectedly materialized`);
          }
        }
      } else {
        const betaSkill = path.join(repo, ".factory", "skills", "beta-skill");
        if (!fs.lstatSync(betaSkill).isSymbolicLink()) throw new Error("fixture beta-skill is not a symlink");
        if (fs.lstatSync(path.join(repo, ".factory", "skills", "alpha-skill"), { throwIfNoEntry: false })) throw new Error("fixture alpha-skill was unexpectedly materialized");
      }
      if (options.userMarketplaceDrift && stub) {
        stub.driftArchiveDigest();
      }
    }

    const launcher = path.join(root, "launch-agent-marketplace");
    const pidFile = path.join(root, "target.pid");
    write(launcher, `#!/bin/sh\ncd -- ${shellQuote(repo)}\nprintf '%s\\n' "$$" > ${shellQuote(pidFile)}\nexec ${shellQuote(binary)} "$@"\n`, 0o700);

    // The disposable roots a user-side wipe deletes. The catalog cache ignores
    // XDG_CACHE_HOME and lives under HOME (internal/cache.CacheDir); the
    // ownership state follows XDG_STATE_HOME (internal/ownership).
    return {
      root,
      home,
      repo,
      launcher,
      pidFile,
      registryURL,
      adHocSourceURL,
      env,
      cacheDir: path.join(home, ".cache", "agent-marketplace"),
      stateDir: path.join(xdg, "state", "agent-marketplace"),
      expectedAlphaSkillContent: alphaSkillContent,
      expectedBetaSkillContent: betaSkillContent,
      expectedRepoConfig,
      expectedLocalConfig: `apiVersion: agent-marketplace/v4\nsources:\n  fixture:\n    git: ${registryURL}\n    ref: main\n    path: skills\n    source_harness: claude\nresources:\n  fixture:\n    include:\n      - skill/alpha-skill\n`,
      userLockPath: options.userMarketplaceDrift ? path.join(home, ".agents", "lock.yaml") : undefined,
      marketplaceDriftedArchiveDigest: stub?.driftedArchiveDigest,
      marketplaceArchiveDigest: stub?.archiveDigest,
      marketplaceURL: stub?.url,
      marketplaceContentDigest: stub?.contentDigest,
      marketplaceArchiveFetches: stub?.archiveFetches,
      orgConfigPath,
      orgLockPath,
      expectedOrgConfig,
      expectedOrgSkillContent,
      marketplaceAcquisitionDataRequestCount,
      marketplaceAcquisitionDataRequestPaths,
      marketplaceDelayNextAcquisition,
      marketplaceDelayedAcquisitionCount,
      pushRegistryChange: (relativePath, content) => {
        if (options.directorySource) throw new Error("directory sources have no Git registry to advance");
        const workingRegistry = options.adHocSource ? adHocSource : source;
        const bareRegistry = options.adHocSource ? adHocBareRegistry : path.join(registries, "fixture.git");
        if (content === null) {
          git(workingRegistry, env, ["rm", "-r", "--quiet", "--", relativePath]);
        } else {
          write(path.join(workingRegistry, relativePath), content);
          git(workingRegistry, env, ["add", "--", relativePath]);
        }
        git(workingRegistry, env, ["-c", "user.name=TUI QA", "-c", "user.email=tui-qa@example.invalid", "commit", "--quiet", "-m", `Change ${relativePath}`]);
        git(workingRegistry, env, ["push", "--quiet", bareRegistry, "main"]);
      },
      resetPersonalState: () => {
        fs.rmSync(path.join(home, ".agents"), { recursive: true, force: true });
        fs.rmSync(path.join(home, ".factory"), { recursive: true, force: true });
        fs.rmSync(path.join(home, ".claude"), { recursive: true, force: true });
        fs.rmSync(path.join(home, ".cursor"), { recursive: true, force: true });
        fs.rmSync(path.join(home, ".gemini"), { recursive: true, force: true });
        fs.rmSync(path.join(home, ".windsurf"), { recursive: true, force: true });
        fs.rmSync(path.join(home, ".config", "devin"), { recursive: true, force: true });
        fs.rmSync(path.join(home, ".codex"), { recursive: true, force: true });
        fs.rmSync(path.join(home, ".local", "state", "agent-marketplace"), { recursive: true, force: true });
        fs.rmSync(path.join(home, ".cache", "agent-marketplace"), { recursive: true, force: true });
        fs.rmSync(path.join(repo, ".agents"), { recursive: true, force: true });
        fs.rmSync(path.join(repo, ".factory"), { recursive: true, force: true });
        fs.rmSync(path.join(repo, ".claude"), { recursive: true, force: true });
        fs.rmSync(path.join(repo, ".cursor"), { recursive: true, force: true });
        fs.rmSync(path.join(repo, ".gemini"), { recursive: true, force: true });
        fs.rmSync(path.join(repo, ".windsurf"), { recursive: true, force: true });
        fs.rmSync(path.join(repo, ".devin"), { recursive: true, force: true });
        fs.rmSync(path.join(repo, ".codex"), { recursive: true, force: true });
        fs.rmSync(path.join(xdg, "state", "agent-marketplace"), { recursive: true, force: true });
        if (options.firstRun) fs.mkdirSync(path.join(home, ".claude"), { recursive: true, mode: 0o700 });
        if (!options.directorySource) {
          const repoConfigPath = path.join(repo, ".agents", "config.yaml");
          fs.mkdirSync(path.dirname(repoConfigPath), { recursive: true, mode: 0o700 });
          fs.writeFileSync(repoConfigPath, expectedRepoConfig, { encoding: "utf8", mode: 0o600 });
        }
      },
      cleanup: () => {
        stub?.close();
        fs.rmSync(root, { recursive: true, force: true });
      }
    };
  } catch (error) {
    stub?.close();
    fs.rmSync(root, { recursive: true, force: true });
    throw error;
  }
};
