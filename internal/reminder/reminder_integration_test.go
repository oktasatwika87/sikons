package reminder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Test di file ini adalah INTEGRATION TEST: butuh Postgres sungguhan.
//
// Kenapa tidak dipalsukan? Karena yang sedang diuji adalah perilaku penguncian
// baris Postgres dengan FOR UPDATE SKIP LOCKED. Database palsu tidak akan
// membuktikan apa pun tentang race condition.

var poolUji *pgxpool.Pool

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		fmt.Println("TEST_DATABASE_URL kosong — test reminder dilewati")
		os.Exit(0)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		fmt.Println("TEST_DATABASE_URL tidak valid:", err)
		os.Exit(1)
	}
	cfg.MaxConns = 30

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolUji, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		fmt.Println("tidak bisa terhubung ke database uji:", err)
		os.Exit(1)
	}
	if err := poolUji.Ping(ctx); err != nil {
		fmt.Println("database uji tidak merespons:", err)
		os.Exit(1)
	}

	kode := m.Run()
	poolUji.Close()
	os.Exit(kode)
}

// penghitungUnik memberi identitas unik per test agar tidak bentrok dengan test lain.
var penghitungUnik atomic.Int64

func unik() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), penghitungUnik.Add(1))
}

// buatDosen membuat user dosen + profil untuk testing.
func buatDosen(t *testing.T) string {
	t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('dosen-' || $1 || '@uji.local', 'x', 'Dosen Uji', 'lecturer')
		RETURNING id::text`, unik()).Scan(&id)
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

// buatMahasiswa membuat user mahasiswa untuk testing.
func buatMahasiswa(t *testing.T) string {
	t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('mhs-' || $1 || '@uji.local', 'x', 'Mahasiswa Uji', 'student')
		RETURNING id::text`, unik()).Scan(&id)
	if err != nil {
		t.Fatalf("membuat mahasiswa: %v", err)
	}
	return id
}

// buatBookingConfirmed membuat booking confirmed dan mengembalikan booking_id.
func buatBookingConfirmed(t *testing.T, studentID, lecturerID string) string {
	t.Helper()
	// Buat slot di masa depan.
	var slotID string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO slots (lecturer_id, start_at, end_at)
		VALUES ($1::uuid, now() + interval '3 hours', now() + interval '3 hours 30 minutes')
		RETURNING id::text`, lecturerID).Scan(&slotID)
	if err != nil {
		t.Fatalf("membuat slot: %v", err)
	}

	var bookingID string
	err = poolUji.QueryRow(context.Background(), `
		INSERT INTO bookings (slot_id, student_id, topic, status)
		VALUES ($1::uuid, $2::uuid, 'Topik uji', 'confirmed')
		RETURNING id::text`, slotID, studentID).Scan(&bookingID)
	if err != nil {
		t.Fatalf("membuat booking: %v", err)
	}
	return bookingID
}

// buatBookingCancelled membuat booking cancelled untuk testing.
// Insert sebagai confirmed dulu, baru update ke cancelled karena constraint check.
func buatBookingCancelled(t *testing.T, studentID, lecturerID string) string {
	t.Helper()
	var slotID string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO slots (lecturer_id, start_at, end_at)
		VALUES ($1::uuid, now() + interval '3 hours', now() + interval '3 hours 30 minutes')
		RETURNING id::text`, lecturerID).Scan(&slotID)
	if err != nil {
		t.Fatalf("membuat slot: %v", err)
	}

	var bookingID string
	err = poolUji.QueryRow(context.Background(), `
		INSERT INTO bookings (slot_id, student_id, topic, status)
		VALUES ($1::uuid, $2::uuid, 'Topik uji cancelled', 'confirmed')
		RETURNING id::text`, slotID, studentID).Scan(&bookingID)
	if err != nil {
		t.Fatalf("membuat booking: %v", err)
	}

	// Update ke cancelled dengan mengisi cancelled_at dan cancelled_by
	// (constraint check mengharuskan field-field ini terisi saat status = cancelled).
	_, err = poolUji.Exec(context.Background(), `
		UPDATE bookings
		SET status = 'cancelled',
		    cancelled_at = now(),
		    cancelled_by = $1::uuid,
		    cancel_reason = 'test cancellation'
		WHERE id = $2::uuid`, studentID, bookingID)
	if err != nil {
		t.Fatalf("mengubah status ke cancelled: %v", err)
	}
	return bookingID
}

// buatNotification membuat notification pending yang sudah due.
func buatNotification(t *testing.T, userID, bookingID string) string {
	t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO notifications (user_id, booking_id, type, payload, scheduled_at)
		VALUES ($1::uuid, $2::uuid, 'booking_reminder', '{}'::jsonb, now() - interval '1 second')
		RETURNING id::text`, userID, bookingID).Scan(&id)
	if err != nil {
		t.Fatalf("membuat notification: %v", err)
	}
	return id
}

// TestReminderSent_Success memverifikasi bahwa:
// - booking confirmed -> sender terpanggil
// - status menjadi 'sent'
// - sent_at terisi
func TestReminderSent_Success(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t)
	bookingID := buatBookingConfirmed(t, mhs, dosen)
	notifID := buatNotification(t, mhs, bookingID)

	fake := NewFakeSender()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := New(poolUji, fake, time.UTC, time.Minute, 5, log)

	ctx := context.Background()
	processed, err := svc.processBatch(ctx)
	if err != nil {
		t.Fatalf("processBatch error: %v", err)
	}
	if processed != 1 {
		t.Errorf("processed = %d, want 1", processed)
	}

	if fake.CallCount() != 1 {
		t.Errorf("sender call count = %d, want 1", fake.CallCount())
	}

	// Verifikasi status di database.
	var status string
	var sentAt *time.Time
	err = poolUji.QueryRow(context.Background(), `
		SELECT status::text, sent_at FROM notifications WHERE id = $1::uuid`, notifID,
	).Scan(&status, &sentAt)
	if err != nil {
		t.Fatalf("query notification: %v", err)
	}
	if status != "sent" {
		t.Errorf("status = %q, want sent", status)
	}
	if sentAt == nil {
		t.Error("sent_at should be set")
	}
}

// TestReminderSkipped_NotConfirmed memverifikasi bahwa:
// - booking sudah cancelled -> sender TIDAK terpanggil
// - status menjadi 'skipped'
func TestReminderSkipped_NotConfirmed(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t)
	bookingID := buatBookingCancelled(t, mhs, dosen)
	notifID := buatNotification(t, mhs, bookingID)

	fake := NewFakeSender()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := New(poolUji, fake, time.UTC, time.Minute, 5, log)

	ctx := context.Background()
	processed, err := svc.processBatch(ctx)
	if err != nil {
		t.Fatalf("processBatch error: %v", err)
	}
	if processed != 1 {
		t.Errorf("processed = %d, want 1", processed)
	}

	// Sender tidak boleh terpanggil karena booking sudah cancelled.
	if fake.CallCount() != 0 {
		t.Errorf("sender call count = %d, want 0 (booking cancelled)", fake.CallCount())
	}

	// Verifikasi status di database.
	var status string
	err = poolUji.QueryRow(context.Background(), `
		SELECT status::text FROM notifications WHERE id = $1::uuid`, notifID,
	).Scan(&status)
	if err != nil {
		t.Fatalf("query notification: %v", err)
	}
	if status != "skipped" {
		t.Errorf("status = %q, want skipped", status)
	}
}

// TestReminderFailed_AfterMaxAttempts memverifikasi bahwa:
// - sender selalu gagal -> setelah maxAttempts kali, status jadi 'failed'
// - sent_at TIDAK terisi (email tidak pernah terkirim)
func TestReminderFailed_AfterMaxAttempts(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t)
	bookingID := buatBookingConfirmed(t, mhs, dosen)

	// Buat notification dengan attempts = maxAttempts - 1
	var notifID string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO notifications (user_id, booking_id, type, payload, scheduled_at, attempts)
		VALUES ($1::uuid, $2::uuid, 'booking_reminder', '{}'::jsonb,
		        now() - interval '1 second', 4)
		RETURNING id::text`, mhs, bookingID).Scan(&notifID)
	if err != nil {
		t.Fatalf("membuat notification: %v", err)
	}

	fake := NewFakeSender()
	fake.SetShouldFail(true, errors.New("always fail"))
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := New(poolUji, fake, time.UTC, time.Minute, 5, log)

	ctx := context.Background()
	processed, err := svc.processBatch(ctx)
	if err != nil {
		t.Fatalf("processBatch error: %v", err)
	}
	if processed != 1 {
		t.Errorf("processed = %d, want 1", processed)
	}

	// Verifikasi sender dipanggil (attempt terakhir).
	if fake.CallCount() != 1 {
		t.Errorf("sender call count = %d, want 1", fake.CallCount())
	}

	// Verifikasi status jadi 'failed'.
	var status string
	var sentAt *time.Time
	err = poolUji.QueryRow(context.Background(), `
		SELECT status::text, sent_at FROM notifications WHERE id = $1::uuid`, notifID,
	).Scan(&status, &sentAt)
	if err != nil {
		t.Fatalf("query notification: %v", err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
	// sent_at tidak boleh terisi karena email tidak pernah terkirim.
	if sentAt != nil {
		t.Errorf("sent_at should be nil (email never sent), got %v", sentAt)
	}
}

// TestReminderConcurrencyVerify_SKIP_LOCKED adalah mutation test untuk membuktikan
// bahwa FOR UPDATE SKIP LOCKED BENAR-BENAR bekerja.
//
// Mutation test steps:
// 1. Comment out FOR UPDATE SKIP LOCKED di reminder.go
// 2. Jalankan test: go test ./internal/reminder -v -run TestReminderConcurrencyVerify_SKIP_LOCKED
// 3. Test akan GAGAL karena sender dipanggil lebih dari sekali (race condition)
// 4. Kembalikan kodenya
//
// HASIL MUTATION TEST (tanpa FOR UPDATE SKIP LOCKED, dijalankan dengan TEST_DATABASE_URL):
// --- FAIL: TestReminderConcurrencyVerify_SKIP_LOCKED (0.05s)
//
//	reminder_integration_test.go:361: sender call count = 2, want 1
//
// Output asli:
// === RUN   TestReminderConcurrencyVerify_SKIP_LOCKED
// time=... level=INFO msg="reminder sent" notification_id=... to=... (dipanggil 2 kali)
//
//	reminder_integration_test.go:361: sender call count = 2, want 1
//
// --- FAIL: TestReminderConcurrencyVerify_SKIP_LOCKED (0.05s)
//
// Kesimpulan: FOR UPDATE SKIP LOCKED DIPERLUKAN untuk mencegah
// double-processing notification yang sama. Race condition mungkin tidak selalu
// muncul (tergantung timing Postgres), tapi race yang pernah terjadi membuktikan
// bahwa tanpa lock, double-send adalah kemungkinan nyata.
func TestReminderConcurrencyVerify_SKIP_LOCKED(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t)
	bookingID := buatBookingConfirmed(t, mhs, dosen)
	_ = buatNotification(t, mhs, bookingID)

	fake := NewFakeSender()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := New(poolUji, fake, time.UTC, time.Minute, 5, log)

	ctx := context.Background()

	// Barrier untuk memastikan kedua goroutine mulai bersamaan.
	mulai := make(chan struct{})
	var wg sync.WaitGroup
	const goroutines = 2
	wg.Add(goroutines)

	// Goroutine-goroutine sama-sama memanggil processBatch.
	// Dengan FOR UPDATE SKIP LOCKED, hanya satu yang boleh memproses notification.
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			<-mulai
			_, _ = svc.processBatch(ctx)
		}(i)
	}

	close(mulai)
	wg.Wait()

	if fake.CallCount() != 1 {
		t.Errorf("sender call count = %d, want 1", fake.CallCount())
	}
}

// TestProcessBatch_EmptyQueue memverifikasi bahwa processBatch tidak error
// ketika tidak ada notification yang due.
func TestProcessBatch_EmptyQueue(t *testing.T) {
	fake := NewFakeSender()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := New(poolUji, fake, time.UTC, time.Minute, 5, log)

	ctx := context.Background()
	processed, err := svc.processBatch(ctx)
	if err != nil {
		t.Fatalf("processBatch error: %v", err)
	}
	if processed != 0 {
		t.Errorf("processed = %d, want 0", processed)
	}
	if fake.CallCount() != 0 {
		t.Errorf("sender call count = %d, want 0", fake.CallCount())
	}
}

// FakeSender adalah mock sender untuk testing.
type FakeSender struct {
	mu         sync.Mutex
	calls      []SendCall
	shouldFail bool
	failErr    error
}

type SendCall struct {
	To      string
	Subject string
	Body    string
}

func NewFakeSender() *FakeSender {
	return &FakeSender{}
}

func (f *FakeSender) Send(ctx context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, SendCall{To: to, Subject: subject, Body: body})
	if f.shouldFail {
		if f.failErr != nil {
			return f.failErr
		}
		return errors.New("fake send error")
	}
	return nil
}

func (f *FakeSender) Calls() []SendCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SendCall{}, f.calls...)
}

func (f *FakeSender) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *FakeSender) SetShouldFail(shouldFail bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shouldFail = shouldFail
	f.failErr = err
}
