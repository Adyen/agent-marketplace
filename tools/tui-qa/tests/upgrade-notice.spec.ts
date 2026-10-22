import assert from "node:assert/strict";
import fs from "node:fs";
import http from "node:http";
import type { AddressInfo } from "node:net";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: upgrade-notice/tui-shows-an-available-release-once
// spec: upgrade-notice/fresh-cache-avoids-network-access
const journey = defineFixture("upgrade-notice", ["tui"]);
const notice = "agent-marketplace v99.0.0 is available";
let server: http.Server | undefined;
let requests = 0;

// A release stub stands in for the GitHub releases API, and the e2e build
// reports a release version so the check compares it.
before(async () => {
  await journey.setup();
  server = http.createServer((_request, response) => {
    requests += 1;
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ tag_name: "v99.0.0", html_url: "https://example.invalid/releases/v99.0.0", published_at: new Date().toISOString() }));
  });
  await new Promise<void>((resolve) => server?.listen(0, "127.0.0.1", resolve));
  const fixture = journey.current();
  fixture.env.AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT = `http://127.0.0.1:${(server.address() as AddressInfo).port}/releases/latest`;
  fixture.env.AGENT_MARKETPLACE_E2E_VERSION = "v1.0.0";
});
after(async () => {
  await new Promise<void>((resolve) => (server ? server.close(() => resolve()) : resolve()));
  journey.cleanup();
});

const statePath = (): string => path.join(journey.current().stateDir, "update-check.json");

journey.test("the first launch looks the release up and caches it", async (terminal) => {
  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  const deadline = Date.now() + 10_000;
  while (!(fs.existsSync(statePath()) && fs.readFileSync(statePath(), "utf8").includes("v99.0.0"))) {
    assert.ok(Date.now() < deadline, `the release lookup was not cached in ${statePath()}`);
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  assert.equal(requests, 1, "the first launch must look the release up once");
  await step("the first launch knew no release yet, so it shows no notice", () => {}, async () => {
    await locate(terminal, notice).expect({ not: true });
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

journey.test("the next launch shows the newer release once from the cache", async (terminal) => {
  await step("the notice names the newer release and the running version", () => {}, async () => {
    await locate(terminal, notice).unique().expect({ timeout: 30_000 });
    await locate(terminal, "you have v1.0.0").unique().expect();
  });
  await step("the list renders normally beside it", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
  });
  assert.equal(requests, 1, "a fresh cache must not look the release up again");
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

journey.test("a launch within a day of the notice shows nothing", async (terminal) => {
  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  // The notice command runs right after the first frame and would have
  // shown within a second.
  await new Promise((resolve) => setTimeout(resolve, 2_000));
  await step("no notice", () => {}, async () => {
    await locate(terminal, notice).expect({ not: true });
  });
  assert.equal(requests, 1, "a fresh cache must not look the release up again");
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});
