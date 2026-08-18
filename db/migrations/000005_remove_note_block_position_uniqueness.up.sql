-- Positions are fractional ordering keys, not block identities. They may be
-- reused by tombstoned blocks and can temporarily collide while a note batch
-- reorders multiple blocks. The existing non-unique index still supports
-- ordered block reads.
ALTER TABLE note_blocks
    DROP CONSTRAINT IF EXISTS note_blocks_note_id_position_key;
