package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFullNoteRequiresAuthentication(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/syncFullNote", bytes.NewBufferString(`{"deviceId":"phone","noteIds":[]}`))
	recorder := httptest.NewRecorder()

	New(nil).FullNote(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
