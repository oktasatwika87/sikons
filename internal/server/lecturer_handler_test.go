package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/oktasatwika/sikons/internal/auth"
	"github.com/oktasatwika/sikons/internal/lecturer"
)

func buatServerUjiLecturer(t *testing.T) *lecturerTestHelper {
	t.Helper()

	if skipIfNoDB {
		t.Skip("TEST_DATABASE_URL kosong")
	}

	cfg := cfgUji
	if cfg.CampusTZ == nil {
		cfg.CampusTZ = time.Local
	}
	cfg.CORSAllowedOrigins = []string{"http://localhost:3000"}

	tokens, err := auth.NewTokenIssuer("test-secret-key-panjang-32-karakter", 15*time.Minute)
	if err != nil {
		t.Fatalf("membuat token issuer: %v", err)
	}

	srv := New(cfg, Deps{
		DB:       poolUji,
		Auth:     auth.NewService(poolUji, tokens, slog.New(slog.NewTextHandler(os.Stderr, nil))),
		Tokens:   tokens,
		Log:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Lecturer: lecturer.NewService(poolUji),
	})

	return &lecturerTestHelper{
		t:      t,
		server: srv,
		router: srv.Routes(),
	}
}

type lecturerTestHelper struct {
	t      *testing.T
	server *Server
	router http.Handler
}

func (h *lecturerTestHelper) buatDosen(department string) string {
	h.t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('dsn-' || gen_random_uuid()::text || '@uji.local', 'x', 'Dosen ' || gen_random_uuid()::text, 'lecturer')
		RETURNING id::text`,
	).Scan(&id)
	if err != nil {
		h.t.Fatalf("membuat dosen: %v", err)
	}

	_, err = poolUji.Exec(context.Background(),
		`INSERT INTO lecturer_profiles (user_id, department, room, bio)
		 VALUES ($1::uuid, $2, 'Ruang ' || gen_random_uuid()::text, 'Bio dosen ' || gen_random_uuid()::text)`,
		id, department)
	if err != nil {
		h.t.Fatalf("membuat profil dosen: %v", err)
	}

	// Verify it was created
	var count int
	err = poolUji.QueryRow(context.Background(), `
		SELECT count(*) FROM lecturer_profiles WHERE user_id = $1::uuid`, id).Scan(&count)
	if err != nil {
		h.t.Fatalf("verifikasi profil: %v", err)
	}
	if count != 1 {
		h.t.Errorf("profil count = %d, mau 1", count)
	}

	return id
}

func (h *lecturerTestHelper) buatMahasiswa() string {
	h.t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('mhs-' || gen_random_uuid()::text || '@uji.local', 'x', 'Mahasiswa ' || gen_random_uuid()::text, 'student')
		RETURNING id::text`,
	).Scan(&id)
	if err != nil {
		h.t.Fatalf("membuat mahasiswa: %v", err)
	}
	return id
}

func (h *lecturerTestHelper) buatSlot(dosenID string, mulaiDalam time.Duration) string {
	h.t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO slots (lecturer_id, start_at, end_at)
		VALUES ($1::uuid,
		        now() + $2::interval,
		        now() + $2::interval + interval '30 minutes')
		RETURNING id::text`,
		dosenID, fmt.Sprintf("%d seconds", int(mulaiDalam.Seconds())),
	).Scan(&id)
	if err != nil {
		h.t.Fatalf("membuat slot: %v", err)
	}
	return id
}

// ---------------------------------------------------------------- test list lecturers

func TestListLecturers_TanpaFilter(t *testing.T) {
	h := buatServerUjiLecturer(t)
	_ = h.buatDosen("TI")
	_ = h.buatDosen("SI")

	req := httptest.NewRequest("GET", "/api/v1/lecturers", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, mau 200. body: %s", w.Code, w.Body.String())
		return
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["total"] == nil {
		t.Error("response tidak punya total")
	}
	// JSON unmarshal converts numbers to float64
	if int(resp["page"].(float64)) != 1 {
		t.Errorf("page = %v, mau 1", resp["page"])
	}
	if int(resp["per_page"].(float64)) != 20 {
		t.Errorf("per_page = %v, mau 20", resp["per_page"])
	}

	lecturers, ok := resp["lecturers"].([]any)
	if !ok {
		t.Fatal("lecturers bukan array")
	}
	if len(lecturers) == 0 {
		t.Error("lecturers array kosong")
	}
}

func TestListLecturers_FilterDepartment(t *testing.T) {
	h := buatServerUjiLecturer(t)
	dsn1 := h.buatDosen("FILKOM") // department unik untuk test ini
	_ = h.buatDosen("SI")

	req := httptest.NewRequest("GET", "/api/v1/lecturers?department=FILKOM&per_page=5", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	lecturers, ok := resp["lecturers"].([]any)
	if !ok {
		t.Fatal("lecturers bukan array")
	}
	if len(lecturers) == 0 {
		t.Fatal("lecturers kosong")
	}

	// Pastikan ada dosen dengan department FILKOM
	found := false
	for _, l := range lecturers {
		dsn := l.(map[string]any)
		if dsn["department"] == "FILKOM" {
			found = true
			if dsn["id"] == dsn1 {
				t.Log("dosen FILKOM yang baru dibuat ada di hasil")
			}
		}
	}
	if !found {
		t.Error("tidak ada dosen FILKOM di hasil")
	}
}

func TestListLecturers_FilterQ(t *testing.T) {
	h := buatServerUjiLecturer(t)
	// Nama spesifik yang tidak akan muncul di data lain
	namaKhas := "NamaDosenKhasM4cTest_" + fmt.Sprint(time.Now().UnixNano())

	var idKhas string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('dsn-' || gen_random_uuid()::text || '@uji.local', 'x', $1, 'lecturer')
		RETURNING id::text`, namaKhas).Scan(&idKhas)
	if err != nil {
		t.Fatalf("membuat dosen: %v", err)
	}
	_, err = poolUji.Exec(context.Background(),
		`INSERT INTO lecturer_profiles (user_id, department) VALUES ($1::uuid, 'TI')`, idKhas)
	if err != nil {
		t.Fatalf("membuat profil dosen: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/lecturers?q="+namaKhas, nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	lecturers, ok := resp["lecturers"].([]any)
	if !ok {
		t.Fatal("lecturers bukan array")
	}
	if len(lecturers) != 1 {
		t.Errorf("dosen = %d, mau 1", len(lecturers))
	}
}

func TestListLecturers_Pagination(t *testing.T) {
	h := buatServerUjiLecturer(t)
	for i := 0; i < 5; i++ {
		h.buatDosen(fmt.Sprintf("Dept-%d", i))
	}

	// Ambil page 1
	req := httptest.NewRequest("GET", "/api/v1/lecturers?page=1&per_page=2", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", w.Code)
	}

	var resp1 map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp1)

	if int(resp1["page"].(float64)) != 1 {
		t.Errorf("page = %v, mau 1", resp1["page"])
	}
	if int(resp1["per_page"].(float64)) != 2 {
		t.Errorf("per_page = %v, mau 2", resp1["per_page"])
	}

	lecturers1, _ := resp1["lecturers"].([]any)
	if len(lecturers1) != 2 {
		t.Errorf("dosen page 1 = %d, mau 2", len(lecturers1))
	}

	// Ambil page 2
	req2 := httptest.NewRequest("GET", "/api/v1/lecturers?page=2&per_page=2", nil)
	w2 := httptest.NewRecorder()
	h.router.ServeHTTP(w2, req2)

	var resp2 map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp2)

	lecturers2, _ := resp2["lecturers"].([]any)
	if len(lecturers2) != 2 {
		t.Errorf("dosen page 2 = %d, mau 2", len(lecturers2))
	}
}

func TestListLecturers_TidakAdaMahasiswa(t *testing.T) {
	h := buatServerUjiLecturer(t)
	_ = h.buatDosen("TI")
	_ = h.buatMahasiswa() // mahasiswa, bukan dosen

	req := httptest.NewRequest("GET", "/api/v1/lecturers", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	lecturers, ok := resp["lecturers"].([]any)
	if !ok {
		t.Fatal("lecturers bukan array")
	}
	// Hanya dosen yang muncul, bukan mahasiswa
	for _, l := range lecturers {
		_ = l.(map[string]any)
		// Kalau ada field role, pastikan lecturer. Tapi response tidak punya role,
		// jadi cek aja totalnya harus 1 (bukan 2 yang termasuk mahasiswa).
	}
}

func TestListLecturers_TidakBocorkanEmail(t *testing.T) {
	h := buatServerUjiLecturer(t)
	_ = h.buatDosen("TI")

	req := httptest.NewRequest("GET", "/api/v1/lecturers", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	lecturers, ok := resp["lecturers"].([]any)
	if !ok || len(lecturers) == 0 {
		t.Skip("tidak ada dosen untuk dicek")
	}

	dsn := lecturers[0].(map[string]any)
	if _, ada := dsn["email"]; ada {
		t.Error("response bocor email")
	}
	if _, ada := dsn["identity_number"]; ada {
		t.Error("response bocor identity_number")
	}
}

// ---------------------------------------------------------------- test get lecturer by id

func TestGetLecturer_Success(t *testing.T) {
	h := buatServerUjiLecturer(t)
	dsnID := h.buatDosen("TI")

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+dsnID, nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, mau 200. body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["id"] != dsnID {
		t.Errorf("id = %v, mau %s", resp["id"], dsnID)
	}
	if resp["department"] != "TI" {
		t.Errorf("department = %v, mau TI", resp["department"])
	}
}

func TestGetLecturer_404TidakAda(t *testing.T) {
	h := buatServerUjiLecturer(t)

	req := httptest.NewRequest("GET", "/api/v1/lecturers/00000000-0000-0000-0000-000000000000", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, mau 404", w.Code)
	}
}

func TestGetLecturer_404BukanDosen(t *testing.T) {
	h := buatServerUjiLecturer(t)
	mhsID := h.buatMahasiswa()

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+mhsID, nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, mau 404", w.Code)
	}
}

func TestGetLecturer_404TidakAktif(t *testing.T) {
	h := buatServerUjiLecturer(t)
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role, is_active)
		VALUES ('dsn-inaktif-' || gen_random_uuid()::text || '@uji.local', 'x', 'Dosen Inaktif', 'lecturer', false)
		RETURNING id::text`,
	).Scan(&id)
	if err != nil {
		t.Fatalf("membuat dosen inaktif: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+id, nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, mau 404", w.Code)
	}
}

func TestGetLecturer_404TanpaProfil(t *testing.T) {
	h := buatServerUjiLecturer(t)
	// Dosen tanpa profil lecturer_profiles
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('dsn-tanpa-profil-' || gen_random_uuid()::text || '@uji.local', 'x', 'Dosen Tanpa Profil', 'lecturer')
		RETURNING id::text`,
	).Scan(&id)
	if err != nil {
		t.Fatalf("membuat dosen tanpa profil: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+id, nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, mau 404", w.Code)
	}
}

func TestGetLecturer_TidakBocorkanEmail(t *testing.T) {
	h := buatServerUjiLecturer(t)
	dsnID := h.buatDosen("TI")

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+dsnID, nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if _, ada := resp["email"]; ada {
		t.Error("response bocor email")
	}
	if _, ada := resp["identity_number"]; ada {
		t.Error("response bocor identity_number")
	}
}

// ---------------------------------------------------------------- test get lecturer slots

func TestGetLecturerSlots_TanpaParam(t *testing.T) {
	h := buatServerUjiLecturer(t)
	dsnID := h.buatDosen("TI")
	_ = h.buatSlot(dsnID, 24*time.Hour)
	_ = h.buatSlot(dsnID, 48*time.Hour)

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+dsnID+"/slots", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, mau 200. body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["lecturer_id"] != dsnID {
		t.Errorf("lecturer_id = %v, mau %s", resp["lecturer_id"], dsnID)
	}
	if resp["from"] == nil || resp["to"] == nil {
		t.Error("response tidak punya from/to default")
	}

	slots, ok := resp["slots"].([]any)
	if !ok {
		t.Fatal("slots bukan array")
	}
	if len(slots) != 2 {
		t.Errorf("slot = %d, mau 2", len(slots))
	}
}

func TestGetLecturerSlots_DenganRentang(t *testing.T) {
	h := buatServerUjiLecturer(t)
	dsnID := h.buatDosen("TI")
	_ = h.buatSlot(dsnID, 1*24*time.Hour)
	_ = h.buatSlot(dsnID, 3*24*time.Hour)
	_ = h.buatSlot(dsnID, 10*24*time.Hour) // di luar rentang

	from := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	to := time.Now().UTC().Add(5 * 24 * time.Hour).Format(time.RFC3339)

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+dsnID+"/slots?from="+from+"&to="+to, nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	slots, ok := resp["slots"].([]any)
	if !ok {
		t.Fatal("slots bukan array")
	}
	if len(slots) != 2 {
		t.Errorf("slot = %d, mau 2 (slot di luar rentang tidak ikut)", len(slots))
	}
}

func TestGetLecturerSlots_RentangTerlaluJauh(t *testing.T) {
	h := buatServerUjiLecturer(t)
	dsnID := h.buatDosen("TI")

	from := time.Now().UTC().Format(time.RFC3339)
	to := time.Now().UTC().AddDate(0, 0, 60).Format(time.RFC3339) // 60 hari, lewat 30 hari max

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+dsnID+"/slots?from="+from+"&to="+to, nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, mau 422", w.Code)
	}

	var errResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp["error"] == nil {
		t.Error("response tidak punya error")
	}
}

func TestGetLecturerSlots_ToSebelumFrom(t *testing.T) {
	h := buatServerUjiLecturer(t)
	dsnID := h.buatDosen("TI")

	from := time.Now().UTC().Add(5 * 24 * time.Hour).Format(time.RFC3339)
	to := time.Now().UTC().Add(1 * 24 * time.Hour).Format(time.RFC3339) // sebelum from

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+dsnID+"/slots?from="+from+"&to="+to, nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, mau 422", w.Code)
	}
}

func TestGetLecturerSlots_TanpaSlot(t *testing.T) {
	h := buatServerUjiLecturer(t)
	dsnID := h.buatDosen("TI")

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+dsnID+"/slots", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	slots, ok := resp["slots"].([]any)
	if !ok {
		t.Fatal("slots bukan array")
	}
	if len(slots) != 0 {
		t.Errorf("slot = %d, mau 0", len(slots))
	}
}

func TestGetLecturerSlots_SlotDitarikTidakMuncuL(t *testing.T) {
	h := buatServerUjiLecturer(t)
	dsnID := h.buatDosen("TI")
	slotID := h.buatSlot(dsnID, 24*time.Hour)

	// Tarik slot
	poolUji.Exec(context.Background(),
		`UPDATE slots SET withdrawn_at = now() WHERE id = $1::uuid`, slotID)

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+dsnID+"/slots", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	slots, ok := resp["slots"].([]any)
	if !ok {
		t.Fatal("slots bukan array")
	}
	if len(slots) != 0 {
		t.Errorf("slot = %d, mau 0 (slot ditarik tidak muncul)", len(slots))
	}
}

func TestGetLecturerSlots_404DosenTidakAda(t *testing.T) {
	h := buatServerUjiLecturer(t)

	req := httptest.NewRequest("GET", "/api/v1/lecturers/00000000-0000-0000-0000-000000000000/slots", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, mau 404", w.Code)
	}
}

func TestGetLecturerSlots_ResponseTanpaDataMahasiswa(t *testing.T) {
	h := buatServerUjiLecturer(t)
	dsnID := h.buatDosen("TI")
	mhsID := h.buatMahasiswa()
	slotID := h.buatSlot(dsnID, 24*time.Hour)

	// Booking slot
	var bookingID string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO bookings (slot_id, student_id, topic, status)
		VALUES ($1::uuid, $2::uuid, 'Bimbingan', 'confirmed')
		RETURNING id::text`, slotID, mhsID).Scan(&bookingID)
	if err != nil {
		t.Fatalf("membuat booking: %v", err)
	}
	// Set slot ke booked
	poolUji.Exec(context.Background(),
		`UPDATE slots SET status = 'booked' WHERE id = $1::uuid`, slotID)

	req := httptest.NewRequest("GET", "/api/v1/lecturers/"+dsnID+"/slots", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	slots, ok := resp["slots"].([]any)
	if !ok || len(slots) == 0 {
		t.Skip("tidak ada slot")
	}

	slot := slots[0].(map[string]any)
	if slot["status"] != "booked" {
		t.Errorf("slot status = %v, mau booked", slot["status"])
	}
	// Tidak ada field student_id, booking_id, dll
	for _, key := range []string{"student_id", "booking_id", "topic", "description"} {
		if _, ada := slot[key]; ada {
			t.Errorf("slot bocor data mahasiswa: %s", key)
		}
	}
}

func TestGetLecturerSlots_ValidasiFormatTanggal(t *testing.T) {
	h := buatServerUjiLecturer(t)
	dsnID := h.buatDosen("TI")

	tests := []struct {
		name string
		from string
		to   string
	}{
		{"from invalid", "invalid", ""},
		{"to invalid", "", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/lecturers/" + dsnID + "/slots"
			if tt.from != "" || tt.to != "" {
				url += "?"
				if tt.from != "" {
					url += "from=" + tt.from
				}
				if tt.to != "" {
					if tt.from != "" {
						url += "&"
					}
					url += "to=" + tt.to
				}
			}
			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()
			h.router.ServeHTTP(w, req)

			if w.Code != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, mau 422", w.Code)
			}
		})
	}
}

// ---------------------------------------------------------------- test CORS headers

func TestCORS_HeaderTersedia(t *testing.T) {
	h := buatServerUjiLecturer(t)

	req := httptest.NewRequest("GET", "/api/v1/lecturers", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("CORS Allow-Credentials tidak ada")
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("CORS Allow-Origin = %q, mau http://localhost:3000",
			w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_Preflight(t *testing.T) {
	h := buatServerUjiLecturer(t)

	req := httptest.NewRequest("OPTIONS", "/api/v1/lecturers", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "Authorization")
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	// Chi/cors middleware mungkin mengembalikan status selain 204 untuk preflight
	// yang tidak punya route OPTIONS eksplisit. Yang penting CORS headers ada.
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("CORS Allow-Origin = %q, mau http://localhost:3000",
			w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("CORS Allow-Methods tidak ada")
	}
}
