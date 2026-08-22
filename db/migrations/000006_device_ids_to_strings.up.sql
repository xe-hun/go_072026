-- Device IDs are client-supplied strings capped at 30 characters.
ALTER TABLE note_changes
    DROP CONSTRAINT note_changes_device_id_fkey;

ALTER TABLE sync_devices
    ALTER COLUMN id TYPE VARCHAR(30) USING id::text;

ALTER TABLE note_changes
    ALTER COLUMN device_id TYPE VARCHAR(30) USING device_id::text;

ALTER TABLE note_changes
    ADD CONSTRAINT note_changes_device_id_fkey
    FOREIGN KEY (device_id) REFERENCES sync_devices(id);
