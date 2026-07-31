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

type Verifier struct {
	issuer   string
	audience string
	keys     *jwksCache
}

type jwtClaims struct {
	jwt.RegisteredClaims
}

func NewVerifier(ctx context.Context, issuer, audience, jwksURL string) (*Verifier, error) {
	cache := newJWKSCache(jwksURL)
	if err := cache.refresh(ctx); err != nil {
		return nil, err
	}
	return &Verifier{issuer: issuer, audience: audience, keys: cache}, nil
}

func (v *Verifier) Verify(ctx context.Context, tokenString string) (Claims, error) {
	claims := &jwtClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, errors.New("unexpected jwt signing method")
		}
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("jwt kid is required")
		}
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
