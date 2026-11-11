## Context

The callback seals the administration session with `maxAdminCookie` = 3900 bytes. The payload stores matching `admin_groups` names, each up to 512 bytes. Per-request authorization reads those names back. A valid callback response is `303` plus the session `Set-Cookie`, the login-cookie clear, `Location` (the sign-in destination), and fixed headers. Measured in review: a sealed value of about 3850 to 3900 bytes returns 502 through nginx's default 4 KiB buffer.

## Decisions

- **Membership bitset instead of names.** The session stores one bit per configured `admin_groups` entry. Matching is still done against the verified claims at callback time, so authorization semantics are unchanged. Cookie size then depends on configured count (32 bytes of bitset per 256 entries) and not on IdP group names or count.
- **Fingerprint of the configured list.** The session carries `sha256(json(admin_groups))` truncated to 16 bytes. A bitset is meaningful only against the list it was built from, so any configuration change makes the session fail closed rather than map bits onto different groups.
- **Sign-in destination bound.** `next` is limited to printable ASCII (0x21 to 0x7e) and 1024 bytes, rejected at login. This bounds the `Location` header, which `http.Redirect` otherwise expands for non-ASCII bytes.
- **Budget, not a new cap.** The 4096-byte claim is checked by a test at maximal identity values, not by a new numeric cap on groups. The test measures the serialized callback response header block.
- **No migration.** Old sessions carry a different payload shape and fail verification once. Administrators sign in again; no dual-read path is kept.

## Risks

- Operators with very large `admin_groups` lists grow the bitset linearly. The budget test pins 256 configured entries; larger lists need a fresh measurement.
- Changing `admin_groups` signs out every administrator until they sign in again. This is intended and is stated in the release note.
