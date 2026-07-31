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

type keySet struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksCache struct {
	url    string
	client *http.Client
	ttl    time.Duration

	mu       sync.RWMutex
	keys     map[string]*rsa.PublicKey
	loadedAt time.Time
}

func newJWKSCache(url string) *jwksCache {
	return &jwksCache{
		url:    url,
		client: &http.Client{Timeout: 10 * time.Second},
		ttl:    5 * time.Minute,
		keys:   map[string]*rsa.PublicKey{},
	}
}

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
