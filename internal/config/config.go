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

type Config struct {
	AppEnv   string
	Port     string
	LogLevel string

	DatabaseURL      string
	MaxDBConnections int32

	JWTIssuer   string
	JWTAudience string
	JWTJWKSURL  string

	SyncMaxOperations       int
	SyncDefaultPullLimit    int
	SyncMaxPullLimit        int
	SnapshotChangeThreshold int64
	SnapshotByteThreshold   int64
	ChangeRetentionDays     int

	RequestTimeout              time.Duration
	MaxCompressedRequestBytes   int64
	MaxDecompressedRequestBytes int64
}

func Load() (Config, error) {
	_ = loadDotEnv(".env")

	cfg := Config{
		AppEnv:                      get("APP_ENV", "development"),
		Port:                        get("PORT", "8080"),
		LogLevel:                    get("LOG_LEVEL", "info"),
		DatabaseURL:                 os.Getenv("DATABASE_URL"),
		JWTIssuer:                   os.Getenv("JWT_ISSUER"),
		JWTAudience:                 os.Getenv("JWT_AUDIENCE"),
		JWTJWKSURL:                  os.Getenv("JWT_JWKS_URL"),
		MaxDBConnections:            int32(getInt("MAX_DB_CONNECTIONS", 10)),
		SyncMaxOperations:           getInt("SYNC_MAX_OPERATIONS", 500),
		SyncDefaultPullLimit:        getInt("SYNC_DEFAULT_PULL_LIMIT", 500),
		SyncMaxPullLimit:            getInt("SYNC_MAX_PULL_LIMIT", 1000),
		SnapshotChangeThreshold:     int64(getInt("SNAPSHOT_CHANGE_THRESHOLD", 100)),
		SnapshotByteThreshold:       int64(getInt("SNAPSHOT_BYTE_THRESHOLD", 524288)),
		ChangeRetentionDays:         getInt("CHANGE_RETENTION_DAYS", 30),
		RequestTimeout:              time.Duration(getInt("REQUEST_TIMEOUT_SECONDS", 30)) * time.Second,
		MaxCompressedRequestBytes:   int64(getInt("MAX_COMPRESSED_REQUEST_BYTES", 2*1024*1024)),
		MaxDecompressedRequestBytes: int64(getInt("MAX_DECOMPRESSED_REQUEST_BYTES", 10*1024*1024)),
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var missing []string
	required := map[string]string{
		"DATABASE_URL": c.DatabaseURL,
		"JWT_ISSUER":   c.JWTIssuer,
		"JWT_AUDIENCE": c.JWTAudience,
		"JWT_JWKS_URL": c.JWTJWKSURL,
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
	return nil
}

func get(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

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
