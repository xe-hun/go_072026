package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	defaultOutDir   = ".dev/auth"
	defaultIssuer   = "notes-dev"
	defaultAudience = "notes-api"
	defaultJWKSURL  = "http://localhost:8081/.well-known/jwks.json"
)

type jwkSet struct {
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

type metadata struct {
	Issuer    string    `json:"issuer"`
	Audience  string    `json:"audience"`
	Subject   string    `json:"subject"`
	KeyID     string    `json:"kid"`
	Algorithm string    `json:"algorithm"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	JWKSURL   string    `json:"jwksUrl"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "generate":
		err = generate(os.Args[2:])
	case "serve":
		err = serve(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage:
  go run ./cmd/devauth generate [flags]
  go run ./cmd/devauth serve [flags]
  go run ./cmd/devauth verify [flags]

This is a development-only fake JWT/JWKS helper. Do not use its generated
private key or long-lived tokens for live user authentication.

`)
}

func generate(args []string) error {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	outDir := fs.String("out", defaultOutDir, "directory for generated dev auth files")
	issuer := fs.String("issuer", envOrDefault("JWT_ISSUER", defaultIssuer), "JWT issuer claim")
	audience := fs.String("audience", envOrDefault("JWT_AUDIENCE", defaultAudience), "JWT audience claim")
	subject := fs.String("subject", "", "JWT subject UUID; generated when empty")
	kid := fs.String("kid", "", "JWT key id; generated when empty")
	days := fs.Int("days", 365, "token lifetime in days")
	jwksURL := fs.String("jwks-url", defaultJWKSURL, "JWKS URL to write into the generated env file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*issuer) == "" {
		return errors.New("issuer is required")
	}
	if strings.TrimSpace(*audience) == "" {
		return errors.New("audience is required")
	}
	if *days < 1 {
		return errors.New("days must be greater than zero")
	}

	userID, err := subjectUUID(*subject)
	if err != nil {
		return err
	}
	keyID := strings.TrimSpace(*kid)
	if keyID == "" {
		raw := strings.ReplaceAll(uuid.NewString(), "-", "")
		keyID = "dev-" + raw[:12]
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(*days) * 24 * time.Hour)
	claims := jwt.RegisteredClaims{
		Issuer:    strings.TrimSpace(*issuer),
		Subject:   userID.String(),
		Audience:  jwt.ClaimStrings{strings.TrimSpace(*audience)},
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		NotBefore: jwt.NewNumericDate(now),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        uuid.NewString(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = keyID
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(*outDir, 0700); err != nil {
		return err
	}
	files := map[string][]byte{
		"private_key.pem": privateKeyPEM(privateKey),
		"jwks.json":       mustJSON(jwkSet{Keys: []jwkKey{publicJWK(&privateKey.PublicKey, keyID)}}),
		"token.txt":       []byte(signedToken + "\n"),
		"metadata.json": mustJSON(metadata{
			Issuer:    claims.Issuer,
			Audience:  claims.Audience[0],
			Subject:   claims.Subject,
			KeyID:     keyID,
			Algorithm: "RS256",
			IssuedAt:  now,
			ExpiresAt: expiresAt,
			JWKSURL:   strings.TrimSpace(*jwksURL),
		}),
		"env": []byte(fmt.Sprintf(
			"JWT_ISSUER=%s\nJWT_AUDIENCE=%s\nJWT_JWKS_URL=%s\n",
			claims.Issuer,
			claims.Audience[0],
			strings.TrimSpace(*jwksURL),
		)),
		"WARNING.txt": []byte("Development-only fake auth fixture. Do not use this private key or token for live user authentication.\n"),
	}
	for name, data := range files {
		mode := os.FileMode(0600)
		if name == "jwks.json" || name == "metadata.json" || name == "env" || name == "WARNING.txt" {
			mode = 0644
		}
		if err := os.WriteFile(filepath.Join(*outDir, name), data, mode); err != nil {
			return err
		}
	}

	fmt.Printf("Generated dev auth fixture in %s\n", *outDir)
	fmt.Printf("Subject: %s\n", claims.Subject)
	fmt.Printf("Issuer: %s\n", claims.Issuer)
	fmt.Printf("Audience: %s\n", claims.Audience[0])
	fmt.Printf("Expires: %s\n", expiresAt.Format(time.RFC3339))
	return nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dir := fs.String("dir", defaultOutDir, "directory containing jwks.json")
	addr := fs.String("addr", ":8081", "HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	jwksPath := filepath.Join(*dir, "jwks.json")
	if _, err := os.Stat(jwksPath); err != nil {
		return fmt.Errorf("jwks file is not available at %s: %w", jwksPath, err)
	}

	mux := jwksMux(jwksPath)
	log.Printf("Serving development JWKS from %s at http://localhost%s/.well-known/jwks.json", jwksPath, *addr)
	return http.ListenAndServe(*addr, mux)
}

func jwksMux(jwksPath string) http.Handler {
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(jwksPath)
		if err != nil {
			http.Error(w, "jwks unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
	mux.HandleFunc("/.well-known/jwks.json", handler)
	mux.HandleFunc("/jwks.json", handler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "development JWKS is available at /.well-known/jwks.json")
	})
	return mux
}

func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	dir := fs.String("dir", defaultOutDir, "directory containing generated files")
	tokenPath := fs.String("token", "", "token file; defaults to <dir>/token.txt")
	jwksPath := fs.String("jwks", "", "JWKS file; defaults to <dir>/jwks.json")
	issuer := fs.String("issuer", "", "expected issuer; defaults to metadata or notes-dev")
	audience := fs.String("audience", "", "expected audience; defaults to metadata or notes-api")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tokenPath == "" {
		*tokenPath = filepath.Join(*dir, "token.txt")
	}
	if *jwksPath == "" {
		*jwksPath = filepath.Join(*dir, "jwks.json")
	}
	meta := readMetadata(filepath.Join(*dir, "metadata.json"))
	expectedIssuer := firstNonEmpty(*issuer, meta.Issuer, envOrDefault("JWT_ISSUER", defaultIssuer))
	expectedAudience := firstNonEmpty(*audience, meta.Audience, envOrDefault("JWT_AUDIENCE", defaultAudience))

	tokenBytes, err := os.ReadFile(*tokenPath)
	if err != nil {
		return err
	}
	jwksBytes, err := os.ReadFile(*jwksPath)
	if err != nil {
		return err
	}
	keys, err := publicKeys(jwksBytes)
	if err != nil {
		return err
	}

	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(strings.TrimSpace(string(tokenBytes)), claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, errors.New("unexpected jwt signing method")
		}
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("jwt kid is required")
		}
		key, ok := keys[kid]
		if !ok {
			return nil, fmt.Errorf("jwt key %q not found", kid)
		}
		return key, nil
	}, jwt.WithIssuer(expectedIssuer), jwt.WithAudience(expectedAudience), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid {
		return errors.New("token did not verify")
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return fmt.Errorf("subject is not a UUID: %w", err)
	}

	fmt.Printf("Verified development token for subject %s\n", claims.Subject)
	fmt.Printf("Issuer: %s\n", expectedIssuer)
	fmt.Printf("Audience: %s\n", expectedAudience)
	if claims.ExpiresAt != nil {
		fmt.Printf("Expires: %s\n", claims.ExpiresAt.Time.Format(time.RFC3339))
	}
	return nil
}

func subjectUUID(raw string) (uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.New(), nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("subject must be a UUID: %w", err)
	}
	return parsed, nil
}

func privateKeyPEM(key *rsa.PrivateKey) []byte {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func publicJWK(key *rsa.PublicKey, kid string) jwkKey {
	return jwkKey{
		Kty: "RSA",
		Kid: kid,
		Use: "sig",
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

func publicKeys(data []byte) (map[string]*rsa.PublicKey, error) {
	var set jwkSet
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, err
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
		return nil, errors.New("jwks contained no usable RSA keys")
	}
	return keys, nil
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

func mustJSON(value any) []byte {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

func readMetadata(path string) metadata {
	data, err := os.ReadFile(path)
	if err != nil {
		return metadata{}
	}
	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return metadata{}
	}
	return meta
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
