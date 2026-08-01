package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "notes-server/db/generated"
)

// JobCreateSnapshot is the outbox job type handled by the snapshot worker.
const JobCreateSnapshot = "create_snapshot"

// SnapshotJobPayload is stored as JSON in outbox_jobs.payload.
type SnapshotJobPayload struct {
	// NoteID identifies the note to snapshot.
	NoteID uuid.UUID `json:"noteId"`
	// OwnerID scopes the snapshot read.
	OwnerID uuid.UUID `json:"ownerId"`
	// ExpectedVersion records the version that triggered the job.
	ExpectedVersion int64 `json:"expectedVersion"`
}

// EnqueueSnapshotJob inserts a snapshot creation job into the PostgreSQL outbox.
func (s *Store) EnqueueSnapshotJob(ctx context.Context, noteID, ownerID uuid.UUID, expectedVersion int64) (OutboxJob, error) {
	payload, err := json.Marshal(SnapshotJobPayload{
		NoteID:          noteID,
		OwnerID:         ownerID,
		ExpectedVersion: expectedVersion,
	})
	if err != nil {
		return OutboxJob{}, err
	}
	job, err := s.q.EnqueueOutboxJob(ctx, db.EnqueueOutboxJobParams{
		JobType: JobCreateSnapshot,
		Payload: payload,
		Column3: nil,
	})
	return fromDBOutboxJob(job), err
}

// ClaimOutboxJob atomically claims one available job. The SQL uses SKIP LOCKED
// so multiple worker processes can run safely.
func (s *Store) ClaimOutboxJob(ctx context.Context, workerID string, lockTimeout time.Duration) (OutboxJob, error) {
	job, err := s.q.ClaimOutboxJob(ctx, db.ClaimOutboxJobParams{
		LockedBy: pgtype.Text{String: workerID, Valid: true},
		Secs:     lockTimeout.Seconds(),
	})
	return fromDBOutboxJob(job), mapNoRows(err)
}

// CompleteOutboxJob marks a job finished and clears lock/error fields.
func (s *Store) CompleteOutboxJob(ctx context.Context, jobID int64) error {
	return s.q.CompleteOutboxJob(ctx, jobID)
}

// FailOutboxJob clears the lock, records the error, and schedules a retry.
func (s *Store) FailOutboxJob(ctx context.Context, jobID int64, message string, retryAfter time.Duration) error {
	return s.q.FailOutboxJob(ctx, db.FailOutboxJobParams{
		ID:        jobID,
		LastError: pgtype.Text{String: message, Valid: true},
		Secs:      retryAfter.Seconds(),
	})
}

// InsertSnapshot stores a full snapshot. The underlying query upserts by
// (note_id, version), making retrying a snapshot job idempotent.
func (s *Store) InsertSnapshot(ctx context.Context, snapshot NoteSnapshot) (NoteSnapshot, error) {
	inserted, err := s.q.InsertSnapshot(ctx, db.InsertSnapshotParams{
		ID:             pgUUID(snapshot.ID),
		NoteID:         pgUUID(snapshot.NoteID),
		OwnerID:        pgUUID(snapshot.OwnerID),
		Version:        snapshot.Version,
		SnapshotFormat: snapshot.SnapshotFormat,
		SchemaVersion:  snapshot.SchemaVersion,
		SnapshotData:   []byte(snapshot.SnapshotData),
		Checksum:       snapshot.Checksum,
	})
	return fromDBSnapshot(inserted), err
}

// GetLatestSnapshotForNote returns the newest snapshot owned by a user.
func (s *Store) GetLatestSnapshotForNote(ctx context.Context, noteID, ownerID uuid.UUID) (NoteSnapshot, error) {
	snapshot, err := s.q.GetLatestSnapshotForNote(ctx, db.GetLatestSnapshotForNoteParams{
		NoteID:  pgUUID(noteID),
		OwnerID: pgUUID(ownerID),
	})
	return fromDBSnapshot(snapshot), mapNoRows(err)
}
