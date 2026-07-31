-- +goose Up
CREATE TABLE categories (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    UNIQUE (owner_id, name)
);

CREATE TABLE notes (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    category_id UUID REFERENCES categories(id),
    title TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}',
    current_version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX notes_owner_updated_idx
    ON notes (owner_id, updated_at DESC);

CREATE INDEX notes_owner_category_idx
    ON notes (owner_id, category_id)
    WHERE deleted_at IS NULL;

CREATE TABLE note_blocks (
    id UUID PRIMARY KEY,
    note_id UUID NOT NULL REFERENCES notes(id),
    block_type TEXT NOT NULL,
    text_content TEXT NOT NULL DEFAULT '',
    position TEXT NOT NULL,
    properties JSONB NOT NULL DEFAULT '{}',
    current_version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    UNIQUE (note_id, position)
);

CREATE INDEX note_blocks_note_position_idx
    ON note_blocks (note_id, position);

CREATE TABLE sync_devices (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    device_name TEXT,
    platform TEXT,
    app_version TEXT,
    protocol_version INTEGER NOT NULL DEFAULT 1,
    last_global_cursor BIGINT NOT NULL DEFAULT 0,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE INDEX sync_devices_owner_idx
    ON sync_devices (owner_id);

CREATE TABLE note_changes (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    note_id UUID NOT NULL REFERENCES notes(id),
    block_id UUID,
    device_id UUID NOT NULL REFERENCES sync_devices(id),
    client_operation_id UUID NOT NULL,
    entity_type TEXT NOT NULL,
    operation_type TEXT NOT NULL,
    base_note_version BIGINT NOT NULL,
    resulting_note_version BIGINT NOT NULL,
    base_block_version BIGINT,
    resulting_block_version BIGINT,
    change_format TEXT NOT NULL DEFAULT 'structured-operation-v1',
    schema_version INTEGER NOT NULL DEFAULT 1,
    change_data JSONB NOT NULL,
    global_sequence BIGINT GENERATED ALWAYS AS IDENTITY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (device_id, client_operation_id),
    UNIQUE (note_id, resulting_note_version)
);

CREATE INDEX note_changes_owner_sequence_idx
    ON note_changes (owner_id, global_sequence);

CREATE INDEX note_changes_note_version_idx
    ON note_changes (note_id, resulting_note_version);

CREATE TABLE note_snapshots (
    id UUID PRIMARY KEY,
    note_id UUID NOT NULL REFERENCES notes(id),
    owner_id UUID NOT NULL,
    version BIGINT NOT NULL,
    snapshot_format TEXT NOT NULL DEFAULT 'note-snapshot-v1',
    schema_version INTEGER NOT NULL DEFAULT 1,
    snapshot_data JSONB NOT NULL,
    checksum TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (note_id, version)
);

CREATE TABLE outbox_jobs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts INTEGER NOT NULL DEFAULT 0,
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    completed_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX outbox_jobs_available_idx
    ON outbox_jobs (available_at, id)
    WHERE completed_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS outbox_jobs;
DROP TABLE IF EXISTS note_snapshots;
DROP TABLE IF EXISTS note_changes;
DROP TABLE IF EXISTS sync_devices;
DROP TABLE IF EXISTS note_blocks;
DROP TABLE IF EXISTS notes;
DROP TABLE IF EXISTS categories;

