DROP TABLE IF EXISTS file_uploads;

ALTER TABLE note_blocks
    DROP CONSTRAINT IF EXISTS note_blocks_id_note_id_unique;

ALTER TABLE notes
    DROP CONSTRAINT IF EXISTS notes_id_owner_id_unique;
