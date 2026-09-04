import assert from "node:assert/strict";
import { test } from "node:test";

import { createFixture, runNonTTY } from "./helpers.js";

test("non-TTY entrypoints print help or fail promptly", (t) => {
  const fixture = createFixture("non-tty");
  t.after(fixture.cleanup);

  const bare = runNonTTY(fixture, []);
  assert.equal(bare.status, 0, `${bare.stdout}\n${bare.stderr}`);
  assert.match(`${bare.stdout}\n${bare.stderr}`, /Usage|Available Commands/);

  const explicit = runNonTTY(fixture, ["tui"]);
  assert.notEqual(explicit.status, 0);
  assert.match(`${explicit.stdout}\n${explicit.stderr}`, /terminal|tty|interactive/i);
});
