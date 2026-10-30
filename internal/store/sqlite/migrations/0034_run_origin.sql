-- What asked for a run, when it was not started by hand from its task: "salie:<conversation id>" for a run an assistant
-- conversation launched. Provenance only, written once when the run is created. It lets Agents point back to the
-- conversation without a second record of the run or of the conversation.
ALTER TABLE runs ADD COLUMN origin TEXT NOT NULL DEFAULT '';
