package slotgen

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// JendelaDefault menghitung rentang tanggal default untuk rekonsiliasi.
// from = tengah malam hari ini di zona tz, to = from + horizonDays.
func JendelaDefault(now time.Time, tz *time.Location, horizonDays int) (from, to time.Time) {
	// Konversi ke zona kampus dulu, baru ambil komponen tanggal.
	// Tanpa ini, midnight UTC bisa berbeda tanggal di zona kampus.
	nowLocal := now.In(tz)
	from = time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, tz)
	to = from.AddDate(0, 0, horizonDays)
	return
}

// Summary berisi hasil rekonsiliasi, dikembalikan ke pemanggil agar dosen tahu
// berapa banyak yang terpengaruh.
type Summary struct {
	Created           int `json:"created"`
	Deleted           int `json:"deleted"`
	Withdrawn         int `json:"withdrawn"`
	Restored          int `json:"restored"`
	BookingsCancelled int `json:"bookings_cancelled"`
}

// Service menangani generation slot.
type Service struct {
	pool        *pgxpool.Pool
	campusTZ    *time.Location
	horizonDays int
}

// Horizon mengembalikan jumlah hari horizon.
func (s *Service) Horizon() int { return s.horizonDays }

// New membuat slotgen service.
// campusTZ harus dari config.CampusTZ yang sudah di-LoadLocation.
func New(pool *pgxpool.Pool, campusTZ *time.Location, horizonDays int) *Service {
	return &Service{
		pool:        pool,
		campusTZ:    campusTZ,
		horizonDays: horizonDays,
	}
}

// Reconcile menjalankan rekonsiliasi untuk satu dosen.
// from dan to adalah tanggal di zona kampus (Asia/Jakarta).
// Semua operasi dalam satu transaksi.
func (s *Service) Reconcile(ctx context.Context, lecturerID string, from, to time.Time, log *slog.Logger) (Summary, error) {
	if to.Before(from) {
		return Summary{}, fmt.Errorf("to sebelum from")
	}
	if lecturerID == "" {
		return Summary{}, fmt.Errorf("lecturerID kosong")
	}

	// Satu sumber waktu untuk seluruh rekonsiliasi: konsisten dan bisa diuji.
	// Konversi ke zona kampus supaya batas "masa lalu" akurat.
	now := time.Now().In(s.campusTZ)

	// Konversi ke UTC untuk query SQL. Postgres menerima timestamptz dan
	// menginterpretasikan time.Time sesuai zona waktu-nya (UTC default).
	// Kalau kita pass Asia/Jakarta tanpa konversi, Postgres salah baca.
	fromUTC := from.UTC()
	toUTC := to.UTC()
	nowUTC := now.UTC()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("memulai transaksi: %w", err)
	}
	defer tx.Rollback(ctx)

	rules, err := s.readActiveRules(ctx, tx, lecturerID, fromUTC, toUTC)
	if err != nil {
		return Summary{}, fmt.Errorf("membaca aturan: %w", err)
	}

	exceptions, err := s.readExceptions(ctx, tx, lecturerID, fromUTC, toUTC)
	if err != nil {
		return Summary{}, fmt.Errorf("membaca pengecualian: %w", err)
	}

	// computeShouldExist pakai from/to dalam zona kampus (bukan UTC)
	// Pass nowUTC sebagai batas: slot yang sudah lewat tidak masuk hasil.
	shouldExist := s.computeShouldExist(from, to, rules, exceptions, nowUTC)

	created, deleted, withdrawn, restored, bookingsCancelled, err := s.syncSlots(ctx, tx, lecturerID, fromUTC, toUTC, nowUTC, shouldExist, log)
	if err != nil {
		return Summary{}, fmt.Errorf("sync slot: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("commit: %w", err)
	}

	return Summary{
		Created:           created,
		Deleted:           deleted,
		Withdrawn:         withdrawn,
		Restored:          restored,
		BookingsCancelled: bookingsCancelled,
	}, nil
}

// ruleRecord adalah representasi aturan dari database.
type ruleRecord struct {
	dayOfWeek       int
	startTime       time.Time // time.Time berisi jam dan menit saja, zona lokal
	endTime         time.Time
	slotDurationMin int
	effectiveFrom   time.Time
	effectiveTo     *time.Time
}

// exceptionRecord adalah representasi pengecualian dari database.
type exceptionRecord struct {
	date      time.Time
	isFullDay bool
	startTime *time.Time
	endTime   *time.Time
}

// slotSpec mendeskripsikan satu slot yang seharusnya ada.
type slotSpec struct {
	startAt time.Time // UTC
	endAt   time.Time // UTC
}

// readActiveRules membaca aturan aktif yang periode berlakunya beririsan dengan [from, to].
// from dan to adalah tanggal dalam zona kampus.
func (s *Service) readActiveRules(ctx context.Context, tx pgx.Tx, lecturerID string, from, to time.Time) ([]ruleRecord, error) {
	// Konversi dari UTC ke DATE string supaya Postgres menginterpretasikan sebagai
	// tanggal di zona kampus, bukan UTC.
	fromStr := from.In(s.campusTZ).Format("2006-01-02")
	toStr := to.In(s.campusTZ).Format("2006-01-02")

	rows, err := tx.Query(ctx, `
		SELECT day_of_week, start_time, end_time, slot_duration_min,
		       effective_from, effective_to
		FROM availability_rules
		WHERE lecturer_id = $1
		  AND is_active = true
		  AND effective_from <= $3::date
		  AND (effective_to IS NULL OR effective_to >= $2::date)
		ORDER BY day_of_week, start_time`,
		lecturerID, fromStr, toStr)
	if err != nil {
		return nil, fmt.Errorf("query aturan: %w", err)
	}
	defer rows.Close()

	var rules []ruleRecord
	for rows.Next() {
		var r ruleRecord
		err := rows.Scan(&r.dayOfWeek, &r.startTime, &r.endTime,
			&r.slotDurationMin, &r.effectiveFrom, &r.effectiveTo)
		if err != nil {
			return nil, fmt.Errorf("scan aturan: %w", err)
		}
		// Konversi effectiveFrom ke midnight di zona kampus.
		// Postgres menyimpan DATE sebagai midnight UTC, tapi secara semantik
		// dia berarti "tanggal tersebut" di zona kampus, bukan jam 7 pagi.
		utcDate := r.effectiveFrom.UTC()
		r.effectiveFrom = time.Date(utcDate.Year(), utcDate.Month(), utcDate.Day(),
			0, 0, 0, 0, s.campusTZ)
		if r.effectiveTo != nil {
			t := r.effectiveTo.UTC()
			te := time.Date(t.Year(), t.Month(), t.Day(),
				0, 0, 0, 0, s.campusTZ)
			r.effectiveTo = &te
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// readExceptions membaca pengecualian dalam rentang [from, to].
// from dan to adalah tanggal dalam zona kampus.
func (s *Service) readExceptions(ctx context.Context, tx pgx.Tx, lecturerID string, from, to time.Time) ([]exceptionRecord, error) {
	fromStr := from.In(s.campusTZ).Format("2006-01-02")
	toStr := to.In(s.campusTZ).Format("2006-01-02")

	rows, err := tx.Query(ctx, `
		SELECT exception_date, is_full_day, start_time, end_time
		FROM availability_exceptions
		WHERE lecturer_id = $1 AND exception_date BETWEEN $2::date AND $3::date`,
		lecturerID, fromStr, toStr)
	if err != nil {
		return nil, fmt.Errorf("query pengecualian: %w", err)
	}
	defer rows.Close()

	var exceptions []exceptionRecord
	for rows.Next() {
		var e exceptionRecord
		err := rows.Scan(&e.date, &e.isFullDay, &e.startTime, &e.endTime)
		if err != nil {
			return nil, fmt.Errorf("scan pengecualian: %w", err)
		}
		// Konversi date ke midnight di zona kampus
		utcDate := e.date.UTC()
		e.date = time.Date(utcDate.Year(), utcDate.Month(), utcDate.Day(),
			0, 0, 0, 0, s.campusTZ)
		if e.startTime != nil {
			t := e.startTime.UTC()
			e.startTime = &t
		}
		if e.endTime != nil {
			t := e.endTime.UTC()
			e.endTime = &t
		}
		exceptions = append(exceptions, e)
	}
	return exceptions, rows.Err()
}

// cekPengecualianFullDay mengecek apakah ada pengecualian full day di daftar.
func cekPengecualianFullDay(exList []exceptionRecord) bool {
	for _, ex := range exList {
		if ex.isFullDay {
			return true
		}
	}
	return false
}

// terblokirOlehPengecualian mengecek apakah slot [start, end) di tanggal d diblokir oleh pengecualian.
func terblokirOlehPengecualian(d time.Time, start, end time.Time, exList []exceptionRecord, campusTZ *time.Location) bool {
	// Pengecualian full day memblokir semua slot di tanggal tersebut.
	if cekPengecualianFullDay(exList) {
		return true
	}
	for _, ex := range exList {
		if ex.startTime != nil && ex.endTime != nil {
			// Bangun interval pengecualian di zona lokal
			excStart := time.Date(d.Year(), d.Month(), d.Day(),
				ex.startTime.Hour(), ex.startTime.Minute(), 0, 0, campusTZ)
			excEnd := time.Date(d.Year(), d.Month(), d.Day(),
				ex.endTime.Hour(), ex.endTime.Minute(), 0, 0, campusTZ)

			// Cek irisan: [start, end) ∩ [excStart, excEnd)
			// Beririsan jika start < excEnd && end > excStart
			if start.Before(excEnd) && end.After(excStart) {
				return true
			}
		}
	}
	return false
}

// computeShouldExist menghitung semua slot yang seharusnya ada.
// Algoritma:
//  1. Untuk setiap tanggal dalam rentang, cari aturan yang berlaku hari itu
//  2. Hitung slot: mulai dari start_time, selebar slot_duration_min
//  3. Buang slot yang beririsan dengan pengecualian
//  4. Buang slot yang sudah lewat (< nowUTC)
//  5. Konversi jam lokal ke UTC
func (s *Service) computeShouldExist(from, to time.Time, rules []ruleRecord, exceptions []exceptionRecord, nowUTC time.Time) []slotSpec {
	// Map pengecualian: date -> []exception (untuk lookup cepat)
	exByDate := make(map[string][]exceptionRecord)
	for _, e := range exceptions {
		key := e.date.Format("2006-01-02")
		exByDate[key] = append(exByDate[key], e)
	}

	var slots []slotSpec

	// Iterasi setiap tanggal
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		dateKey := d.Format("2006-01-02")
		dow := int(d.Weekday()) // 0=Minggu, 6=Sabtu
		exList := exByDate[dateKey]

		// Cari aturan yang berlaku di tanggal ini
		for _, rule := range rules {
			// Cek day of week
			if rule.dayOfWeek != dow {
				continue
			}
			// Cek periode berlaku
			if d.Before(rule.effectiveFrom) || (rule.effectiveTo != nil && d.After(*rule.effectiveTo)) {
				continue
			}

			// Cek pengecualian full day
			if cekPengecualianFullDay(exList) {
				continue
			}

			// Bangun jam mulai dan selesai di zona lokal
			slotStartLocal := time.Date(d.Year(), d.Month(), d.Day(),
				rule.startTime.Hour(), rule.startTime.Minute(), 0, 0, s.campusTZ)
			slotEndLimitLocal := time.Date(d.Year(), d.Month(), d.Day(),
				rule.endTime.Hour(), rule.endTime.Minute(), 0, 0, s.campusTZ)

			slotDuration := time.Duration(rule.slotDurationMin) * time.Minute

			// Generate slot selebar slotDurationMin, mulai dari startTime
			for {
				slotEndLocal := slotStartLocal.Add(slotDuration)

				// Buang slot yang tidak muat penuh sebelum end_time
				if slotEndLocal.After(slotEndLimitLocal) {
					break
				}

				// Cek pengecualian parsial
				if !terblokirOlehPengecualian(d, slotStartLocal, slotEndLocal, exList, s.campusTZ) {
					// Konversi ke UTC
					utcStart := slotStartLocal.UTC()
					utcEnd := slotEndLocal.UTC()

					// Buang slot yang sudah lewat (start_at <= nowUTC)
					if utcStart.After(nowUTC) {
						slots = append(slots, slotSpec{startAt: utcStart, endAt: utcEnd})
					}
				}

				slotStartLocal = slotEndLocal
			}
		}
	}

	return slots
}

// kunciSlot menghasilkan int64 Unix timestamp (UTC, detik) dari waktu yang diberikan.
// Dipakai sebagai map key untuk menghindari masalah timezone saat bandingkan
// string RFC3339 yang hasilnya berbeda tergantung Location.
func kunciSlot(t time.Time) int64 {
	return t.UTC().Truncate(time.Second).Unix()
}

// syncSlots menyinkronkan slot: insert yang belum ada, delete/update yang tidak seharusnya ada.
// nowUTC adalah batas waktu "masa lalu" — slot dengan start_at <= nowUTC tidak disentuh.
func (s *Service) syncSlots(ctx context.Context, tx pgx.Tx, lecturerID string, from, to, nowUTC time.Time, shouldExist []slotSpec, log *slog.Logger) (created, deleted, withdrawn, restored, bookingsCancelled int, err error) {
	// Baca semua slot dosen dalam rentang, tapi HANYA yang start_at > nowUTC.
	// Slot masa lalu tidak disentuh sama sekali.
	rows, err := tx.Query(ctx, `
		SELECT id::text, start_at, end_at, status, withdrawn_at
		FROM slots
		WHERE lecturer_id = $1
		  AND start_at >= $2
		  AND start_at < $3
		  AND start_at > $4`,
		lecturerID, from.UTC(), to.AddDate(0, 0, 1).UTC(), nowUTC)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("baca slot: %w", err)
	}

	type existingSlot struct {
		id          string
		startAt     time.Time
		endAt       time.Time
		status      string
		withdrawnAt *time.Time
	}

	var existing []existingSlot
	for rows.Next() {
		var slot existingSlot
		err := rows.Scan(&slot.id, &slot.startAt, &slot.endAt, &slot.status, &slot.withdrawnAt)
		if err != nil {
			rows.Close()
			return 0, 0, 0, 0, 0, fmt.Errorf("scan slot: %w", err)
		}
		existing = append(existing, slot)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, 0, 0, 0, err
	}

	existingMap := make(map[int64]*existingSlot)
	for i := range existing {
		key := kunciSlot(existing[i].startAt)
		existingMap[key] = &existing[i]
	}

	// Untuk setiap slot yang seharusnya ada, tiga kemungkinan:
	//   - belum ada                              -> INSERT
	//   - sudah ada, withdrawn, status bukan 'booked' -> restore (NULL withdrawn_at)
	//   - selain itu                              -> biarkan apa adanya
	//
	// Slot yang ada di shouldExist adalah SAH, apa pun status dan isi
	// withdraw-nya. Jangan menitipkan slot sah ke loop pembersihan di bawah
	// — loop itu hanya untuk slot yang benar-benar tidak ada di shouldExist,
	// dan kalau slot sah jatuh ke sana, booking confirmed milik mahasiswa
	// akan dibatalkan tanpa alasan. SELALU hapus dari existingMap di akhir.
	for _, slot := range shouldExist {
		key := kunciSlot(slot.startAt)
		existingSlot, exists := existingMap[key]
		if !exists {
			ct, err := tx.Exec(ctx, `
				INSERT INTO slots (lecturer_id, start_at, end_at, status)
				VALUES ($1::uuid, $2, $3, 'open')
				ON CONFLICT (lecturer_id, start_at) DO NOTHING`,
				lecturerID, slot.startAt, slot.endAt)
			if err != nil {
				return 0, 0, 0, 0, 0, fmt.Errorf("insert slot: %w", err)
			}
			if ct.RowsAffected() > 0 {
				created++
			}
		} else if existingSlot.withdrawnAt != nil && existingSlot.status != "booked" {
			if _, err := tx.Exec(ctx, `
				UPDATE slots SET withdrawn_at = NULL
				WHERE id = $1::uuid`,
				existingSlot.id); err != nil {
				return 0, 0, 0, 0, 0, fmt.Errorf("restore slot: %w", err)
			}
			restored++
		}
		// Slot sah: lepas dari peta agar loop pembersihan tidak menyentuhnya.
		delete(existingMap, key)
	}

	// Handle slot yang tidak seharusnya ada lagi.
	//
	// Setelah loop di atas, existingMap hanya berisi slot yang TIDAK ada di
	// shouldExist. Untuk setiap slot, tiga kasus:
	//   - tidak ada bookings         -> DELETE; slot murni generator, tidak
	//                                   ada histori yang harus dijaga
	//   - bookings tapi tidak confirmed -> SET withdrawn_at; slot punya
	//                                   histori dan FK RESTRICT dari bookings
	//                                   menahan DELETE, jadi ditandai saja
	//   - ada bookings confirmed     -> selain withdraw, batalkan juga
	//                                   booking-nya. Mahasiswa yang jadwalnya
	//                                   jatuh di luar jadwal baru harus diberi
	//                                   tahu bahwa konsultasi-nya sudah tidak
	//                                   valid; kalau tidak, dia datang ke jam
	//                                   yang dosennya sudah menyatakan tidak
	//                                   tersedia.
	//
	// AMAN: query SELECT di awal sudah membatasi `start_at > $4` (nowUTC),
	// jadi slot masa lalu tidak pernah sampai ke blok ini. Tidak perlu
	// perlindungan kedua — satu perbaikan untuk satu masalah.
	for _, slot := range existingMap {
		var bookings []struct {
			id        string
			studentID string
			status    string
		}
		rows, err := tx.Query(ctx, `
			SELECT id::text, student_id::text, status::text
			FROM bookings
			WHERE slot_id = $1::uuid
			ORDER BY created_at`,
			slot.id)
		if err != nil {
			return 0, 0, 0, 0, 0, fmt.Errorf("baca booking untuk slot: %w", err)
		}
		for rows.Next() {
			var b struct {
				id        string
				studentID string
				status    string
			}
			if err := rows.Scan(&b.id, &b.studentID, &b.status); err != nil {
				rows.Close()
				return 0, 0, 0, 0, 0, fmt.Errorf("scan booking: %w", err)
			}
			bookings = append(bookings, b)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, 0, 0, 0, 0, err
		}

		// Kasus 1: tidak ada riwayat booking sama sekali. Slot murni generator,
		// tidak ada yang menahan — hapus saja.
		if len(bookings) == 0 {
			if _, err := tx.Exec(ctx,
				`DELETE FROM slots WHERE id = $1::uuid`, slot.id); err != nil {
				return 0, 0, 0, 0, 0, fmt.Errorf("hapus slot: %w", err)
			}
			deleted++
			continue
		}

		// Kasus 2 & 3: ada riwayat booking. Slot tidak boleh dihapus (FK
		// RESTRICT dari bookings), jadi tandai withdrawn. Kalau ada yang
		// confirmed, batalkan juga dan kirim notifikasi ke mahasiswanya.
		if _, err := tx.Exec(ctx, `
			UPDATE slots SET withdrawn_at = now() WHERE id = $1::uuid`,
			slot.id); err != nil {
			return 0, 0, 0, 0, 0, fmt.Errorf("withdraw slot: %w", err)
		}
		withdrawn++

		for _, b := range bookings {
			if b.status != "confirmed" {
				// booking sudah selesai/cancelled/batal — tidak perlu dinotifikasi
				continue
			}
			// Batalkan booking: status, cancelled_at, cancelled_by, cancel_reason.
			// cancelled_by = lecturer_id karena ini pembatalan yang dipicu
			// perubahan aturan ketersediaan dosen yang bersangkutan.
			if _, err := tx.Exec(ctx, `
				UPDATE bookings
				SET status = 'cancelled',
				    cancelled_at = now(),
				    cancelled_by = $1::uuid,
				    cancel_reason = 'Dosen mengubah jadwal ketersediaan'
				WHERE id = $2::uuid`,
				lecturerID, b.id); err != nil {
				return 0, 0, 0, 0, 0, fmt.Errorf("batalkan booking: %w", err)
			}
			// Kalau tidak ada lagi booking aktif untuk slot ini, kembalikan
			// status slot ke 'open'. Slot sudah di-withdraw, tapi konsistensi
			// status 'open'/'booked' tetap dijaga supaya tidak ada 'booked'
			// yang menggantung tanpa booking aktif. Partial index
			// one_active_booking_per_slot sudah menjaga integritas referensi.
			if _, err := tx.Exec(ctx, `
				UPDATE slots SET status = 'open'
				WHERE id = $1::uuid
				  AND NOT EXISTS (
				      SELECT 1 FROM bookings
				      WHERE slot_id = $1::uuid AND status <> 'cancelled'
				  )`,
				slot.id); err != nil {
				return 0, 0, 0, 0, 0, fmt.Errorf("kembalikan status slot: %w", err)
			}

			// INSERT outbox: niat kirim notifikasi ke mahasiswa. Worker
			// reminder (M4) yang akan mengeksekusi. partial unique index
			// notifications_dedupe_key(booking_id, type) WHERE booking_id
			// IS NOT NULL mencegah dobel notifikasi kalau reconcile
			// dijalankan dua kali — predicate pada ON CONFLICT harus
			// mengikuti predicate index persis.
			if _, err := tx.Exec(ctx, `
				INSERT INTO notifications (user_id, booking_id, type, payload, scheduled_at)
				VALUES ($1::uuid, $2::uuid, 'booking_cancelled_by_schedule_change',
				        jsonb_build_object(
				            'slot_id', $3::text,
				            'lecturer_id', $4::text,
				            'reason', 'Dosen mengubah jadwal ketersediaan'
				        ),
				        now())
				ON CONFLICT (booking_id, type) WHERE booking_id IS NOT NULL DO NOTHING`,
				b.studentID, b.id, slot.id, lecturerID); err != nil {
				return 0, 0, 0, 0, 0, fmt.Errorf("insert notifikasi: %w", err)
			}

			// Jejak audit untuk keluhan "booking saya hilang": saat dosen
			// mempersempit jadwal, booking mahasiswa yang jatuh di luar
			// jadwal baru dibatalkan. Tanpa log di sini, mahasiswa tidak
			// punya cara menelusuri kenapa booking-nya tiba-tiba cancelled.
			log.Info("booking dibatalkan karena jadwal dosen berubah",
				"lecturer_id", lecturerID,
				"booking_id", b.id,
				"student_id", b.studentID,
				"slot_start", slot.startAt)
			bookingsCancelled++
		}
	}

	return created, deleted, withdrawn, restored, bookingsCancelled, nil
}
