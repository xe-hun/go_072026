ALTER TABLE note_changes ALTER COLUMN note_id SET NOT NULL;
ALTER TABLE notes ADD COLUMN IF NOT EXISTS category_id UUID REFERENCES categories(id);
CREATE INDEX IF NOT EXISTS notes_owner_category_idx
    ON notes (owner_id, category_id)
    WHERE deleted_at IS NULL;
