# Living Product Map

Product Map documents implemented features, scenarios, relationships and supporting source locations. It does not add roadmap features or execute the application to prove behaviour.

## Generate and explore

Open a repository-backed Personal project → **Product Map**. Choose a connected Codex or eligible Claude runtime and a committed target branch, then **Generate Product Map**. A repository/provider disclosure explains that permitted source context is sent to that provider. Werkbord remembers acknowledgement for that repository and provider. Claude analysis requires detected API authentication and a runtime supporting safe-mode isolation; a Claude subscription alone is not treated as eligibility.

Analysis runs in the local controller while the rest of Werkbord remains usable. Its stages are preparing source, discovering features, analysing scenarios and relationships, independently validating evidence, and saving. Counts represent files actually read. Cancel stops the provider; a cancelled, failed or interrupted job leaves the previous map intact. A controller restart marks running jobs interrupted and requires explicit retry.

Explore the canvas or keyboard-accessible list, search by feature, and select a feature to read its behaviour, scenarios, relationships, source references and recent snapshot history. Inference and coverage limitations remain visible. Snapshots are immutable and versioned in the controller's SQLite database.

## Update

Opening a map checks the selected branch's locally known committed revision without invoking a model or fetching from a remote. Freshness is relative to this local revision; normal Git fetches make newer remote revisions available. Uncommitted edits are excluded.

**Update Map** skips all model turns when permitted source content has not changed. Where changed files are covered by previous evidence, analysis uses the affected features and relationship closure. Unrelated feature records are retained deterministically. Missing or diverged history, changed analysis rules, shared configuration/schema changes, large diffs and changes outside known evidence require a full scan. The job records its reason. **Rebuild Map** always requests fresh analysis.

Each job pins a commit. The start request can pin the revision shown for review, and publication compares the previous map version inside a transaction. Changes arriving during a scan cannot silently replace a newer map.

## Team sharing

The Team service never opens a checkout or runs an agent. In the desktop sidebar, the Product Map button beside a Team project lets an authorised member choose their corresponding Personal repository project, open its local map for generation/update, and review the saved snapshot for sharing.

The desktop shell grants publication of one exact snapshot to one registered Team project for ten minutes. The repository identity must match. The review shows the outgoing descriptions, scenarios, relationships, limitations and source references. Native confirmation sends a durable proposal with a stable delivery ID; an uncertain retry uses the same ID and content. Source files, transcripts, local absolute paths and provider credentials are not transferred.

Contributors propose. Project owners and reviewers explicitly publish or decline, including their own scans. Publication rechecks membership, permissions, Premium requirements, repository identity, the reviewed digest and the current published version. Readers use the published snapshot without another analysis. Team records proposal and review activity.

Team freshness uses recent, permission-checked branch reports. A different reported target revision shows **Updates available**; a matching report is qualified as current according to that report. Missing, stale or inaccessible evidence shows **Freshness unknown**. Repository identity changes hide snapshots for the previous repository.

## Boundaries and limits

- Analysis uses existing Go provider adapters in isolated sessions, with their native execution tools disabled. The only source operations are bounded inventory, filename search and committed-file reads owned by Werkbord. No new Salie board permission is granted.
- Git tree/blob reads exclude symlinks and submodules. Source policy excludes hidden paths, credentials/secrets, repository agent instructions, dependency trees and common binary/build artefacts. Binary or credential-like file contents are refused. Exclusions reduce exposure; they are not a general guarantee that arbitrary source text contains no confidential information.
- Limits: 20,000 inventory files, 256 KiB per readable file, 8 MiB of source reads per job, 100 turns per analysis pass, 30 minutes per job, and one active job per repository. Maps are bounded to 512 KiB; views show the ten most recent local snapshots. All saved snapshots remain in the database.
- Each successful map has two separate provider sessions. The second pass must read its own source evidence. Deterministic validation checks content structure, IDs, relationships, permitted paths and cited line ranges. This validates references and consistency, not semantic correctness or runtime behaviour.
- Team stores only the neutral map contract. Its SQLite and replicated storage use the same transaction and version checks. Existing architecture tests remain in force.

## Interfaces and verification

Local owner API: `GET /api/projects/{id}/product-map`, `GET .../snapshot`, `POST .../jobs`, and `POST .../jobs/{job}/cancel`. Job requests accept mode, provider, model, branch, expected map version, optional expected commit and disclosure acknowledgement; they accept no commands or filesystem paths. These routes are not exposed through the network, phone, MCP or narrow local-access credentials.

Team API: `GET /api/team/v1/projects/{id}/product-map`, `POST .../proposals`, and `POST .../proposals/{mid}/review`. Proposals carry a stable ID, expected published version and the validated snapshot; decisions carry the reviewed digest and expected version.

Scripted-provider tests cover generation, independent validation, unchanged-source skips, incremental updates, source boundaries, cancellation, restart and stale revisions. Team tests cover visibility, permissions, repository matching, retries, publication conflicts and Premium. The browser fixture runs the actual engine and API against synthetic content; no model is invoked. Real-provider accuracy still needs a user-initiated scan and evidence review before making an accuracy claim.
