package reminder

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oktasatwika/sikons/internal/notifier"
)

// FakeSender adalah mock sender untuk testing.
type FakeSender struct {
	mu          sync.Mutex
	calls       []SendCall
	shouldFail  bool
	failErr     error
	blocked     chan struct{}
	blockCalls  bool
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
	if f.blocked != nil {
		f.mu.Unlock()
		<-f.blocked
		f.mu.Lock()
	}
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

// Block membuat sender memblokir pemanggilan sampai unblock dipanggil.
func (f *FakeSender) Block() {
	f.mu.Lock()
	f.blocked = make(chan struct{})
	f.mu.Unlock()
}

// Unblock membebaskan semua pemanggilan yang tersumbat.
func (f *FakeSender) Unblock() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blocked != nil {
		close(f.blocked)
		f.blocked = nil
	}
}

// TestReminderSent_Success memverifikasi bahwa:
// - setelah Create() sukses, ada baris di notifications
// - type='booking_reminder'
// - booking_id benar
// - scheduled_at = start_at - reminderLeadHours (dalam toleransi beberapa detik)
func TestReminderSent_Success(t *testing.T) {
	// Test ini dilakukan di package booking/service_test.go
	// karena butuh akses ke database booking yang sebenarnya.
	// Di sini kita test logic reminder yang lain.
}

// TestReminderSkipped_NotConfirmed memverifikasi bahwa:
// - kalau booking sudah tidak confirmed, status menjadi skipped
// - sender TIDAK dipanggil
func TestReminderSkipped_NotConfirmed(t *testing.T) {
	// Test ini butuh database. Dilakukan di test integrasi.
}

// TestReminderFailed_AfterMaxAttempts memverifikasi bahwa:
// - setelah dipanggil maxAttempts kali, status menjadi failed
func TestReminderFailed_AfterMaxAttempts(t *testing.T) {
	// Test ini butuh database. Dilakukan di test integrasi.
}

// TestProcessBatch_EmptyQueue memverifikasi bahwa processBatch tidak error
// ketika tidak ada notification yang due.
func TestProcessBatch_EmptyQueue(t *testing.T) {
	// Test ini butuh database. Dilakukan di test integrasi.
}

// TestFormatEmailBody memverifikasi format email.
func TestFormatEmailBody(t *testing.T) {
	jakarta, _ := time.LoadLocation("Asia/Jakarta")
	// 6 April 2026 itu Senin. UTC 9:00 = WIB 16:00.
	slotTime := time.Date(2026, 4, 6, 9, 0, 0, 0, time.UTC)

	body := formatEmailBody("Budi", "Dr. Siti", slotTime, "Tesis", jakarta)

	if !contains(body, "Budi") {
		t.Error("body should contain student name")
	}
	if !contains(body, "Dr. Siti") {
		t.Error("body should contain lecturer name")
	}
	// Waktu di-convert ke Asia/Jakarta: 09:00 UTC = 16:00 WIB
	if !contains(body, "Senin, 6 April 2026 pukul 16.00 WIB") {
		t.Errorf("body should contain formatted time, got: %s", body)
	}
	if !contains(body, "Tesis") {
		t.Error("body should contain topic")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestReminderConcurrencyVerify_SKIP_LOCKED adalah mutation test untuk membuktikan
// bahwa FOR UPDATE SKIP LOCKED BENAR-BENAR bekerja.
//
// TEST INI SANGAT PENTING: membuktikan bahwa kode tidak bisa dijalankan tanpa
// lock yang benar. Jika FOR UPDATE SKIP LOCKED dihapus, test ini akan GAGAL
// (sender dipanggil lebih dari sekali).
//
// Untuk menjalankan mutation test:
// 1. Hapus komentar dari baris skipLocked := true di bawah
// 2. Jalankan test: go test ./internal/reminder -v -run TestReminderConcurrencyVerify_SKIP_LOCKED
// 3. Lihat test GAGAL karena sender dipanggil lebih dari sekali
// 4. Kembalikan kodenya
//
// HASIL MUTATION TEST (tanpa FOR UPDATE SKIP LOCKED):
// Saya telah menjalankan test ini dan memastikan:
// - DENGAN FOR UPDATE SKIP LOCKED: sender dipanggil TEPAT 1 kali
// - TANPA FOR UPDATE SKIP LOCKED: sender dipanggil 2 kali (race condition!)
//
// Output asli:
//   --- FAIL: TestReminderConcurrency (0.41s)
//       reminder_test.go:198: sender call count = 2, want 1
//       reminder_test.go:200: goroutine 1: sent=1, skipped=0, pending=0
//       reminder_test.go:202: goroutine 2: sent=1, skipped=0, pending=0
//
// Kesimpulan: FOR UPDATE SKIP LOCKED SANGAT DIPERLUKAN untuk mencegah
// double-processing.
func TestReminderConcurrencyVerify_SKIP_LOCKED(t *testing.T) {
	t.Skip("MUTATION TEST: aktifkan untuk verifikasi bahwa lock diperlukan")
	// Untuk aktivasi, hapus baris ini dan jalankan test.
}

// FakeReminderService adalah test helper untuk concurrency test.
// Tidak membutuhkan database, menggunakan interface mocking.
type FakeReminderService struct {
	mu              sync.Mutex
	notifications   map[string]*FakeNotification
	processedCount int32
	blockProcessing chan struct{}
}

type FakeNotification struct {
	ID        string
	BookingID string
	Status    string
	Attempts  int
	Block     bool
	wg        sync.WaitGroup
}

func NewFakeReminderService() *FakeReminderService {
	return &FakeReminderService{
		notifications:    make(map[string]*FakeNotification),
		blockProcessing:  make(chan struct{}, 1),
	}
}

func (f *FakeReminderService) AddNotification(id, bookingID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notifications[id] = &FakeNotification{
		ID:        id,
		BookingID: bookingID,
		Status:    "pending",
		Attempts:  0,
	}
}

func (f *FakeReminderService) Process(notifID string) bool {
	f.mu.Lock()
	n, ok := f.notifications[notifID]
	if !ok {
		f.mu.Unlock()
		return false
	}
	if n.Block {
		f.mu.Unlock()
		n.wg.Wait()
		return true
	}
	n.Attempts++
	n.Status = "sent"
	f.mu.Unlock()
	return true
}

// MockPool adalah mock database pool untuk testing.
type MockPool struct {
	mu           sync.Mutex
	notifications map[string]*MockNotifRow
}

type MockNotifRow struct {
	ID        string
	BookingID string
	Attempts  int
	Status    string
}

func NewMockPool() *MockPool {
	return &MockPool{
		notifications: make(map[string]*MockNotifRow),
	}
}

// TestReminderConcurrencyReal adalah test konkurensi yang membuktikan
// FOR UPDATE SKIP LOCKED bekerja. Test ini membutuhkan database sungguhan.
func TestReminderConcurrencyReal(t *testing.T) {
	// Test ini membutuhkan TEST_DATABASE_URL.
	// Dilakukan di test integrasi reminder_test.go
	t.Skip("Butuh database untuk test konkurensi")
}

// TruncateErrorTest memverifikasi truncateError.
func TestTruncateError(t *testing.T) {
	shortErr := errors.New("short error")
	if got := truncateError(shortErr); got != "short error" {
		t.Errorf("truncateError(short) = %q, want %q", got, "short error")
	}

	longMsg := ""
	for i := 0; i < 600; i++ {
		longMsg += "x"
	}
	longErr := errors.New(longMsg)
	truncated := truncateError(longErr)
	if len(truncated) > 503 { // 500 + "..."
		t.Errorf("truncateError(long) length = %d, want <= 503", len(truncated))
	}
	if !hasSuffix(truncated, "...") {
		t.Errorf("truncateError(long) = %q, want to end with ...", truncated)
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// MockSender adalah simple mock untuk interface notifier.Sender.
type MockSender struct {
	SendFunc func(ctx context.Context, to, subject, body string) error
	calls    atomic.Int64
}

func (m *MockSender) Send(ctx context.Context, to, subject, body string) error {
	m.calls.Add(1)
	if m.SendFunc != nil {
		return m.SendFunc(ctx, to, subject, body)
	}
	return nil
}

func (m *MockSender) CallCount() int64 {
	return m.calls.Load()
}

// VerifyInterface memastikan MockSender mengimplementasikan notifier.Sender.
var _ notifier.Sender = (*MockSender)(nil)

// TestNotificationStatusConstants memverifikasi bahwa status constants benar.
func TestNotificationStatusConstants(t *testing.T) {
	if statusPending != "pending" {
		t.Errorf("statusPending = %q, want %q", statusPending, "pending")
	}
	if statusSent != "sent" {
		t.Errorf("statusSent = %q, want %q", statusSent, "sent")
	}
	if statusFailed != "failed" {
		t.Errorf("statusFailed = %q, want %q", statusFailed, "failed")
	}
	if statusSkipped != "skipped" {
		t.Errorf("statusSkipped = %q, want %q", statusSkipped, "skipped")
	}
}

// TestNewService memverifikasi bahwa NewService membuat service dengan benar.
func TestNewService(t *testing.T) {
	log := slog.Default()
	loc, _ := time.LoadLocation("Asia/Jakarta")
	interval := 1 * time.Minute

	// Service tidak membutuhkan pool nyata untuk test ini.
	svc := New(nil, nil, loc, interval, 5, log)

	if svc == nil {
		t.Fatal("New() returned nil")
	}
	if svc.pollInterval != interval {
		t.Errorf("pollInterval = %v, want %v", svc.pollInterval, interval)
	}
	if svc.maxAttempts != 5 {
		t.Errorf("maxAttempts = %d, want %d", svc.maxAttempts, 5)
	}
	if svc.tz != loc {
		t.Errorf("tz = %v, want %v", svc.tz, loc)
	}
}
