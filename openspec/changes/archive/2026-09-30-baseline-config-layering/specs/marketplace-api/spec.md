## REMOVED Requirements

### Requirement: Consumer Marketplace configuration

**Reason**: Consumer configuration belongs to the configuration-layering capability rather than the Marketplace protocol surface, and the requirement's *Layered config wiring* scenario title no longer matches its layered-merge content.
**Migration**: Re-added in `config-layering` as the consumer Marketplace configuration requirements; the merge scenario is retitled to match its content and every consumer-configuration test annotation moves to the new identities in the same pull request.
