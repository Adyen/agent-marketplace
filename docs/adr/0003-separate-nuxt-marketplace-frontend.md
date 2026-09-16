---
status: accepted
---

# Run the Marketplace frontend as a separate Nuxt application

The Marketplace browser is a separate process and release artifact in the same repository, not HTML embedded in the Go binary. It uses Nuxt 4, Nuxt UI core, and a small reviewed set of Inspira UI components; it consumes a TypeScript client generated from the Go-owned OpenAPI contract. This preserves independent frontend development, SSR and SEO for public resource pages, accessible components, and external self-hosting without coupling the open product to the private Bento design system.

## Consequences

The API and frontend are deployed behind one origin by default, with a strict optional CORS allowlist for split-domain deployments. Nuxt exclusively owns `/r/**`, including latest and exact-revision browser routes; machine-readable resources remain under `/api/v1/**`, and API-only deployments do not vary `/r/**` by returning JSON. The frontend is read-only in v1, accepts runtime API configuration, ships independently as an OCI image, and must meet WCAG 2.2 AA, reduced-motion, responsive-layout, and performance-budget gates.
