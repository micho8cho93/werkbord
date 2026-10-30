-- A project's queue has one shared order. Existing tickets keep the order they have always had (creation order).
ALTER TABLE tickets ADD COLUMN position REAL NOT NULL DEFAULT 0;
UPDATE tickets SET position = number;
