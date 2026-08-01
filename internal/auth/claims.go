package auth

import (
	"context"

	"github.com/google/uuid"
)

// Claims is the small authenticated identity object the application needs after
// JWT validation. Additional token claims can be added here later if services
// need them.
type Claims struct {
	// UserID is parsed from the JWT subject and used as owner_id in database
	// queries.
	UserID uuid.UUID
}

// claimsKey is private so only this package can write/read auth claims from a
// context without collision.
type claimsKey struct{}

// WithClaims stores authenticated claims on a context for downstream handlers
// and services.
func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

// ClaimsFromContext retrieves the authenticated claims added by auth middleware.
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(Claims)
	return claims, ok
}

// UserIDFromContext is a convenience helper for handlers that only need the
// authenticated owner ID.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	claims, ok := ClaimsFromContext(ctx)
	return claims.UserID, ok
}
