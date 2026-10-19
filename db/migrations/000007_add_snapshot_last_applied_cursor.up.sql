-- Store the global change cursor represented by each full-note snapshot.
ALTER TABLE note_snapshots
    ADD COLUMN last_applied_cursor BIGINT;

-- Backfill existing snapshots from the change history represented by their
-- stored note version before enforcing the non-null invariant.
UPDATE note_snapshots s
SET last_applied_cursor = COALESCE((
    SELECT MAX(c.global_sequence)
    FROM note_changes c
    WHERE c.note_id = s.note_id
      AND c.resulting_note_version <= s.version
), 0);

ALTER TABLE note_snapshots
    ALTER COLUMN last_applied_cursor SET DEFAULT 0,
    ALTER COLUMN last_applied_cursor SET NOT NULL;
