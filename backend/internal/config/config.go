package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPAddr       string
	GinMode        string
	LogDir         string
	LogMaxSizeMB   int
	LogMaxBackups  int
	LogMaxAgeDays  int
	DatabaseURL    string
	RedisAddr      string
	RedisPassword  string
	RedisDB        int
	SessionTTL     time.Duration
	CookieSecure   bool
	CookieSameSite string
	CORSOrigins    []string
	R2Endpoint     string
	R2AccessKey    string
	R2SecretKey    string
	R2Bucket       string
	R2Region       string
	R2RootPrefix   string
	R2CacheTTL     time.Duration
	AdminUsername  string
	AdminPassword  string
}

func Load() Config {
	loadDotenv()

	return Config{
		HTTPAddr:       env("PANSHOW_HTTP_ADDR", ":5245"),
		GinMode:        env("PANSHOW_GIN_MODE", "release"),
		LogDir:         resolveAppPath(env("PANSHOW_LOG_DIR", "logs")),
		LogMaxSizeMB:   envInt("PANSHOW_LOG_MAX_SIZE_MB", 50),
		LogMaxBackups:  envInt("PANSHOW_LOG_MAX_BACKUPS", 14),
		LogMaxAgeDays:  envInt("PANSHOW_LOG_MAX_AGE_DAYS", 30),
		DatabaseURL:    env("PANSHOW_DATABASE_URL", ""),
		RedisAddr:      env("PANSHOW_REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:  env("PANSHOW_REDIS_PASSWORD", ""),
		RedisDB:        envInt("PANSHOW_REDIS_DB", 0),
		SessionTTL:     time.Duration(envInt("PANSHOW_SESSION_TTL_HOURS", 24)) * time.Hour,
		CookieSecure:   envBool("PANSHOW_COOKIE_SECURE", false),
		CookieSameSite: env("PANSHOW_COOKIE_SAME_SITE", "lax"),
		CORSOrigins:    envList("PANSHOW_CORS_ORIGINS", []string{"http://localhost:5173", "http://127.0.0.1:5173"}),
		R2Endpoint:     env("PANSHOW_R2_ENDPOINT", ""),
		R2AccessKey:    env("PANSHOW_R2_ACCESS_KEY", ""),
		R2SecretKey:    env("PANSHOW_R2_SECRET_KEY", ""),
		R2Bucket:       env("PANSHOW_R2_BUCKET", ""),
		R2Region:       env("PANSHOW_R2_REGION", "auto"),
		R2RootPrefix:   env("PANSHOW_R2_ROOT_PREFIX", ""),
		R2CacheTTL:     time.Duration(envInt("PANSHOW_R2_CACHE_TTL_SECONDS", 60*30)) * time.Second,
		AdminUsername:  env("PANSHOW_ADMIN_USERNAME", ""),
		AdminPassword:  env("PANSHOW_ADMIN_PASSWORD", ""),
	}
}

func loadDotenv() {
	for _, path := range dotenvPaths() {
		_ = godotenv.Load(path)
	}
}

func dotenvPaths() []string {
	paths := make([]string, 0, 6)
	if executable, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executable)
		paths = append(paths,
			filepath.Join(executableDir, ".env"),
			filepath.Join(executableDir, "config", ".env"),
		)
	}

	paths = append(paths,
		".env",
		filepath.Join("config", ".env"),
		filepath.Join("backend", ".env"),
	)
	return uniquePaths(paths)
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		key := filepath.Clean(path)
		if absolute, err := filepath.Abs(path); err == nil {
			key = filepath.Clean(absolute)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, path)
	}
	return unique
}

func resolveAppPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(appDir(), path)
}

func appDir() string {
	if executable, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executable)
		if tempDir := os.TempDir(); tempDir != "" {
			if rel, err := filepath.Rel(tempDir, executableDir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				if cwd, err := os.Getwd(); err == nil {
					return cwd
				}
			}
		}
		return executableDir
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func envList(key string, fallback []string) []string {
	raw := os.Getenv(key)
	if raw == "" {
		if legacy := os.Getenv("PANSHOW_CORS_ORIGIN"); legacy != "" {
			raw = legacy
		} else {
			return fallback
		}
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return fallback
	}
	return values
}
