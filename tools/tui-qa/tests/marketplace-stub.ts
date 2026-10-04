import { createHash } from "node:crypto";
import http from "node:http";
import type { AddressInfo } from "node:net";

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
const contentDigest = "sha256:dba586be27c4593b6d6d6a718c01050323490600b2e7084f950e3ee0e5fc3adf";
const archiveDigest = "sha256:dc95981d712d7bd33732e1fb5b8ac9a3f4a3406c9e27ef2c05e8bf0c6083c4b5";
const materializedSkillContent =
  "---\nname: org-review\ndescription: Organization review fixture\nExtra: null\n---\nReview organization changes carefully.";

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
const artifact = Buffer.from(
  "H4sIAAAAAAAE/+yVz2/bOgzH86cYPDt98u9Etz68N+ywYcPS2zAEtEw7WhUpk2S3XZD/fZDTDGkD7LJ0w7B8LpIYgqJC88s1atmS81efndGTl4Exxso8H1fG2POVlWV5sAUmLEmqpJxE7GXSeUrvPNoJ++m7QubHj/tD2IITK1rjciDrpNHAYUggBrRetij8c/ut1A1wcLdSKYhB45qAg7Hd1NIg6Q5isNRJ5+3DUgZP38vpF5wejMe/O9V3wMEbo9xoH+RjChCDMNqT9stGduR8uHKFaVHypsZiVtaUViIv5lldNmVTYpXMBEtYwbI0y+esZKxOqWKzvJ0XjDIiRkUrMmxaCG8TKznQaWgxL+azpKmStKnqJsuqLKWkrYt6hmKOWZtjlrNSzCmtqE0FK2hWt0yUbJaJvC4gho01A2nUgoBvoSNNFr2xwAE70n66RntLfqNQUCRQGy0Fqgjt/s+OhgR2Mbh+szHWU7N8WhsH/GOozqfd+T4uS870VtD5Ip4S+qIqinE9bpFweOTpPhndo+I01Pn5y/v/UP9/xo5+mSkQavwj/c+rbPR5ZMKSjBXJRf9/BVu47kgHfYUYrpUyd9TcjILMda9UDNe269ek/Wt58PrXNA/A4cOo95GxHWr5Fb00OhIr1B25SKCltlfq4Qpi+I+csHLj99Pl3bH/fmZErbz3vSWI4f97D3y7GzcWDzm8NQ0p4CHF92TX0oUx4faOR4YFeeAtKkcxLEZVA76FV1KFEQUxvJGagLNdDIu+HvV4H3Ox19vg6A5X3ljZdWS/n08H3RlF+MKFCxd+A98CAAD//5bS3aIADgAA",
  "base64"
);
const driftedArchiveDigest = "sha256:2403ab01774eb6afedc20066f8a0f42d72f4099b4793d88b79270b8e9a4f5493";
const driftedArtifact = Buffer.from(
  "H4sIAAAAAAAE/+yVz27bOBDG/SjCnJVkqL+WblnsLvawRYs6t6IIKGoks5FJg6ScpIbfvaBsp04CBCgauymq34UUMSY/cjzfLLiSDVl3/sVqNTkOiIhZkgwjIj4dMc2z/ZpngozlLJsEeBw5j+mt42aCP32WV354ud+ENVgxpwW/XpGxUisoYcUgBG6cbLhwT9dvpKqhBHsjuw5CUHxBUII27ZmhlaRbCMFPdltBCEIrR8pd17Il6/xP5zxKs7KueDrNKopykaRFXGV1Vmc8Z1OBDFOMozgpMEOsIspxmjRFihQTIaWNiHndgNco5nJFz7aOEox5hSzPE6oy3lAtIsQsa6YcmySq86hJsCiqJC/iejqt8iLKsZpSwZMmTYoYQlgavSLFlSAo19CSIsOdNlACb0m5swU3N+SWHRcUCK60koJ3Ad89WrBisAnB9sulNo7q68dvbKH85F/58+bX/0kMWd0bQcdU4usiT9NhPCwR/7Hj8ZwN4UF6TFF7/vD63+f/Yqjo43QBn+OX/D/J4yFmxwRZjCkb/f8UrOGyJeV9GUK47Dp9S/WV1p2FUvVdF8KlafsFKfef3Ef9pet7KOHj4PeBNi1X8it3UqtAzLlqyQaCG2r6rrs/hxD+JiuMXLptd3l/GO9bBd0GjbxzvSEI4Z87B+V6M0wM32t4p2vqoPQSP5BZSOvbi90GHizMyEHZ8M5SCLPB1bx5/ys736IghP+lIihxE8Ksr/j3a8+2Pu0DH659ZWTbknn4ft7o3oB5vwLcWnL2FTZ6AV/XP+j/SYyj/5+Cbf4vaiMbd8bO3Z17/VN9/l/yf8R8iNkxQRalLB79/xQMiQ/YKY4aGRkZGXlDfAsAAP//bfDWawAUAAA=",
  "base64"
);
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
    assessment_digest: "sha256:5cfa82198ccae9f92e6e1b4bde0d13c61e51903ae724105d839c2c179d60e490",
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

// A loopback stand-in for a hosted Marketplace endpoint. It serves one exact,
// assessed skill and its canonical artifact so a missing organization lock
// exercises acquisition, verification, resolution, and materialization.
export const createMarketplaceStub = async (): Promise<MarketplaceStub> => {
  const instanceID = "tui-qa-marketplace";
  const acquisitionDataRequests: string[] = [];
  let nextAcquisitionDelayMs = 0;
  let delayedAcquisitions = 0;
  let servedArchiveDigest = archiveDigest;
  const server = http.createServer(async (request, response) => {
    const url = new URL(request.url ?? "/", "http://127.0.0.1");
    if (/^\/api\/v1\/(?:registries|resources|artifacts|manifests)\//.test(url.pathname)) {
      acquisitionDataRequests.push(url.pathname);
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
      const items = terms.some((term) => inspectionSkillName.includes(term))
        ? [inspectionSearchItem]
        : [];
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
      response.end(JSON.stringify({ ...revision, archive_digest: servedArchiveDigest }));
      return;
    }
    if (request.method === "GET" && url.pathname.startsWith("/api/v1/artifacts/sha256/")) {
      const requested = `sha256:${url.pathname.slice("/api/v1/artifacts/sha256/".length)}`;
      const blob = requested === archiveDigest ? artifact : requested === driftedArchiveDigest ? driftedArtifact : undefined;
      if (blob) {
        response.writeHead(200, { "content-type": "application/vnd.agent-marketplace.artifact.v1.tar+gzip" });
        response.end(blob);
        return;
      }
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
