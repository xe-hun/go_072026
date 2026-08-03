package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestGenerateAndVerify(t *testing.T) {
	dir := t.TempDir()
	subject := uuid.NewString()

	if err := generate([]string{
		"-out", dir,
		"-issuer", "test-issuer",
		"-audience", "test-audience",
		"-subject", subject,
		"-days", "365",
	}); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"private_key.pem", "jwks.json", "token.txt", "metadata.json", "env", "WARNING.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s to be generated: %v", name, err)
		}
	}

	if err := verify([]string{
		"-dir", dir,
		"-issuer", "test-issuer",
		"-audience", "test-audience",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestJWKSHandlerServesGeneratedFile(t *testing.T) {
	dir := t.TempDir()
	jwksPath := filepath.Join(dir, "jwks.json")
	if err := os.WriteFile(jwksPath, []byte(`{"keys":[]}`), 0600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	rec := httptest.NewRecorder()
	jwksMux(jwksPath).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected json content type, got %q", rec.Header().Get("Content-Type"))
	}
	if strings.TrimSpace(rec.Body.String()) != `{"keys":[]}` {
		t.Fatalf("unexpected jwks body %q", rec.Body.String())
	}
}
