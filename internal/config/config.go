// Package config membaca konfigurasi dari environment variable.
//
// Semua konfigurasi masuk lewat env, tidak ada file config yang di-commit.
// Alasannya: proses yang sama harus bisa dijalankan di laptop, di CI, dan di
// VPS tanpa dibangun ulang — yang berbeda cuma env-nya.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

type Config struct {
	Env                string // "development" | "production"
	HTTPPort           string
	DatabaseURL        string
	RedisURL           string
	LogLevel           slog.Level
	JWTSecret          string
	AccessTokenTTL     time.Duration
	CampusTZ           *time.Location // zona waktu kampus, misal Asia/Jakarta
	SlotHorizonDays    int             // berapa hari ke depan slot harus di-generate
	BookingMinLeadMin  int             // minimal menit sebelum slot boleh dipesan
}

// Load membaca env dan mengembalikan error kalau ada yang wajib tapi kosong.
//
// Perhatikan: fungsi ini MENGEMBALIKAN error, bukan panic. Di Go, panic
// disediakan untuk keadaan yang benar-benar tidak bisa dilanjutkan (bug
// programmer), sedangkan "user lupa mengisi env" adalah kondisi yang wajar
// dan harus dilaporkan dengan rapi. Aturan praktisnya: kalau pemanggil masih
// mungkin melakukan sesuatu terhadap kegagalan itu, kembalikan error.
func Load() (Config, error) {
	cfg := Config{
		Env:         env("APP_ENV", "development"),
		HTTPPort:    env("HTTP_PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		RedisURL:    env("REDIS_URL", "redis://localhost:6379/0"),
		LogLevel:    parseLevel(env("LOG_LEVEL", "info")),
		JWTSecret:   os.Getenv("JWT_SECRET"),
	}

	ttl, err := time.ParseDuration(env("ACCESS_TOKEN_TTL", "15m"))
	if err != nil {
		return Config{}, fmt.Errorf("ACCESS_TOKEN_TTL tidak valid: %w", err)
	}
	cfg.AccessTokenTTL = ttl

	// Zona waktu kampus. Default Asia/Jakarta. Gagal muat = gagal start.
	campusTZ := env("CAMPUS_TIMEZONE", "Asia/Jakarta")
	loc, err := time.LoadLocation(campusTZ)
	if err != nil {
		return Config{}, fmt.Errorf("CAMPUS_TIMEZONE %q tidak valid: %w", campusTZ, err)
	}
	cfg.CampusTZ = loc

	// Berapa hari ke depan slot di-generate. Default 30 hari.
	cfg.SlotHorizonDays = 30
	if h := os.Getenv("SLOT_HORIZON_DAYS"); h != "" {
		var n int
		_, err := fmt.Sscanf(h, "%d", &n)
		if err != nil || n < 1 {
			return Config{}, fmt.Errorf("SLOT_HORIZON_DAYS %q harus angka positif: %w", h, err)
		}
		cfg.SlotHorizonDays = n
	}

	// Minimal menit sebelum slot boleh dipesan. Default 60 menit.
	cfg.BookingMinLeadMin = 60
	if l := os.Getenv("BOOKING_MIN_LEAD_MINUTES"); l != "" {
		var n int
		_, err := fmt.Sscanf(l, "%d", &n)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("BOOKING_MIN_LEAD_MINUTES %q harus angka non-negatif: %w", l, err)
		}
		cfg.BookingMinLeadMin = n
	}

	// Dikumpulkan dulu semuanya, baru dilaporkan sekaligus. Melaporkan satu
	// per satu memaksa orang menjalankan ulang program berkali-kali untuk
	// menemukan bahwa ada 3 env yang kurang.
	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("env wajib belum diisi: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

func (c Config) IsDevelopment() bool { return c.Env == "development" }

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
