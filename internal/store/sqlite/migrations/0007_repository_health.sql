-- Repository health findings, and when each project's health was last checked.
--
-- Findings are recomputed from Git metadata and Dev Board's own records, so a
-- row here is not the source of truth about the repository: it is the memory of
-- what was found. That memory is what gives a finding a life: when it was first
-- seen (and so how long it has waited), that it has since resolved, and that
-- the user has said they know about it. Without it every recalculation would
-- start from nothing and "detected 3 days ago" could not be said.
--
-- A finding's identity (id) is derived from the project, the rule and what it is
-- about, so the same problem is the same row on every recalculation. Everything a
-- screen needs to show it lives in `body` (title, explanation, subject, evidence,
-- action) as JSON, so wording and evidence can improve without a migration; what
-- the database filters and orders on is in columns.
--
-- The states are the whole lifecycle, and the CHECKs keep each one honest:
-- 'open' (currently true), 'resolved' (it stopped being true) and 'dismissed' (the
-- user said they know; hidden until it gets worse).

CREATE TABLE health_findings (
    project_id         TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    id                 TEXT    NOT NULL,
    type               TEXT    NOT NULL CHECK (type != ''),
    category           TEXT    NOT NULL CHECK (category != ''),
    severity           TEXT    NOT NULL CHECK (severity IN ('info', 'attention', 'risk', 'critical')),
    basis              TEXT    NOT NULL CHECK (basis IN ('deterministic', 'heuristic')),
    state              TEXT    NOT NULL CHECK (state IN ('open', 'resolved', 'dismissed')),
    body               TEXT    NOT NULL CHECK (json_valid(body)),
    detected_at        INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL,
    resolved_at        INTEGER,
    dismissed_at       INTEGER,
    dismissed_severity TEXT    NOT NULL DEFAULT '' CHECK (dismissed_severity IN ('', 'info', 'attention', 'risk', 'critical')),

    PRIMARY KEY (project_id, id),

    CHECK (state != 'open'      OR (resolved_at IS NULL AND dismissed_at IS NULL)),
    CHECK (state != 'resolved'  OR resolved_at IS NOT NULL),
    CHECK (state != 'dismissed' OR (dismissed_at IS NOT NULL AND dismissed_severity != '' AND resolved_at IS NULL))
) STRICT, WITHOUT ROWID;

-- The Control Center reads the open findings of every project, worst first.
CREATE INDEX health_findings_open ON health_findings (state, severity) WHERE state = 'open';

-- When a project's health was last worked out, how long that took, and whether
-- it failed. A project that has never been checked has no row.
CREATE TABLE health_checks (
    project_id  TEXT    PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    checked_at  INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    error       TEXT    NOT NULL DEFAULT ''
) STRICT;
