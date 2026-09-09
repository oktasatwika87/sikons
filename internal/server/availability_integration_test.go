package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// seninKe mengembalikan tengah malam Senin ke-n dari sekarang, di zona
// kampus. n=1 adalah Senin berikutnya yang belum lewat.
func seninKe(t *testing.T, n int) time.Time {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatal("gagal load zona kampus:", err)
	}
	sekarang := time.Now().In(campusTZ)
	// weekday Senin = 1, Minggu = 0
	hariIni := int(sekarang.Weekday()) // 0=Minggu, 1=Senin, ..., 6=Sabtu
	var hariKeSenin int
	if hariIni == 0 {
		hariKeSenin = 1 // hari ini Minggu -> Senin besok
	} else {
		hariKeSenin = 8 - hariIni // Senin depan (8 - hariIni karena Sen=1, Sel=2, ...)
	}
	// Hari ini sengaja dilewati supaya slot pagi yang sudah lewat tidak
	// mengacaukan hitungan.
	mingguPertama := sekarang.AddDate(0, 0, hariKeSenin)
	mingguN := mingguPertama.AddDate(0, 0, 7*(n-1))
	return time.Date(mingguN.Year(), mingguN.Month(), mingguN.Day(), 0, 0, 0, 0, campusTZ)
}

// Helper untuk login sebagai dosen
func loginDosen(t *testing.T, h http.Handler) (accessToken string, lecturerID string) {
	t.Helper()
	email := emailUnik("dosen")
	password := "password-yang-cukup-panjang"

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email":     email,
		"password":  password,
		"full_name": "Dosen Test",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %s", rec.Body.String())
	}

	// Promosikan ke lecturer via SQL
	pool := dbUji(t)
	var userID string
	if err := pool.QueryRow(context.Background(), `SELECT id::text FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
		t.Fatalf("ambil userID: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE users SET role = 'lecturer' WHERE id = $1`, userID); err != nil {
		t.Fatalf("promosi ke lecturer: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO lecturer_profiles (user_id, department) VALUES ($1, 'Informatika')`, userID); err != nil {
		t.Fatalf("insert lecturer_profile: %v", err)
	}

	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %s", rec.Body.String())
	}

	var hasil struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hasil)
	return hasil.AccessToken, userID
}

func TestAvailabilityRule_DosenBikinAturan(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1, // Senin
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    "2026-09-14",
	}, token)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create rule: status=%d body=%s", rec.Code, rec.Body.String())
	}

	var hasil struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hasil)
	if hasil.ID == "" {
		t.Error("ID aturan kosong")
	}
}

func TestAvailabilityRule_AturanBentrok(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)
	pool := dbUji(t)

	// Buat aturan pertama
	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1, // Senin
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    "2026-09-14",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create first rule: %s", rec.Body.String())
	}

	// Ambil ID aturan pertama
	var hasil1 struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hasil1)

	// Aturan kedua: hari sama, jam beririsan, periode beririsan
	rec = kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1, // Senin
		"start_time":        "10:00",
		"end_time":          "14:00",
		"slot_duration_min": 30,
		"effective_from":    "2026-09-14",
		"effective_to":      "2026-12-31",
	}, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("overlapping rule: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "ATURAN_BENTROK" {
		t.Errorf("code=%s, mau ATURAN_BENTROK", kodeError(t, rec))
	}

	// Verifikasi aturan pertama masih ada
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM availability_rules WHERE id = $1 AND is_active = true`,
		hasil1.ID).Scan(&count); err != nil {
		t.Fatalf("hitung aturan: %v", err)
	}
	if count != 1 {
		t.Errorf("aturan pertama seharusnya masih aktif")
	}
}

func TestAvailabilityRule_AturanTidakBentrok(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	// Buat aturan pertama: efektif sampai 2026-09-30
	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    "2026-09-14",
		"effective_to":      "2026-09-30",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create first rule: %s", rec.Body.String())
	}

	// Aturan kedua: hari sama, jam sama, tapi periode TIDAK beririsan (setelah 2026-09-30)
	rec = kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    "2026-10-01",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("non-overlapping rule: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAvailabilityRule_JendelaTerlaluKecil(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "09:20",
		"slot_duration_min": 30,
		"effective_from":    "2026-09-14",
	}, token)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("small window: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "VALIDATION_ERROR" {
		t.Errorf("code=%s, mau VALIDATION_ERROR", kodeError(t, rec))
	}
}

func TestAvailabilityRule_MahasiswaTidakBisa(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginLengkap(t, h) // student

	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    "2026-09-14",
	}, token)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("student create rule: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAvailabilityRule_DosenLainTidakBisaAkses(t *testing.T) {
	h := serverLengkap(t)
	token1, _ := loginDosen(t, h)
	token2, _ := loginDosen(t, h)

	// Dosen 1 buat aturan
	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    "2026-09-14",
	}, token1)
	if rec.Code != http.StatusCreated {
		t.Fatalf("dosen 1 create: %s", rec.Body.String())
	}

	var hasil struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hasil)

	// Dosen 2 coba akses aturan dosen 1 via PATCH -> harus 404
	rec = kirim(t, h, http.MethodPatch, "/api/v1/availability-rules/"+hasil.ID, map[string]any{
		"effective_to": "2026-12-31",
	}, token2)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("dosen 2 patch aturan dosen 1: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Dosen 2 coba DELETE aturan dosen 1 -> harus 404
	rec = kirim(t, h, http.MethodDelete, "/api/v1/availability-rules/"+hasil.ID, nil, token2)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("dosen 2 delete aturan dosen 1: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// List aturan dosen 2 tidak boleh mengandung aturan dosen 1
	rec = kirim(t, h, http.MethodGet, "/api/v1/availability-rules", nil, token2)
	if rec.Code != http.StatusOK {
		t.Fatalf("dosen 2 list: %s", rec.Body.String())
	}
	var daftar struct {
		Rules []struct{ ID string } `json:"rules"`
	}
	json.Unmarshal(rec.Body.Bytes(), &daftar)
	for _, r := range daftar.Rules {
		if r.ID == hasil.ID {
			t.Error("dosen 2 tidak boleh melihat aturan dosen 1")
		}
	}
}

func TestAvailabilityRule_PatchTidakBolehUbahJam(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	// Buat aturan
	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    "2026-09-14",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %s", rec.Body.String())
	}

	var hasil struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hasil)

	// Coba PATCH dengan start_time -> harus ditolak
	rec = kirim(t, h, http.MethodPatch, "/api/v1/availability-rules/"+hasil.ID, map[string]any{
		"start_time": "10:00",
	}, token)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("patch with start_time: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAvailabilityRule_PatchEffectiveTo(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    "2026-09-14",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %s", rec.Body.String())
	}

	var hasil struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hasil)

	// PATCH effective_to
	rec = kirim(t, h, http.MethodPatch, "/api/v1/availability-rules/"+hasil.ID, map[string]any{
		"effective_to": "2026-12-31",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch effective_to: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = kirim(t, h, http.MethodGet, "/api/v1/availability-rules", nil, token)
	var daftar struct {
		Rules []struct {
			ID          string  `json:"id"`
			EffectiveTo *string `json:"effective_to"`
		} `json:"rules"`
	}
	json.Unmarshal(rec.Body.Bytes(), &daftar)
	for _, r := range daftar.Rules {
		if r.ID == hasil.ID {
			if r.EffectiveTo == nil || *r.EffectiveTo != "2026-12-31" {
				t.Errorf("effective_to tidak berubah: %v", r.EffectiveTo)
			}
		}
	}
}

func TestAvailabilityRule_DeleteSoftDelete(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)
	pool := dbUji(t)

	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    "2026-09-14",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %s", rec.Body.String())
	}

	var hasil struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hasil)

	// DELETE
	rec = kirim(t, h, http.MethodDelete, "/api/v1/availability-rules/"+hasil.ID, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Verifikasi is_active=false (soft delete)
	var isActive bool
	if err := pool.QueryRow(context.Background(), `
		SELECT is_active FROM availability_rules WHERE id = $1`, hasil.ID).Scan(&isActive); err != nil {
		t.Fatalf("ambil is_active: %v", err)
	}
	if isActive {
		t.Error("aturan seharusnya di-soft delete (is_active=false)")
	}

	// Tapi barisnya masih ada
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM availability_rules WHERE id = $1`, hasil.ID).Scan(&count); err != nil {
		t.Fatalf("hitung aturan: %v", err)
	}
	if count != 1 {
		t.Error("baris seharusnya masih ada di database")
	}
}

// =============================================================================
// Availability Exceptions
// =============================================================================

func TestAvailabilityException_CreateFullDay(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-exceptions", map[string]any{
		"exception_date": "2026-09-15",
		"reason":         "Libur Nasional",
		"is_full_day":    true,
	}, token)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create full day exception: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAvailabilityException_CreatePartialDay(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-exceptions", map[string]any{
		"exception_date": "2026-09-15",
		"reason":         "Rapat Senat",
		"is_full_day":    false,
		"start_time":     "09:00",
		"end_time":       "12:00",
	}, token)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create partial exception: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAvailabilityException_PartialWithoutTime(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	// is_full_day=false tapi tanpa start_time
	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-exceptions", map[string]any{
		"exception_date": "2026-09-15",
		"reason":         "Rapat",
		"is_full_day":    false,
	}, token)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("partial without time: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAvailabilityException_ListWithFilter(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	// Buat exception
	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-exceptions", map[string]any{
		"exception_date": "2026-09-15",
		"reason":         "Libur",
		"is_full_day":    true,
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %s", rec.Body.String())
	}

	// List dengan filter
	rec = kirim(t, h, http.MethodGet, "/api/v1/availability-exceptions?from=2026-09-01&to=2026-09-30", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %s", rec.Body.String())
	}

	var hasil struct {
		Exceptions []struct{ ID string } `json:"exceptions"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hasil)
	if len(hasil.Exceptions) == 0 {
		t.Error("seharusnya ada 1 exception")
	}
}

func TestAvailabilityException_Delete(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	// Buat exception
	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-exceptions", map[string]any{
		"exception_date": "2026-09-15",
		"reason":         "Libur",
		"is_full_day":    true,
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %s", rec.Body.String())
	}

	var hasil struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hasil)

	// DELETE
	rec = kirim(t, h, http.MethodDelete, "/api/v1/availability-exceptions/"+hasil.ID, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Test slot generation via HTTP handler (Reconcile wired)
// =============================================================================

// TestAvailabilityRule_AturanBaruLangsungBikinSlot memverifikasi bahwa POST aturan
// memicu Reconcile, dan slot yang dibuat benar-benar ada di tabel slots.
// Jika Reconcile dimatikan, test ini akan gagal karena slots.created > 0 tapi
// tabel slots kosong.
func TestAvailabilityRule_AturanBaruLangsungBikinSlot(t *testing.T) {
	h := serverLengkap(t)
	token, lecturerID := loginDosen(t, h)
	pool := dbUji(t)

	m1 := seninKe(t, 1)
	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    m1.Format("2006-01-02"),
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create rule: %s", rec.Body.String())
	}

	// Decode response — periksa error Unmarshal
	var hasil struct {
		ID    string `json:"id"`
		Slots struct {
			Created int `json:"created"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &hasil); err != nil {
		t.Fatalf("response bukan JSON valid: %v", err)
	}
	if hasil.ID == "" {
		t.Fatal("ID aturan kosong")
	}
	if hasil.Slots.Created == 0 {
		t.Fatal("slots.created == 0, tapi rule baru harusnya bikin slot")
	}

	// Verifikasi: hitung langsung dari tabel slots
	var countDB int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM slots
		WHERE lecturer_id = $1::uuid
		  AND status = 'open'
		  AND withdrawn_at IS NULL`,
		lecturerID).Scan(&countDB); err != nil {
		t.Fatalf("hitung slot: %v", err)
	}

	if countDB != hasil.Slots.Created {
		t.Errorf("slots di tabel = %d, tapi slots.created = %d", countDB, hasil.Slots.Created)
	}
}

// TestAvailabilityRule_NonaktifkanAturanMenghapusSlot memverifikasi bahwa DELETE aturan
// (soft delete) memicu Reconcile dan slot yang terkait dihapus.
// Jika Reconcile dimatikan, slot tidak akan dihapus dan test gagal.
func TestAvailabilityRule_NonaktifkanAturanMenghapusSlot(t *testing.T) {
	h := serverLengkap(t)
	token, lecturerID := loginDosen(t, h)
	pool := dbUji(t)

	m1 := seninKe(t, 1)
	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    m1.Format("2006-01-02"),
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %s", rec.Body.String())
	}

	var hasil struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hasil)

	// Hitung slot open sebelum delete
	var countSebelum int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM slots
		WHERE lecturer_id = $1::uuid AND status = 'open' AND withdrawn_at IS NULL`,
		lecturerID).Scan(&countSebelum); err != nil {
		t.Fatalf("hitung slot: %v", err)
	}
	if countSebelum == 0 {
		t.Fatal("seharusnya ada slot setelah buat aturan")
	}

	// Soft delete aturan
	rec = kirim(t, h, http.MethodDelete, "/api/v1/availability-rules/"+hasil.ID, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %s", rec.Body.String())
	}

	var delResult struct {
		Slots struct {
			Deleted int `json:"deleted"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &delResult); err != nil {
		t.Fatalf("response bukan JSON valid: %v", err)
	}
	if delResult.Slots.Deleted == 0 {
		t.Fatal("slots.deleted == 0, soft delete aturan harusnya hapus slot terkait")
	}

	// Verifikasi: hitung langsung dari tabel
	var countSesudah int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM slots
		WHERE lecturer_id = $1::uuid AND status = 'open' AND withdrawn_at IS NULL`,
		lecturerID).Scan(&countSesudah); err != nil {
		t.Fatalf("hitung slot: %v", err)
	}

	if countSesudah != 0 {
		t.Errorf("slot open tersisa = %d, mau 0 setelah aturan dinonaktifkan", countSesudah)
	}
}

// TestAvailabilityRule_EffectiveToMemotongSlot memverifikasi bahwa PATCH effective_to
// pada aturan memicu Reconcile dan slot setelah batas itu dihapus.
// Perbandingan tanggal pakai zona kampus (Asia/Jakarta), bukan string.
func TestAvailabilityRule_EffectiveToMemotongSlot(t *testing.T) {
	h := serverLengkap(t)
	token, lecturerID := loginDosen(t, h)
	pool := dbUji(t)

	m1 := seninKe(t, 1) // Senin pertama
	m2 := seninKe(t, 2) // Senin kedua

	// Buat aturan tanpa effective_to (berlaku indefinite) dari Senin pertama
	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    m1.Format("2006-01-02"),
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %s", rec.Body.String())
	}

	var hasil struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hasil)

	// PATCH effective_to ke hari Rabu setelah Senin pertama (m1 + 2 hari).
	// Ini memastikan kita bisa memverifikasi slot di Senin kedua (m2)
	// benar-benar dihapus, bukan sekadar tidak pernah di-generate.
	batasDate := m1.AddDate(0, 0, 2) // Rabu setelah Senin pertama
	rec = kirim(t, h, http.MethodPatch, "/api/v1/availability-rules/"+hasil.ID, map[string]any{
		"effective_to": batasDate.Format("2006-01-02"),
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %s", rec.Body.String())
	}

	var patchResult struct {
		Slots struct {
			Deleted int `json:"deleted"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &patchResult); err != nil {
		t.Fatalf("response bukan JSON valid: %v", err)
	}
	if patchResult.Slots.Deleted == 0 {
		t.Fatal("slots.deleted == 0, effective_to baru harusnya hapus slot setelah batas")
	}

	// Senin kedua (m2) harus TIDAK ada (dibuktikan dihapus oleh reconcile).
	var countSeninBerikutnya int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM slots
		WHERE lecturer_id = $1::uuid
		  AND status = 'open'
		  AND withdrawn_at IS NULL
		  AND start_at >= $2
		  AND start_at < $3`,
		lecturerID, m2, m2.AddDate(0, 0, 1)).Scan(&countSeninBerikutnya); err != nil {
		t.Fatalf("hitung slot Senin berikutnya: %v", err)
	}

	if countSeninBerikutnya != 0 {
		t.Errorf("slot open di %s (Senin setelah batas) = %d, mau 0",
			m2.Format("2006-01-02"), countSeninBerikutnya)
	}

	// Semua slot yang ada harus <= batasDate (tidak ada yang setelah batas).
	var countSetelahBatas int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM slots
		WHERE lecturer_id = $1::uuid
		  AND status = 'open'
		  AND withdrawn_at IS NULL
		  AND start_at > $2`,
		lecturerID, batasDate).Scan(&countSetelahBatas); err != nil {
		t.Fatalf("hitung slot setelah batas: %v", err)
	}

	if countSetelahBatas != 0 {
		t.Errorf("slot open setelah batas (start_at > %s) = %d, mau 0", batasDate, countSetelahBatas)
	}

	// Slot di Senin pertama (dalam periode) harus masih ada.
	var countDalamPeriode int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM slots
		WHERE lecturer_id = $1::uuid
		  AND status = 'open'
		  AND withdrawn_at IS NULL
		  AND start_at >= $2
		  AND start_at < $3`,
		lecturerID, m1, m1.AddDate(0, 0, 1)).Scan(&countDalamPeriode); err != nil {
		t.Fatalf("hitung slot dalam periode: %v", err)
	}

	if countDalamPeriode == 0 {
		t.Errorf("slot open di tanggal %s (dalam periode) = 0, mau > 0", m1.Format("2006-01-02"))
	}
}

// TestAvailabilityException_FullDayMenghapusSlotSeharian memverifikasi bahwa POST
// pengecualian full day memicu Reconcile dan menghapus slot di tanggal itu.
// DELETE pengecualian merestore slot kembali.
func TestAvailabilityException_FullDayMenghapusSlotSeharian(t *testing.T) {
	h := serverLengkap(t)
	token, lecturerID := loginDosen(t, h)
	pool := dbUji(t)

	m1 := seninKe(t, 1)

	// Buat aturan untuk Senin pertama
	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       1,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    m1.Format("2006-01-02"),
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create rule: %s", rec.Body.String())
	}

	// Hitung slot di Senin pertama sebelum exception
	var countSebelum int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM slots
		WHERE lecturer_id = $1::uuid
		  AND status = 'open'
		  AND withdrawn_at IS NULL
		  AND start_at >= $2
		  AND start_at < $3`,
		lecturerID, m1, m1.AddDate(0, 0, 1)).Scan(&countSebelum); err != nil {
		t.Fatalf("hitung slot: %v", err)
	}
	if countSebelum == 0 {
		t.Fatal("seharusnya ada slot di Senin pertama sebelum exception")
	}

	// POST pengecualian full day di Senin pertama
	rec = kirim(t, h, http.MethodPost, "/api/v1/availability-exceptions", map[string]any{
		"exception_date": m1.Format("2006-01-02"),
		"reason":         "Libur Nasional",
		"is_full_day":    true,
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create exception: %s", rec.Body.String())
	}

	var exc struct {
		ID    string `json:"id"`
		Slots struct {
			Deleted int `json:"deleted"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &exc); err != nil {
		t.Fatalf("response bukan JSON valid: %v", err)
	}
	if exc.Slots.Deleted == 0 {
		t.Fatal("slots.deleted == 0, exception full day harusnya hapus slot di tanggal itu")
	}

	// Verifikasi: tidak ada slot open di Senin pertama
	var countSesudah int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM slots
		WHERE lecturer_id = $1::uuid
		  AND status = 'open'
		  AND withdrawn_at IS NULL
		  AND start_at >= $2
		  AND start_at < $3`,
		lecturerID, m1, m1.AddDate(0, 0, 1)).Scan(&countSesudah); err != nil {
		t.Fatalf("hitung slot: %v", err)
	}

	if countSesudah != 0 {
		t.Errorf("slot open di %s setelah exception = %d, mau 0", m1.Format("2006-01-02"), countSesudah)
	}

	// DELETE pengecualian -> slot harus muncul kembali
	rec = kirim(t, h, http.MethodDelete, "/api/v1/availability-exceptions/"+exc.ID, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete exception: %s", rec.Body.String())
	}

	var delExc struct {
		Slots struct {
			Created int `json:"created"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &delExc); err != nil {
		t.Fatalf("response bukan JSON valid: %v", err)
	}
	if delExc.Slots.Created == 0 {
		t.Fatal("slots.created == 0 setelah delete exception, slot harus muncul kembali")
	}

	// Verifikasi: slot di Senin pertama muncul lagi
	var countRestore int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM slots
		WHERE lecturer_id = $1::uuid
		  AND status = 'open'
		  AND withdrawn_at IS NULL
		  AND start_at >= $2
		  AND start_at < $3`,
		lecturerID, m1, m1.AddDate(0, 0, 1)).Scan(&countRestore); err != nil {
		t.Fatalf("hitung slot: %v", err)
	}

	if countRestore == 0 {
		t.Errorf("slot open di %s setelah delete exception = 0, mau > 0", m1.Format("2006-01-02"))
	}
}
