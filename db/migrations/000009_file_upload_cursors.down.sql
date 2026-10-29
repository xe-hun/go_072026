DROP INDEX IF EXISTS file_uploads_owner_cursor_idx;

ALTER TABLE file_uploads
    DROP CONSTRAINT IF EXISTS file_uploads_cursor_unique,
    DROP CONSTRAINT IF EXISTS file_uploads_device_id_fkey,
    DROP COLUMN IF EXISTS device_id,
    DROP COLUMN IF EXISTS cursor;
