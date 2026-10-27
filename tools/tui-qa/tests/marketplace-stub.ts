import { createHash } from "node:crypto";
import http from "node:http";
import type { AddressInfo } from "node:net";
import { gunzipSync, gzipSync } from "node:zlib";

export interface MarketplaceStub {
  url: string;
  instanceID: string;
  registryID: string;
  registrySlug: string;
  resourceName: string;
  contentDigest: string;
  archiveDigest: string;
  materializedSkillContent: string;
  driftedArchiveDigest: string;
  acquisitionDataRequestCount(): number;
  acquisitionDataRequestPaths(): string[];
  archiveFetches(): { placeholder: string[]; final: string[] };
  delayNextAcquisition(ms: number): void;
  delayedAcquisitionCount(): number;
  driftArchiveDigest(): void;
  close(): void;
}

const wellKnownPath = "/.well-known/agent-marketplace.json";
const searchPath = "/api/v1/search";
const registryID = "tui-qa-registry";
const registrySlug = "tools";
const resourceName = "org-review";
const contentDigest = "sha256:9bb07ca2a6db2f0a63c9c80cd2c86520ad2a69545e32b4ea4b0e2eddd24d85ad";
const materializedSkillContent =
  "---\nname: org-review\ndescription: Organization review fixture\n---\nReview organization changes carefully.";

const inspectionSkillName = "pack-notes-with-a-much-longer-catalog-row-label";
const sha256 = (data: string | Buffer): string => `sha256:${createHash("sha256").update(data).digest("hex")}`;
const inspectionRevision = `sha256:${"a0".repeat(32)}`;
const inspectionPrimary = "# Pack Notes\n\nRemote inspection primary copy.\n\nUnicode marker: αβ日本語 selection tail.\n";
const inspectionSupport =
  "# Notes\n\nRemote supporting notes copy.\n\n" +
  Array.from({ length: 60 }, (_, index) => `Support line ${String(index + 1).padStart(2, "0")}`).join("\n\n") +
  "\n";
const inspectionFile = (filePath: string, body: string): { path: string; content_digest: string; media_type: string; size: number; inspection_class: string } => ({
  path: filePath,
  content_digest: sha256(body),
  media_type: "text/markdown",
  size: Buffer.byteLength(body),
  inspection_class: "text"
});
const inspectionFiles = [inspectionFile("SKILL.md", inspectionPrimary), inspectionFile("support/notes.md", inspectionSupport)];
const inspectionCoordinate = { marketplace: "tui-qa-marketplace", registry: registryID, kind: "skill", name: inspectionSkillName };
const inspectionPayload = JSON.stringify({
  schema_version: "v1",
  coordinate: inspectionCoordinate,
  revision_digest: inspectionRevision,
  primary_path: "SKILL.md",
  files: inspectionFiles
});
const inspectionBodies = new Map<string, { media: string; body: string }>([
  [inspectionFiles[0].content_digest, { media: "text/markdown", body: inspectionPrimary }],
  [inspectionFiles[1].content_digest, { media: "text/markdown", body: inspectionSupport }]
]);
const inspectionAssessmentDigest = sha256(
  JSON.stringify({
    coordinate: inspectionCoordinate,
    content_digest: inspectionRevision,
    analyzer_version: "agent-marketplace-tui-qa/v1",
    policy_digest: `sha256:${"cc".repeat(32)}`,
    decision: "allow",
    findings: [],
    inspection_manifest_digest: sha256(inspectionPayload)
  })
);
const inspectionRevisionDoc = {
  coordinate: inspectionCoordinate,
  content_digest: inspectionRevision,
  assessment: {
    coordinate: inspectionCoordinate,
    content_digest: inspectionRevision,
    assessment_digest: inspectionAssessmentDigest,
    analyzer_version: "agent-marketplace-tui-qa/v1",
    policy_digest: `sha256:${"cc".repeat(32)}`,
    decision: "allow",
    inspection_manifest_digest: sha256(inspectionPayload),
    assessed_at: "1970-01-01T00:00:01Z"
  },
  occurrence: {
    coordinate: inspectionCoordinate,
    content_digest: inspectionRevision,
    commit: "f".repeat(40),
    manifest_digest: `sha256:${"b1".repeat(32)}`
  }
};
const inspectionSearchItem = {
  registry_id: registryID,
  registry_slug: registrySlug,
  kind: "skill",
  name: inspectionSkillName,
  revision: inspectionRevision,
  content_digest: inspectionRevision,
  description: "Uninstalled inspection fixture",
  availability: "available"
};
const artifactMediaType = "application/vnd.agent-marketplace.artifact.v1.tar+gzip";
const archiveDigestPlaceholder = `sha256:${"0".repeat(64)}`;

interface ServedArchive {
  digest: string;
  placeholder: Buffer;
  final: Buffer;
  // The placeholder form compressed differently: a valid archive with the
  // same content whose bytes do not hash to the digest.
  mismatched: Buffer;
}

export interface MarketplaceStubOptions {
  // A journey can hold acquisition until its pending-state checks finish.
  acquisitionGate?: () => Promise<void>;
  // Serve placeholder-form bytes that do not hash to the requested digest.
  mismatchedPlaceholderArchive?: boolean;
  // Answer a query matching the installable skill with its exact revision.
  searchableInstall?: boolean;
  // Assess the installable skill with one warning finding, so installing it
  // requires consent.
  installFindings?: boolean;
}

// servedArchive serves a canonical artifact the way a current Marketplace
// does: the placeholder form, whose manifest carries the zeroed archive digest
// and whose bytes hash to the archive digest, and the final form, whose
// manifest carries that digest. Node's zlib compresses both, which no Go
// toolchain reproduces, so a client that verifies by re-encoding fails under
// every Go version and only one that hashes the received bytes succeeds.
const servedArchive = (canonical: Buffer): ServedArchive => {
  const tar = gunzipSync(canonical);
  const field = Buffer.from('"archive_digest":"');
  const at = tar.indexOf(field) + field.length;
  if (at < field.length || tar.indexOf(field, at) >= 0 || !/^sha256:[0-9a-f]{64}$/.test(tar.toString("latin1", at, at + archiveDigestPlaceholder.length))) {
    throw new Error("canonical artifact must carry exactly one manifest archive_digest");
  }
  // Digests share one length, so rewriting the value in place keeps every tar header valid.
  const withDigest = (digest: string): Buffer => {
    const copy = Buffer.from(tar);
    copy.write(digest, at, "latin1");
    return copy;
  };
  const placeholder = gzipSync(withDigest(archiveDigestPlaceholder), { level: 9 });
  const mismatched = gzipSync(withDigest(archiveDigestPlaceholder), { level: 1 });
  if (mismatched.equals(placeholder)) throw new Error("mismatched placeholder archive must differ from the placeholder");
  const digest = sha256(placeholder);
  return { digest, placeholder, final: gzipSync(withDigest(digest), { level: 9 }), mismatched };
};

// Declared support is staged separately from the canonical skill definition.
const original = servedArchive(Buffer.from(
  "H4sIAAAAAAAE/+xVS2/cNhDes3+FML3KDqm3eHPRBj30hTjoJQgWI3KkZc0ltyQlx13sfy+w8rqpgyBB7TUQePkdRhgSo3l+s0arewrx4s/g7OI4hzHGqqJYsPk8lKyqqoNuvue85tUiYcdw5iHGENEv2KP/NfvODoE80tqzYQtBrmiNy4l80M6CgIlDCuij7lHGh/prbRUICNfaGEjB4ppAgPPDuadJ0w2k4GnQIfrbpVYgII76/C88Pyg/vg9mHEBAdM6EvX7Sdy5ACtLZSDYulR4oRBAQVpiVlWi7jtUSM6xUl/UMq1y2smFSZbKpyoyhyrBqy6KkPOsKwqJjlJFSKitUU6LaxyZXeqJPTPOOq6Zuc+ScFXVRSKyLtuA9l01bFllT5WVb8EY2Equ84HnHZFE3dduWhJ3qOKSw8W4ii1YSiC0MZMljdB4E4EA2nq/RX1PcGJSUSLTOaokmQR91jzImE4ddCmHcbJyPpJZBrmiNy4l80M4GEO9g4vB+93Q94im40Ut6EmOfAWOM1WV5PxqzuJfzzM9y/ub750n5OYNPiRc+/4f6v9pP9HG2wJf4v6iqB/XPWZkvkkdz8tfghdd/C5cD2QgCIIVLY9wNqbfOmQDCjsakcOmHcU02/qQPr7536hYEvKFJ003i/IBW/41RO5vIFdqBQiLRUz8ac3sBKfxAQXq9idpZEPDbx+/nnZH0+kMcPUEKP36IILa7FH5xigwIgBR+J7/WIWhnA4jt7j+KK4ogejSBUrjaExmILbzWhuYt8rO2BILtUrgaO/w30qtxs3E+vtaGAoh3YF2kcLFW8D6Ft14PA/n7DHy643Zf01jfBsKciKM6+z/4vyqyE/8/B//f1f/VYQCOkfIv8T+rssW8+vdYMJ7lNT/x/3Pw/3fJm5mDf3WRwtnZH+R1r0kld42h7ZBoG6IfZdTOhouzY3TICSeccMIJz41/BgDzDB/5ABQAAA==",
  "base64"
));
// The same content digest with a different archive: an extra asset stands in
// for a repaired artifact encoding.
const drifted = servedArchive(Buffer.from(
  "H4sIAAAAAAAE/+xXTW/jNhD1Ob9CmF5lh6Ik2uItRbvooV/YLHpZLAyKHMlsaNIlKSWp4f9ewIrTbRaLbDe2gUWkd6AxIsZDvpk3o7WwusEQZ38GZyeneQghhBXFhAzP05Uwxg624X2WzTM2ScgpgnmKLkThJ+TF/zXETg4HeaG3s2ELQa5wLZY9+qCdBQ59BikIH3UjZHxqv9FWAYdwo42BFKxYI3Bwvp167DXeQgoeWx2iv19qBRxip6d/ienB+PH7YLoWOETnTNjbe/0QAqQgnY1o41LpFkMEDmElaMl4VddkLgUVTNW0IYLlspILIhWVC1ZSIhQVrCqLEnNaFyiKmiBFpRQt1KIUan82udI9fuI6q2qKuVC0quYScykrxjJKCS3ofLEoi2YhGyaLnDUVkUWZiTxnNW0WVY1zpVQDKWy869EKKxH4Flq06EV0HjiIFm2croW/wbgxQmIihXVWS2ES4aNuhIxJn8EuhdBtNs5HVMsgV7gWyx590M4G4O+hz+DD7ng54jG4zks8irPPgBBC5mX5WBrD8rgONT+sw+9svz0pP+fwmHjl9X/g/3Jf0afpAs/pf8HYE/5zUuaT5MWa/CV45fxv4apFG4EDpHBljLtF9c45E4DbzpgUrnzbrdHGn/Rh1/dO3QOHt9hrvE2cb4XVf4uonU3kStgWQyKFx6Yz5n4GKfyAQXq9idpZ4PDbx/uHnpE0+i52HiGFH+8i8O0uhV+cQgMcIIXf0a91CNrZAHy7+4/hGiPwRpiAKVzvhQz4Ft5og0MX+VlbBE52KVx3tfj3pNfdZuN8fKMNBuDvwbqIYbZW8CGFd163LfrHG/i0x+2+JLG+DYgQMIbTxvoV+l/kZNT/c+j/wP+l8rqJ02wW7+LxL/05/SdkPhla/x4TktEyG/X/LPq/Jz7Jjk/6iG8CYWiEJ2VrX+H/b/5nBR31/xz6/8D/5WEAOkUiPKv/jB5y4+H7j+bzbJz/zzH/f5e8HWbwX13EcHHxB3rdaFTJQ2Jo2ybahug7GbWzYXZxigwZMWLEiBHnxj8DAD6t0iUAGgAA",
  "base64"
));
const archiveDigest = original.digest;
const driftedArchiveDigest = drifted.digest;
const servedArchives = new Map([original, drifted].map((archive) => [archive.digest, archive]));
const revision = {
  coordinate: {
    marketplace: "tui-qa-marketplace",
    registry: registryID,
    kind: "skill",
    name: resourceName
  },
  content_digest: contentDigest,
  archive_digest: archiveDigest,
  assessment: {
    coordinate: {
      marketplace: "tui-qa-marketplace",
      registry: registryID,
      kind: "skill",
      name: resourceName
    },
    content_digest: contentDigest,
    assessment_digest: "sha256:617ddc3e26f8d6105a794138aec92096efe0f30a9f7623e26545dc89adbdc549",
    analyzer_version: "agent-marketplace-tui-qa/v1",
    policy_digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
    decision: "allow",
    inspection_manifest_digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
    assessed_at: "1970-01-01T00:00:01Z"
  },
  occurrence: {
    coordinate: {
      marketplace: "tui-qa-marketplace",
      registry: registryID,
      kind: "skill",
      name: resourceName
    },
    content_digest: contentDigest,
    commit: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
    manifest_digest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
  }
};

export const installFinding = {
  code: "tui-qa.shell-access",
  severity: "medium",
  policy: "warn",
  message: "Fixture skill runs shell commands"
};
// The digest covers the same fields, in the same order, as the Go canonical
// assessment identity (pkg/marketplace/store/assessment.go).
const findingsAssessment = {
  ...revision.assessment,
  decision: "warn",
  findings: [installFinding],
  assessment_digest: sha256(
    JSON.stringify({
      coordinate: revision.assessment.coordinate,
      content_digest: revision.assessment.content_digest,
      analyzer_version: revision.assessment.analyzer_version,
      policy_digest: revision.assessment.policy_digest,
      decision: "warn",
      findings: [installFinding],
      inspection_manifest_digest: revision.assessment.inspection_manifest_digest
    })
  )
};
const installSearchItem = {
  registry_id: registryID,
  registry_slug: registrySlug,
  kind: "skill",
  name: resourceName,
  revision: contentDigest,
  content_digest: contentDigest,
  description: "Organization review fixture",
  availability: "available"
};

// A loopback stand-in for a hosted Marketplace endpoint. It serves one exact,
// assessed skill and both forms of its canonical archive so a missing
// organization lock exercises acquisition, verification by received bytes,
// resolution, and materialization.
export const createMarketplaceStub = async (options: MarketplaceStubOptions = {}): Promise<MarketplaceStub> => {
  const instanceID = "tui-qa-marketplace";
  const acquisitionDataRequests: string[] = [];
  let nextAcquisitionDelayMs = 0;
  let delayedAcquisitions = 0;
  let servedArchiveDigest = archiveDigest;
  const server = http.createServer(async (request, response) => {
    const url = new URL(request.url ?? "/", "http://127.0.0.1");
    if (/^\/api\/v1\/(?:registries|resources|archives|artifacts|manifests)\//.test(url.pathname)) {
      acquisitionDataRequests.push(url.pathname);
      if (options.acquisitionGate) {
        await options.acquisitionGate();
        if (response.destroyed) return;
      }
      if (nextAcquisitionDelayMs > 0) {
        const delay = nextAcquisitionDelayMs;
        nextAcquisitionDelayMs = 0;
        delayedAcquisitions++;
        await new Promise<void>((resolve) => setTimeout(resolve, delay));
      }
    }
    if (request.method === "GET" && url.pathname === wellKnownPath) {
      // Mirrors the wire shape of the Go API type: the capabilities fields are
      // promoted to the top level of the document.
      response.writeHead(200, { "content-type": "application/json" });
      response.end(
        JSON.stringify({
          instance_id: instanceID,
          version: "v1",
          canonical_url: "https://example.com",
          api_base: "/api/v1",
          api_versions: ["v1"],
          artifact_schema_versions: ["v1"],
          resource_kinds: ["skill", "command", "subagent", "hook", "mcpserver", "collection"],
          acquisition_modes: ["mirror", "index-only"],
          features: []
        })
      );
      return;
    }
    if (request.method === "GET" && url.pathname === searchPath) {
      response.writeHead(200, { "content-type": "application/json" });
      const offset = Number(url.searchParams.get("offset") ?? "") || 0;
      const limit = Number(url.searchParams.get("limit") ?? "") || 0;
      const terms = (url.searchParams.get("q") ?? "").toLowerCase().split(/\s+/).filter(Boolean);
      const items = [
        ...(terms.some((term) => inspectionSkillName.includes(term)) ? [inspectionSearchItem] : []),
        ...(options.searchableInstall && terms.some((term) => resourceName.includes(term)) ? [installSearchItem] : [])
      ];
      response.end(JSON.stringify({ items, total: items.length, offset, limit }));
      return;
    }
    if (request.method === "GET" && url.pathname.includes(`/skill/${inspectionSkillName}`)) {
      const inspectionPrefix = `/api/v1/resources/${registryID}/skill/${inspectionSkillName}/revisions/${inspectionRevision}/inspection`;
      if (url.pathname === inspectionPrefix) {
        response.writeHead(200, { "content-type": "application/json" });
        response.end(inspectionPayload);
        return;
      }
      if (url.pathname === `${inspectionPrefix}/content`) {
        response.writeHead(200, { "content-type": "text/markdown" });
        response.end(inspectionPrimary);
        return;
      }
      if (url.pathname.startsWith(`${inspectionPrefix}/content/`)) {
        const body = inspectionBodies.get(url.pathname.slice(`${inspectionPrefix}/content/`.length));
        if (body) {
          response.writeHead(200, { "content-type": body.media });
          response.end(body.body);
          return;
        }
      }
      if (
        url.pathname === `/api/v1/resources/${registrySlug}/skill/${inspectionSkillName}` ||
        url.pathname === `/api/v1/resources/${registryID}/skill/${inspectionSkillName}` ||
        url.pathname === `/api/v1/resources/${registrySlug}/skill/${inspectionSkillName}/revisions/${inspectionRevision}` ||
        url.pathname === `/api/v1/resources/${registryID}/skill/${inspectionSkillName}/revisions/${inspectionRevision}`
      ) {
        response.writeHead(200, { "content-type": "application/json" });
        response.end(JSON.stringify(inspectionRevisionDoc));
        return;
      }
    }
    if (request.method === "GET" && (url.pathname === `/api/v1/registries/${registrySlug}` || url.pathname === `/api/v1/registries/${registryID}`)) {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(
        JSON.stringify({
          id: registryID,
          slug: registrySlug,
          origins: ["https://example.com/tui-qa.git"],
          ref: "main",
          roots: [{ path: ".", source_harness: "factory" }],
          acquisition: "mirror",
          listed: true
        })
      );
      return;
    }
    if (
      request.method === "GET" &&
      (url.pathname === `/api/v1/resources/${registrySlug}/skill/${resourceName}` ||
        url.pathname.startsWith(`/api/v1/resources/${registrySlug}/skill/${resourceName}/revisions/`) ||
        url.pathname === `/api/v1/resources/${registryID}/skill/${resourceName}` ||
        url.pathname.startsWith(`/api/v1/resources/${registryID}/skill/${resourceName}/revisions/`))
    ) {
      response.writeHead(200, { "content-type": "application/json" });
      const assessment = options.installFindings ? findingsAssessment : revision.assessment;
      response.end(JSON.stringify({ ...revision, assessment, archive_digest: servedArchiveDigest }));
      return;
    }
    const archiveRoute = /^\/api\/v1\/(archives|artifacts)\/sha256\/([0-9a-f]{64})$/.exec(url.pathname);
    const archive = request.method === "GET" && archiveRoute ? servedArchives.get(`sha256:${archiveRoute[2]}`) : undefined;
    if (archiveRoute && archive) {
      response.writeHead(200, { "content-type": artifactMediaType });
      const placeholder = options.mismatchedPlaceholderArchive ? archive.mismatched : archive.placeholder;
      response.end(archiveRoute[1] === "archives" ? placeholder : archive.final);
      return;
    }
    response.writeHead(404, { "content-type": "application/json" });
    response.end(JSON.stringify({ message: "not found" }));
  });

  const listening = new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve();
    });
  });
  try {
    await listening;
  } catch (error) {
    server.close();
    throw error;
  }

  const address = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${address.port}`,
    instanceID,
    registryID,
    registrySlug,
    resourceName,
    contentDigest,
    archiveDigest,
    driftedArchiveDigest,
    materializedSkillContent,
    acquisitionDataRequestCount: () => acquisitionDataRequests.length,
    acquisitionDataRequestPaths: () => [...acquisitionDataRequests],
    archiveFetches: () => {
      const fetched = (route: string): string[] =>
        acquisitionDataRequests.filter((requested) => requested.startsWith(route)).map((requested) => `sha256:${requested.slice(route.length)}`);
      return { placeholder: fetched("/api/v1/archives/sha256/"), final: fetched("/api/v1/artifacts/sha256/") };
    },
    delayNextAcquisition: (ms: number) => {
      if (!Number.isSafeInteger(ms) || ms < 1) throw new Error("acquisition delay must be a positive integer");
      nextAcquisitionDelayMs = ms;
    },
    delayedAcquisitionCount: () => delayedAcquisitions,
    driftArchiveDigest: () => {
      servedArchiveDigest = driftedArchiveDigest;
    },
    close: () => {
      server.closeAllConnections();
      server.close();
    }
  };
};
