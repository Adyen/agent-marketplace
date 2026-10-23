import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

const journey = defineFixture("resize-and-recover", ["tui"]);
before(journey.setup);
after(journey.cleanup);

// spec: tui-rendering/undersized-terminal
// spec: tui-mouse-navigation/wheel-input-below-the-minimum-terminal-size-is-ignored
journey.test("minimum-size gate preserves selection and blocks toggles", async (terminal) => {
  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+available/).unique().expect();
  });

  await step("resize below minimum", () => terminal.resize(49, 11), async () => {
    await locate(terminal, "Resize required").unique().expect();
    await locate(terminal, "Minimum terminal: 50x12").unique().expect();
    await locate(terminal, "q quit · Ctrl+C quit").unique().expect();
    await locate(terminal, "alpha-skill").expect({ not: true });
    await locate(terminal, "beta-skill").expect({ not: true });
  });

  const stillResizeRequired = async (): Promise<void> => {
    await locate(terminal, "Resize required").unique().expect();
    await locate(terminal, "Minimum terminal: 50x12").unique().expect();
    await locate(terminal, "q quit · Ctrl+C quit").unique().expect();
    await locate(terminal, "toggle local").expect({ not: true });
    await locate(terminal, "alpha-skill").expect({ not: true });
    await locate(terminal, "beta-skill").expect({ not: true });
  };
  await step("ignore toggle below minimum", async () => {
    await terminal.write(" ");
    await new Promise((resolve) => setTimeout(resolve, 250));
  }, stillResizeRequired);
  // Wheel up would select the Available header above alpha-skill; recovery
  // below shows the selection unchanged.
  await step("ignore wheel below minimum", async () => {
    await terminal.write("\x1b[<64;4;6M");
    await new Promise((resolve) => setTimeout(resolve, 250));
  }, stillResizeRequired);

  await step("recover at exact minimum", () => terminal.resize(50, 12), async () => {
    await locate(terminal, "Resize required").expect({ not: true });
    await locate(terminal, /Available\s+\(1\)/).unique().expect();
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /: (commands|cmds) · \? help · q quit/).unique().expect();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});
