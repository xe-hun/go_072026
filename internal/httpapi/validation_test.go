package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsMultipleDocuments(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/", strings.NewReader(`{"ok":true} {"ok":false}`))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]bool
	if err := DecodeJSON(req, &body); err == nil {
		t.Fatal("expected multiple JSON documents to fail")
	}
}
