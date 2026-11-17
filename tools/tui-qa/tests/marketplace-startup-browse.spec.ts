import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { performance } from "node:perf_hooks";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { targetPID } from "./process-helpers.js";

// spec: tui-startup/cold-startup-browses-beyond-the-first-page
// spec: tui-startup/warm-startup-and-empty-endpoints-remain-usable
// spec: tui-startup/search-during-loading-and-clear-restore-complete-browsing
const journey = defineFixture("marketplace-startup-browse", ["tui"], 80, 24, { marketplaceDiscovery: "startup-browse" });
const warmJourney = defineFixture("marketplace-startup-browse-warm", ["tui"], 80, 24, { marketplaceDiscovery: "startup-browse" });
const searchJourney = defineFixture("marketplace-startup-browse-search", ["tui"], 80, 24, { marketplaceDiscovery: "startup-browse" });
before(journey.setup);
after(journey.cleanup);
before(warmJourney.setup);
after(warmJourney.cleanup);
before(searchJourney.setup);
after(searchJourney.cleanup);

const waitForBrowseLaterPage = async (fixture: ReturnType<typeof journey.current>): Promise<void> => {
  const wait = fixture.marketplaceWaitForBrowseLaterPage?.();
  assert.ok(wait, "fixture has no held-page signal");
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("later Marketplace page was not requested")), 5_000);
    wait.then(
      () => {
        clearTimeout(timer);
        resolve();
      },
      (error: unknown) => {
        clearTimeout(timer);
        reject(error);
      }
    );
  });
};

const watchPeakRSS = (pid: number): (() => number) => {
  let peakRSSKB = 0;
  let sampleError: Error | undefined;
  const sample = (): void => {
    if (sampleError) return;
    try {
      const rssKB = Number(execFileSync("ps", ["-o", "rss=", "-p", String(pid)], { encoding: "utf8" }).trim());
      if (!Number.isSafeInteger(rssKB) || rssKB <= 0) throw new Error(`target process ${pid} returned invalid RSS ${rssKB}`);
      peakRSSKB = Math.max(peakRSSKB, rssKB);
    } catch (error) {
      sampleError = error instanceof Error ? error : new Error(String(error));
    }
  };
  sample();
  const sampler = setInterval(sample, 100);
  return () => {
    clearInterval(sampler);
    if (sampleError) throw sampleError;
    assert.ok(peakRSSKB > 0, `target process ${pid} had no RSS samples`);
    return peakRSSKB;
  };
};

journey.test("startup progressively browses the complete Marketplace catalog without writes", async (terminal) => {
  const fixture = journey.current();
  const pid = await targetPID(fixture);
  const startedAt = fs.statSync(fixture.pidFile).mtimeMs;
  const watched = [
    path.join(fixture.home, ".agents", "config.yaml"),
    path.join(fixture.home, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".agents", "config.yaml"),
    path.join(fixture.repo, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md")
  ];
  const before = watched.map((file) => (fs.existsSync(file) ? fs.readFileSync(file) : undefined));
  const stopRSS = watchPeakRSS(pid);
  let coldFirstFrameMS = 0;
  let navigationAckMS = 0;
  try {
    await step("cold startup publishes the first page while page two is held", () => {}, async () => {
      await locate(terminal, "browse-resource-00").expect({ timeout: 15_000 });
      await waitForBrowseLaterPage(fixture);
      await locate(terminal, /Marketplace: 1 loading/).unique().expect();
    });
    coldFirstFrameMS = Date.now() - startedAt;

    await step("navigate to the last currently available resource", () => terminal.write("G"), async () => {
      await locate(terminal, /Name:\s+browse-resource-49/).unique().expect();
    });
    let navigationStarted = 0;
    await step("move the selection while page two remains held", () => {
      navigationStarted = performance.now();
      return terminal.write("k");
    }, async () => {
      await locate(terminal, /Name:\s+browse-resource-48/).unique().expect();
    });
    navigationAckMS = performance.now() - navigationStarted;

    fixture.marketplaceReleaseBrowsePages?.();
    await step("the released page finishes loading", () => {}, async () => {
      await locate(terminal, /Marketplace: 1 loading/).unique().expect({ not: true });
    });
    await step("reach the final resource without searching", () => terminal.write("G"), async () => {
      await locate(terminal, /Name:\s+browse-resource-60/).unique().expect({ timeout: 10_000 });
    });
    assert.equal(fixture.marketplaceBrowseRequestCount?.(), 2, "cold browsing did not fetch both pages exactly once");

    assert.equal(fixture.marketplaceSearchRequestCount?.(), 2, "cold browse issued unexpected search requests");
    for (let i = 0; i < watched.length; i++) {
      const file = watched[i];
      if (before[i] === undefined) {
        assert.equal(fs.existsSync(file), false, `browsing created ${file}`);
      } else {
        assert.deepEqual(fs.readFileSync(file), before[i], `browsing changed ${file}`);
      }
    }
  } finally {
    const peakRSSKB = stopRSS();
    console.log(
      `fresh-process cold startup measurements: first-useful-frame=${coldFirstFrameMS}ms navigation-ack=${navigationAckMS.toFixed(1)}ms ` +
        `search-requests=${fixture.marketplaceSearchRequestCount?.()} browse-page-requests=${fixture.marketplaceBrowseRequestCount?.()} ` +
        `peak-rss=${peakRSSKB}KiB`
    );
  }
});

// spec: tui-startup/search-during-loading-and-clear-restore-complete-browsing
searchJourney.test("search and clear preserve browse inventory while a later page is held", async (terminal) => {
  const fixture = searchJourney.current();
  const watched = [
    path.join(fixture.home, ".agents", "config.yaml"),
    path.join(fixture.home, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".agents", "config.yaml"),
    path.join(fixture.repo, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md")
  ];
  const before = watched.map((file) => (fs.existsSync(file) ? fs.readFileSync(file) : undefined));
  await step("the first browse page is visible while page two is held", () => {}, async () => {
    await locate(terminal, "browse-resource-00").expect({ timeout: 15_000 });
    await waitForBrowseLaterPage(fixture);
    await locate(terminal, /Marketplace: 1 loading/).unique().expect();
  });
  await step("open search without cancelling the held browse", () => terminal.write("/"), async () => {
    await locate(terminal, /\/\s{20,}/).unique().expect();
  });
  await step("type a Marketplace query", () => terminal.write("browse-resource-55"), async () => {
    await locate(terminal, "browse-resource-55").expect();
  });
  await step("apply the search while the browse page is held", () => terminal.write("\r"), async () => {
    await locate(terminal, "1 match · esc clear").expect();
    await locate(terminal, "browse-resource-55").expect();
  });
  fixture.marketplaceReleaseBrowsePages?.();
  await step("the held page arrives without replacing the query", () => {}, async () => {
    await locate(terminal, /Marketplace: 1 loading/).unique().expect({ not: true });
    await locate(terminal, "browse-resource-55").expect();
  });
  await step("clear search and restore the complete browse list", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "1 match · esc clear").expect({ not: true });
    await terminal.write("G");
    await locate(terminal, /Name:\s+browse-resource-60/).unique().expect({ timeout: 10_000 });
  });
  assert.equal(fixture.marketplaceBrowseRequestCount?.(), 2, "search interrupted or repeated browse pages");
  assert.equal(fixture.marketplaceSearchRequestCount?.(), 3, "search request count changed unexpectedly");
  for (let i = 0; i < watched.length; i++) {
    const file = watched[i];
    if (before[i] === undefined) {
      assert.equal(fs.existsSync(file), false, `browsing created ${file}`);
    } else {
      assert.deepEqual(fs.readFileSync(file), before[i], `browsing changed ${file}`);
    }
  }
});

// spec: tui-startup/warm-startup-and-empty-endpoints-remain-usable
warmJourney.test("prepare a complete browse catalog cache independently", async (terminal) => {
  const fixture = warmJourney.current();
  await step("warm-cache setup receives the first page and holds page two", () => {}, async () => {
    await locate(terminal, "browse-resource-00").expect({ timeout: 15_000 });
    await waitForBrowseLaterPage(fixture);
    await locate(terminal, /Marketplace: 1 loading/).unique().expect();
  });
  fixture.marketplaceReleaseBrowsePages?.();
  await step("warm-cache setup finishes the released page", () => {}, async () => {
    await locate(terminal, /Marketplace: 1 loading/).unique().expect({ not: true });
  });
  await step("warm-cache setup completes the catalog before exit", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+browse-resource-60/).unique().expect({ timeout: 10_000 });
  });
  assert.equal(fixture.marketplaceBrowseRequestCount?.(), 2, "warm-cache setup did not fetch both browse pages");
  await step("quit after seeding a complete catalog cache", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

warmJourney.test("warm startup displays the complete cached catalog in a fresh process", async (terminal) => {
  const fixture = warmJourney.current();
  const pid = await targetPID(fixture);
  const startedAt = fs.statSync(fixture.pidFile).mtimeMs;
  const searchRequestsBefore = fixture.marketplaceSearchRequestCount?.() ?? 0;
  const browseRequestsBefore = fixture.marketplaceBrowseRequestCount?.() ?? 0;
  const stopRSS = watchPeakRSS(pid);
  let firstFrameMS = 0;
  let navigationAckMS = 0;
  try {
    await step("fresh warm startup displays cached Marketplace rows", () => {}, async () => {
      await locate(terminal, "browse-resource-00").expect({ timeout: 15_000 });
    });
    firstFrameMS = Date.now() - startedAt;

    let navigationStarted = 0;
    await step("navigate to the last cached resource", () => {
      navigationStarted = performance.now();
      return terminal.write("G");
    }, async () => {
      await locate(terminal, /Name:\s+browse-resource-60/).unique().expect();
    });
    navigationAckMS = performance.now() - navigationStarted;
    assert.equal((fixture.marketplaceBrowseRequestCount?.() ?? 0) - browseRequestsBefore, 0, "fresh warm startup fetched browse pages");
  } finally {
    const peakRSSKB = stopRSS();
    console.log(
      `fresh-process warm startup measurements: first-useful-frame=${firstFrameMS}ms navigation-ack=${navigationAckMS.toFixed(1)}ms ` +
        `search-requests=${(fixture.marketplaceSearchRequestCount?.() ?? 0) - searchRequestsBefore} ` +
        `browse-page-requests=${(fixture.marketplaceBrowseRequestCount?.() ?? 0) - browseRequestsBefore} peak-rss=${peakRSSKB}KiB`
    );
  }
});
