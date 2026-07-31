package auth

import (
	"context"

	"github.com/google/uuid"
)

type Claims struct {
	UserID uuid.UUID
}

type claimsKey struct{}

func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(Claims)
	return claims, ok
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	claims, ok := ClaimsFromContext(ctx)
	return claims.UserID, ok
}
