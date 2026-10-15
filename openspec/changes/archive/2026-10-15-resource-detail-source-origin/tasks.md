## 1. API source provenance

- [x] 1.1 Expose optional `source` on resource revision responses using the catalog source sanitizer
- [x] 1.2 Document `source` on the `ResourceRevision` OpenAPI schema and regenerate the web API types
- [x] 1.3 Add Go tests for exposed and omitted source provenance with spec annotations

## 2. Web detail header

- [x] 2.1 Add source label and browse URL helpers with forge-specific resource links
- [x] 2.2 Render the source as the first header metadata item and remove the header Scanned group
- [x] 2.3 Add unit and component tests, and update the e2e mock and assertions
