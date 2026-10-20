package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the immutable runtime configuration used by the API, WebSocket,
// and worker processes.
// Values are read once at startup so the rest of the code can depend on a stable,
// validated configuration object.
type Config struct {
	// AppEnv names the runtime environment, for example development or production.
	AppEnv string
	// Port is the HTTP port used by the API process.
	Port string
	// WebSocketPort is the HTTP port used by the WebSocket process.
	WebSocketPort string
	// LogLevel controls slog verbosity.
	LogLevel string

	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// MaxDBConnections limits the pgxpool size per process.
	MaxDBConnections int32

	// JWTIssuer must match the issuer claim in accepted access tokens.
	JWTIssuer string
	// JWTAudience must match the audience claim in accepted access tokens.
	JWTAudience string
	// JWTJWKSURL points to the JSON Web Key Set used to verify JWT signatures.
	JWTJWKSURL string

	// SyncMaxOperations caps the number of operations accepted in one sync batch.
	SyncMaxOperations int
	// SyncDefaultPullLimit is used when the client omits a pull limit.
	SyncDefaultPullLimit int
	// SyncMaxPullLimit prevents very large pull responses.
	SyncMaxPullLimit int
	// SnapshotChangeThreshold queues snapshots after this many changes.
	SnapshotChangeThreshold int64
	// SnapshotByteThreshold queues snapshots after this much change payload data.
	SnapshotByteThreshold int64
	// ChangeRetentionDays is reserved for cleanup/compaction policy.
	ChangeRetentionDays int

	// RequestTimeout bounds handler execution time.
	RequestTimeout time.Duration
	// MaxCompressedRequestBytes limits the wire-size request body.
	MaxCompressedRequestBytes int64
	// MaxDecompressedRequestBytes limits the expanded gzip body.
	MaxDecompressedRequestBytes int64

	// SupabaseURL is the project URL used to reach Supabase Storage.
	SupabaseURL string
	// SupabaseSecretKey is used only by trusted backend requests to Storage.
	SupabaseSecretKey string
	// SupabaseStorageBucket is the private bucket used for note attachments.
	SupabaseStorageBucket string
	// SupabaseStorageMaxFileSize is the application-side upload limit.
	SupabaseStorageMaxFileSize int64
	// SupabaseStorageAllowedMimeTypes is the application-side MIME allowlist.
	SupabaseStorageAllowedMimeTypes []string
	// SupabaseStorageMaxPendingUploads limits active upload authorizations per user.
	SupabaseStorageMaxPendingUploads int
	// SupabaseStorageUploadTTL is the documented lifetime of a signed upload URL.
	SupabaseStorageUploadTTL time.Duration
	// SupabaseStorageDownloadTTL controls the lifetime of signed download URLs.
	SupabaseStorageDownloadTTL time.Duration
	// SupabaseStorageCleanupGrace delays abandoned-upload cleanup after expiry.
	SupabaseStorageCleanupGrace time.Duration
	// SupabaseStorageHTTPTimeout bounds one Storage API request.
	SupabaseStorageHTTPTimeout time.Duration
}

// Load reads .env for local development, reads process environment variables,
// applies defaults for optional values, and validates required values.
func Load() (Config, error) {
	// Missing .env is allowed: production deployments usually supply real
	// environment variables directly.
	_ = loadDotEnv(".env")

	cfg := Config{
		AppEnv:                           get("APP_ENV", "development"),
		Port:                             get("PORT", "8080"),
		WebSocketPort:                    get("WEBSOCKET_PORT", "8082"),
		LogLevel:                         get("LOG_LEVEL", "info"),
		DatabaseURL:                      os.Getenv("DATABASE_URL"),
		JWTIssuer:                        os.Getenv("JWT_ISSUER"),
		JWTAudience:                      os.Getenv("JWT_AUDIENCE"),
		JWTJWKSURL:                       os.Getenv("JWT_JWKS_URL"),
		MaxDBConnections:                 int32(getInt("MAX_DB_CONNECTIONS", 10)),
		SyncMaxOperations:                getInt("SYNC_MAX_OPERATIONS", 500),
		SyncDefaultPullLimit:             getInt("SYNC_DEFAULT_PULL_LIMIT", 500),
		SyncMaxPullLimit:                 getInt("SYNC_MAX_PULL_LIMIT", 1000),
		SnapshotChangeThreshold:          int64(getInt("SNAPSHOT_CHANGE_THRESHOLD", 100)),
		SnapshotByteThreshold:            int64(getInt("SNAPSHOT_BYTE_THRESHOLD", 524288)),
		ChangeRetentionDays:              getInt("CHANGE_RETENTION_DAYS", 30),
		RequestTimeout:                   time.Duration(getInt("REQUEST_TIMEOUT_SECONDS", 30)) * time.Second,
		MaxCompressedRequestBytes:        int64(getInt("MAX_COMPRESSED_REQUEST_BYTES", 2*1024*1024)),
		MaxDecompressedRequestBytes:      int64(getInt("MAX_DECOMPRESSED_REQUEST_BYTES", 10*1024*1024)),
		SupabaseURL:                      os.Getenv("SUPABASE_URL"),
		SupabaseSecretKey:                os.Getenv("SUPABASE_SECRET_KEY"),
		SupabaseStorageBucket:            get("SUPABASE_STORAGE_BUCKET", "documents"),
		SupabaseStorageMaxFileSize:       int64(getInt("SUPABASE_STORAGE_MAX_FILE_SIZE", 10*1024*1024)),
		SupabaseStorageAllowedMimeTypes:  getCSV("SUPABASE_STORAGE_ALLOWED_MIME_TYPES", "application/pdf,image/jpeg,image/png"),
		SupabaseStorageMaxPendingUploads: getInt("SUPABASE_STORAGE_MAX_PENDING_UPLOADS", 20),
		SupabaseStorageUploadTTL:         time.Duration(getInt("SUPABASE_STORAGE_UPLOAD_TTL_SECONDS", 7200)) * time.Second,
		SupabaseStorageDownloadTTL:       time.Duration(getInt("SUPABASE_STORAGE_DOWNLOAD_TTL_SECONDS", 600)) * time.Second,
		SupabaseStorageCleanupGrace:      time.Duration(getInt("SUPABASE_STORAGE_CLEANUP_GRACE_SECONDS", 3600)) * time.Second,
		SupabaseStorageHTTPTimeout:       time.Duration(getInt("SUPABASE_STORAGE_HTTP_TIMEOUT_SECONDS", 15)) * time.Second,
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks settings that would make the process unsafe or unable to
// serve requests if left unset.
func (c Config) Validate() error {
	var missing []string
	required := map[string]string{
		"DATABASE_URL":            c.DatabaseURL,
		"JWT_ISSUER":              c.JWTIssuer,
		"JWT_AUDIENCE":            c.JWTAudience,
		"JWT_JWKS_URL":            c.JWTJWKSURL,
		"SUPABASE_URL":            c.SupabaseURL,
		"SUPABASE_SECRET_KEY":     c.SupabaseSecretKey,
		"SUPABASE_STORAGE_BUCKET": c.SupabaseStorageBucket,
	}
	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if c.MaxDBConnections < 1 {
		return errors.New("MAX_DB_CONNECTIONS must be greater than zero")
	}
	if c.SyncMaxOperations < 1 {
		return errors.New("SYNC_MAX_OPERATIONS must be greater than zero")
	}
	if c.SyncDefaultPullLimit < 1 || c.SyncMaxPullLimit < c.SyncDefaultPullLimit {
		return errors.New("sync pull limits are invalid")
	}
	if c.RequestTimeout <= 0 {
		return errors.New("REQUEST_TIMEOUT_SECONDS must be greater than zero")
	}
	if c.MaxCompressedRequestBytes <= 0 || c.MaxDecompressedRequestBytes <= 0 {
		return errors.New("request byte limits must be greater than zero")
	}
	if c.SupabaseStorageMaxFileSize <= 0 {
		return errors.New("SUPABASE_STORAGE_MAX_FILE_SIZE must be greater than zero")
	}
	if strings.ContainsAny(c.SupabaseStorageBucket, "/\\") {
		return errors.New("SUPABASE_STORAGE_BUCKET must be a single bucket name")
	}
	if len(c.SupabaseStorageAllowedMimeTypes) == 0 {
		return errors.New("SUPABASE_STORAGE_ALLOWED_MIME_TYPES must contain at least one MIME type")
	}
	if c.SupabaseStorageMaxPendingUploads <= 0 {
		return errors.New("SUPABASE_STORAGE_MAX_PENDING_UPLOADS must be greater than zero")
	}
	if c.SupabaseStorageUploadTTL <= 0 || c.SupabaseStorageUploadTTL > 2*time.Hour {
		return errors.New("SUPABASE_STORAGE_UPLOAD_TTL_SECONDS must be between 1 and 7200")
	}
	if c.SupabaseStorageDownloadTTL <= 0 {
		return errors.New("SUPABASE_STORAGE_DOWNLOAD_TTL_SECONDS must be greater than zero")
	}
	if c.SupabaseStorageCleanupGrace <= 0 || c.SupabaseStorageHTTPTimeout <= 0 {
		return errors.New("Supabase cleanup grace and HTTP timeout must be greater than zero")
	}
	return nil
}

// get returns a trimmed environment value or the supplied fallback.
func get(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// getInt returns an integer environment value or the fallback when absent or
// malformed. Validation later catches impossible values.
func getInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getCSV(key, fallback string) []string {
	value := get(key, fallback)
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// loadDotEnv is a small development-only .env loader. It intentionally supports
// simple KEY=value files and avoids adding another runtime dependency.
func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"`)
		value = strings.Trim(value, `'`)
		if key != "" {
			_ = os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
