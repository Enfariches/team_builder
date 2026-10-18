package platform

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const minJWTSecretLen = 32

type Config struct {
	DatabaseURL string
	JWTSecret   []byte
	Port        string
	AppEnv      string
	LogLevel    string
	CORSOrigins []string
	AccessTTL   time.Duration
	RefreshTTL  time.Duration
}

// LoadConfig читает конфиг только из env; обязательные значения без дефолта,
// чтобы неверно сконфигурированный сервис падал на старте, а не в рантайме.
func LoadConfig() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   []byte(os.Getenv("JWT_SECRET")),
		Port:        getenv("PORT", "8080"),
		AppEnv:      getenv("APP_ENV", "local"),
		LogLevel:    getenv("LOG_LEVEL", "info"),
		AccessTTL:   30 * time.Minute,
		RefreshTTL:  30 * 24 * time.Hour,
	}
	for _, o := range strings.Split(os.Getenv("CORS_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			cfg.CORSOrigins = append(cfg.CORSOrigins, o)
		}
	}

	var err error
	if cfg.AccessTTL, err = durationEnv("ACCESS_TTL", cfg.AccessTTL); err != nil {
		return Config{}, err
	}
	if cfg.RefreshTTL, err = durationEnv("REFRESH_TTL", cfg.RefreshTTL); err != nil {
		return Config{}, err
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < minJWTSecretLen {
		return Config{}, fmt.Errorf("JWT_SECRET must be at least %d bytes", minJWTSecretLen)
	}
	return cfg, nil
}

// SecureCookies: на http-стендах (local/test) Secure-cookie браузер не вернёт.
func (c Config) SecureCookies() bool {
	return c.AppEnv != "local" && c.AppEnv != "test"
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func durationEnv(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s: invalid duration %q", key, v)
	}
	return d, nil
}
