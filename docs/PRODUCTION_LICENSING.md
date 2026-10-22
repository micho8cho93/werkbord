# Production licensing system

The operator requested production issuance, a complete record of licenses,
expansion/replacement, commercial closure and ongoing issuance without a fixed
batch limit. Michel is the initial operator and recovery custodian, with a written
procedure for recovery and for authorising additional operators later.

On 2026-10-22 the operator clarified that revocation means closing/replacing an
entitlement when a customer upgrades or ends their Team license. Customer licensing
remains offline. There is no new vendor validation interval or automatic remote
revocation requirement. Previously signed files retain their original validity.

The implemented system is a hosted operator console on GitHub Pages, a protected
Supabase signer and a transaction-backed register, with an optional local signer. Customers,
immutable signed revisions, stable request references, reasons, actual delivery
records and lifecycle events are retained. A three-to-ten-seat expansion creates
revision two of the same entitlement and preserves revision one as replaced.
Closing the entitlement is a separate action for a customer's shutdown.

The dedicated project is `werkbord-licensing` in the authorised `micho8cho93`
organisation, separate from the waitlist. MFA, configured operator identity and
live-session checks guard every operation. Browser clients cannot write history
or complete issuance directly. The hosted key is held as a backend-only Supabase Edge Function secret. It is
never bundled in the browser, linked into the customer service or shipped in the DMG.
This intentionally replaces the original local-only signing custody policy.
The operator URL is https://micho8cho93.github.io/werkbord/.

The managed path includes signature verification, idempotent retries, pending
operation recovery/cancellation, encrypted exports, key and snapshot checks, and
restoration into an empty destination. The exact commands, operating procedures,
size/hosting considerations and test evidence are in
[LICENSE_OPERATIONS.md](LICENSE_OPERATIONS.md).

**Status: implemented, activation pending.** Production is not declared ready
until the real operator login/MFA enrolment, primary-key encryption, independent
backups, restoration drill and hosting arrangements have been verified. The ten
prepared files remain migration inputs until the authorised operator imports them;
no payment or delivery is assumed. Public preview publication is authorised for
evaluation; stable Team admission remains governed by its existing release gate.
