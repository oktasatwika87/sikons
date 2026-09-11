package booking

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oktasatwika87/sikons/internal/slotgen"
)

// Test di file ini adalah INTEGRATION TEST: butuh Postgres sungguhan.
//
// Kenapa tidak dipalsukan? Karena yang sedang diuji BUKAN kode Go-nya,
// melainkan perilaku penguncian baris Postgres. Database palsu akan selalu
// meluluskan test ini sekaligus tidak membuktikan apa pun.
//
// Jalankan dengan:  make test-integration
// Kalau TEST_DATABASE_URL kosong, seluruh file ini dilewati — supaya
// `go test ./...` biasa tetap cepat dan tidak menuntut Docker hidup.

var poolUji *pgxpool.Pool

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		fmt.Println("TEST_DATABASE_URL kosong — test integrasi booking dilewati")
		os.Exit(0)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		fmt.Println("TEST_DATABASE_URL tidak valid:", err)
		os.Exit(1)
	}
	// Sengaja dinaikkan dari default. Dengan pool kekecilan, 100 goroutine akan
	// mengantre di pool DULU sebelum sempat berebut baris slot — testnya lulus,
	// tapi yang terbukti cuma antrean pool, bukan penguncian baris. Angka ini
	// harus cukup besar supaya perebutan benar-benar terjadi di Postgres.
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

// ---------------------------------------------------------------- helper

// Tidak ada TRUNCATE di file ini, dan itu disengaja.
//
// `go test ./...` menjalankan test antar-package SECARA PARALEL. Package lain
// yang memakai database uji yang sama akan menghapus data package ini di
// tengah jalan, dan gejalanya menyesatkan: "mahasiswa tidak ditemukan" pada
// mahasiswa yang jelas-jelas baru dibuat tiga baris sebelumnya.
//
// Isolasi yang benar bukan membersihkan meja sebelum mulai, melainkan memakai
// meja sendiri: setiap test membuat data dengan pengenal yang unik, dan setiap
// pemeriksaan disaring berdasarkan pengenal itu. Dengan begitu test ini aman
// dijalankan berbarengan dengan apa pun.
var penghitungUnik atomic.Int64

func unik() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), penghitungUnik.Add(1))
}

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

func buatMahasiswa(t *testing.T, jumlah int) []string {
	t.Helper()
	rows, err := poolUji.Query(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		SELECT 'mhs-' || $2 || '-' || i || '@uji.local', 'x', 'Mahasiswa ' || i, 'student'
		FROM generate_series(1, $1) i
		RETURNING id::text`, jumlah, unik())
	if err != nil {
		t.Fatalf("membuat mahasiswa: %v", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("membaca id mahasiswa: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("membaca id mahasiswa: %v", err)
	}
	return ids
}

// buatSlot membuat slot yang mulai `mulaiDalam` dari sekarang.
func buatSlot(t *testing.T, dosenID string, mulaiDalam time.Duration) string {
	t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO slots (lecturer_id, start_at, end_at)
		VALUES ($1::uuid, now() + $2::interval, now() + $2::interval + interval '30 minutes')
		RETURNING id::text`,
		dosenID, fmt.Sprintf("%d seconds", int(mulaiDalam.Seconds()))).Scan(&id)
	if err != nil {
		t.Fatalf("membuat slot: %v", err)
	}
	return id
}

// buatSlotDimulai membuat slot yang SUDAH dimulai sejak `yangLalu`.
// Ini untuk test Complete/NoShow yang butuh slot yang sudah dimulai.
func buatSlotDimulai(t *testing.T, dosenID string, yangLalu time.Duration) string {
	t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO slots (lecturer_id, start_at, end_at)
		VALUES ($1::uuid,
		        now() - $2::interval,
		        now() - $2::interval + interval '30 minutes')
		RETURNING id::text`,
		dosenID, fmt.Sprintf("%d seconds", int(yangLalu.Seconds()))).Scan(&id)
	if err != nil {
		t.Fatalf("membuat slot: %v", err)
	}
	return id
}

func hitungBookingAktif(t *testing.T, slotID string) int {
	t.Helper()
	var n int
	err := poolUji.QueryRow(context.Background(),
		`SELECT count(*) FROM bookings WHERE slot_id = $1::uuid AND status <> 'cancelled'`,
		slotID).Scan(&n)
	if err != nil {
		t.Fatalf("menghitung booking: %v", err)
	}
	return n
}

func statusSlot(t *testing.T, slotID string) string {
	t.Helper()
	var s string
	err := poolUji.QueryRow(context.Background(),
		`SELECT status::text FROM slots WHERE id = $1::uuid`, slotID).Scan(&s)
	if err != nil {
		t.Fatalf("membaca status slot: %v", err)
	}
	return s
}

// ---------------------------------------------------------------- test dasar

func TestCreate_SuksesSaatSlotMasihOpen(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	slot := buatSlot(t, dosen, 72*time.Hour)

	result, err := NewService(poolUji, 60, 3, 1).Create(context.Background(), CreateInput{
		SlotID: slot, StudentID: mhs[0], Topic: "Bimbingan skripsi",
	})
	if err != nil {
		t.Fatalf("mau sukses, dapat error: %v", err)
	}
	if result.Replay {
		t.Error("replay = true, mau false")
	}
	if result.Booking == nil {
		t.Fatal("booking = nil")
	}

	if result.Booking.Status != "confirmed" {
		t.Errorf("status = %q, mau confirmed", result.Booking.Status)
	}
	if result.Booking.Topic != "Bimbingan skripsi" {
		t.Errorf("topic = %q", result.Booking.Topic)
	}
	if got := statusSlot(t, slot); got != "booked" {
		t.Errorf("status slot = %q, mau booked", got)
	}
}

func TestCreate_SlotTidakAda(t *testing.T) {
	mhs := buatMahasiswa(t, 1)

	_, err := NewService(poolUji, 60, 3, 1).Create(context.Background(), CreateInput{
		SlotID:    "00000000-0000-0000-0000-000000000000",
		StudentID: mhs[0], Topic: "x",
	})
	if !errors.Is(err, ErrSlotNotFound) {
		t.Fatalf("err = %v, mau ErrSlotNotFound", err)
	}
}

func TestCreate_MembuatReminderNotification(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	// Slot 2 jam dari sekarang — jauh dari minLeadMinutes (60 menit)
	// supaya test fokus ke notification, bukan valdasi lead time.
	slot := buatSlot(t, dosen, 2*time.Hour)

	result, err := NewService(poolUji, 60, 3, 1).Create(context.Background(), CreateInput{
		SlotID: slot, StudentID: mhs[0], Topic: "Bimbingan",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verifikasi baris notification terbuat.
	var notifCount int
	err = poolUji.QueryRow(context.Background(), `
		SELECT count(*) FROM notifications
		WHERE booking_id = $1::uuid AND type = 'booking_reminder'`,
		result.Booking.ID).Scan(&notifCount)
	if err != nil {
		t.Fatalf("query notification: %v", err)
	}
	if notifCount != 1 {
		t.Fatalf("notification count = %d, want 1", notifCount)
	}

	// Verifikasi scheduled_at = slot_start - reminderLeadHours (dalam toleransi 10 detik).
	var scheduledAt time.Time
	var slotStartAt time.Time
	err = poolUji.QueryRow(context.Background(), `
		SELECT n.scheduled_at, s.start_at
		FROM notifications n
		JOIN bookings b ON b.id = n.booking_id
		JOIN slots s ON s.id = b.slot_id
		WHERE n.booking_id = $1::uuid AND n.type = 'booking_reminder'`,
		result.Booking.ID).Scan(&scheduledAt, &slotStartAt)
	if err != nil {
		t.Fatalf("query scheduled_at: %v", err)
	}

	// scheduled_at seharusnya = slot_start - 1 jam.
	expectedScheduled := slotStartAt.Add(-time.Hour)
	diff := scheduledAt.Sub(expectedScheduled)
	if diff < -10*time.Second || diff > 10*time.Second {
		t.Errorf("scheduled_at = %v, want ~%v (diff=%v)", scheduledAt, expectedScheduled, diff)
	}
}

func TestCreate_SlotSudahDipesanOrangLain(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 2)
	slot := buatSlot(t, dosen, 72*time.Hour)
	svc := NewService(poolUji, 60, 3, 1)

	_, err := svc.Create(context.Background(),
		CreateInput{SlotID: slot, StudentID: mhs[0], Topic: "duluan"})
	if err != nil {
		t.Fatalf("booking pertama gagal: %v", err)
	}

	_, err = svc.Create(context.Background(),
		CreateInput{SlotID: slot, StudentID: mhs[1], Topic: "telat"})
	if !errors.Is(err, ErrSlotAlreadyBooked) {
		t.Fatalf("err = %v, mau ErrSlotAlreadyBooked", err)
	}

	if n := hitungBookingAktif(t, slot); n != 1 {
		t.Errorf("booking aktif = %d, mau 1", n)
	}
}

func TestCreate_BatasTigaBookingAktif(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	for i := 0; i < DefaultMaxActiveBookings; i++ {
		slot := buatSlot(t, dosen, time.Duration(24+i)*time.Hour)
		_, err := svc.Create(context.Background(),
			CreateInput{SlotID: slot, StudentID: mhs[0], Topic: "x"})
		if err != nil {
			t.Fatalf("booking ke-%d gagal: %v", i+1, err)
		}
	}

	slot := buatSlot(t, dosen, 100*time.Hour)
	_, err := svc.Create(context.Background(),
		CreateInput{SlotID: slot, StudentID: mhs[0], Topic: "kelebihan"})
	if !errors.Is(err, ErrLimitReached) {
		t.Fatalf("err = %v, mau ErrLimitReached", err)
	}
}

// Booking yang jadwalnya sudah lewat tidak boleh ikut menahan jatah.
func TestCreate_BookingLampauTidakMenghabiskanJatah(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	// Tiga slot di masa lalu, diisi langsung lewat SQL karena Create wajar saja
	// menolak slot yang sudah lewat nanti di M3.
	for i := 1; i <= 3; i++ {
		slot := buatSlot(t, dosen, -time.Duration(i)*24*time.Hour)
		_, err := poolUji.Exec(context.Background(), `
			INSERT INTO bookings (slot_id, student_id, topic) VALUES ($1::uuid, $2::uuid, 'lampau')`,
			slot, mhs[0])
		if err != nil {
			t.Fatalf("menyiapkan booking lampau: %v", err)
		}
	}

	slot := buatSlot(t, dosen, 48*time.Hour)
	_, err := svc.Create(context.Background(),
		CreateInput{SlotID: slot, StudentID: mhs[0], Topic: "baru"})
	if err != nil {
		t.Fatalf("mau sukses, dapat: %v", err)
	}
}

// TestCreate_SlotDitarik tidak boleh dipesan.
func TestCreate_SlotDitarik(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	slot := buatSlot(t, dosen, 72*time.Hour)

	// Tarik slotnya
	_, err := poolUji.Exec(context.Background(),
		`UPDATE slots SET withdrawn_at = now() WHERE id = $1::uuid`, slot)
	if err != nil {
		t.Fatalf("menarik slot: %v", err)
	}

	// Pesan — harus gagal dengan ErrSlotWithdrawn
	_, err = svc.Create(context.Background(),
		CreateInput{SlotID: slot, StudentID: mhs[0], Topic: "coba"})
	if !errors.Is(err, ErrSlotWithdrawn) {
		t.Fatalf("err = %v, mau ErrSlotWithdrawn", err)
	}
}

// ---------------------------------------------------------------- concurrency

// INI TEST TERPENTING DI SELURUH REPO.
//
// 100 goroutine menembak slot yang SAMA pada saat yang bersamaan, masing-masing
// atas nama mahasiswa yang berbeda. Tepat satu harus menang.
//
// Perhatikan channel `mulai`: tanpa itu, goroutine ke-1 sudah selesai sebelum
// goroutine ke-100 sempat dibuat, dan testnya lulus tanpa pernah ada perebutan.
// Semua goroutine diblokir di `<-mulai`, lalu `close(mulai)` melepas semuanya
// dalam satu tarikan. Ini pola barrier standar di Go: menutup channel
// membangunkan SEMUA yang menunggu sekaligus, bukan satu per satu.
func TestCreate_SeratusRequestParalelHanyaSatuYangMenang(t *testing.T) {
	const jumlah = 100

	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, jumlah)
	slot := buatSlot(t, dosen, 72*time.Hour)
	svc := NewService(poolUji, 60, 3, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mulai := make(chan struct{})
	// Tiap goroutine menulis ke indeksnya SENDIRI, jadi tidak ada dua goroutine
	// yang menyentuh alamat memori yang sama — aman tanpa mutex. `go test -race`
	// yang akan membuktikan klaim ini, bukan keyakinan saya.
	hasil := make([]error, jumlah)
	ids := make([]string, jumlah)

	var wg sync.WaitGroup
	for i := 0; i < jumlah; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-mulai
			result, err := svc.Create(ctx, CreateInput{
				SlotID: slot, StudentID: mhs[i], Topic: "rebutan",
			})
			if err == nil && result != nil && result.Booking != nil {
				ids[i] = result.Booking.ID
			}
			hasil[i] = err
		}(i)
	}

	close(mulai)
	wg.Wait()

	var sukses, konflik int
	var lain []error
	var idPertama string
	for i, err := range hasil {
		switch {
		case err == nil:
			sukses++
			if idPertama == "" {
				idPertama = ids[i]
			}
		case errors.Is(err, ErrSlotAlreadyBooked):
			konflik++
		default:
			lain = append(lain, err)
		}
	}

	if sukses != 1 {
		t.Errorf("sukses = %d, mau tepat 1", sukses)
	}
	if konflik != jumlah-1 {
		t.Errorf("konflik = %d, mau %d", konflik, jumlah-1)
	}
	// Error jenis lain berarti ada yang salah dan sedang tersamar sebagai
	// "seolah-olah berhasil ditolak" — misalnya timeout pool atau deadlock.
	if len(lain) > 0 {
		t.Errorf("ada %d error di luar dugaan, contoh pertama: %v", len(lain), lain[0])
	}

	// Semua id harus sama.
	for i, id := range ids {
		if id != "" && id != idPertama {
			t.Errorf("booking id[%d] = %q, mau %q", i, id, idPertama)
		}
	}

	// Hitungan di memori bisa saja benar sementara databasenya kacau.
	// Kebenaran yang sesungguhnya ada di sini.
	if n := hitungBookingAktif(t, slot); n != 1 {
		t.Errorf("baris booking di database = %d, mau 1", n)
	}
	if s := statusSlot(t, slot); s != "booked" {
		t.Errorf("status slot = %q, mau booked", s)
	}
}

// Membuktikan langkah 1 (kunci baris mahasiswa) benar-benar bekerja.
//
// Satu mahasiswa menembak 10 slot BERBEDA sekaligus. Karena slotnya berbeda,
// kunci slot tidak menolong sama sekali — tidak ada dua transaksi yang berebut
// baris yang sama. Yang menahan batas 3 hanyalah kunci pada baris mahasiswa.
//
// Kalau `FOR NO KEY UPDATE` di langkah 1 dihapus, test ini akan menyimpan
// 8-10 booking alih-alih 3.
func TestCreate_SatuMahasiswaMenembakSepuluhSlotSekaligus(t *testing.T) {
	const jumlah = 10

	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	slots := make([]string, jumlah)
	for i := range slots {
		slots[i] = buatSlot(t, dosen, time.Duration(24+i)*time.Hour)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mulai := make(chan struct{})
	hasil := make([]error, jumlah)

	var wg sync.WaitGroup
	for i := 0; i < jumlah; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-mulai
			_, err := svc.Create(ctx, CreateInput{
				SlotID: slots[i], StudentID: mhs[0], Topic: "borong",
			})
			hasil[i] = err
		}(i)
	}

	close(mulai)
	wg.Wait()

	var sukses, kenaBatas int
	var lain []error
	for _, err := range hasil {
		switch {
		case err == nil:
			sukses++
		case errors.Is(err, ErrLimitReached):
			kenaBatas++
		default:
			lain = append(lain, err)
		}
	}

	if sukses != DefaultMaxActiveBookings {
		t.Errorf("sukses = %d, mau %d", sukses, DefaultMaxActiveBookings)
	}
	if kenaBatas != jumlah-DefaultMaxActiveBookings {
		t.Errorf("kena batas = %d, mau %d", kenaBatas, jumlah-DefaultMaxActiveBookings)
	}
	if len(lain) > 0 {
		t.Errorf("ada %d error di luar dugaan, contoh pertama: %v", len(lain), lain[0])
	}

	var tersimpan int
	if err := poolUji.QueryRow(context.Background(),
		`SELECT count(*) FROM bookings WHERE student_id = $1::uuid AND status = 'confirmed'`,
		mhs[0]).Scan(&tersimpan); err != nil {
		t.Fatalf("menghitung booking: %v", err)
	}
	if tersimpan != DefaultMaxActiveBookings {
		t.Errorf("booking tersimpan = %d, mau %d — batas jebol", tersimpan, DefaultMaxActiveBookings)
	}
}

// ---------------------------------------------------------------- idempotency

func TestCreate_IdempotencyKeyKlaimBerhasil(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	slot := buatSlot(t, dosen, 72*time.Hour)
	svc := NewService(poolUji, 60, 3, 1)

	key := "test-key-" + unik()
	hash := ComputeRequestHash(slot, "Topik", "")

	// Request pertama: klaim berhasil, booking dibuat.
	r1, err1 := svc.Create(context.Background(), CreateInput{
		SlotID: slot, StudentID: mhs[0], Topic: "Topik",
		IdempotencyKey: key, RequestHash: hash,
	})
	if err1 != nil {
		t.Fatalf("request pertama gagal: %v", err1)
	}
	if r1.Replay {
		t.Error("r1.Replay = true, mau false")
	}
	if r1.Booking == nil {
		t.Fatal("r1.Booking = nil")
	}

	// Request kedua: key sama, hash sama -> replay.
	r2, err2 := svc.Create(context.Background(), CreateInput{
		SlotID: slot, StudentID: mhs[0], Topic: "Topik",
		IdempotencyKey: key, RequestHash: hash,
	})
	if err2 != nil {
		t.Fatalf("request kedua gagal: %v", err2)
	}
	if !r2.Replay {
		t.Error("r2.Replay = false, mau true")
	}
	if !bytesContains(r2.Body, []byte(r1.Booking.ID)) {
		t.Errorf("body replay tidak berisi id booking pertama: %s", r2.Body)
	}

	// Request ketiga: key sama, hash berbeda -> ErrIdempotencyReused.
	hash2 := ComputeRequestHash(slot, "Topik Lain", "")
	_, err3 := svc.Create(context.Background(), CreateInput{
		SlotID: slot, StudentID: mhs[0], Topic: "Topik Lain",
		IdempotencyKey: key, RequestHash: hash2,
	})
	if !errors.Is(err3, ErrIdempotencyReused) {
		t.Fatalf("err3 = %v, mau ErrIdempotencyReused", err3)
	}

	// Hanya ada 1 booking.
	if n := hitungBookingAktif(t, slot); n != 1 {
		t.Errorf("booking = %d, mau 1", n)
	}
}

// bytesContains adalah pembantu kecil — tanpa strings.Contains supaya test ini
// tidak bergantung pada modul yang lebih berat.
func bytesContains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- test cancel

func TestCancel_Success(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	// Slot mulai 5 jam dari sekarang — lebih dari H-3.
	slot := buatSlot(t, dosen, 5*time.Hour)
	b, err := svc.Create(context.Background(), CreateInput{
		SlotID: slot, StudentID: mhs[0], Topic: "Test cancel",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	view, err := svc.Cancel(context.Background(), b.Booking.ID, mhs[0], "Tidak jadi")
	if err != nil {
		t.Fatalf("cancel gagal: %v", err)
	}
	if view.Status != "cancelled" {
		t.Errorf("status = %q, mau cancelled", view.Status)
	}
	if s := statusSlot(t, slot); s != "open" {
		t.Errorf("status slot = %q, mau open", s)
	}
}

func TestCancel_TerlaluDekatH3Jam(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	// Slot mulai 2 jam dari sekarang — kurang dari H-3.
	slot := buatSlot(t, dosen, 2*time.Hour)
	b, err := svc.Create(context.Background(), CreateInput{
		SlotID: slot, StudentID: mhs[0], Topic: "Terlalu dekat",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	_, err = svc.Cancel(context.Background(), b.Booking.ID, mhs[0], "")
	if !errors.Is(err, ErrCancelTooLate) {
		t.Fatalf("err = %v, mau ErrCancelTooLate", err)
	}
	// Slot tidak berubah.
	if s := statusSlot(t, slot); s != "booked" {
		t.Errorf("status slot = %q, mau booked", s)
	}
	if n := hitungBookingAktif(t, slot); n != 1 {
		t.Errorf("booking aktif = %d, mau 1", n)
	}
}

func TestCancel_SudahCancelled(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	slot := buatSlot(t, dosen, 5*time.Hour)
	b, err := svc.Create(context.Background(), CreateInput{
		SlotID: slot, StudentID: mhs[0], Topic: "Dua kali cancel",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	_, err = svc.Cancel(context.Background(), b.Booking.ID, mhs[0], "")
	if err != nil {
		t.Fatalf("cancel pertama gagal: %v", err)
	}

	_, err = svc.Cancel(context.Background(), b.Booking.ID, mhs[0], "")
	if !errors.Is(err, ErrBookingAlreadyCancelled) {
		t.Fatalf("err = %v, mau ErrBookingAlreadyCancelled", err)
	}
}

func TestCancel_Completed(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	// Buat slot yang sudah dimulai 30 menit lalu.
	slot := buatSlotDimulai(t, dosen, 30*time.Minute)

	// Booking langsung via SQL karena Create tidak mau buat di slot yang sudah dimulai.
	var bookingID string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO bookings (slot_id, student_id, topic, status)
		VALUES ($1::uuid, $2::uuid, 'Test completed', 'confirmed')
		RETURNING id::text`, slot, mhs[0]).Scan(&bookingID)
	if err != nil {
		t.Fatalf("membuat booking: %v", err)
	}

	// Dosen complete dulu.
	_, err = svc.Complete(context.Background(), bookingID, dosen, "Selesai")
	if err != nil {
		t.Fatalf("complete gagal: %v", err)
	}

	// Mahasiswa coba cancel — harus gagal.
	_, err = svc.Cancel(context.Background(), bookingID, mhs[0], "")
	if !errors.Is(err, ErrBookingAlreadyFinalized) {
		t.Fatalf("err = %v, mau ErrBookingAlreadyFinalized", err)
	}
}

// ---------------------------------------------------------------- test complete

// helper buatBooking langsung INSERT booking tanpa melewati Create (yang punya
// constraint minLeadMinutes).
func buatBooking(t *testing.T, slotID, studentID, status string) string {
	t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO bookings (slot_id, student_id, topic, status)
		VALUES ($1::uuid, $2::uuid, 'Test', $3)
		RETURNING id::text`, slotID, studentID, status).Scan(&id)
	if err != nil {
		t.Fatalf("membuat booking: %v", err)
	}
	return id
}

func TestComplete_SebelumStartAt(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	// Slot di masa depan (belum dimulai) tapi booking sudah ada.
	slot := buatSlot(t, dosen, 5*time.Hour)
	bookingID := buatBooking(t, slot, mhs[0], "confirmed")

	_, err := svc.Complete(context.Background(), bookingID, dosen, "")
	if !errors.Is(err, ErrSessionNotStarted) {
		t.Fatalf("err = %v, mau ErrSessionNotStarted", err)
	}
}

func TestComplete_Success(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	// Slot di masa lalu (sudah dimulai).
	slot := buatSlotDimulai(t, dosen, 15*time.Minute)
	bookingID := buatBooking(t, slot, mhs[0], "confirmed")

	// Set slot ke booked (Complete/NoShow tidak mengubah slot).
	poolUji.Exec(context.Background(),
		`UPDATE slots SET status = 'booked' WHERE id = $1::uuid`, slot)

	view, err := svc.Complete(context.Background(), bookingID, dosen, "Topik terbahas semua")
	if err != nil {
		t.Fatalf("complete gagal: %v", err)
	}
	if view.Status != "completed" {
		t.Errorf("status = %q, mau completed", view.Status)
	}
	// Slot tetap 'booked' karena booking completed — tidak ada logika pengembalian.
	if s := statusSlot(t, slot); s != "booked" {
		t.Errorf("status slot = %q, mau booked", s)
	}
}

func TestComplete_SudahCompleted(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	slot := buatSlotDimulai(t, dosen, 30*time.Minute)
	bookingID := buatBooking(t, slot, mhs[0], "confirmed")

	_, err := svc.Complete(context.Background(), bookingID, dosen, "Pertama")
	if err != nil {
		t.Fatalf("complete pertama gagal: %v", err)
	}

	_, err = svc.Complete(context.Background(), bookingID, dosen, "Kedua")
	if !errors.Is(err, ErrBookingNotConfirmed) {
		t.Fatalf("err = %v, mau ErrBookingNotConfirmed", err)
	}
}

// ---------------------------------------------------------------- test no-show

func TestNoShow_SebelumStartAt(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	slot := buatSlot(t, dosen, 5*time.Hour)
	bookingID := buatBooking(t, slot, mhs[0], "confirmed")

	_, err := svc.NoShow(context.Background(), bookingID, dosen)
	if !errors.Is(err, ErrSessionNotStarted) {
		t.Fatalf("err = %v, mau ErrSessionNotStarted", err)
	}
}

func TestNoShow_Success(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	slot := buatSlotDimulai(t, dosen, 20*time.Minute)
	bookingID := buatBooking(t, slot, mhs[0], "confirmed")
	// Set slot ke booked (mirroring normal flow).
	poolUji.Exec(context.Background(),
		`UPDATE slots SET status = 'booked' WHERE id = $1::uuid`, slot)

	view, err := svc.NoShow(context.Background(), bookingID, dosen)
	if err != nil {
		t.Fatalf("no-show gagal: %v", err)
	}
	if view.Status != "no_show" {
		t.Errorf("status = %q, mau no_show", view.Status)
	}
	// Slot tetap 'booked' karena NoShow tidak mengubah slot.
	if s := statusSlot(t, slot); s != "booked" {
		t.Errorf("status slot = %q, mau booked", s)
	}
}

// ---------------------------------------------------------------- test deadlock

// TestCancel_BarengReconcileTidakDeadlock membuktikan urutan kunci slot->booking
// di Cancel tidak menyebabkan deadlock dengan Reconcile.
//
// Mekanisme: dua goroutine dijalankan SERENTAN lewat barrier close(mulai),
// satu memanggil Cancel, satu memanggil Reconcile untuk slot yang SAMA.
// Context timeout pendek (10 detik) menangkap kalau deadlock benar-benar terjadi.
//
// Setiap iterasi membuat slot dan booking BARU. Ini penting karena Reconcile
// hanya mengunci baris bookings kalau ada booking berstatus confirmed di slot
// yang ditarik. Kalau booking sudah dicancel di iterasi sebelumnya, Reconcile
// tidak lagi menyentuh baris booking — race dua-lock yang mau dibuktikan tidak
// terjadi di iterasi selanjutnya. Dengan booking baru setiap iterasi, setiap
// pengulangan menguji kondisi race yang sama.
func TestCancel_BarengReconcileTidakDeadlock(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	// Jalankan 10 kali dengan slot dan booking berbeda setiap iterasi.
	for i := 0; i < 10; i++ {
		t.Logf("iterasi %d: membuat slot dan booking baru", i+1)

		// Buat slot dan booking baru untuk iterasi ini.
		slot := buatSlot(t, dosen, 5*time.Hour)
		b, err := svc.Create(context.Background(), CreateInput{
			SlotID: slot, StudentID: mhs[0], Topic: "Deadlock test",
		})
		if err != nil {
			t.Fatalf("booking gagal: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		mulai := make(chan struct{})

		// Goroutine A: Cancel.
		var errA error
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-mulai
			_, errA = svc.Cancel(ctx, b.Booking.ID, mhs[0], "")
		}()

		// Goroutine B: Reconcile.
		// Hapus aturan ketersediaan dulu supaya slot masuk ke branch withdrawn.
		poolUji.Exec(ctx, `DELETE FROM availability_rules WHERE lecturer_id = $1::uuid`, dosen)

		var errB error
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-mulai
			// Buat aturan baru yang TIDAK cover slot ini — agar Reconcile menariknya.
			_, err = poolUji.Exec(ctx, `
				INSERT INTO availability_rules (lecturer_id, day_of_week, start_time, end_time,
				                               slot_duration_min, effective_from)
				VALUES ($1::uuid, 9, '09:00', '10:00', 60, now()::date + 7)`,
				dosen)
			if err != nil {
				errB = err
				return
			}
			sg := slotgen.New(poolUji, time.Local, 30)
			testLog := slog.New(slog.NewTextHandler(io.Discard, nil))
			_, errB = sg.Reconcile(ctx, dosen, time.Now(), time.Now().Add(30*24*time.Hour), testLog)
		}()

		close(mulai)
		wg.Wait()
		cancel()

		// Tidak boleh ada error deadlock (40P01) dari Postgres.
		if errA != nil && isDeadlockError(errA) {
			t.Fatalf("iterasi %d: Cancel kena deadlock: %v", i, errA)
		}
		if errB != nil && isDeadlockError(errB) {
			t.Fatalf("iterasi %d: Reconcile kena deadlock: %v", i, errB)
		}
	}
}

func isDeadlockError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40P01"
	}
	return strings.Contains(err.Error(), "deadlock")
}

// ---------------------------------------------------------------- test authorization

// Mahasiswa lain (bukan pemilik) tidak boleh membatalkan booking orang lain.
func TestCancel_BukanPemilik(t *testing.T) {
	dosen := buatDosen(t)
	mhs := buatMahasiswa(t, 2)
	svc := NewService(poolUji, 60, 3, 1)

	slot := buatSlot(t, dosen, 5*time.Hour)
	b, err := svc.Create(context.Background(), CreateInput{
		SlotID: slot, StudentID: mhs[0], Topic: "Booking punya MHS-0",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	// MHS-1 coba cancel booking MHS-0 — harus 404.
	_, err = svc.Cancel(context.Background(), b.Booking.ID, mhs[1], "")
	if !errors.Is(err, ErrBookingNotFound) {
		t.Fatalf("err = %v, mau ErrBookingNotFound", err)
	}
}

// Dosen lain (bukan pemilik slot) tidak boleh menandai no-show booking.
func TestNoShow_DosenLain(t *testing.T) {
	dosen := buatDosen(t)
	dosen2 := buatDosen(t)
	mhs := buatMahasiswa(t, 1)
	svc := NewService(poolUji, 60, 3, 1)

	slot := buatSlotDimulai(t, dosen, 15*time.Minute)
	bookingID := buatBooking(t, slot, mhs[0], "confirmed")
	poolUji.Exec(context.Background(),
		`UPDATE slots SET status = 'booked' WHERE id = $1::uuid`, slot)

	// Dosen-2 (bukan pemilik slot) coba no-show — harus 404.
	_, err := svc.NoShow(context.Background(), bookingID, dosen2)
	if !errors.Is(err, ErrBookingNotFound) {
		t.Fatalf("err = %v, mau ErrBookingNotFound", err)
	}
}
