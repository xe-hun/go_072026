-- Device IDs are UUIDs stored under the explicit device_id column. The old
-- per-device cursor is removed because sync cursors are client-owned state.
ALTER TABLE note_changes
    DROP CONSTRAINT note_changes_device_id_fkey;

ALTER TABLE file_uploads
    DROP CONSTRAINT file_uploads_device_id_fkey;

ALTER TABLE sync_devices
    RENAME COLUMN id TO device_id;

ALTER TABLE sync_devices
    ALTER COLUMN device_id TYPE UUID USING device_id::uuid,
    DROP COLUMN last_global_cursor;

ALTER TABLE note_changes
    ALTER COLUMN device_id TYPE UUID USING device_id::uuid;

ALTER TABLE file_uploads
    ALTER COLUMN device_id TYPE UUID USING device_id::uuid;

ALTER TABLE note_changes
    ADD CONSTRAINT note_changes_device_id_fkey
    FOREIGN KEY (device_id) REFERENCES sync_devices(device_id);

ALTER TABLE file_uploads
    ADD CONSTRAINT file_uploads_device_id_fkey
    FOREIGN KEY (device_id) REFERENCES sync_devices(device_id);
