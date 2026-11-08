ALTER TABLE sync_devices
    ADD COLUMN device_identifier VARCHAR(100) NOT NULL,
    ALTER COLUMN device_name SET NOT NULL;
