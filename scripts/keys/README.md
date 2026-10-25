# Release manifest public trust anchor

`werkbord-release.pub` is the public Ed25519 verification key for unified
Werkbord checksum manifests. It is separate from the license issuer, Apple
Developer ID and Sparkle keys. The corresponding private key is never part of
this repository or GitHub Actions.

Raw URL-base64 public key:
`7Mgz5QF2zhAKxvSNq1kxu6vlslfjXWwAiXK1722AID0`

SHA-256 fingerprint of the decoded 32-byte public key:
`94a8a0b18379967e1ae69f0f1bf5453918b9bb01cd94c5074a431b6ff64659d9`

Compare this fingerprint through an independently trusted publisher channel.
Downloading a key beside an archive does not establish its authenticity. The
shell installer must itself come from a trusted source, and accepts an
independently provisioned PEM through `WERKBORD_RELEASE_PUBLIC_KEY_FILE` (also
supported with the `DEVBOARD_` prefix). This checked-in PEM is its default only
when that installer/source copy is already trusted.

The build receives the matching raw key through `RELEASE_PUBLIC_KEY`; the
repository variable was provisioned and read back during owner-assisted
acceptance. Key rotation requires a reviewed source/trust-channel migration;
never silently substitute a key received from an artifact server.
