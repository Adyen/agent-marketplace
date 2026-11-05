## 1. Resolver and pipeline

- [x] 1.1 Add `resolver.WithSelectedCollections` and capture selected collection roots before expansion
- [x] 1.2 Add `ResolveResult.SelectedCollections` and `DisplayArtifacts()`
- [x] 1.3 Add `marketplace.PinArtifact` and report direct Marketplace collection pins as selected roots

## 2. List

- [x] 2.1 Resolve Marketplace selections from the verified cache in `list`
- [x] 2.2 Build list rows from display artifacts; keep dry-run drift on `Resolved`

## 3. TUI

- [x] 3.1 Render selected collections as installed rows in verified inventory
- [x] 3.2 Render selected collections as installed-pending rows in the provisional stage
- [x] 3.3 Reuse `marketplace.PinArtifact` for Marketplace row identity

## 4. Verification

- [x] 4.1 Resolver, pipeline, TUI, and command tests bound to `// spec:` scenarios
- [x] 4.2 `make spec-check` and `make validate` (`make tui-qa` pending: blocked by host pnpm install)
