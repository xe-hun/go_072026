DROP INDEX IF EXISTS notes_owner_category_idx;
ALTER TABLE notes DROP COLUMN IF EXISTS category_id;
ALTER TABLE note_changes ALTER COLUMN note_id DROP NOT NULL;
