package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientUsesSupabaseStorageContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "service-secret" || r.Header.Get("Authorization") != "Bearer service-secret" {
			t.Fatalf("missing trusted storage headers: %v", r.Header)
		}
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/storage/v1/object/upload/sign/"):
			if r.Header.Get("x-upsert") != "" {
				t.Fatal("signed upload creation must not enable upsert")
			}
			_, _ = w.Write([]byte(`{"url":"/object/upload/sign/documents/notes/n%2Ffile.pdf?token=upload-token"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/storage/v1/object/info/"):
			_, _ = w.Write([]byte(`{"size":12,"content_type":"application/pdf"}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/storage/v1/object/sign/"):
			_, _ = w.Write([]byte(`{"signedURL":"/object/sign/documents/notes/file.pdf?token=download-token"}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/storage/v1/object/"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "service-secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	signed, err := client.CreateSignedUploadURL(ctx, "documents", "notes/file.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if signed.Token != "upload-token" || !strings.Contains(signed.URL, "/storage/v1/object/upload/sign/documents/notes/n/file.pdf") {
		t.Fatalf("unexpected signed upload: %+v", signed)
	}
	info, err := client.GetObjectInfo(ctx, "documents", "notes/file.pdf")
	if err != nil || info.Size != 12 || info.ContentType != "application/pdf" {
		t.Fatalf("unexpected object info: %+v, %v", info, err)
	}
	downloadURL, err := client.CreateSignedDownloadURL(ctx, "documents", "notes/file.pdf", 60)
	if err != nil || !strings.Contains(downloadURL, "download-token") {
		t.Fatalf("unexpected download URL: %s, %v", downloadURL, err)
	}
	if err := client.DeleteObject(ctx, "documents", "notes/file.pdf"); err != nil {
		t.Fatal(err)
	}
}
