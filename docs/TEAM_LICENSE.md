# Team offline licensing

Team has no vendor runtime licensing request. It verifies a document using the public Ed25519 issuer key embedded at build time, stores the signed document with replicated workspace data, and enforces it during mutations. No private vendor key ships in Team.

Schema 2 claims use the exact field order emitted by `license.CanonicalClaims`:

```json
{"schema":2,"product":"werkbord-team","id":"lic_example","customer":"org_example","edition":"team","seats":10,"issuedAt":"2026-10-08T00:00:00Z","supportEndsAt":"2027-10-08T00:00:00Z"}
```

Dates are UTC RFC3339 whole seconds. `expiresAt` and `supportEndsAt` are omitted when absent. Product/schema/edition, ID/customer, positive bounded seats and consistent dates are mandatory. Unknown or duplicate payload fields, noncanonical encoding, other editions/schemas, tampering, future activation or runtime expiry fail verification. A support entitlement ending does not stop the installed runtime. There are no separate edition features today: edition `team` enables Team; other names cannot unlock behavior.

The signed bytes are UTF-8 `werkbord-team/license/v2`, a NUL byte, then those exact canonical JSON bytes. The document is `{"claims":<that object>,"signature":"<64-byte Ed25519 signature in unpadded URL base64>"}`. Keep schema/field changes versioned; do not reconstruct signed claims with a different JSON implementation.

The offline vendor tool is a separate source command and is not packaged with customers:

```sh
# Run on the secured offline signing workstation. The private PKCS#8 PEM
# comes from secure local input, never a flag value, URL or environment value.
go run ./cmd/werkbord-team/vendor license \
  --input claims.json --out new-license.json < /secure/license-issuer.pem
```

Output is a new owner-only file; it never overwrites an existing license. Secure files may be unlocked from a hardware/offline store before this command. Key generation, custodian authorization, backups and signing-machine hygiene are operator responsibilities. Use a separate key for release manifests. Build customers with `LICENSE_ISSUER_PUBLIC_KEY=<32-byte raw URL base64 public key>`; only that public value is embedded. No production issuer has been invented or provisioned by this security gate.

Supply a valid document via `WERKBORD_TEAM_LICENSE_FILE` before CLI workspace creation, or import it through the Team app before creation. All production create/serve/daemon entry points install enforcement. For an existing 2.x database, the first licensed mutation records the verified initial document transactionally and rejects a license below the current member count.

Seats count live workspace members including owner/admin; one person's additional devices/hosts use no extra seat. Direct member creation, project invitation redemption and device enrollment finalize through the same guarded insertion. Concurrent hosts cannot oversubscribe the seat limit. Renew or increase it locally as the workspace owner:

```sh
WERKBORD_TEAM_TOKEN=<local-owner-credential> \
  werkbord-team license import --file renewal.json
```

The app also imports a renewal through its locally authenticated device interface. Owner-only renewal remains usable when the prior license expired. Reducing seats below live membership is refused. Normal mutations stop at runtime expiry; stored data remains readable with current authentication. Permission-limited containment also remains available: revoke devices, remove members, demote admins and replace existing member credentials. These operations cannot add seats or authority and still require current authorization and quorum. Backup/recovery operations are not a method to bypass signatures. Restarts/failover use the replicated document even when the initial license file is absent.

Schema 0 legacy v1 documents with mandatory expiry are accepted for the Team 3.x transition window. They cannot create a perpetual license or select another edition. Issue schema 2 renewals now; the next major removes legacy acceptance. Offline enforcement relies on customer clocks and unmodified customer software. It cannot stop a root operator changing code or provide instantaneous vendor revocation. An already valid offline license remains usable if every vendor server disappears.
