// Package db menangani koneksi ke PostgreSQL.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect membuka connection pool dan memastikan databasenya benar-benar
// hidup sebelum aplikasi dinyatakan siap.
//
// pgxpool, bukan satu koneksi tunggal: setiap request HTTP di Go dilayani
// goroutine-nya sendiri, dan satu koneksi Postgres tidak aman dipakai dua
// goroutine bersamaan. Pool yang mengatur antreannya.
//
// Ukuran pool sengaja tidak dinaikkan tinggi-tinggi. Nanti di concurrency
// test, 100 request paralel akan MENGANTRE di pool ini — dan itu memang yang
// kita inginkan: yang menang tetap satu, yang lain menunggu lalu dapat 409.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// %w membungkus error asli, bukan menyalin teksnya. Pemanggil di atas
		// masih bisa memeriksa penyebab aslinya dengan errors.Is / errors.As.
		return nil, fmt.Errorf("dsn tidak valid: %w", err)
	}

	poolCfg.MaxConns = 10
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("gagal membuat pool: %w", err)
	}

	// Beri batas waktu sendiri untuk ping. Tanpa ini, kalau host database
	// salah ketik, aplikasi bisa menggantung tanpa pesan apa pun.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database tidak merespons: %w", err)
	}

	return pool, nil
}
