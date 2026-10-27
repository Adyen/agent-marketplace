import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runAM } from "./process-helpers.js";

// spec: tui-feedback/toggle-hints-follow-row-eligibility
// spec: session-feedback/notification-overflow
const journey = defineFixture("required-toggle-refusal", ["tui"], 80, 24, { organization: "published-lock" });
before(journey.setup);
after(journey.cleanup);

journey.test("space on a required organization resource is refused without writes", async (terminal) => {
  const fixture = journey.current();
  const { orgConfigPath, orgLockPath } = fixture;
  assert.ok(orgConfigPath, "organization fixture must expose its config path");
  assert.ok(orgLockPath, "organization fixture must expose its lock path");
  const orgReview = path.join(fixture.home, ".factory", "skills", "org-review");
  const watched = [
    orgConfigPath,
    orgLockPath,
    path.join(fixture.home, ".agents", "config.yaml"),
    path.join(fixture.home, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".agents", "config.yaml"),
    path.join(fixture.repo, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".agents", "config.local.yaml")
  ];

  await step("settled baseline with the required organization resource", () => {}, async () => {
    await locate(terminal, "org-review").expect();
    await locate(terminal, /Required\s+\(1\)/).unique().expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  const snapshot = watched.map((file) => ({ file, bytes: fs.existsSync(file) ? fs.readFileSync(file) : undefined }));
  const link = fs.readlinkSync(orgReview);

  await step("select the required resource", () => terminal.write("j"), async () => {
    await locate(terminal, /Name:\s+org-review/).unique().expect();
    await locate(terminal, /Status:\s+required \(org\)/).unique().expect();
    // Neither the detail pane nor the footer offers a toggle the row refuses.
    await locate(terminal, "enter = full view").unique().expect();
    await locate(terminal, "space = toggle").expect({ not: true });
    await locate(terminal, "space toggle").expect({ not: true });
  });
  const firstNoticeStarted = performance.now();
  await step("space is refused with an explanation and no scope modal", () => terminal.write(" "), async () => {
    await locate(terminal, "required items cannot be toggled").expect();
    await locate(terminal, "toggle project").expect({ not: true });
    await locate(terminal, /Status:\s+required \(org\)/).unique().expect();
    await locate(terminal, "~ in sync").unique().expect();
  });
  for (let count = 1; count <= 3; count += 1) {
    await step(`in-sync confirmation ${count} joins the notification stack`, () => terminal.write("s"), async () => {
      await locate(terminal, "already in sync").nth(count - 1).expect();
      assert.equal(await locate(terminal, "already in sync").count(), count);
      if (count < 3) await locate(terminal, "required items cannot be toggled").unique().expect();
    });
  }
  await step("the fourth notice evicts the first and leaves three visible", () => {}, async () => {
    assert.equal(await locate(terminal, "required items cannot be toggled").count(), 0, "the oldest notice must be evicted, not awaited until expiry");
    assert.equal(await locate(terminal, "already in sync").count(), 3);
    assert.ok(performance.now() - firstNoticeStarted < 2_000, "all four notifications and the cap check must precede the first two-second expiry");
    await locate(terminal, /Name:\s+org-review/).unique().expect();
    await locate(terminal, /: (commands|cmds) · \? help · q quit/).unique().expect();
  });
  await step("all three notifications expire without further input", () => {}, async () => {
    await locate(terminal, "already in sync").expect({ not: true });
    await locate(terminal, /Status:\s+required \(org\)/).unique().expect();
    await locate(terminal, "~ in sync").unique().expect();
  });
  for (const { file, bytes } of snapshot) {
    if (bytes === undefined) assert.equal(fs.existsSync(file), false, `the refused toggle created ${file}`);
    else assert.deepEqual(fs.readFileSync(file), bytes, `the refused toggle changed ${file}`);
  }
  assert.equal(fs.readlinkSync(orgReview), link, "the refused toggle re-pointed the installed org-review");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  // The drift check talks to the organization Marketplace stub, so it uses the
  // event-loop-free runner.
  const check = await runAM(fixture, ["sync", "--check"]);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});
