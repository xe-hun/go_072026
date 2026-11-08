ALTER TABLE note_changes
    DROP CONSTRAINT note_changes_device_id_fkey;

ALTER TABLE file_uploads
    DROP CONSTRAINT file_uploads_device_id_fkey;

ALTER TABLE sync_devices
    ADD COLUMN last_global_cursor BIGINT NOT NULL DEFAULT 0;

ALTER TABLE sync_devices
    ALTER COLUMN device_id TYPE VARCHAR(30) USING device_id::text;

ALTER TABLE note_changes
    ALTER COLUMN device_id TYPE VARCHAR(30) USING device_id::text;

ALTER TABLE file_uploads
    ALTER COLUMN device_id TYPE VARCHAR(30) USING device_id::text;

ALTER TABLE sync_devices
    RENAME COLUMN device_id TO id;

ALTER TABLE note_changes
    ADD CONSTRAINT note_changes_device_id_fkey
    FOREIGN KEY (device_id) REFERENCES sync_devices(id);

ALTER TABLE file_uploads
    ADD CONSTRAINT file_uploads_device_id_fkey
    FOREIGN KEY (device_id) REFERENCES sync_devices(id);
