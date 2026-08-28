package syncapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSyncFullNoteHTTPRequiresAuthentication(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/syncFullNote", bytes.NewBufferString(`{"deviceId":"phone","noteIds":[]}`))
	recorder := httptest.NewRecorder()

	NewHandler(nil).SyncFullNoteHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
