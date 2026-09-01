import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

import type { Sandbox } from "./sandbox.js";

export interface InvariantFixtureIdentity {
  id: "local-bare-git-registry-v1";
  version: "1";
}

const git = (cwd: string, env: NodeJS.ProcessEnv, args: string[]): void => {
  execFileSync("git", args, { cwd, env, stdio: ["ignore", "pipe", "pipe"], timeout: 30_000 });
};

export const seedInvariantFixture = (sandbox: Sandbox, env: NodeJS.ProcessEnv): InvariantFixtureIdentity => {
  const source = path.join(sandbox.registries, "fixture-source");
  const skillDirectory = path.join(source, "skills", "alpha-skill");
  fs.mkdirSync(skillDirectory, { recursive: true });
  fs.writeFileSync(
    path.join(skillDirectory, "SKILL.md"),
    "---\nname: alpha-skill\ndescription: Synthetic TUI QA fixture\n---\n\n# Alpha Skill\n",
    { encoding: "utf8", mode: 0o600 }
  );
  git(source, env, ["init", "--quiet", "--initial-branch=main"]);
  git(source, env, ["add", "."]);
  git(source, env, ["-c", "user.name=TUI QA", "-c", "user.email=tui-qa@example.invalid", "commit", "--quiet", "-m", "Seed invariant fixture"]);
  const bareRegistry = path.join(sandbox.registries, "fixture.git");
  git(sandbox.registries, env, ["clone", "--quiet", "--bare", source, bareRegistry]);

  const userConfigDirectory = path.join(sandbox.home, ".agents");
  fs.mkdirSync(userConfigDirectory, { recursive: true });
  fs.writeFileSync(
    path.join(userConfigDirectory, "config.yaml"),
    "apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\nregistries: []\n",
    { encoding: "utf8", mode: 0o600 }
  );

  const projectConfigDirectory = path.join(sandbox.repo, ".agents");
  fs.mkdirSync(projectConfigDirectory, { recursive: true });
  fs.writeFileSync(
    path.join(projectConfigDirectory, "config.yaml"),
    `apiVersion: agent-marketplace/v2\nregistries:\n  - url: ${pathToFileURL(bareRegistry).href}\n    ref: main\n    path: skills\n    default: true\n`,
    { encoding: "utf8", mode: 0o600 }
  );

  git(sandbox.repo, env, ["init", "--quiet", "--initial-branch=main"]);
  return { id: "local-bare-git-registry-v1", version: "1" };
};
