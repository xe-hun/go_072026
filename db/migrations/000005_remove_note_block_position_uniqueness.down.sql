ALTER TABLE note_blocks
    ADD CONSTRAINT note_blocks_note_id_position_key
    UNIQUE (note_id, position);
