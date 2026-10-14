# GitHub release display sequence

> **One release.** From `werkbord-v4.0.0-preview.1` there is a single release series, so a single display sequence. The
> Individual and Team sequences below are history; continue the **Individual** sequence for the unified releases and
> reserve no more Team numbers.

GitHub release titles use a separate display sequence for each product, beginning at
`0.1.0` for its first published release. This is a presentation convention, not the
application's semantic version. Product tags, committed VERSION files, archive names,
embedded versions, checksums, update feeds and installer version checks keep their
original build versions. Never renumber a Git tag to match a display number.

Titles use `Werkbord Individual — Release <sequence>` or
`Werkbord Team — Release <sequence>`. Previews include `-preview.N`; additional
qualifiers such as `unsigned Mac desktop` can follow the title.

Before publishing, reserve the display number in the table below. Keep patch releases
in their display minor series, advance the display minor for a feature release or new
product generation, and advance the preview counter for another preview of the same
release. Count published releases, not every local commit or unpublished tag. Stable
publication of a preview uses the same display base without the preview suffix.

Each release description begins with:

```markdown
**Release sequence:** <sequence> · **Original build version:** <build version>

The release sequence is a display label for this product’s published history. Downloads and installed apps use the original build version; the Git tag is `<product tag>`.

---
```

Preserve the existing release notes below that introduction. The publishing workflow
reads the reserved display sequence from this table when assembling the draft.
The original build version remains derived from the tag.
Editing presentation must preserve tags, publication dates, prerelease status, assets
and latest selection. Only stable unified Werkbord releases may become latest.

The current release scope is macOS only; Linux and Windows publication is deferred.
Stable unified releases are published only after the signed, notarized universal macOS app
and public download and signed update-feed checks succeed. The GUI app carries the Team
service and installer; do not attach standalone Team Host CLI archives. A pushed tag
is not proof that a release is installable.

## Recorded and reserved numbers

| Product | Git tag / original build version | Display sequence | Status |
| --- | --- | --- | --- |
| Individual | `werkbord-v0.8.2` | `0.1.0` | Published |
| Individual | `werkbord-v0.9.0` | `0.2.0` | Published |
| Individual | `werkbord-v0.9.1` | `0.2.1` | Published |
| Individual | `werkbord-v0.9.2` | `0.2.2` | Published |
| Individual | `werkbord-v0.10.0` | `0.3.0` | Published |
| Individual | `werkbord-v0.11.0` | `0.4.0` | Published |
| Individual | `werkbord-v1.0.0` | `0.5.0` | Published |
| Individual | `werkbord-v1.3.1-preview.1` | `0.6.0-preview.1` | Published |
| Individual | `werkbord-v1.3.1-preview.2` | `0.6.0-preview.2` | Published |
| Individual | `werkbord-v1.4.0-preview.2` | `0.7.0-preview.1` | Published |
| Team | `werkbord-team-v2.0.0` | `0.1.0` | Published |
| Team | `werkbord-team-v2.1.0` | `0.2.0` | Published |
| Team | `werkbord-team-v2.1.1` | `0.2.1` | Published |
| Team | `werkbord-team-v2.1.2` | `0.2.2` | Published |
| Team | `werkbord-team-v2.1.3` | `0.2.3` | Published |
| Team | `werkbord-team-v2.2.0` | `0.3.0` | Published |
| Team | `werkbord-team-v2.2.1` | `0.3.1` | Published |
| Team | `werkbord-team-v2.3.0` | `0.4.0` | Published |
| Team | `werkbord-team-v2.5.0` | `0.5.0` | Published |
| Team | `werkbord-team-v2.6.1` | `0.6.0` | Published |
| Team | `werkbord-team-v2.8.0` | `0.7.0` | Published |
| Team | `werkbord-team-v2.8.1` | `0.7.1` | Published |
| Individual | `werkbord-v1.4.0-preview.3` | `0.7.0-preview.2` | Published CLI preview |
| Team | `werkbord-team-v3.2.0` | `0.8.0` | Abandoned empty draft, deleted; superseded by the unified release. Do not reuse |
| Individual | `werkbord-v4.5.4-preview.1` | `0.8.0-preview.1` | Published unified CLI preview; no Mac app asset |
| Individual | `werkbord-v4.24.6` | `0.8.0` | Published stable; signed Mac app and macOS/Linux/Windows CLI archives |
| Individual | `werkbord-v4.25.0` | — | Unpublished; superseded by 4.28.4 |
| Individual | `werkbord-v4.28.4` | `0.9.0` | Unpublished; superseded by 4.28.5-preview.1; stable display number remains reserved |
| Individual | `werkbord-v4.28.5-preview.1` | — | Unpublished; CI browser fixture lacked a Git committer identity; superseded by preview.2 |
| Individual | `werkbord-v4.28.5-preview.2` | — | Unpublished; desktop browser fixture used an obsolete invitation label; superseded by preview.3 |
| Individual | `werkbord-v4.28.5-preview.3` | — | Unpublished signed draft; publish job lacked repository context; superseded by preview.4 |
| Individual | `werkbord-v4.28.5-preview.4` | — | Unpublished; timing-sensitive Git evidence assertion; superseded by preview.5 |
| Individual | `werkbord-v4.28.5-preview.5` | — | Unpublished; public verifier did not retry an Apple ticket-service network error; superseded by preview.6 |
| Individual | `werkbord-v4.28.5-preview.6` | `0.9.0-preview.1` | Published public signed macOS preview; Team production admission remains blocked (previous status: Reserved public signed preview) |
| Individual | `werkbord-v4.29.0-preview.1` | — | Unpublished; empty Team workspace analytics encountered a null project list; superseded by preview.2 |
| Individual | `werkbord-v4.29.0-preview.2` | `0.10.0-preview.1` | Reserved public signed macOS preview; focused analytics views and Universal startup; Team production admission remains blocked |
