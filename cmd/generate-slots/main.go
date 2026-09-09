// Command generate-slots menjalankan rekonsiliasi slot untuk semua dosen aktif.
// Idempoten: dijalankan dua kali aman, dijalankan setelah perubahan aturan akan
// menyesuaikan slot sesuai aturan baru.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/oktasatwika/sikons/internal/config"
	"github.com/oktasatwika/sikons/internal/db"
	"github.com/oktasatwika/sikons/internal/slotgen"
)

func main() {
	if err := run(); err != nil {
		slog.Error("generate-slots gagal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(log)

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Info("terhubung ke database")

	svc := slotgen.New(pool, cfg.CampusTZ, cfg.SlotHorizonDays)

	// Hitung rentang tanggal
	from, to := slotgen.JendelaDefault(time.Now(), cfg.CampusTZ, cfg.SlotHorizonDays)

	log.Info("merekonciliasi slot",
		"from", from.Format("2006-01-02"),
		"to", to.Format("2006-01-02"),
		"horizon_days", cfg.SlotHorizonDays,
	)

	// Ambil semua dosen aktif
	rows, err := pool.Query(ctx, `
		SELECT u.id::text
		FROM users u
		JOIN lecturer_profiles lp ON lp.user_id = u.id
		WHERE u.role = 'lecturer' AND u.is_active = true`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var lecturerIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		lecturerIDs = append(lecturerIDs, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	log.Info("menemukan dosen aktif", "count", len(lecturerIDs))

	var totalCreated, totalDeleted, totalWithdrawn, totalRestored, totalCancelled int

	for _, lecturerID := range lecturerIDs {
		summary, err := svc.Reconcile(ctx, lecturerID, from, to, log)
		if err != nil {
			log.Error("rekonsiliasi gagal", "lecturer_id", lecturerID, "err", err)
			continue // lanjut ke dosen berikutnya
		}

		if summary.Created > 0 || summary.Deleted > 0 || summary.Withdrawn > 0 ||
			summary.Restored > 0 || summary.BookingsCancelled > 0 {
			log.Info("rekonsiliasi selesai",
				"lecturer_id", lecturerID,
				"created", summary.Created,
				"deleted", summary.Deleted,
				"withdrawn", summary.Withdrawn,
				"restored", summary.Restored,
				"bookings_cancelled", summary.BookingsCancelled,
			)
		}

		totalCreated += summary.Created
		totalDeleted += summary.Deleted
		totalWithdrawn += summary.Withdrawn
		totalRestored += summary.Restored
		totalCancelled += summary.BookingsCancelled
	}

	log.Info("semua dosen selesai",
		"total_created", totalCreated,
		"total_deleted", totalDeleted,
		"total_withdrawn", totalWithdrawn,
		"total_restored", totalRestored,
		"total_bookings_cancelled", totalCancelled,
	)

	return nil
}
