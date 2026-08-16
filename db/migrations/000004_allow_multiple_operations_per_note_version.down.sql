ALTER TABLE note_changes
    ADD CONSTRAINT note_changes_note_id_resulting_note_version_key
    UNIQUE (note_id, resulting_note_version);
