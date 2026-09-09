package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

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
	pool.QueryRow(context.Background(), `SELECT id::text FROM users WHERE email = $1`, email).Scan(&userID)
	pool.Exec(context.Background(), `UPDATE users SET role = 'lecturer' WHERE id = $1`, userID)
	pool.Exec(context.Background(), `INSERT INTO lecturer_profiles (user_id, department) VALUES ($1, 'Informatika')`, userID)

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
	pool.QueryRow(context.Background(), `
		SELECT count(*) FROM availability_rules WHERE id = $1 AND is_active = true`,
		hasil1.ID).Scan(&count)
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
	if rec.Code != http.StatusNoContent {
		t.Fatalf("patch effective_to: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Verifikasi
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
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Verifikasi is_active=false (soft delete)
	var isActive bool
	pool.QueryRow(context.Background(), `
		SELECT is_active FROM availability_rules WHERE id = $1`, hasil.ID).Scan(&isActive)
	if isActive {
		t.Error("aturan seharusnya di-soft delete (is_active=false)")
	}

	// Tapi barisnya masih ada
	var count int
	pool.QueryRow(context.Background(), `
		SELECT count(*) FROM availability_rules WHERE id = $1`, hasil.ID).Scan(&count)
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
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
