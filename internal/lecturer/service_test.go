package lecturer

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Test di file ini butuh Postgres sungguhan karena menguji perilaku query.
var poolUji *pgxpool.Pool

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		fmt.Println("TEST_DATABASE_URL kosong — test lecturer dilewati")
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Println("tidak bisa terhubung ke database uji:", err)
		os.Exit(1)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Println("database uji tidak merespons:", err)
		os.Exit(1)
	}
	poolUji = pool

	kode := m.Run()
	poolUji.Close()
	os.Exit(kode)
}

// ---------------------------------------------------------------- helpers

func buatDosenUji(t *testing.T) string {
	t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('dsn-' || gen_random_uuid()::text || '@uji.local', 'x', 'Dosen Uji', 'lecturer')
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

func seedSlotFixed(t *testing.T, lecturerID string, startAt, endAt time.Time) string {
	t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO slots (lecturer_id, start_at, end_at, status)
		VALUES ($1::uuid, $2, $3, 'open')
		RETURNING id::text`, lecturerID, startAt, endAt).Scan(&id)
	if err != nil {
		t.Fatalf("seed slot: %v", err)
	}
	return id
}

func hapusSlot(t *testing.T, slotID string) {
	t.Helper()
	poolUji.Exec(context.Background(), `DELETE FROM slots WHERE id = $1::uuid`, slotID)
}

// TestGetSlots_SlotStartsInRangeButEndsAfterTo memastikan filter berdasarkan
// start_at saja (< $3), bukan end_at (<= $3). Slot yang mulai dalam rentang
// tapi selesai setelah `to` tetap harus muncul.
func TestGetSlots_SlotStartsInRangeButEndsAfterTo(t *testing.T) {
	svc := NewService(poolUji)

	dsn := buatDosenUji(t)
	ctx := context.Background()

	// Slot: 09:00-10:00
	// Query rentang: 00:00 - 09:30
	// Filter lama (end_at <= $3) MENOLAK slot ini karena 10:00 > 09:30.
	// Filter baru (start_at < $3) MENERIMA slot ini karena 09:00 < 09:30.
	slotStart := time.Date(2025, 7, 15, 9, 0, 0, 0, time.UTC)
	slotEnd := time.Date(2025, 7, 15, 10, 0, 0, 0, time.UTC)
	from := time.Date(2025, 7, 15, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 7, 15, 9, 30, 0, 0, time.UTC)

	slotID := seedSlotFixed(t, dsn, slotStart, slotEnd)
	defer hapusSlot(t, slotID)

	got, err := svc.GetSlots(ctx, dsn, from, to)
	if err != nil {
		t.Fatalf("GetSlots error: %v", err)
	}

	found := false
	for _, s := range got {
		if s.ID == slotID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("slot (start=%v, end=%v) harus muncul karena start < to=%v", slotStart, slotEnd, to)
	}
}
