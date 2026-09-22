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
  // Installs a v4 organization layer at <root>/etc/agent-marketplace that
  // declares one Marketplace source:
  //  - "missing-lock" leaves the organization acquisition lock absent, so every
  //    load fails closed with v4.marketplace-lock-required naming the
  //    organization lock path. The initial fixture sync is skipped because it
  //    cannot succeed on such a machine.
  //  - "published-lock" publishes the lock alongside the config, so the
  //    organization layer hydrates a Marketplace endpoint with no pinned
  //    resources and the workspace stays healthy.
  organization?: OrganizationFixtureMode;
}

export interface Fixture {
  root: string;
  home: string;
  repo: string;
  launcher: string;
  pidFile: string;
  registryURL: string;
  env: Record<string, string>;
  cacheDir: string;
  stateDir: string;
  expectedAlphaSkillContent: string;
  expectedBetaSkillContent: string;
  expectedRepoConfig: string;
  expectedLocalConfig: string;
  orgConfigPath?: string;
  orgLockPath?: string;
  expectedOrgConfig?: string;
  publishOrgLock?: () => void;
  cleanup(): void;
}

const git = (cwd: string, env: Record<string, string>, args: string[]): void => {
  execFileSync("git", args, { cwd, env, stdio: ["ignore", "pipe", "pipe"], timeout: 30_000 });
};

const shellQuote = (value: string): string => `'${value.replaceAll("'", `'"'"'`)}'`;

const orgConfigContent = (marketplaceURL: string): string =>
  `apiVersion: agent-marketplace/v4\nsources:\n  org-market:\n    marketplace: ${marketplaceURL}\n`;

const orgLockContent = (marketplaceURL: string, instanceID: string): string =>
  `apiVersion: agent-marketplace-lock/v2\nscope: organization\nmarketplaces:\n  - url: ${marketplaceURL}\n    instance_id: ${instanceID}\n`;

export const createFixture = async (name: string, options: FixtureOptions = {}): Promise<Fixture> => {
  if (!/^[a-z0-9-]+$/.test(name)) throw new Error(`unsafe fixture name ${JSON.stringify(name)}`);
  if (!fs.existsSync(binary)) throw new Error(`TUI QA binary not found: ${binary}`);
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
    let publishOrgLock: (() => void) | undefined;
    if (options.organization) {
      stub = await createMarketplaceStub();
      const configPath = path.join(root, "etc", "agent-marketplace", "config.yaml");
      const config = orgConfigContent(stub.url);
      write(configPath, config);
      // The runtime derives the lock path from the canonical (symlink-free)
      // config path and names that path in its errors, so compute the expected
      // location the same way (macOS keeps the real temp dir behind /var).
      const lockPath = path.join(path.dirname(fs.realpathSync(configPath)), "lock.yaml");
      const lock = orgLockContent(stub.url, stub.instanceID);
      if (options.organization === "published-lock") write(lockPath, lock);
      orgConfigPath = configPath;
      orgLockPath = lockPath;
      expectedOrgConfig = config;
      publishOrgLock = () => write(lockPath, lock);
    }

    const env = isolatedEnvironment(root, home, tmp, xdg, orgConfigPath);
    const source = path.join(registries, "source");
    write(path.join(source, "skills", "alpha-skill", "SKILL.md"), alphaSkillContent);
    write(path.join(source, "skills", "beta-skill", "SKILL.md"), betaSkillContent);
    git(source, env, ["init", "--quiet", "--initial-branch=main"]);
    git(source, env, ["add", "."]);
    git(source, env, ["-c", "user.name=TUI QA", "-c", "user.email=tui-qa@example.invalid", "commit", "--quiet", "-m", "Seed TUI fixture"]);
    const bareRegistry = path.join(registries, "fixture.git");
    git(registries, env, ["clone", "--quiet", "--bare", source, bareRegistry]);
    const registryURL = pathToFileURL(bareRegistry).href;

    const expectedRepoConfig = `apiVersion: agent-marketplace/v2\nregistries:\n  - url: ${registryURL}\n    ref: main\n    path: skills\n    skills:\n      include:\n        - beta-skill\n`;
    write(path.join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\nregistries: []\n");
    write(path.join(repo, ".agents", "config.yaml"), expectedRepoConfig);
    git(repo, env, ["init", "--quiet", "--initial-branch=main"]);

    // A machine whose organization layer cannot load never reaches a successful
    // sync, so the blocked fixture keeps its cache cold and its outputs empty;
    // materialization is asserted later in the journey, after the organization
    // lock is published. The initial sync runs without blocking the event loop
    // so the organization marketplace stub can answer it.
    if (options.organization !== "missing-lock") {
      const initialSync = await runAM({ repo, env }, ["sync"]);
      if (initialSync.status !== 0) {
        throw new Error(`fixture sync failed (exit ${initialSync.status}):\n${initialSync.stdout}\n${initialSync.stderr}`);
      }

      const betaSkill = path.join(repo, ".factory", "skills", "beta-skill");
      if (!fs.lstatSync(betaSkill).isSymbolicLink()) throw new Error("fixture beta-skill is not a symlink");
      if (fs.lstatSync(path.join(repo, ".factory", "skills", "alpha-skill"), { throwIfNoEntry: false })) throw new Error("fixture alpha-skill was unexpectedly materialized");
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
      env,
      cacheDir: path.join(home, ".cache", "agent-marketplace"),
      stateDir: path.join(xdg, "state", "agent-marketplace"),
      expectedAlphaSkillContent: alphaSkillContent,
      expectedBetaSkillContent: betaSkillContent,
      expectedRepoConfig,
      expectedLocalConfig: `registries:\n  - url: ${registryURL}\n    ref: main\n    path: skills\n    skills:\n      include:\n        - alpha-skill\n`,
      orgConfigPath,
      orgLockPath,
      expectedOrgConfig,
      publishOrgLock,
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
