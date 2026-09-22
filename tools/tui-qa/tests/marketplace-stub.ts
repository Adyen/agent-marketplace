import http from "node:http";
import type { AddressInfo } from "node:net";

export interface MarketplaceStub {
  url: string;
  instanceID: string;
  close(): void;
}

const wellKnownPath = "/.well-known/agent-marketplace.json";
const searchPath = "/api/v1/search";

// A loopback stand-in for a hosted Marketplace endpoint. The organization
// fixture declares the endpoint without pinning resources, so the stub only
// needs the well-known identity document and an empty search envelope; nothing
// else in the journeys' API surface is exercised.
export const createMarketplaceStub = async (): Promise<MarketplaceStub> => {
  const instanceID = "tui-qa-marketplace";
  const server = http.createServer((request, response) => {
    const url = new URL(request.url ?? "/", "http://127.0.0.1");
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
      response.end(JSON.stringify({ items: [], total: 0, offset, limit }));
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
    close: () => {
      server.closeAllConnections();
      server.close();
    }
  };
};
