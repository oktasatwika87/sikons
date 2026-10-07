// Command seed mengisi data awal ke database.
// Idempoten: dijalankan dua kali tidak menggandakan data.
//
// Password semua akun demo sengaja diketahui publik untuk keperluan pengujian.
// Ini AMAN karena hanya dipakai di lingkungan development/test.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/oktasatwika87/sikons/internal/auth"
	"github.com/oktasatwika87/sikons/internal/db"
)

// Ambil DATABASE_URL langsung dari env, tidak lewat config.Load() yang juga
// butuh konfigurasi lain yang tidak diperlukan seed.
func databaseURL() string {
	url := os.Getenv("DATABASE_URL")
	if url != "" {
		return url
	}

	// Fallback untuk compose: host postgres, port 5432.
	host := getEnvOr("POSTGRES_HOST", "localhost")
	port := getEnvOr("POSTGRES_PORT", "5433")
	user := getEnvOr("POSTGRES_USER", "sikons")
	pass := getEnvOr("POSTGRES_PASSWORD", "sikons_dev")
	dbname := getEnvOr("POSTGRES_DB", "sikons")
	return "postgres://" + user + ":" + pass + "@" + host + ":" + port + "/" + dbname + "?sslmode=disable"
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

const passwordDemo = "password123" // akun demo, sengaja publik

func main() {
	if err := run(); err != nil {
		slog.Error("seed gagal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	pool, err := db.Connect(ctx, databaseURL())
	if err != nil {
		return err
	}
	defer pool.Close()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(log)
	slog.Info("terhubung ke database")

	// Hash password sekali, dipakai semua akun.
	// Pakai auth.HashPassword supaya konsisten dengan jalur register.
	hash, err := auth.HashPassword(passwordDemo)
	if err != nil {
		return err
	}

	// ON CONFLICT DO NOTHING: idempoten. Email adalah unique constraint.
	// Kalau email sudah ada, tidak ada yang berubah.

	// --- Admin
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ($1, $2, $3, 'admin')
		ON CONFLICT (lower(email)) DO NOTHING`,
		"admin@sikons.test", hash, "Administrator SIKONS",
	); err != nil {
		return err
	}

	// --- Dosen dengan lecturer_profiles
	lecturers := []struct {
		email      string
		fullName   string
		nidn       string
		department string
		room       string
	}{
		{"budi@sikons.test", "Dr. Budi Santoso", "198501012010121001", "Informatika", "Gedung C, Ruang 301"},
		{"siti@sikons.test", "Siti Nurhaliza, M.T.", "199002152020122001", "Sistem Informasi", "Gedung A, Ruang 205"},
	}

	for _, l := range lecturers {
		// Ambil user_id yang baru diinsert (atau yang sudah ada).
		var userID string
		err := pool.QueryRow(ctx, `
			INSERT INTO users (email, password_hash, full_name, role, identity_number)
			VALUES ($1, $2, $3, 'lecturer', $4)
			ON CONFLICT (lower(email)) DO UPDATE SET email = EXCLUDED.email
			RETURNING id::text`,
			l.email, hash, l.fullName, l.nidn,
		).Scan(&userID)
		if err != nil {
			return err
		}

		// lecturer_profiles tidak punya unique constraint yang bagus untuk ON CONFLICT.
		// Pakai ON CONFLICT DO NOTHING terhadap foreign key (user_id).
		if _, err := pool.Exec(ctx, `
			INSERT INTO lecturer_profiles (user_id, department, room)
			VALUES ($1, $2, $3)
			ON CONFLICT (user_id) DO NOTHING`,
			userID, l.department, l.room,
		); err != nil {
			return err
		}
	}

	// --- Mahasiswa
	students := []struct {
		email    string
		fullName string
		nim      string
	}{
		{"ani@sikons.test", "Ani Rahmawati", "G65120001"},
		{"dedi@sikons.test", "Dedi Kurniawan", "G65120002"},
		{"rina@sikons.test", "Rina Wulandari", "G65120003"},
	}

	for _, s := range students {
		if _, err := pool.Exec(ctx, `
			INSERT INTO users (email, password_hash, full_name, role, identity_number)
			VALUES ($1, $2, $3, 'student', $4)
			ON CONFLICT (lower(email)) DO NOTHING`,
			s.email, hash, s.fullName, s.nim,
		); err != nil {
			return err
		}
	}

	// --- Aturan ketersediaan untuk dosen demo
	// Idempoten: cek dulu apakah aturan aktif sudah ada.
	// EXCLUDE constraint mencegah duplikat tapi tidak bisa pakai ON CONFLICT DO
	// NOTHING, jadi pengecekan manual diperlukan.
	for _, l := range lecturers {
		var userID string
		err := pool.QueryRow(ctx, `
			SELECT id::text FROM users WHERE lower(email) = lower($1)`,
			l.email,
		).Scan(&userID)
		if err != nil {
			slog.Warn("dosen tidak ditemukan di seed, lewati aturan ketersediaan",
				"email", l.email)
			continue
		}

		// Senin–Jumat, 09:00–12:00 dan 13:00–17:00, slot 30 menit.
		// Waktu Jakarta, senin=1 .. jumat=5.
		rules := [][4]string{
			{"1", "09:00", "12:00"},
			{"1", "13:00", "17:00"},
			{"2", "09:00", "12:00"},
			{"2", "13:00", "17:00"},
			{"3", "09:00", "12:00"},
			{"3", "13:00", "17:00"},
			{"4", "09:00", "12:00"},
			{"4", "13:00", "17:00"},
			{"5", "09:00", "12:00"},
			{"5", "13:00", "17:00"},
		}
		for _, rule := range rules {
			dayOfWeek := rule[0]
			startTime := rule[1]
			endTime := rule[2]

			var exists bool
			err := pool.QueryRow(ctx, `
				SELECT true FROM availability_rules
				WHERE lecturer_id = $1::uuid
				  AND day_of_week = $2
				  AND start_time = $3::time
				  AND end_time = $4::time
				  AND is_active = true`,
				userID, dayOfWeek, startTime, endTime,
			).Scan(&exists)
			if err == nil && exists {
				// Sudah ada, lewati.
				continue
			}

			_, err = pool.Exec(ctx, `
				INSERT INTO availability_rules
					(lecturer_id, day_of_week, start_time, end_time,
					 slot_duration_min, effective_from)
				VALUES ($1::uuid, $2, $3::time, $4::time, 30, CURRENT_DATE)
				ON CONFLICT DO NOTHING`,
				userID, dayOfWeek, startTime, endTime,
			)
			if err != nil {
				return fmt.Errorf("gagal insert aturan ketersediaan %s %s–%s: %w",
					dayOfWeek, startTime, endTime, err)
			}
		}
		slog.Info("aturan ketersediaan dibuat", "email", l.email)
	}

	slog.Info("seed selesai")
	return nil
}
