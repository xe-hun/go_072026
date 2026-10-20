-- The composite keys make the note/block relationships enforceable at the
-- database boundary, not only in the upload service.
ALTER TABLE notes
    ADD CONSTRAINT notes_id_owner_id_unique UNIQUE (id, owner_id);

ALTER TABLE note_blocks
    ADD CONSTRAINT note_blocks_id_note_id_unique UNIQUE (id, note_id);

CREATE TABLE file_uploads (
    id UUID PRIMARY KEY,
    note_id UUID NOT NULL,
    owner_id UUID NOT NULL,
    block_id UUID NOT NULL,
    original_filename TEXT NOT NULL,
    storage_bucket TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    content_type TEXT NOT NULL,
    expected_size BIGINT NOT NULL CHECK (expected_size > 0),
    actual_size BIGINT CHECK (actual_size IS NULL OR actual_size >= 0),
    verified_content_type TEXT,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'COMPLETED', 'FAILED', 'EXPIRED', 'CANCELLED', 'DELETED')),
    error_message TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (storage_bucket, storage_path),
    FOREIGN KEY (note_id, owner_id) REFERENCES notes(id, owner_id),
    FOREIGN KEY (block_id, note_id) REFERENCES note_blocks(id, note_id)
);

CREATE INDEX file_uploads_owner_idx
    ON file_uploads (owner_id, created_at DESC);

CREATE INDEX file_uploads_note_idx
    ON file_uploads (note_id, created_at DESC);

CREATE INDEX file_uploads_block_idx
    ON file_uploads (block_id, created_at DESC);

CREATE INDEX file_uploads_status_expiry_idx
    ON file_uploads (status, expires_at)
    WHERE status = 'PENDING';
