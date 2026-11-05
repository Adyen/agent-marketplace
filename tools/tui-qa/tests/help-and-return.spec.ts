import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

const journey = defineFixture("help-and-return", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("help closes back to the selected alpha-skill", async (terminal) => {
  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+available/).unique().expect();
  });

  await step("open keyboard shortcuts", () => terminal.write("?"), async () => {
    await locate(terminal, "agent-marketplace — Keyboard Shortcuts").unique().expect();
    await locate(terminal, "jump to bottom").unique().expect();
    await locate(terminal, "fuzzy search").unique().expect();
    await locate(terminal, "clear search / close").unique().expect();
  });

  await step("close keyboard shortcuts", () => terminal.write("?"), async () => {
    await locate(terminal, "agent-marketplace — Keyboard Shortcuts").expect({ not: true });
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+available/).unique().expect();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

const narrow = defineFixture("narrow-help", ["tui"], 50, 12);
before(narrow.setup);
after(narrow.cleanup);

// spec: tui-navigation/narrow-help
narrow.test("every help binding is reachable at 50x12 and reopening retains the final scroll position", async (terminal) => {
  await step("settled minimum-size list", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("select alpha-skill at minimum size", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("open narrow help", () => terminal.write("?"), async () => {
    await locate(terminal, "Keyboard Shortcuts").unique().expect();
    await locate(terminal, "Navigation").unique().expect();
  });
  const seen: string[] = [];
  let bottom = "";
  for (let line = 0; line < 120; line += 1) {
    const screen = await terminal.text();
    seen.push(screen);
    await locate(terminal, "esc/? close").unique().expect();
    await locate(terminal, "Ctrl+C quit").unique().expect();
    if (screen.includes("Press esc or ? to close")) {
      bottom = screen;
      break;
    }
    const oldScreen = locate(terminal, new RegExp(screen.trim().split(/\s+/)
      .map((word) => word.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("\\s+")));
    await oldScreen.expect();
    await step(`scroll narrow help line ${line + 1}`, () => terminal.write("j"), async () => {
      await oldScreen.expect({ not: true });
      assert.deepEqual((await terminal.text()).split("\n").slice(0, -2), screen.split("\n").slice(1, -1),
        "each help scroll must expose one new line above the fixed footer");
    });
  }
  assert.notEqual(bottom, "", "the final help group must be reachable by scrolling");
  // Reconstruct the observed content once, without overlapping frames or the fixed footer.
  const content = seen.flatMap((screen, index) => {
    const lines = screen.split("\n").slice(0, -1);
    return index === 0 ? lines : lines.slice(-1);
  }).join("\n");
  const groups = [
    { title: "Navigation", bindings: [
      "\u2191/k move up", "\u2193/j move down", "ctrl+d half-page down", "ctrl+u half-page up",
      "f/ctrl+f/pgdn page down", "b/ctrl+b/pgup page up", "g/home jump to top", "G/end jump to bottom"
    ] },
    { title: "Actions", bindings: [
      "/ fuzzy search", "esc clear search / close", "enter view details / toggle fold",
      "space toggle resource / all in section", "h/\u2190 collapse section", "l/\u2192 expand section",
      "za toggle current fold", "zM collapse all", "zR expand all", "v view settings",
      "H harness defaults", "E edit config", "s sync pending", "u update caches", "ctrl+k/: command palette"
    ] },
    { title: "Toggle Modal", bindings: [
      "g toggle global", "p toggle project", "l toggle local", "e toggle local exclude", "esc cancel"
    ] },
    { title: "Harness Modal", bindings: [
      "g edit global", "p edit project", "space toggle resource / all in section", "enter save", "esc cancel"
    ] },
    { title: "Edit Config Modal", bindings: ["g edit global", "p edit project", "l edit local", "esc cancel"] },
    { title: "Other", bindings: ["r reload from disk", "e view errors", "? this help", "q quit"] }
  ];
  for (const [index, group] of groups.entries()) {
    const start = content.indexOf(`\n${group.title}\n`);
    const end = index + 1 < groups.length ? content.indexOf(`\n${groups[index + 1].title}\n`) : content.length;
    assert.ok(start >= 0 && end > start, `unreachable or misordered help group: ${group.title}`);
    const visible = content.slice(start, end).replace(/\s+/g, " ");
    for (const binding of group.bindings) {
      assert.ok(visible.includes(binding), `unreachable ${group.title} binding: ${binding}`);
    }
  }
  await step("close narrow help", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Press esc or ? to close").expect({ not: true });
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("reopen at the same final offset", () => terminal.write("?"), async () => {
    await locate(terminal, "Press esc or ? to close").unique().expect();
    assert.equal(await terminal.text(), bottom);
  });
  await step("hard quit remains reachable from narrow help", () => terminal.write("\x03"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});
