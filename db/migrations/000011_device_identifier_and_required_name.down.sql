ALTER TABLE sync_devices
    DROP COLUMN device_identifier,
    ALTER COLUMN device_name DROP NOT NULL;
