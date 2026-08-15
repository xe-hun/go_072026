ALTER TABLE note_changes
    ADD COLUMN IF NOT EXISTS entity_type TEXT NOT NULL DEFAULT '';

ALTER TABLE note_changes
    ALTER COLUMN entity_type DROP DEFAULT;
