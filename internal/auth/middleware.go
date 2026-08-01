package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"notes-server/internal/httpapi"
)

// Verifier validates JWT access tokens using issuer/audience rules and a JWKS
// cache. It is safe to share across requests.
type Verifier struct {
	// issuer is the expected token issuer.
	issuer string
	// audience is the expected token audience.
	audience string
	// keys caches RSA public keys from the configured JWKS endpoint.
	keys *jwksCache
}

// jwtClaims embeds the standard registered JWT claims used by Supabase-style
// access tokens. The subject claim is interpreted as the application user UUID.
type jwtClaims struct {
	jwt.RegisteredClaims
}

// NewVerifier creates a verifier and eagerly loads JWKS keys so bad auth
// configuration fails during startup rather than during the first request.
func NewVerifier(ctx context.Context, issuer, audience, jwksURL string) (*Verifier, error) {
	cache := newJWKSCache(jwksURL)
	if err := cache.refresh(ctx); err != nil {
		return nil, err
	}
	return &Verifier{issuer: issuer, audience: audience, keys: cache}, nil
}

// Verify parses a bearer token, validates the signature and registered claims,
// then converts the JWT subject into the authenticated user ID.
func (v *Verifier) Verify(ctx context.Context, tokenString string) (Claims, error) {
	claims := &jwtClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		// Only RSA signing methods are accepted because JWKS contains RSA public
		// keys for this initial Supabase-compatible implementation.
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, errors.New("unexpected jwt signing method")
		}
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("jwt kid is required")
		}
		// The kid header selects the correct public key. The cache refreshes when
		// stale or when a key is missing.
		return v.keys.get(ctx, kid)
	}, jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return Claims{}, errors.New("invalid token")
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return Claims{}, errors.New("invalid subject")
	}
	return Claims{UserID: userID}, nil
}

// Middleware enforces bearer authentication and places auth.Claims into request
// context for handlers and services.
func Middleware(verifier *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				httpapi.WriteError(w, r, httpapi.Unauthorized())
				return
			}
			scheme, token, ok := strings.Cut(header, " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
				httpapi.WriteError(w, r, httpapi.Unauthorized())
				return
			}
			claims, err := verifier.Verify(r.Context(), strings.TrimSpace(token))
			if err != nil {
				httpapi.WriteError(w, r, httpapi.Unauthorized())
				return
			}
			next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
		})
	}
}
