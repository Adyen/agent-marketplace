import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { performance } from "node:perf_hooks";
import { after, before } from "node:test";

import { defineFixture, locate, seedStartupBrowseCache, step, waitForExit, waitForSignal } from "./helpers.js";
import { targetPID } from "./process-helpers.js";

// spec: tui-startup/cold-startup-browses-beyond-the-first-page
// spec: tui-startup/search-during-loading-and-clear-restore-complete-browsing
const journey = defineFixture("marketplace-startup-browse", ["tui"], 80, 24, { marketplaceDiscovery: "startup-browse" });
const warmJourney = defineFixture("marketplace-startup-browse-warm", ["tui"], 80, 24, { marketplaceDiscovery: "startup-browse" });
const searchJourney = defineFixture("marketplace-startup-browse-search", ["tui"], 80, 24, { marketplaceDiscovery: "startup-browse" });
type WatchedFiles = { paths: string[]; before: (Buffer | undefined)[] };
const watchedFiles = (fixture: ReturnType<typeof journey.current>): string[] => [
  path.join(fixture.home, ".agents", "config.yaml"),
  path.join(fixture.home, ".agents", "lock.yaml"),
  path.join(fixture.repo, ".agents", "config.yaml"),
  path.join(fixture.repo, ".agents", "lock.yaml"),
  path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md")
];
const snapshotWatchedFiles = (fixture: ReturnType<typeof journey.current>): WatchedFiles => {
  const paths = watchedFiles(fixture);
  return { paths, before: paths.map((file) => (fs.existsSync(file) ? fs.readFileSync(file) : undefined)) };
};
const assertWatchedFilesUnchanged = (snapshot: WatchedFiles): void => {
  for (let i = 0; i < snapshot.paths.length; i++) {
    const file = snapshot.paths[i];
    if (snapshot.before[i] === undefined) {
      assert.equal(fs.existsSync(file), false, `browsing created ${file}`);
    } else {
      assert.deepEqual(fs.readFileSync(file), snapshot.before[i], `browsing changed ${file}`);
    }
  }
};
let coldSnapshot: WatchedFiles;
let warmSnapshot: WatchedFiles;
let searchSnapshot: WatchedFiles;
before(async () => {
  await journey.setup();
  coldSnapshot = snapshotWatchedFiles(journey.current());
});
after(journey.cleanup);
before(async () => {
  await warmJourney.setup();
  await seedStartupBrowseCache(warmJourney.current());
  warmSnapshot = snapshotWatchedFiles(warmJourney.current());
});
after(warmJourney.cleanup);
before(async () => {
  await searchJourney.setup();
  searchSnapshot = snapshotWatchedFiles(searchJourney.current());
});
after(searchJourney.cleanup);

const waitForBrowseFirstPage = async (fixture: ReturnType<typeof journey.current>): Promise<void> => {
  const wait = fixture.marketplaceWaitForBrowseFirstPage?.();
  assert.ok(wait, "fixture has no held first-page signal");
  await waitForSignal(wait, "first Marketplace page");
};

const waitForBrowseLaterPage = async (fixture: ReturnType<typeof journey.current>): Promise<void> => {
  const wait = fixture.marketplaceWaitForBrowseLaterPage?.();
  assert.ok(wait, "fixture has no held-page signal");
  await waitForSignal(wait, "later Marketplace page");
};

const watchPeakRSS = (pid: number): (() => Promise<number>) => {
  let peakRSSKB = 0;
  let sampleError: Error | undefined;
  let sampleRunning = false;
  let stopped = false;
  let sampleInFlight: Promise<void> = Promise.resolve();
  const sample = (): void => {
    if (sampleError || sampleRunning || stopped) return;
    sampleRunning = true;
    sampleInFlight = new Promise<void>((resolve) => {
      execFile("ps", ["-o", "rss=", "-p", String(pid)], { encoding: "utf8", timeout: 5_000 }, (error, stdout) => {
        sampleRunning = false;
        if (stopped) {
          resolve();
          return;
        }
        if (error) {
          sampleError = error;
        } else {
          const rssKB = Number(stdout.trim());
          if (!Number.isSafeInteger(rssKB) || rssKB <= 0) {
            sampleError = new Error(`target process ${pid} returned invalid RSS ${rssKB}`);
          } else {
            peakRSSKB = Math.max(peakRSSKB, rssKB);
          }
        }
        resolve();
      });
    });
  };
  sample();
  const sampler = setInterval(sample, 100);
  return async () => {
    clearInterval(sampler);
    await sampleInFlight;
    stopped = true;
    if (sampleError) throw sampleError;
    assert.ok(peakRSSKB > 0, `target process ${pid} had no RSS samples`);
    return peakRSSKB;
  };
};

const finishRSSSampling = async (stop: () => Promise<number>, priorFailure: unknown): Promise<number | undefined> => {
  try {
    return await stop();
  } catch (error) {
    if (priorFailure !== undefined) {
      console.error(`RSS sampling also failed after journey failure: ${String(error)}`);
      return undefined;
    }
    throw error;
  }
};

journey.test("startup progressively browses the complete Marketplace catalog without writes", async (terminal) => {
  const fixture = journey.current();
  const pid = await targetPID(fixture);
  const startedAt = fixture.launchStartedAtMS ?? Date.now();
  const stopRSS = watchPeakRSS(pid);
  let coldFirstFrameMS = 0;
  let navigationAckMS = 0;
  let priorFailure: unknown;
  try {
    await step("cold startup reports progress before the first page arrives", () => {}, async () => {
      await waitForBrowseFirstPage(fixture);
      await locate(terminal, /Marketplace: 1 loading/).unique().expect();
      await locate(terminal, "browse-resource-00").expect({ not: true });
    });
    fixture.marketplaceReleaseBrowseFirstPage?.();
    await step("cold startup publishes the first page while page two is held", () => {}, async () => {
      await locate(terminal, "browse-resource-00").expect({ timeout: 15_000 });
    });
    coldFirstFrameMS = Date.now() - startedAt;
    await step("the second page stays held after the first page is usable", () => {}, async () => {
      await waitForBrowseLaterPage(fixture);
      await locate(terminal, /Marketplace: 1 loading/).unique().expect();
      await locate(terminal, /~ in sync/).unique().expect();
    });

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
    assertWatchedFilesUnchanged(coldSnapshot);
  } catch (error) {
    priorFailure = error;
    throw error;
  } finally {
    const peakRSSKB = await finishRSSSampling(stopRSS, priorFailure);
    if (peakRSSKB !== undefined) {
      console.log(
        `fresh-process cold startup measurements: first-useful-frame=${coldFirstFrameMS}ms navigation-ack=${navigationAckMS.toFixed(1)}ms ` +
          `search-requests=${fixture.marketplaceSearchRequestCount?.()} browse-page-requests=${fixture.marketplaceBrowseRequestCount?.()} ` +
          `peak-rss=${peakRSSKB}KiB`
      );
    }
  }
});

// spec: tui-startup/search-during-loading-and-clear-restore-complete-browsing
searchJourney.test("search and clear preserve browse inventory while a later page is held", async (terminal) => {
  const fixture = searchJourney.current();
  await step("startup reports progress before the first browse page", () => {}, async () => {
    await waitForBrowseFirstPage(fixture);
    await locate(terminal, /Marketplace: 1 loading/).unique().expect();
    await locate(terminal, "browse-resource-00").expect({ not: true });
  });
  fixture.marketplaceReleaseBrowseFirstPage?.();
  await step("the first browse page is visible while page two is held", () => {}, async () => {
    await locate(terminal, "browse-resource-00").expect({ timeout: 15_000 });
    await waitForBrowseLaterPage(fixture);
    await locate(terminal, /Marketplace: 1 loading/).unique().expect();
    await locate(terminal, /~ in sync/).unique().expect();
  });
  await step("open search without cancelling the held browse", () => terminal.write("/"), async () => {
    await locate(terminal, /\/\s{20,}/).unique().expect();
  });
  await step("type a query that exceeds the per-endpoint search limit", () => terminal.write("browse-resource"), async () => {
    await locate(terminal, "browse-resource").expect();
  });
  await step("the limited search and browse loading feedback remain visible", () => terminal.write("\r"), async () => {
    await locate(terminal, "50 matches · esc clear").expect();
    await locate(terminal, /Marketplace: 1 loading/).unique().expect();
    await locate(terminal, "browse-resource-00").expect();
  });
  await step("a wider footer keeps both search and browse progress visible", () => terminal.resize(120, 24), async () => {
    await locate(terminal, /Marketplace: 1 loading/).unique().expect();
    await locate(terminal, /Marketplace: 1 limited/).unique().expect();
  });
  fixture.marketplaceReleaseBrowsePages?.();
  await step("the held page arrives without replacing the query", () => {}, async () => {
    await locate(terminal, /Marketplace: 1 loading/).unique().expect({ not: true });
    await locate(terminal, "61 matches · esc clear").expect();
    await locate(terminal, /Marketplace: 1 limited/).unique().expect();
  });
  await step("clear search and restore the complete browse list", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "61 matches · esc clear").expect({ not: true });
    await terminal.write("G");
    await locate(terminal, /Name:\s+browse-resource-60/).unique().expect({ timeout: 10_000 });
  });
  assert.equal(fixture.marketplaceBrowseRequestCount?.(), 2, "search interrupted or repeated browse pages");
  assert.equal(fixture.marketplaceSearchRequestCount?.(), 3, "search request count changed unexpectedly");
  assertWatchedFilesUnchanged(searchSnapshot);
});

// spec: tui-startup/warm-startup-and-empty-endpoints-remain-usable
warmJourney.test("warm startup displays the complete cached catalog in a fresh process", async (terminal) => {
  const fixture = warmJourney.current();
  const pid = await targetPID(fixture);
  const startedAt = fixture.launchStartedAtMS ?? Date.now();
  const searchRequestsBefore = fixture.marketplaceSearchRequestsAtLaunch ?? 0;
  const browseRequestsBefore = fixture.marketplaceBrowseRequestsAtLaunch ?? 0;
  const stopRSS = watchPeakRSS(pid);
  let firstFrameMS = 0;
  let navigationAckMS = 0;
  let priorFailure: unknown;
  try {
    await step("fresh warm startup displays cached Marketplace rows", () => {}, async () => {
      await locate(terminal, "browse-resource-00").expect({ timeout: 15_000 });
      await locate(terminal, /~ in sync/).unique().expect();
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
    assertWatchedFilesUnchanged(warmSnapshot);
  } catch (error) {
    priorFailure = error;
    throw error;
  } finally {
    const peakRSSKB = await finishRSSSampling(stopRSS, priorFailure);
    if (peakRSSKB !== undefined) {
      console.log(
        `fresh-process warm startup measurements: first-useful-frame=${firstFrameMS}ms navigation-ack=${navigationAckMS.toFixed(1)}ms ` +
          `search-requests=${(fixture.marketplaceSearchRequestCount?.() ?? 0) - searchRequestsBefore} ` +
          `browse-page-requests=${(fixture.marketplaceBrowseRequestCount?.() ?? 0) - browseRequestsBefore} peak-rss=${peakRSSKB}KiB`
      );
    }
  }
});
