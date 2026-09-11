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
//
// Tidak ada TRUNCATE di file ini, dan itu disengaja — pola yang sama dengan
// internal/booking/service_test.go. Package lain (booking, server, dll) memakai
// database uji yang sama dan insert notification asli lewat transaction booking.
// Kalau kita TRUNCATE sebelum test, kita akan menghapus data mereka.
//
// Isolasi yang benar bukan membersihkan meja sebelum mulai, melainkan
// memakai meja sendiri: setiap test menyuntik notification dengan booking_id
// dan user_id unik, lalu memakai processNotification(ctx, notifID) supaya
// query worker juga disaring ke id itu. processBatch sengaja TIDAK dipakai
// di test ini karena akan ikut memproses baris pending milik test lain
// di database bersama.
//
// Jalankan dengan:  TEST_DATABASE_URL=... go test ./internal/reminder/...
// Kalau TEST_DATABASE_URL kosong, seluruh file ini dilewati — supaya
// `go test ./...` biasa tetap cepat dan tidak menuntut Docker hidup.

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
// - booking confirmed -> sender terpanggil untuk notif ini
// - status notif menjadi 'sent'
// - sent_at terisi
//
// Pemakaian processNotification(ctx, notifID) (bukan processBatch) menjamin
// test hanya memproses baris miliknya sendiri dan tidak ikut memproses
// pending notification milik test lain di database uji bersama.
func TestReminderSent_Success(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t)
	bookingID := buatBookingConfirmed(t, mhs, dosen)
	notifID := buatNotification(t, mhs, bookingID)

	fake := NewFakeSender()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := New(poolUji, fake, time.UTC, time.Minute, 5, log)

	ctx := context.Background()
	processed, err := svc.processNotification(ctx, notifID)
	if err != nil {
		t.Fatalf("processNotification error: %v", err)
	}
	if processed != 1 {
		t.Errorf("processed = %d, want 1", processed)
	}

	if fake.CallCount() != 1 {
		t.Errorf("sender call count = %d, want 1", fake.CallCount())
	}

	// Verifikasi status di database — disaring ke notifID ini.
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
	processed, err := svc.processNotification(ctx, notifID)
	if err != nil {
		t.Fatalf("processNotification error: %v", err)
	}
	if processed != 1 {
		t.Errorf("processed = %d, want 1", processed)
	}

	// Sender tidak boleh terpanggil karena booking sudah cancelled.
	if fake.CallCount() != 0 {
		t.Errorf("sender call count = %d, want 0 (booking cancelled)", fake.CallCount())
	}

	// Verifikasi status di database — disaring ke notifID ini.
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
	processed, err := svc.processNotification(ctx, notifID)
	if err != nil {
		t.Fatalf("processNotification error: %v", err)
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

// TestReminderConcurrencyVerify_SKIP_LOCKED memverifikasi bahwa FOR UPDATE SKIP LOCKED
// bekerja secara deterministik.
//
// DESAIN TEST:
//  1. Buat SATU notification pending yang due.
//  2. Blok FakeSender SEBELUM goroutine A jalan.
//     A akan berhenti di tengah Send() sementara transaksinya masih terbuka —
//     baris terkunci FOR UPDATE SKIP LOCKED.
//  3. Saat A sudah ngeblock di Send(), jalankan goroutine B.
//     B harus melihat baris terkunci dan skip (processed == 0), bukan tunggu.
//  4. Unblock A, tunggu selesai, assert CallCount == 1.
//
// Catatan: test ini memakai processNotification(ctx, notifID) — bukan
// processBatch — supaya query kedua goroutine hanya melihat baris notifID
// ini (idempotent). Kalau pakai processBatch, setiap goroutine akan scan
// semua pending dan ikut memproses baris dari test lain di database bersama.
//
// MUTATION TEST:
//  1. Comment out FOR UPDATE SKIP LOCKED di reminder.go pada query
//     processNotification (baris ~198) DAN pada query di processBatch.
//  2. Jalankan: go test ./internal/reminder -v -run TestReminderConcurrencyVerify_SKIP_LOCKED
//  3. Tanpa lock, MVCC READ COMMITTED membuat B melihat baris yang sama
//     dengan A (tidak ada lock) dan ikut memproses. Dengan FakeSender
//     di-block, A tidak commit, B akan timeout ("tidak selesai dalam 5 detik")
//     atau muncul panic saat tx tutup. TEST AKAN FAIL.
//  4. Kembalikan kodenya, test PASS.
func TestReminderConcurrencyVerify_SKIP_LOCKED(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t)
	bookingID := buatBookingConfirmed(t, mhs, dosen)
	_ = buatNotification(t, mhs, bookingID)
	// Ambil id notif yang barusan dibuat supaya bisa di-scope ke processNotification.
	var notifID string
	err := poolUji.QueryRow(context.Background(), `
		SELECT id::text FROM notifications
		WHERE booking_id = $1::uuid ORDER BY scheduled_at DESC LIMIT 1`, bookingID,
	).Scan(&notifID)
	if err != nil {
		t.Fatalf("mengambil id notification: %v", err)
	}

	fake := NewFakeSender()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := New(poolUji, fake, time.UTC, time.Minute, 5, log)

	// Blok Send() SEBELUM goroutine A mulai.
	// Saat A memanggil processNotification, transaksinya akan terbuka dan baris
	// notification terkunci FOR UPDATE SKIP LOCKED.
	fake.Block()

	ctx := context.Background()

	type result struct {
		processed int
		err       error
	}
	resultA := make(chan result, 1)
	resultB := make(chan result, 1)

	// Goroutine A: memanggil processNotification.
	// Dia akan berhenti di FakeSender.Send() karena di-block.
	go func() {
		processed, err := svc.processNotification(ctx, notifID)
		resultA <- result{processed, err}
	}()

	// Tunggu sampai A benar-benar masuk ke Send() dan berhenti.
	for i := 0; i < 5000; i++ {
		if fake.SemaphoreCount() >= 1 {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}
	if fake.SemaphoreCount() < 1 {
		t.Fatal("goroutine A tidak pernah sampai ke Send() — polling timeout")
	}

	// Sekarang goroutine B: dia harus melihat baris terkunci dan SKIP.
	// processNotification dengan ID yang sama, FOR UPDATE SKIP LOCKED, akan
	// melihat baris dipegang A dan tidak memilihnya → return 0.
	go func() {
		processed, err := svc.processNotification(ctx, notifID)
		resultB <- result{processed, err}
	}()

	// B harus pulang dengan processed == 0 (baris di-skip karena terkunci A).
	select {
	case r := <-resultB:
		if r.processed != 0 {
			t.Errorf("goroutine B processed = %d, want 0 (baris masih terkunci A)", r.processed)
		}
		if r.err != nil {
			t.Errorf("goroutine B error: %v", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Error("goroutine B tidak selesai dalam 5 detik")
	}

	// Unblock A, tunggu selesai.
	fake.Unblock()

	select {
	case r := <-resultA:
		if fake.CallCount() != 1 {
			t.Errorf("sender call count = %d, want 1", fake.CallCount())
		}
		if r.processed != 1 {
			t.Errorf("goroutine A processed = %d, want 1", r.processed)
		}
		if r.err != nil {
			t.Errorf("goroutine A error: %v", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Error("goroutine A tidak selesai dalam 5 detik setelah Unblock")
	}
}

// TestProcessNotification_NotFound memverifikasi bahwa processNotification
// dengan id yang tidak ada di tabel mengembalikan (0, nil) tanpa error.
// Pengganti TestProcessBatch_EmptyQueue: versi lama tidak bisa lagi
// mengasumsikan queue kosong (database uji dipakai bersama).
func TestProcessNotification_NotFound(t *testing.T) {
	fake := NewFakeSender()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := New(poolUji, fake, time.UTC, time.Minute, 5, log)

	ctx := context.Background()
	// UUID ini dijamin tidak ada di tabel notifications.
	processed, err := svc.processNotification(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("processNotification error: %v", err)
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
	// blocked digunakan untuk sinkronisasi blocking. Kalau nil, Send() langsung lanjut.
	blocked chan struct{}
	// Semaphore untuk menghitung berapa pemanggilan yang sedang terblok di Send().
	// Poll menggunakan ini untuk mengetahui apakah ada goroutine yang sedang di-block.
	semaphore int32
}

type SendCall struct {
	To      string
	Subject string
	Body    string
}

func NewFakeSender() *FakeSender {
	return &FakeSender{}
}

// Block membuat FakeSender memblok setiap pemanggilan Send() sampai Unblock() dipanggil.
// Ini dipakai untuk memastikan overlap transaksi yang sebenarnya di Postgres.
func (f *FakeSender) Block() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blocked = make(chan struct{})
}

// Unblock melepaskan semua goroutine yang sedang menunggu di Send().
func (f *FakeSender) Unblock() {
	f.mu.Lock()
	ch := f.blocked
	f.blocked = nil
	f.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// SemaphoreCount mengembalikan jumlah pemanggilan yang sedang terblok.
// Positif berarti ada goroutine yang sedang di-block di Send().
func (f *FakeSender) SemaphoreCount() int32 {
	return atomic.LoadInt32(&f.semaphore)
}

func (f *FakeSender) Send(ctx context.Context, to, subject, body string) error {
	// Pertama: cek apakah perlu block.
	f.mu.Lock()
	if f.blocked != nil {
		blocked := f.blocked
		atomic.AddInt32(&f.semaphore, 1) // catat: ada yang mulai block
		f.mu.Unlock()
		<-blocked                         // tunggu sampai di-Unblock()
		atomic.AddInt32(&f.semaphore, -1) // catat: sudah keluar dari block
	} else {
		f.mu.Unlock()
	}
	// Kedua: catat pemanggilan.
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
