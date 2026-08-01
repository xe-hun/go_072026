package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// keySet matches the top-level JWKS response shape returned by auth providers.
type keySet struct {
	Keys []jwkKey `json:"keys"`
}

// jwkKey contains the RSA fields needed to construct a public key for token
// verification. The N and E values are base64url-encoded modulus/exponent.
type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// jwksCache keeps public keys in memory for a short period so every request does
// not need to call the remote JWKS endpoint.
type jwksCache struct {
	// url is the configured JWKS endpoint.
	url string
	// client is bounded by a timeout so key refresh cannot hang request handling.
	client *http.Client
	// ttl controls how long keys are considered fresh.
	ttl time.Duration

	// mu protects keys and loadedAt because requests can verify tokens
	// concurrently.
	mu sync.RWMutex
	// keys maps JWT kid header values to RSA public keys.
	keys map[string]*rsa.PublicKey
	// loadedAt records when keys were last refreshed.
	loadedAt time.Time
}

// newJWKSCache builds the cache with conservative defaults. It does not perform
// I/O; callers decide when to refresh.
func newJWKSCache(url string) *jwksCache {
	return &jwksCache{
		url:    url,
		client: &http.Client{Timeout: 10 * time.Second},
		ttl:    5 * time.Minute,
		keys:   map[string]*rsa.PublicKey{},
	}
}

// refresh downloads the JWKS document, converts usable RSA keys, and atomically
// replaces the cache contents.
func (c *jwksCache) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("jwks endpoint returned status %d", resp.StatusCode)
	}

	var set keySet
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return err
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, raw := range set.Keys {
		// Ignore unsupported or malformed keys instead of failing the entire
		// refresh, as long as at least one usable RSA key remains.
		if raw.Kty != "RSA" || raw.Kid == "" {
			continue
		}
		key, err := rsaPublicKey(raw.N, raw.E)
		if err != nil {
			continue
		}
		keys[raw.Kid] = key
	}
	if len(keys) == 0 {
		return errors.New("jwks contained no usable RSA keys")
	}

	c.mu.Lock()
	c.keys = keys
	c.loadedAt = time.Now()
	c.mu.Unlock()
	return nil
}

// get returns the key for a JWT kid. It uses a fast read lock for fresh keys and
// refreshes from the remote endpoint when the cache is stale or incomplete.
func (c *jwksCache) get(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	key, ok := c.keys[kid]
	fresh := time.Since(c.loadedAt) < c.ttl
	c.mu.RUnlock()
	if ok && fresh {
		return key, nil
	}

	if err := c.refresh(ctx); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	key, ok = c.keys[kid]
	if !ok {
		return nil, fmt.Errorf("jwt key %q not found", kid)
	}
	return key, nil
}

// rsaPublicKey converts the base64url modulus/exponent values from JWK format
// into the rsa.PublicKey required by the JWT library.
func rsaPublicKey(n64, e64 string) (*rsa.PublicKey, error) {
	modulusBytes, err := base64.RawURLEncoding.DecodeString(n64)
	if err != nil {
		return nil, err
	}
	exponentBytes, err := base64.RawURLEncoding.DecodeString(e64)
	if err != nil {
		return nil, err
	}
	exponent := 0
	for _, b := range exponentBytes {
		exponent = exponent<<8 + int(b)
	}
	if exponent == 0 {
		return nil, errors.New("invalid RSA exponent")
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(modulusBytes),
		E: exponent,
	}, nil
}
