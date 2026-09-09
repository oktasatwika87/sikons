package slotgen

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Test integrasi untuk slotgen.
// Memerlukan database Postgres sungguhan.

var poolUji *pgxpool.Pool

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://sikons:sikons_dev@localhost:5433/sikons?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var err error
	poolUji, err = pgxpool.New(ctx, dsn)
	if err != nil {
		os.Exit(0)
	}
	if err := poolUji.Ping(ctx); err != nil {
		poolUji.Close()
		os.Exit(0)
	}

	kode := m.Run()
	poolUji.Close()
	os.Exit(kode)
}

func getSvc() *Service {
	campusTZ, _ := time.LoadLocation("Asia/Jakarta")
	return New(poolUji, campusTZ, 30)
}

// nextMondayAfterDate mengembalikan Senin setelah tanggal yang diberikan.
func nextMondayAfter(d time.Time) time.Time {
	campusTZ, _ := time.LoadLocation("Asia/Jakarta")
	dayStart := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, campusTZ)
	daysUntilMonday := (8 - int(dayStart.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	return dayStart.AddDate(0, 0, daysUntilMonday)
}

func buatDosen(t *testing.T) string {
	t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('slotgen-' || gen_random_uuid() || '@uji.local', 'x', 'Dosen Uji', 'lecturer')
		RETURNING id::text`).Scan(&id)
	if err != nil {
		t.Fatalf("membuat dosen: %v", err)
	}

	_, err = poolUji.Exec(context.Background(),
		`INSERT INTO lecturer_profiles (user_id, department) VALUES ($1::uuid, 'TI')`, id)
	if err != nil {
		t.Fatalf("membuat profil dosen: %v", err)
	}
	return id
}

func buatMahasiswa(t *testing.T) string {
	t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('mhs-slotgen-' || gen_random_uuid() || '@uji.local', 'x', 'Mahasiswa Uji', 'student')
		RETURNING id::text`).Scan(&id)
	if err != nil {
		t.Fatalf("membuat mahasiswa: %v", err)
	}
	return id
}

func HapusAturanDosen(t *testing.T, lecturerID string) {
	t.Helper()
	_, _ = poolUji.Exec(context.Background(),
		`DELETE FROM availability_exceptions WHERE lecturer_id = $1::uuid`, lecturerID)
	_, _ = poolUji.Exec(context.Background(),
		`DELETE FROM availability_rules WHERE lecturer_id = $1::uuid`, lecturerID)
	_, _ = poolUji.Exec(context.Background(),
		`DELETE FROM bookings WHERE slot_id IN (SELECT id FROM slots WHERE lecturer_id = $1::uuid)`, lecturerID)
	_, _ = poolUji.Exec(context.Background(),
		`DELETE FROM notifications WHERE booking_id IN (SELECT id FROM bookings WHERE slot_id IN (SELECT id FROM slots WHERE lecturer_id = $1::uuid))`, lecturerID)
	_, _ = poolUji.Exec(context.Background(),
		`DELETE FROM slots WHERE lecturer_id = $1::uuid`, lecturerID)
}

func TestReconcile_AturanSeninSlot30Menit(t *testing.T) {
	dosen := buatDosen(t)
	defer HapusAturanDosen(t, dosen)

	campusTZ, _ := time.LoadLocation("Asia/Jakarta")
	monday := time.Date(2026, 9, 14, 0, 0, 0, 0, campusTZ)
	from := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, campusTZ)
	to := from.AddDate(0, 0, 1)

	_, err := poolUji.Exec(context.Background(), `
		INSERT INTO availability_rules
		(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, is_active)
		VALUES ($1::uuid, 1, '09:00'::time, '12:00'::time, 30, $2::date, true)`,
		dosen, from.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert aturan: %v", err)
	}

	svc := getSvc()
	summary, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if summary.Created != 6 {
		t.Errorf("created = %d, mau 6", summary.Created)
	}
	if summary.Deleted != 0 {
		t.Errorf("deleted = %d, mau 0", summary.Deleted)
	}
	if summary.Withdrawn != 0 {
		t.Errorf("withdrawn = %d, mau 0", summary.Withdrawn)
	}

	rows, err := poolUji.Query(context.Background(), `
		SELECT start_at AT TIME ZONE 'UTC' FROM slots
		WHERE lecturer_id = $1::uuid
		ORDER BY start_at`, dosen)
	if err != nil {
		t.Fatalf("query slot: %v", err)
	}
	defer rows.Close()

	var slotTimes []time.Time
	for rows.Next() {
		var startAt time.Time
		if err := rows.Scan(&startAt); err != nil {
			t.Fatalf("scan slot: %v", err)
		}
		slotTimes = append(slotTimes, startAt)
	}

	expectedFirstLocal := time.Date(monday.Year(), monday.Month(), monday.Day(), 9, 0, 0, 0, campusTZ)
	expectedFirst := expectedFirstLocal.UTC()
	if slotTimes[0] != expectedFirst {
		t.Errorf("slot pertama = %v, mau %v", slotTimes[0], expectedFirst)
	}

	expectedLastLocal := time.Date(monday.Year(), monday.Month(), monday.Day(), 11, 30, 0, 0, campusTZ)
	expectedLast := expectedLastLocal.UTC()
	if slotTimes[5] != expectedLast {
		t.Errorf("slot terakhir = %v, mau %v", slotTimes[5], expectedLast)
	}
}

func TestReconcile_Idempoten(t *testing.T) {
	dosen := buatDosen(t)
	defer HapusAturanDosen(t, dosen)

	campusTZ, _ := time.LoadLocation("Asia/Jakarta")
	monday := time.Date(2026, 9, 14, 0, 0, 0, 0, campusTZ)
	from := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, campusTZ)
	to := from.AddDate(0, 0, 1)

	_, err := poolUji.Exec(context.Background(), `
		INSERT INTO availability_rules
		(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, is_active)
		VALUES ($1::uuid, 1, '09:00'::time, '10:00'::time, 30, $2::date, true)`,
		dosen, from.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert aturan: %v", err)
	}

	svc := getSvc()

	sum1, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if sum1.Created != 2 {
		t.Errorf("created di reconcile 1 = %d, mau 2", sum1.Created)
	}

	sum2, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if sum2.Created != 0 {
		t.Errorf("created di reconcile 2 = %d, mau 0 (tidak boleh buat slot baru)", sum2.Created)
	}

	var count int
	err = poolUji.QueryRow(context.Background(),
		`SELECT count(*) FROM slots WHERE lecturer_id = $1::uuid`, dosen).Scan(&count)
	if err != nil {
		t.Fatalf("count slot: %v", err)
	}
	if count != 2 {
		t.Errorf("jumlah slot = %d, mau 2", count)
	}
}

func TestReconcile_PengecualianFullDay(t *testing.T) {
	dosen := buatDosen(t)
	defer HapusAturanDosen(t, dosen)

	campusTZ, _ := time.LoadLocation("Asia/Jakarta")
	monday := time.Date(2026, 9, 14, 0, 0, 0, 0, campusTZ)
	from := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, campusTZ)
	to := from.AddDate(0, 0, 1)

	_, err := poolUji.Exec(context.Background(), `
		INSERT INTO availability_rules
		(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, is_active)
		VALUES ($1::uuid, 1, '09:00'::time, '12:00'::time, 30, $2::date, true)`,
		dosen, from.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert aturan: %v", err)
	}

	_, err = poolUji.Exec(context.Background(), `
		INSERT INTO availability_exceptions
		(lecturer_id, exception_date, reason, is_full_day)
		VALUES ($1::uuid, $2::date, 'Cuti', true)`,
		dosen, from.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert pengecualian: %v", err)
	}

	svc := getSvc()
	summary, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if summary.Created != 0 {
		t.Errorf("created = %d, mau 0 (diblokir full day)", summary.Created)
	}
}

func TestReconcile_PengecualianPartial(t *testing.T) {
	dosen := buatDosen(t)
	defer HapusAturanDosen(t, dosen)

	campusTZ, _ := time.LoadLocation("Asia/Jakarta")
	monday := time.Date(2026, 9, 14, 0, 0, 0, 0, campusTZ)
	from := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, campusTZ)
	to := from.AddDate(0, 0, 1)

	_, err := poolUji.Exec(context.Background(), `
		INSERT INTO availability_rules
		(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, is_active)
		VALUES ($1::uuid, 1, '09:00'::time, '12:00'::time, 30, $2::date, true)`,
		dosen, from.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert aturan: %v", err)
	}

	_, err = poolUji.Exec(context.Background(), `
		INSERT INTO availability_exceptions
		(lecturer_id, exception_date, reason, is_full_day, start_time, end_time)
		VALUES ($1::uuid, $2::date, 'Rapat', false, '10:00'::time, '11:00'::time)`,
		dosen, from.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert pengecualian: %v", err)
	}

	svc := getSvc()
	summary, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if summary.Created != 4 {
		t.Errorf("created = %d, mau 4 (slot 10:00 dan 10:30 dibuang)", summary.Created)
	}
}

func TestReconcile_AturanDinonaktifkan(t *testing.T) {
	dosen := buatDosen(t)
	defer HapusAturanDosen(t, dosen)

	campusTZ, _ := time.LoadLocation("Asia/Jakarta")
	monday := time.Date(2026, 9, 14, 0, 0, 0, 0, campusTZ)
	from := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, campusTZ)
	to := from.AddDate(0, 0, 1)

	_, err := poolUji.Exec(context.Background(), `
		INSERT INTO availability_rules
		(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, is_active)
		VALUES ($1::uuid, 1, '09:00'::time, '10:00'::time, 30, $2::date, true)`,
		dosen, from.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert aturan: %v", err)
	}

	svc := getSvc()

	sum1, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if sum1.Created != 2 {
		t.Errorf("created di reconcile 1 = %d, mau 2", sum1.Created)
	}

	_, err = poolUji.Exec(context.Background(), `
		UPDATE availability_rules SET is_active = false WHERE lecturer_id = $1::uuid`,
		dosen)
	if err != nil {
		t.Fatalf("update aturan: %v", err)
	}

	sum2, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if sum2.Created != 0 {
		t.Errorf("created di reconcile 2 = %d, mau 0", sum2.Created)
	}
	if sum2.Deleted != 2 {
		t.Errorf("deleted = %d, mau 2 (slot open harus dihapus)", sum2.Deleted)
	}
}

func TestReconcile_SlotSudahDipesanLaluDiperkecil(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t)
	defer HapusAturanDosen(t, dosen)

	campusTZ, _ := time.LoadLocation("Asia/Jakarta")
	monday := time.Date(2026, 9, 14, 0, 0, 0, 0, campusTZ)
	from := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, campusTZ)
	to := from.AddDate(0, 0, 1)

	_, err := poolUji.Exec(context.Background(), `
		INSERT INTO availability_rules
		(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, is_active)
		VALUES ($1::uuid, 1, '09:00'::time, '10:00'::time, 30, $2::date, true)`,
		dosen, from.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert aturan: %v", err)
	}

	svc := getSvc()

	sum1, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if sum1.Created != 2 {
		t.Errorf("created di reconcile 1 = %d, mau 2", sum1.Created)
	}

	var slotID string
	err = poolUji.QueryRow(context.Background(), `
		SELECT id::text FROM slots WHERE lecturer_id = $1::uuid ORDER BY start_at DESC LIMIT 1`,
		dosen).Scan(&slotID)
	if err != nil {
		t.Fatalf("select slot: %v", err)
	}

	_, err = poolUji.Exec(context.Background(), `
		INSERT INTO bookings (slot_id, student_id, topic, status)
		VALUES ($1::uuid, $2::uuid, 'Bimbingan', 'confirmed')`,
		slotID, mhs)
	if err != nil {
		t.Fatalf("insert booking: %v", err)
	}

	// Production: booking.Create menandai slot 'booked' sebagai bagian dari
	// transaksi yang sama. Tiru di sini supaya status slot konsisten dengan
	// kenyataan (ada booking aktif -> slot 'booked').
	_, err = poolUji.Exec(context.Background(),
		`UPDATE slots SET status = 'booked' WHERE id = $1::uuid`, slotID)
	if err != nil {
		t.Fatalf("tandai slot booked: %v", err)
	}

	_, err = poolUji.Exec(context.Background(), `
		UPDATE availability_rules SET end_time = '09:30'::time WHERE lecturer_id = $1::uuid`,
		dosen)
	if err != nil {
		t.Fatalf("update aturan: %v", err)
	}

	sum2, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if sum2.Created != 0 {
		t.Errorf("created = %d, mau 0", sum2.Created)
	}
	// Booking ada di slot 09:30 (slot terakhir) -> slot itu jatuh di luar
	// jadwal baru (end_time dipersempit ke 09:30, jadi cuma slot 09:00 yang
	// muat), di-withdraw + booking-nya dibatalkan.
	// Slot 09:00 tidak punya booking dan masih masuk shouldExist (open),
	// jadi tidak dihapus.
	if sum2.Deleted != 0 {
		t.Errorf("deleted = %d, mau 0 (slot tanpa booking masih sah)", sum2.Deleted)
	}
	if sum2.Withdrawn != 1 {
		t.Errorf("withdrawn = %d, mau 1 (slot 09:30 dengan booking confirmed di-withdraw)", sum2.Withdrawn)
	}
	if sum2.BookingsCancelled != 1 {
		t.Errorf("bookings_cancelled = %d, mau 1 (booking di slot 09:30 di luar jadwal baru)", sum2.BookingsCancelled)
	}

	// Booking yang jatuh di luar jadwal baru HARUS dibatalkan, supaya
	// mahasiswanya tahu konsultasi-nya sudah tidak valid.
	var bookingStatus string
	err = poolUji.QueryRow(context.Background(),
		`SELECT status::text FROM bookings WHERE slot_id = $1::uuid`, slotID).Scan(&bookingStatus)
	if err != nil {
		t.Fatalf("select booking: %v", err)
	}
	if bookingStatus != "cancelled" {
		t.Errorf("booking status = %q, mau cancelled (booking di luar jadwal baru)", bookingStatus)
	}

	// cancelled_at dan cancelled_by harus terisi, sesuai CHECK constraint.
	var cancelledAt *time.Time
	var cancelledBy *string
	var cancelReason *string
	err = poolUji.QueryRow(context.Background(), `
		SELECT cancelled_at, cancelled_by::text, cancel_reason
		FROM bookings WHERE slot_id = $1::uuid`, slotID).
		Scan(&cancelledAt, &cancelledBy, &cancelReason)
	if err != nil {
		t.Fatalf("select cancel fields: %v", err)
	}
	if cancelledAt == nil {
		t.Error("cancelled_at harus terisi")
	}
	if cancelledBy == nil || *cancelledBy != dosen {
		t.Errorf("cancelled_by = %v, mau %s (lecturer_id)", cancelledBy, dosen)
	}
	if cancelReason == nil || *cancelReason == "" {
		t.Error("cancel_reason harus terisi")
	}

	// Notifikasi harus dikirim ke mahasiswa supaya dia tahu jadwalnya batal.
	var notifCount int
	err = poolUji.QueryRow(context.Background(),
		`SELECT count(*) FROM notifications WHERE booking_id IN (SELECT id FROM bookings WHERE slot_id = $1::uuid)`,
		slotID).Scan(&notifCount)
	if err != nil {
		t.Fatalf("select notif: %v", err)
	}
	if notifCount != 1 {
		t.Errorf("notif count = %d, mau 1", notifCount)
	}

	// payload->>'lecturer_id' HARUS berisi id dosen, bukan id mahasiswa.
	// Tanpa cek ini, bug 'lecturer_id = studentID' bisa balik tanpa ketahuan
	// karena test di atas hanya verifikasi notifikasi dibuat, bukan isinya.
	var payloadLecturerID string
	err = poolUji.QueryRow(context.Background(), `
		SELECT payload->>'lecturer_id'
		FROM notifications
		WHERE booking_id IN (SELECT id FROM bookings WHERE slot_id = $1::uuid)
		LIMIT 1`, slotID).Scan(&payloadLecturerID)
	if err != nil {
		t.Fatalf("select payload->>lecturer_id: %v", err)
	}
	if payloadLecturerID != dosen {
		t.Errorf("payload lecturer_id = %q, mau %q (id dosen, bukan id mahasiswa)", payloadLecturerID, dosen)
	}

	// Baris slotnya tetap ada (FK RESTRICT dari bookings), hanya ditandai
	// withdrawn.
	var slotExists bool
	err = poolUji.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM slots WHERE id = $1::uuid)`, slotID).Scan(&slotExists)
	if err != nil {
		t.Fatalf("select slot: %v", err)
	}
	if !slotExists {
		t.Error("slot harus tetap ada karena FK RESTRICT dari bookings")
	}

	// withdrawn_at harus terisi.
	var withdrawnAt *time.Time
	err = poolUji.QueryRow(context.Background(),
		`SELECT withdrawn_at FROM slots WHERE id = $1::uuid`, slotID).Scan(&withdrawnAt)
	if err != nil {
		t.Fatalf("select withdrawn_at: %v", err)
	}
	if withdrawnAt == nil {
		t.Error("withdrawn_at harus terisi untuk slot dengan booking yang dibatalkan")
	}

	// Slot 09:00 (yang tidak di-booking) harus masih ada dan masih open
	// karena dia tetap masuk shouldExist setelah end_time dipersempit.
	var otherSlotID string
	var otherStatus string
	err = poolUji.QueryRow(context.Background(), `
		SELECT id::text, status::text FROM slots
		WHERE lecturer_id = $1::uuid AND id <> $2::uuid`,
		dosen, slotID).Scan(&otherSlotID, &otherStatus)
	if err != nil {
		t.Fatalf("select slot 09:00: %v", err)
	}
	if otherStatus != "open" {
		t.Errorf("slot 09:00 status = %q, mau open", otherStatus)
	}
}

// TestReconcile_BookingYangMasihSahTidakDisentuh: ini test yang selama ini
// tidak ada dan yang paling penting. Setelah dosen mempunyai slot dengan
// booking confirmed, reconcile berikutnya yang tidak mengubah slot itu
// (karena aturannya masih mencakup slot-nya) HARUS tidak menyentuhnya.
// booking tetap 'confirmed', slot tetap 'booked', withdrawn_at tetap NULL,
// tidak ada notifikasi.
//
// Tanpa test ini, regresi seperti "cabang booked yang menitipkan slot ke
// loop pembersihan" bisa lewat karena TestReconcile_SlotSudahDipesanLaluDiperkecil
// selalu mempersempit jadwal. Padahal kejadian nyatanya: dosen mengubah
// aturan yang tidak relevan, atau menjalankan reconcile manual tanpa mengubah
// apa-apa, atau menyentuh pengecualian di hari lain. Semua itu TIDAK boleh
// membatalkan booking confirmed yang sah.
//
// Dua variasi: (1) reconcile tanpa mengubah apa-apa, (2) tambah pengecualian
// di tanggal lain yang tidak menyentuh slot.
func TestReconcile_BookingYangMasihSahTidakDisentuh(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t)
	defer HapusAturanDosen(t, dosen)

	campusTZ, _ := time.LoadLocation("Asia/Jakarta")
	monday := time.Date(2026, 9, 14, 0, 0, 0, 0, campusTZ)
	from := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, campusTZ)
	to := from.AddDate(0, 0, 1)
	_, err := poolUji.Exec(context.Background(), `
		INSERT INTO availability_rules
		(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, is_active)
		VALUES ($1::uuid, 1, '09:00'::time, '10:00'::time, 30, $2::date, true)`,
		dosen, from.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert aturan: %v", err)
	}

	svc := getSvc()

	sum1, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if sum1.Created != 2 {
		t.Errorf("created di reconcile 1 = %d, mau 2", sum1.Created)
	}

	var slotID string
	err = poolUji.QueryRow(context.Background(), `
		SELECT id::text FROM slots WHERE lecturer_id = $1::uuid ORDER BY start_at LIMIT 1`,
		dosen).Scan(&slotID)
	if err != nil {
		t.Fatalf("select slot: %v", err)
	}

	_, err = poolUji.Exec(context.Background(), `
		INSERT INTO bookings (slot_id, student_id, topic, status)
		VALUES ($1::uuid, $2::uuid, 'Bimbingan', 'confirmed')`,
		slotID, mhs)
	if err != nil {
		t.Fatalf("insert booking: %v", err)
	}
	_, err = poolUji.Exec(context.Background(),
		`UPDATE slots SET status = 'booked' WHERE id = $1::uuid`, slotID)
	if err != nil {
		t.Fatalf("tandai slot booked: %v", err)
	}

	// Variasi 1: reconcile tanpa mengubah aturan. Sama sekali tidak boleh
	// menyentuh booking, status slot, withdrawn_at, atau membuat notifikasi.
	sum2, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if sum2.Withdrawn != 0 {
		t.Errorf("variasi 1: withdrawn = %d, mau 0 (tidak ada yang harus di-withdraw)", sum2.Withdrawn)
	}
	if sum2.BookingsCancelled != 0 {
		t.Errorf("variasi 1: bookings_cancelled = %d, mau 0 (booking masih sah)", sum2.BookingsCancelled)
	}

	assertBookingDanSlotUtuh(t, slotID)

	// Variasi 2: tambah pengecualian full day di SELASA — hari yang berbeda
	// dari slot Senin. Aturan Senin masih berlaku, slot 09:00 masih sah,
	// jadi sekali lagi TIDAK boleh ada yang disentuh.
	_, err = poolUji.Exec(context.Background(), `
		INSERT INTO availability_exceptions
		(lecturer_id, exception_date, reason, is_full_day)
		VALUES ($1::uuid, $2::date, 'Rapat', true)`,
		dosen, from.AddDate(0, 0, 1).Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert pengecualian: %v", err)
	}

	sum3, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile 3: %v", err)
	}
	if sum3.Withdrawn != 0 {
		t.Errorf("variasi 2: withdrawn = %d, mau 0 (pengecualian di hari lain)", sum3.Withdrawn)
	}
	if sum3.BookingsCancelled != 0 {
		t.Errorf("variasi 2: bookings_cancelled = %d, mau 0 (pengecualian di hari lain)", sum3.BookingsCancelled)
	}

	assertBookingDanSlotUtuh(t, slotID)
}

// assertBookingDanSlotUtuh memverifikasi bahwa booking confirmed, slot 'booked',
// withdrawn_at NULL, dan tidak ada notifikasi — dengan kata lain, reconcile
// sebelumnya tidak menyentuhnya. Dipakai oleh
// TestReconcile_BookingYangMasihSahTidakDisentuh untuk dua variasi.
func assertBookingDanSlotUtuh(t *testing.T, slotID string) {
	t.Helper()

	var bookingStatus string
	if err := poolUji.QueryRow(context.Background(),
		`SELECT status::text FROM bookings WHERE slot_id = $1::uuid`, slotID).Scan(&bookingStatus); err != nil {
		t.Fatalf("select booking: %v", err)
	}
	if bookingStatus != "confirmed" {
		t.Errorf("booking status = %q, mau confirmed (tidak boleh disentuh)", bookingStatus)
	}

	var slotStatus string
	var withdrawnAt *time.Time
	if err := poolUji.QueryRow(context.Background(), `
		SELECT status::text, withdrawn_at FROM slots WHERE id = $1::uuid`, slotID).
		Scan(&slotStatus, &withdrawnAt); err != nil {
		t.Fatalf("select slot: %v", err)
	}
	if slotStatus != "booked" {
		t.Errorf("slot status = %q, mau booked", slotStatus)
	}
	if withdrawnAt != nil {
		t.Errorf("withdrawn_at = %v, mau NULL", withdrawnAt)
	}

	var notifCount int
	if err := poolUji.QueryRow(context.Background(),
		`SELECT count(*) FROM notifications WHERE booking_id IN (SELECT id FROM bookings WHERE slot_id = $1::uuid)`,
		slotID).Scan(&notifCount); err != nil {
		t.Fatalf("select notif: %v", err)
	}
	if notifCount != 0 {
		t.Errorf("notif count = %d, mau 0 (booking tidak boleh dinotifikasi)", notifCount)
	}
}

func TestReconcile_WithdrawnSlotTidakBisaDipesan(t *testing.T) {
	dosen := buatDosen(t)
	defer HapusAturanDosen(t, dosen)

	campusTZ, _ := time.LoadLocation("Asia/Jakarta")
	monday := time.Date(2026, 9, 14, 0, 0, 0, 0, campusTZ)
	from := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, campusTZ)
	to := from.AddDate(0, 0, 1)

	_, err := poolUji.Exec(context.Background(), `
		INSERT INTO availability_rules
		(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, is_active)
		VALUES ($1::uuid, 1, '09:00'::time, '09:30'::time, 30, $2::date, true)`,
		dosen, from.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert aturan: %v", err)
	}

	svc := getSvc()
	sum, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if sum.Created != 1 {
		t.Errorf("created = %d, mau 1", sum.Created)
	}

	var slotID string
	err = poolUji.QueryRow(context.Background(), `
		SELECT id::text FROM slots WHERE lecturer_id = $1::uuid`, dosen).Scan(&slotID)
	if err != nil {
		t.Fatalf("select slot: %v", err)
	}

	_, err = poolUji.Exec(context.Background(), `
		UPDATE slots SET withdrawn_at = now() WHERE id = $1::uuid`, slotID)
	if err != nil {
		t.Fatalf("withdraw slot: %v", err)
	}

	var withdrawnAt *time.Time
	err = poolUji.QueryRow(context.Background(),
		`SELECT withdrawn_at FROM slots WHERE id = $1::uuid`, slotID).Scan(&withdrawnAt)
	if err != nil {
		t.Fatalf("select withdrawn_at: %v", err)
	}
	if withdrawnAt == nil {
		t.Error("slot harus sudah withdrawn")
	}
}

// TestReconcile_SlotMasaLalu_DenganBookingConfirmed: slot masa lalu dengan booking
// confirmed tidak boleh disentuh sama sekali — tidak dihapus, tidak di-withdraw,
// booking tidak dibatalkan, tidak ada notification.
func TestReconcile_SlotMasaLalu_DenganBookingConfirmed(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t)
	defer HapusAturanDosen(t, dosen)

	campusTZ, _ := time.LoadLocation("Asia/Jakarta")

	// Slot 2 jam di belakang now, supaya slot selalu di masa lalu dan selalu
	// di dalam rentang [from, to] tanpa bergantung jam berapa test dijalankan.
	now := time.Now().UTC()
	slotStart := now.Add(-2 * time.Hour)
	slotEnd := slotStart.Add(30 * time.Minute)

	var slotID string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO slots (lecturer_id, start_at, end_at, status)
		VALUES ($1::uuid, $2, $3, 'booked')
		RETURNING id::text`,
		dosen, slotStart, slotEnd).Scan(&slotID)
	if err != nil {
		t.Fatalf("insert slot: %v", err)
	}

	// Booking confirmed untuk slot ini
	_, err = poolUji.Exec(context.Background(), `
		INSERT INTO bookings (slot_id, student_id, topic, status)
		VALUES ($1::uuid, $2::uuid, 'Bimbingan', 'confirmed')`,
		slotID, mhs)
	if err != nil {
		t.Fatalf("insert booking: %v", err)
	}

	// Aturan untuk membuat reconcile jalan
	from := now.Add(-6 * time.Hour)
	to := now.AddDate(0, 0, 7)

	_, err = poolUji.Exec(context.Background(), `
		INSERT INTO availability_rules
		(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, is_active)
		VALUES ($1::uuid, 1, '09:00'::time, '10:00'::time, 30, $2::date, true)`,
		dosen, from.In(campusTZ).Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert aturan: %v", err)
	}

	svc := getSvc()
	summary, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Summary tidak boleh menghitung slot masa lalu
	if summary.Deleted != 0 {
		t.Errorf("deleted = %d, mau 0", summary.Deleted)
	}
	if summary.Withdrawn != 0 {
		t.Errorf("withdrawn = %d, mau 0", summary.Withdrawn)
	}
	if summary.BookingsCancelled != 0 {
		t.Errorf("bookings_cancelled = %d, mau 0", summary.BookingsCancelled)
	}

	// Booking tetap confirmed
	var bookingStatus string
	err = poolUji.QueryRow(context.Background(),
		`SELECT status::text FROM bookings WHERE slot_id = $1::uuid`, slotID).Scan(&bookingStatus)
	if err != nil {
		t.Fatalf("select booking: %v", err)
	}
	if bookingStatus != "confirmed" {
		t.Errorf("booking status = %q, mau confirmed", bookingStatus)
	}

	// Slot tetap ada
	var slotExists bool
	err = poolUji.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM slots WHERE id = $1::uuid)`, slotID).Scan(&slotExists)
	if err != nil {
		t.Fatalf("select slot: %v", err)
	}
	if !slotExists {
		t.Error("slot masa lalu tidak boleh dihapus")
	}

	// Slot tidak di-withdraw
	var withdrawnAt *time.Time
	err = poolUji.QueryRow(context.Background(),
		`SELECT withdrawn_at FROM slots WHERE id = $1::uuid`, slotID).Scan(&withdrawnAt)
	if err != nil {
		t.Fatalf("select withdrawn_at: %v", err)
	}
	if withdrawnAt != nil {
		t.Error("slot masa lalu tidak boleh di-withdraw")
	}

	// Tidak ada notification
	var notifCount int
	err = poolUji.QueryRow(context.Background(),
		`SELECT count(*) FROM notifications WHERE booking_id IN (SELECT id FROM bookings WHERE slot_id = $1::uuid)`,
		slotID).Scan(&notifCount)
	if err != nil {
		t.Fatalf("select notif: %v", err)
	}
	if notifCount != 0 {
		t.Errorf("notif count = %d, mau 0", notifCount)
	}
}

// TestReconcile_SlotMasaLalu_OpenTanpaBooking: slot masa lalu tanpa booking
// tidak boleh dihapus.
func TestReconcile_SlotMasaLalu_OpenTanpaBooking(t *testing.T) {
	dosen := buatDosen(t)
	defer HapusAturanDosen(t, dosen)

	campusTZ, _ := time.LoadLocation("Asia/Jakarta")

	// Slot 2 jam di belakang now, supaya slot selalu di masa lalu dan selalu
	// di dalam rentang [from, to] tanpa bergantung jam berapa test dijalankan.
	now := time.Now().UTC()
	slotStart := now.Add(-2 * time.Hour)
	slotEnd := slotStart.Add(30 * time.Minute)

	var slotID string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO slots (lecturer_id, start_at, end_at, status)
		VALUES ($1::uuid, $2, $3, 'open')
		RETURNING id::text`,
		dosen, slotStart, slotEnd).Scan(&slotID)
	if err != nil {
		t.Fatalf("insert slot: %v", err)
	}

	// Aturan untuk membuat reconcile jalan
	from := now.Add(-6 * time.Hour)
	to := now.AddDate(0, 0, 7)

	_, err = poolUji.Exec(context.Background(), `
		INSERT INTO availability_rules
		(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, is_active)
		VALUES ($1::uuid, 1, '09:00'::time, '10:00'::time, 30, $2::date, true)`,
		dosen, from.In(campusTZ).Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert aturan: %v", err)
	}

	svc := getSvc()
	summary, err := svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Summary tidak boleh menghitung slot masa lalu
	if summary.Deleted != 0 {
		t.Errorf("deleted = %d, mau 0", summary.Deleted)
	}

	// Slot tetap ada
	var slotExists bool
	err = poolUji.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM slots WHERE id = $1::uuid)`, slotID).Scan(&slotExists)
	if err != nil {
		t.Fatalf("select slot: %v", err)
	}
	if !slotExists {
		t.Error("slot masa lalu open tidak boleh dihapus")
	}
}

// TestReconcile_TakAdaSlotMelampauiHorizon: slot yang dibuat tidak boleh melampaui horizon.
func TestReconcile_TakAdaSlotMelampauiHorizon(t *testing.T) {
	dosen := buatDosen(t)
	defer HapusAturanDosen(t, dosen)

	campusTZ, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, campusTZ)
	to := from.AddDate(0, 0, 30)

	// Aturan setiap hari kerja jam 9-10, 30 menit (2 slot per hari)
	_, err := poolUji.Exec(context.Background(), `
		INSERT INTO availability_rules
		(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, is_active)
		VALUES ($1::uuid, 1, '09:00'::time, '10:00'::time, 30, $2::date, true)`,
		dosen, from.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert aturan: %v", err)
	}

	svc := getSvc()
	_, err = svc.Reconcile(context.Background(), dosen, from, to, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Ambil semua slot dan cek tidak ada yang melampaui horizon
	horizonLimit := from.AddDate(0, 0, 30).UTC()
	rows, err := poolUji.Query(context.Background(), `
		SELECT start_at FROM slots WHERE lecturer_id = $1::uuid ORDER BY start_at`, dosen)
	if err != nil {
		t.Fatalf("query slot: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var startAt time.Time
		if err := rows.Scan(&startAt); err != nil {
			t.Fatalf("scan slot: %v", err)
		}
		if startAt.After(horizonLimit) {
			t.Errorf("slot melampaui horizon: start_at=%v, limit=%v", startAt, horizonLimit)
		}
	}
	if rows.Err() != nil {
		t.Fatalf("iterasi slot: %v", rows.Err())
	}
}
