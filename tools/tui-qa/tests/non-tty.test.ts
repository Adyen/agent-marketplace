import assert from "node:assert/strict";
import { test } from "node:test";

import { createProcessFixture, runNonTTY } from "./process-helpers.js";

test("non-TTY entrypoints print help or fail promptly", (t) => {
  const fixture = createProcessFixture("non-tty");
  t.after(fixture.cleanup);

  const bare = runNonTTY(fixture, []);
  assert.equal(bare.error, undefined);
  assert.equal(bare.status, 0, `${bare.stdout}\n${bare.stderr}`);
  assert.match(`${bare.stdout}\n${bare.stderr}`, /Usage:\s+agent-marketplace/);
  assert.match(`${bare.stdout}\n${bare.stderr}`, /Available Commands:/);

  const explicit = runNonTTY(fixture, ["tui"]);
  assert.equal(explicit.error, undefined);
  assert.notEqual(explicit.status, 0);
  assert.match(`${explicit.stdout}\n${explicit.stderr}`, /interactive terminal is required|could not open( a new)? TTY/i);
});
