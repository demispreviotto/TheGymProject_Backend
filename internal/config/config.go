// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	Addr            string
	DatabaseURL     string
	SupabaseJWTKey  string // HS256 secret of the Supabase project (Settings → API → JWT secret)
	AllowedOrigin   string
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Addr:            getenv("ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		SupabaseJWTKey:  os.Getenv("SUPABASE_JWT_SECRET"),
		AllowedOrigin:   getenv("ALLOWED_ORIGIN", "http://localhost:4200"),
		ShutdownTimeout: 10 * time.Second,
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.SupabaseJWTKey == "" {
		return Config{}, fmt.Errorf("SUPABASE_JWT_SECRET is required")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
