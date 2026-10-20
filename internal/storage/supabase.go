// Package storage contains the trusted server-side Supabase Storage client.
package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrNotFound means that the requested object does not exist.
	ErrNotFound = errors.New("storage object not found")
	// ErrUnavailable means the Storage service could not be reached or returned
	// a temporary server-side failure.
	ErrUnavailable = errors.New("storage service unavailable")
	// ErrUpstream means Storage returned a non-success response that is not
	// safe to expose to an API client.
	ErrUpstream = errors.New("storage upstream error")
	// ErrInvalidResponse means Storage returned an unexpected successful payload.
	ErrInvalidResponse = errors.New("invalid storage response")
)

const maxResponseBodyBytes = 1 << 20

// Storage is the subset of Storage operations used by the uploads service.
// Keeping this interface small makes the business service easy to test without
// issuing live requests to Supabase.
type Storage interface {
	CreateSignedUploadURL(ctx context.Context, bucket, objectPath string) (SignedUpload, error)
	GetObjectInfo(ctx context.Context, bucket, objectPath string) (ObjectInfo, error)
	CreateSignedDownloadURL(ctx context.Context, bucket, objectPath string, expiresInSeconds int) (string, error)
	DeleteObject(ctx context.Context, bucket, objectPath string) error
}

// SignedUpload contains the temporary browser upload capability. It must never
// be persisted or written to logs.
type SignedUpload struct {
	URL   string
	Token string
}

// ObjectInfo is the independently observed metadata returned by Storage.
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// Client is a server-side HTTP client for the Supabase Storage REST API.
type Client struct {
	baseURL    *url.URL
	secretKey  string
	httpClient *http.Client
}

// NewClient creates a Storage client. The supplied project URL may be either a
// Supabase project URL or an already-qualified /storage/v1 URL.
func NewClient(projectURL, secretKey string, timeout time.Duration) (*Client, error) {
	if strings.TrimSpace(projectURL) == "" || strings.TrimSpace(secretKey) == "" {
		return nil, errors.New("Supabase URL and secret key are required")
	}
	if timeout <= 0 {
		return nil, errors.New("storage HTTP timeout must be greater than zero")
	}

	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(projectURL), "/"))
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("Supabase URL must be an absolute URL without credentials or query parameters")
	}
	if !strings.HasSuffix(strings.TrimRight(base.Path, "/"), "/storage/v1") {
		base.Path = strings.TrimRight(base.Path, "/") + "/storage/v1"
	}
	base.RawPath = ""

	return &Client{
		baseURL:   base,
		secretKey: secretKey,
		httpClient: &http.Client{
			Timeout: timeout,
			// A redirect could carry the service credential to an untrusted host.
			// Treat it as an upstream failure instead of following it.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// CreateSignedUploadURL requests Supabase's documented two-hour signed upload
// capability. Upsert is intentionally omitted so a generated path cannot be
// overwritten.
func (c *Client) CreateSignedUploadURL(ctx context.Context, bucket, objectPath string) (SignedUpload, error) {
	target, err := c.objectURL("/object/upload/sign", bucket, objectPath)
	if err != nil {
		return SignedUpload{}, err
	}
	var payload struct {
		URL string `json:"url"`
	}
	if err := c.doJSON(ctx, http.MethodPost, target, []byte(`{}`), http.StatusOK, &payload); err != nil {
		return SignedUpload{}, fmt.Errorf("create storage upload authorization: %w", err)
	}
	signedURL, token, err := c.resolveSignedURL(payload.URL)
	if err != nil {
		return SignedUpload{}, fmt.Errorf("parse storage upload authorization: %w", err)
	}
	return SignedUpload{URL: signedURL, Token: token}, nil
}

// GetObjectInfo reads object metadata from the Storage info endpoint. The
// current API returns size/content_type at the top level; metadata.size and
// metadata.mimetype are accepted because older Storage deployments returned
// those fields there.
func (c *Client) GetObjectInfo(ctx context.Context, bucket, objectPath string) (ObjectInfo, error) {
	target, err := c.objectURL("/object/info", bucket, objectPath)
	if err != nil {
		return ObjectInfo{}, err
	}
	var payload struct {
		Size            json.RawMessage            `json:"size"`
		ContentType     string                     `json:"content_type"`
		ContentTypeJSON string                     `json:"contentType"`
		Metadata        map[string]json.RawMessage `json:"metadata"`
	}
	if err := c.doJSON(ctx, http.MethodGet, target, nil, http.StatusOK, &payload); err != nil {
		return ObjectInfo{}, fmt.Errorf("get storage object info: %w", err)
	}

	size, err := numberFromJSON(payload.Size)
	if err != nil && payload.Metadata != nil {
		size, err = numberFromJSON(payload.Metadata["size"])
	}
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("storage object size: %w: %w", ErrInvalidResponse, err)
	}
	contentType := strings.TrimSpace(payload.ContentType)
	if contentType == "" {
		contentType = strings.TrimSpace(payload.ContentTypeJSON)
	}
	if contentType == "" && payload.Metadata != nil {
		for _, key := range []string{"mimetype", "contentType", "content_type"} {
			if raw := payload.Metadata[key]; len(raw) > 0 {
				_ = json.Unmarshal(raw, &contentType)
				if strings.TrimSpace(contentType) != "" {
					break
				}
			}
		}
	}
	if size < 0 || contentType == "" {
		return ObjectInfo{}, fmt.Errorf("storage object metadata is incomplete: %w", ErrInvalidResponse)
	}
	return ObjectInfo{Size: size, ContentType: contentType}, nil
}

// CreateSignedDownloadURL creates a short-lived signed URL for a private
// object. The URL is returned only to the authorized application caller.
func (c *Client) CreateSignedDownloadURL(ctx context.Context, bucket, objectPath string, expiresInSeconds int) (string, error) {
	if expiresInSeconds <= 0 {
		return "", errors.New("download URL expiration must be greater than zero")
	}
	target, err := c.objectURL("/object/sign", bucket, objectPath)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(map[string]int{"expiresIn": expiresInSeconds})
	if err != nil {
		return "", err
	}
	var payload struct {
		SignedURL string `json:"signedURL"`
	}
	if err := c.doJSON(ctx, http.MethodPost, target, body, http.StatusOK, &payload); err != nil {
		return "", fmt.Errorf("create storage download authorization: %w", err)
	}
	signedURL, _, err := c.resolveSignedURL(payload.SignedURL)
	if err != nil {
		return "", fmt.Errorf("parse storage download authorization: %w", err)
	}
	return signedURL, nil
}

// DeleteObject removes one object using only the trusted path stored by the
// application. A missing object is surfaced so callers can make deletion
// idempotent without hiding other Storage failures.
func (c *Client) DeleteObject(ctx context.Context, bucket, objectPath string) error {
	target, err := c.objectURL("/object", bucket, objectPath)
	if err != nil {
		return err
	}
	if err := c.doJSON(ctx, http.MethodDelete, target, nil, http.StatusOK, nil); err != nil {
		return fmt.Errorf("delete storage object: %w", err)
	}
	return nil
}

func (c *Client) objectURL(prefix, bucket, objectPath string) (string, error) {
	if strings.TrimSpace(bucket) == "" || strings.ContainsAny(bucket, "/\\") {
		return "", errors.New("invalid storage bucket")
	}
	escapedPath, err := escapeObjectPath(objectPath)
	if err != nil {
		return "", err
	}
	base := strings.TrimRight(c.baseURL.String(), "/")
	return base + prefix + "/" + url.PathEscape(bucket) + "/" + escapedPath, nil
}

func escapeObjectPath(objectPath string) (string, error) {
	if objectPath == "" || strings.Contains(objectPath, "\\") || strings.HasPrefix(objectPath, "/") || strings.HasSuffix(objectPath, "/") {
		return "", errors.New("invalid storage object path")
	}
	segments := strings.Split(objectPath, "/")
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("invalid storage object path")
		}
		escaped = append(escaped, url.PathEscape(segment))
	}
	return strings.Join(escaped, "/"), nil
}

func (c *Client) resolveSignedURL(raw string) (string, string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", "", fmt.Errorf("%w: signed URL is missing", ErrInvalidResponse)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("%w: malformed signed URL", ErrInvalidResponse)
	}
	base := *c.baseURL
	if parsed.IsAbs() {
		if parsed.Scheme != base.Scheme || !strings.EqualFold(parsed.Host, base.Host) {
			return "", "", fmt.Errorf("%w: signed URL host is not the configured Storage host", ErrInvalidResponse)
		}
	} else {
		origin := base
		origin.Path = ""
		origin.RawPath = ""
		if strings.HasPrefix(parsed.Path, "/storage/v1/") {
			parsed = origin.ResolveReference(parsed)
		} else if strings.HasPrefix(parsed.Path, "/") {
			parsed = base.ResolveReference(&url.URL{Path: strings.TrimRight(base.Path, "/") + parsed.Path, RawQuery: parsed.RawQuery})
		} else {
			parsed = base.ResolveReference(parsed)
		}
	}
	token := parsed.Query().Get("token")
	if token == "" {
		return "", "", fmt.Errorf("%w: signed URL token is missing", ErrInvalidResponse)
	}
	return parsed.String(), token, nil
}

func (c *Client) doJSON(ctx context.Context, method, target string, body []byte, expectedStatus int, result any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	request.Header.Set("apikey", c.secretKey)
	request.Header.Set("Authorization", "Bearer "+c.secretKey)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()

	statusMatches := response.StatusCode == expectedStatus || result == nil && expectedStatus == http.StatusOK && response.StatusCode == http.StatusNoContent
	if !statusMatches {
		if response.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: status %d", ErrNotFound, response.StatusCode)
		}
		if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			return fmt.Errorf("%w: status %d", ErrUnavailable, response.StatusCode)
		}
		return fmt.Errorf("%w: status %d", ErrUpstream, response.StatusCode)
	}
	if result == nil {
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBodyBytes))
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("%w: response JSON could not be decoded", ErrInvalidResponse)
	}
	return nil
}

func numberFromJSON(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, errors.New("size is missing")
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		value, err := strconv.ParseInt(string(number), 10, 64)
		if err == nil {
			return value, nil
		}
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		value, err := strconv.ParseInt(text, 10, 64)
		if err == nil {
			return value, nil
		}
	}
	return 0, errors.New("size is not an integer")
}
