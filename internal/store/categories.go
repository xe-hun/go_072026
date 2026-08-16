package store

import (
	"context"

	"github.com/google/uuid"

	db "notes-server/db/generated"
)

// CreateCategory inserts a user-owned category.
func (s *Store) CreateCategory(ctx context.Context, id, ownerID uuid.UUID, name string) error {
	return s.q.CreateCategory(ctx, db.CreateCategoryParams{
		ID:      pgUUID(id),
		OwnerID: pgUUID(ownerID),
		Name:    name,
	})
}

// LockCategoryForOwner locks an active category without loading its fields.
func (s *Store) LockCategoryForOwner(ctx context.Context, id, ownerID uuid.UUID) error {
	_, err := s.q.GetCategoryForOwnerForUpdate(ctx, db.GetCategoryForOwnerForUpdateParams{
		ID:      pgUUID(id),
		OwnerID: pgUUID(ownerID),
	})
	return mapNoRows(err)
}

// UpdateCategory renames an active category.
func (s *Store) UpdateCategory(ctx context.Context, id, ownerID uuid.UUID, name string) error {
	return s.q.UpdateCategory(ctx, db.UpdateCategoryParams{
		ID:      pgUUID(id),
		OwnerID: pgUUID(ownerID),
		Name:    name,
	})
}

// DeleteCategory soft deletes an active category.
func (s *Store) DeleteCategory(ctx context.Context, id, ownerID uuid.UUID) error {
	return s.q.DeleteCategory(ctx, db.DeleteCategoryParams{
		ID:      pgUUID(id),
		OwnerID: pgUUID(ownerID),
	})
}
