// Package reminder menangani pengiriman reminder email via polling.
package reminder

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oktasatwika87/sikons/internal/notifier"
)

// notifRow menyimpan satu baris notification yang sudah diambil dari DB
// dan siap diproses. Dipakai oleh processBatch dan processNotification.
type notifRow struct {
	id        string
	bookingID string
	attempts  int
}

// Service worker untuk mengirim reminder email.
type Service struct {
	pool         *pgxpool.Pool
	sender       notifier.Sender
	tz           *time.Location
	pollInterval time.Duration
	maxAttempts  int
	log          *slog.Logger
}

// New membuat reminder service.
func New(pool *pgxpool.Pool, sender notifier.Sender, tz *time.Location, pollInterval time.Duration, maxAttempts int, log *slog.Logger) *Service {
	return &Service{
		pool:         pool,
		sender:       sender,
		tz:           tz,
		pollInterval: pollInterval,
		maxAttempts:  maxAttempts,
		log:          log,
	}
}

// Run memulai polling loop. Berjalan sampai ctx dibatalkan.
// Setiap tick memanggil processBatch.
func (s *Service) Run(ctx context.Context) error {
	s.log.Info("reminder worker started", "poll_interval", s.pollInterval)
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Info("reminder worker stopping")
			return ctx.Err()
		case <-ticker.C:
			_, err := s.processBatch(ctx)
			if err != nil {
				s.log.Error("process batch error", "err", err)
			}
		}
	}
}

// processBatch satu putaran polling. Dipisah dari Run supaya bisa dites langsung.
//
// BACAAN untuk integration test: query ini memilih SEMUA notification pending
// yang due (LIMIT 20), bukan satu per test. Test yang ingin deterministik
// harus pakai processNotification(ctx, id) — lihat catatan di integration test.
func (s *Service) processBatch(ctx context.Context) (processed int, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Ambil baris notification yang sudah waktunya.
	// FOR UPDATE SKIP LOCKED: kalau ada worker lain yang sedang memproses baris
	// yang sama, skip baris itu (bukan tunggu). Ini mencegah dua worker
	// memproses notifikasi yang sama secara bersamaan.
	rows, err := tx.Query(ctx, `
		SELECT n.id::text, n.booking_id::text, n.attempts
		FROM notifications n
		WHERE n.type = 'booking_reminder'
		  AND n.status = 'pending'
		  AND n.scheduled_at <= now()
		ORDER BY n.scheduled_at
		LIMIT 20
		FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return 0, fmt.Errorf("query notifications: %w", err)
	}

	var rowsToProcess []notifRow
	for rows.Next() {
		var r notifRow
		if err := rows.Scan(&r.id, &r.bookingID, &r.attempts); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan row: %w", err)
		}
		rowsToProcess = append(rowsToProcess, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rows error: %w", err)
	}

	for _, r := range rowsToProcess {
		if err := s.processNotificationInTx(ctx, tx, r); err != nil {
			// Error tak terduga dari UPDATE. Tetap commit agar progres baris
			// sebelumnya tidak hilang, lalu mulai transaksi baru.
			s.log.Error("process notification error",
				"notification_id", r.id, "err", err)
		}

		// Commit per baris. Ini trade-off: window antara Send() dan UPDATE
		// status adalah window di mana email bisa terkirim dobel kalau proses
		// crash tepat di tengah. Untuk fitur reminder, ini adalah trade-off
		// yang diterima (bukan exactly-once delivery) dan lebih baik daripada
		// menunggu semua 20 baris selesai baru commit.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return processed, fmt.Errorf("commit: %w", commitErr)
		}
		processed++

		// Mulai transaksi baru untuk baris berikutnya.
		tx, err = s.pool.Begin(ctx)
		if err != nil {
			return processed, fmt.Errorf("begin next transaction: %w", err)
		}
		defer tx.Rollback(ctx)
	}

	return processed, nil
}

// processNotification memproses SATU notification berdasarkan id.
//
// Dipakai oleh:
//   - integration test, supaya setiap test memproses hanya baris miliknya
//     sendiri dan tidak ikut memproses baris pending milik test lain di
//     database uji bersama.
//   - production (cadangan) untuk retry manual atau trigger spesifik.
//
// Return 1 kalau notification ditemukan dan diproses (status apapun:
// sent / failed / skipped). Return 0 kalau tidak ada baris cocok — bisa
// karena id tidak ada, status bukan 'pending', belum due, atau baris
// sedang dipegang lock worker lain (FOR UPDATE SKIP LOCKED).
//
// Semantik SKIP LOCKED tetap dijaga: kalau worker lain memegang lock
// pada baris yang dimaksud, query ini melihat nol baris dan return 0.
func (s *Service) processNotification(ctx context.Context, notificationID string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT n.id::text, n.booking_id::text, n.attempts
		FROM notifications n
		WHERE n.id = $1::uuid
		  AND n.type = 'booking_reminder'
		  AND n.status = 'pending'
		  AND n.scheduled_at <= now()
		FOR UPDATE SKIP LOCKED`, notificationID)
	if err != nil {
		return 0, fmt.Errorf("query notification: %w", err)
	}

	var row notifRow
	found := false
	for rows.Next() {
		if err := rows.Scan(&row.id, &row.bookingID, &row.attempts); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan row: %w", err)
		}
		found = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rows error: %w", err)
	}
	if !found {
		return 0, nil
	}

	if err := s.processNotificationInTx(ctx, tx, row); err != nil {
		return 1, fmt.Errorf("process notification: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 1, fmt.Errorf("commit: %w", err)
	}
	return 1, nil
}

// processNotificationInTx berisi logika inti: ambil data booking via JOIN,
// validasi status, kirim email, UPDATE status notification. Dipakai oleh
// processBatch dan processNotification.
//
// Tidak mengelola commit/rollback — caller yang bertanggung jawab.
// Mengembalikan error hanya untuk kesalahan tak terduga (kegagalan UPDATE
// atau kesalahan tak terduga lainnya). Kasus bisnis biasa (booking tidak
// ditemukan → status jadi 'failed'; status booking bukan 'confirmed' →
// status jadi 'skipped'; email gagal → attempts++ atau 'failed') ditangani
// secara internal dan tidak mengembalikan error.
func (s *Service) processNotificationInTx(ctx context.Context, tx pgx.Tx, r notifRow) error {
	// Ambil data booking terkini dengan JOIN.
	// Menggunakan status TERKINI dari database untuk keputusan skipped/sent.
	var studentEmail, studentName, lecturerName, topic string
	var slotStart time.Time
	var bookingStatus string
	err := tx.QueryRow(ctx, `
		SELECT u.email, u.full_name,
		       l.full_name,
		       b.topic,
		       sl.start_at,
		       b.status::text
		FROM bookings b
		JOIN slots sl ON sl.id = b.slot_id
		JOIN users u ON u.id = b.student_id
		JOIN users l ON l.id = sl.lecturer_id
		WHERE b.id = $1::uuid`, r.bookingID,
	).Scan(&studentEmail, &studentName, &lecturerName, &topic, &slotStart, &bookingStatus)
	if err != nil {
		// Booking tidak ditemukan atau error — abaikan, tetap lanjut.
		s.log.Warn("booking not found for notification",
			"notification_id", r.id, "booking_id", r.bookingID, "err", err)
		// Tandai sebagai failed karena booking tidak valid.
		_, execErr := tx.Exec(ctx, `
			UPDATE notifications
			SET status = 'failed', last_error = $2
			WHERE id = $1::uuid`,
			r.id, fmt.Sprintf("booking lookup failed: %v", err))
		if execErr != nil {
			return fmt.Errorf("update notification (booking failed): %w", execErr)
		}
		return nil
	}

	// Kalau booking sudah tidak confirmed lagi, skip.
	if bookingStatus != "confirmed" {
		_, execErr := tx.Exec(ctx, `
			UPDATE notifications
			SET status = 'skipped', last_error = 'booking sudah tidak confirmed'
			WHERE id = $1::uuid`, r.id)
		if execErr != nil {
			return fmt.Errorf("update notification (skipped): %w", execErr)
		}
		return nil
	}

	// Kirim email.
	subject := fmt.Sprintf("Reminder: Konsultasi dengan %s", lecturerName)
	body := formatEmailBody(studentName, lecturerName, slotStart, topic, s.tz)

	sendErr := s.sender.Send(ctx, studentEmail, subject, body)

	if sendErr != nil {
		// Gagal kirim. Increment attempts dan periksa apakah sudah max.
		newAttempts := r.attempts + 1
		if newAttempts >= s.maxAttempts {
			// Sudah mencapai batas percobaan. Tandai failed.
			_, execErr := tx.Exec(ctx, `
				UPDATE notifications
				SET status = 'failed', attempts = $2,
				    last_error = $3
				WHERE id = $1::uuid`,
				r.id, newAttempts, truncateError(sendErr))
			if execErr != nil {
				return fmt.Errorf("update notification (failed): %w", execErr)
			}
			s.log.Warn("reminder failed permanently",
				"notification_id", r.id, "attempts", newAttempts, "err", sendErr)
		} else {
			// Masih bisa dicoba lagi di tick berikutnya.
			_, execErr := tx.Exec(ctx, `
				UPDATE notifications
				SET attempts = $2, last_error = $3
				WHERE id = $1::uuid`,
				r.id, newAttempts, truncateError(sendErr))
			if execErr != nil {
				return fmt.Errorf("update notification (attempts): %w", execErr)
			}
			s.log.Warn("reminder send failed, will retry",
				"notification_id", r.id, "attempts", newAttempts, "err", sendErr)
		}
	} else {
		// Sukses. Tandai sent.
		_, execErr := tx.Exec(ctx, `
			UPDATE notifications
			SET status = 'sent', sent_at = now()
			WHERE id = $1::uuid`, r.id)
		if execErr != nil {
			return fmt.Errorf("update notification (sent): %w", execErr)
		}
		s.log.Info("reminder sent",
			"notification_id", r.id, "to", studentEmail)
	}
	return nil
}

// truncateError memotong error message supaya muat di kolom last_error (text).
func truncateError(err error) string {
	msg := err.Error()
	if len(msg) > 500 {
		return msg[:500] + "..."
	}
	return msg
}

// formatEmailBody membuat body email reminder.
func formatEmailBody(studentName, lecturerName string, slotStart time.Time, topic string, tz *time.Location) string {
	// Format waktu dalam zona kampus.
	localTime := slotStart.In(tz)

	// Nama hari dan bulan dalam Bahasa Indonesia.
	dayNames := []string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}
	monthNames := []string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
		"Juli", "Agustus", "September", "Oktober", "November", "Desember"}

	day := dayNames[localTime.Weekday()]
	dayOfMonth := localTime.Day()
	month := monthNames[localTime.Month()]
	year := localTime.Year()
	hour := localTime.Hour()
	minute := localTime.Minute()

	formatted := fmt.Sprintf("%s, %d %s %d pukul %02d.%02d WIB",
		day, dayOfMonth, month, year, hour, minute)

	return fmt.Sprintf(`<html>
<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333;">
  <h2>Reminder Konsultasi</h2>
  <p>Halo <strong>%s</strong>,</p>
  <p>Ini adalah pengingat bahwa kamu memiliki jadwal konsultasi yang akan segera dimulai:</p>
  <table style="background: #f5f5f5; padding: 15px; border-radius: 8px; margin: 20px 0;">
    <tr><td style="padding: 5px;"><strong>Dosen:</strong></td><td style="padding: 5px;">%s</td></tr>
    <tr><td style="padding: 5px;"><strong>Waktu:</strong></td><td style="padding: 5px;">%s</td></tr>
    <tr><td style="padding: 5px;"><strong>Topik:</strong></td><td style="padding: 5px;">%s</td></tr>
  </table>
  <p>Harap pastikan kamu sudah siap. Jika ada kendala, hubungi dosen terkait.</p>
  <p> Salam,<br/>SIKONS</p>
</body>
</html>`, studentName, lecturerName, formatted, topic)
}

// notificationStatus adalah type-safe constant untuk status.
type notificationStatus string

const (
	statusPending notificationStatus = "pending"
	statusSent    notificationStatus = "sent"
	statusFailed  notificationStatus = "failed"
	statusSkipped notificationStatus = "skipped"
)
