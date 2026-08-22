ALTER TABLE note_changes
    DROP CONSTRAINT note_changes_device_id_fkey;

ALTER TABLE note_changes
    ALTER COLUMN device_id TYPE UUID USING device_id::uuid;

ALTER TABLE sync_devices
    ALTER COLUMN id TYPE UUID USING id::uuid;

ALTER TABLE note_changes
    ADD CONSTRAINT note_changes_device_id_fkey
    FOREIGN KEY (device_id) REFERENCES sync_devices(id);
