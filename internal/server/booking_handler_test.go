package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/oktasatwika/sikons/internal/auth"
	"github.com/oktasatwika/sikons/internal/booking"
)

// bookingTestHelper menyediakan helper untuk test booking handler.
// Pakai poolUji dan cfgUji dari main_test.go.
type bookingTestHelper struct {
	t      *testing.T
	server *Server
	router http.Handler
}

func buatServerUji(t *testing.T) *bookingTestHelper {
	t.Helper()

	cfg := cfgUji
	if cfg.CampusTZ == nil {
		cfg.CampusTZ = time.Local
	}

	tokens, err := auth.NewTokenIssuer("test-secret-key-panjang-32-karakter", 15*time.Minute)
	if err != nil {
		t.Fatalf("membuat token issuer: %v", err)
	}

	srv := New(cfg, Deps{
		Pool:    poolUji,
		Auth:    auth.NewService(poolUji, tokens, slog.New(slog.NewTextHandler(io.Discard, nil))),
		Tokens:  tokens,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Booking: booking.NewService(poolUji, cfg.BookingMinLeadMin),
	})

	return &bookingTestHelper{
		t:      t,
		server: srv,
		router: srv.Routes(),
	}
}

func (h *bookingTestHelper) buatDosen() (id, token string) {
	h.t.Helper()
	var idDsn string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('dosen-' || gen_random_uuid()::text || '@uji.local', 'x', 'Dosen Uji', 'lecturer')
		RETURNING id::text`,
	).Scan(&idDsn)
	if err != nil {
		h.t.Fatalf("membuat dosen: %v", err)
	}
	_, err = poolUji.Exec(context.Background(),
		`INSERT INTO lecturer_profiles (user_id, department) VALUES ($1::uuid, 'TI')`, idDsn)
	if err != nil {
		h.t.Fatalf("membuat profil dosen: %v", err)
	}

	accessToken, _, err := h.server.tokens.Issue(auth.Identity{UserID: idDsn, Role: "lecturer"})
	if err != nil {
		h.t.Fatalf("membuat access token: %v", err)
	}
	return idDsn, accessToken
}

func (h *bookingTestHelper) buatMahasiswa() (id, token string) {
	h.t.Helper()
	var idMhs string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('mhs-' || gen_random_uuid()::text || '@uji.local', 'x', 'Mahasiswa Uji', 'student')
		RETURNING id::text`,
	).Scan(&idMhs)
	if err != nil {
		h.t.Fatalf("membuat mahasiswa: %v", err)
	}

	accessToken, _, err := h.server.tokens.Issue(auth.Identity{UserID: idMhs, Role: "student"})
	if err != nil {
		h.t.Fatalf("membuat access token: %v", err)
	}
	return idMhs, accessToken
}

func (h *bookingTestHelper) buatSlot(dosenID string, mulaiDalam time.Duration) string {
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

func (h *bookingTestHelper) statusSlot(slotID string) string {
	h.t.Helper()
	var s string
	err := poolUji.QueryRow(context.Background(),
		`SELECT status::text FROM slots WHERE id = $1::uuid`, slotID).Scan(&s)
	if err != nil {
		h.t.Fatalf("membaca status slot: %v", err)
	}
	return s
}

func (h *bookingTestHelper) hitungBooking(slotID string) int {
	h.t.Helper()
	var n int
	err := poolUji.QueryRow(context.Background(),
		`SELECT count(*) FROM bookings WHERE slot_id = $1::uuid AND status <> 'cancelled'`,
		slotID).Scan(&n)
	if err != nil {
		h.t.Fatalf("menghitung booking: %v", err)
	}
	return n
}

func (h *bookingTestHelper) buat3BookingAktif(mhsID, dosenID string) {
	h.t.Helper()
	for i := 0; i < 3; i++ {
		slot := h.buatSlot(dosenID, time.Duration(24+i)*time.Hour)
		_, _, err := booking.NewService(poolUji, 60).Create(context.Background(), booking.CreateInput{
			SlotID: slot, StudentID: mhsID, Topic: "Bimbingan",
		})
		if err != nil {
			h.t.Fatalf("membuat booking ke-%d: %v", i+1, err)
		}
	}
}

// ---------------------------------------------------------------- test

func TestCreateBooking_HappyPath(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	_, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 72*time.Hour)

	reqBody := map[string]string{"slot_id": slot, "topic": "Bimbingan skripsi", "description": "Tentang proposal"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, mau 201. body: %s", w.Code, w.Body.String())
	}

	if s := h.statusSlot(slot); s != "booked" {
		t.Errorf("status slot = %q, mau booked", s)
	}

	if n := h.hitungBooking(slot); n != 1 {
		t.Errorf("booking = %d, mau 1", n)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["id"] == "" {
		t.Error("response tidak punya id")
	}
}

func TestCreateBooking_IdempotencyKeySama(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	_, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 72*time.Hour)

	key := "test-key-12345678"
	reqBody := map[string]string{"slot_id": slot, "topic": "Bimbingan skripsi", "description": ""}
	body, _ := json.Marshal(reqBody)

	req1 := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+tokenMhs)
	req1.Header.Set("Idempotency-Key", key)
	w1 := httptest.NewRecorder()
	h.router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("request pertama gagal: %d, body: %s", w1.Code, w1.Body.String())
	}
	var resp1 map[string]string
	json.Unmarshal(w1.Body.Bytes(), &resp1)

	req2 := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+tokenMhs)
	req2.Header.Set("Idempotency-Key", key)
	w2 := httptest.NewRecorder()
	h.router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Errorf("request kedua gagal: %d, body: %s", w2.Code, w2.Body.String())
	}

	var resp2 map[string]string
	json.Unmarshal(w2.Body.Bytes(), &resp2)
	if resp1["id"] != resp2["id"] {
		t.Errorf("booking id berbeda: %s vs %s", resp1["id"], resp2["id"])
	}

	if w2.Header().Get("Idempotency-Replayed") != "true" {
		t.Errorf("header Idempotency-Replayed = %q, mau true", w2.Header().Get("Idempotency-Replayed"))
	}

	if n := h.hitungBooking(slot); n != 1 {
		t.Errorf("booking = %d, mau 1", n)
	}
}

func TestCreateBooking_IdempotencyKeyBodyBeda(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	_, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 72*time.Hour)

	key := "test-key-body-beda"

	body1 := map[string]string{"slot_id": slot, "topic": "Topik Satu", "description": ""}
	b1, _ := json.Marshal(body1)
	req1 := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(b1))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+tokenMhs)
	req1.Header.Set("Idempotency-Key", key)
	w1 := httptest.NewRecorder()
	h.router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("request pertama gagal: %d", w1.Code)
	}

	body2 := map[string]string{"slot_id": slot, "topic": "Topik Berbeda", "description": ""}
	b2, _ := json.Marshal(body2)
	req2 := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(b2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+tokenMhs)
	req2.Header.Set("Idempotency-Key", key)
	w2 := httptest.NewRecorder()
	h.router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, mau 422", w2.Code)
	}

	var errResp map[string]map[string]string
	json.Unmarshal(w2.Body.Bytes(), &errResp)
	if errResp["error"]["code"] != "IDEMPOTENCY_KEY_REUSED" {
		t.Errorf("code = %q, mau IDEMPOTENCY_KEY_REUSED", errResp["error"]["code"])
	}
}

func TestCreateBooking_ConcurrencyDenganIdempotencyKey(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	// SATU mahasiswa, 10 retry dengan idempotency key yang sama.
	// Idempotency key punya scope (user_id, key), jadi ini menguji skenario:
	// user menekan tombol beberapa kali karena timeout.
	_, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 72*time.Hour)

	key := "concurrent-test-key-12345678"
	reqBody := map[string]string{"slot_id": slot, "topic": "Bimbingan skripsi", "description": ""}
	body, _ := json.Marshal(reqBody)

	// Test: request pertama langsung, sisanya dijalankan SEQUENTIAL
	// setelah request pertama selesai (retry karena timeout).
	results := make([]*httptest.ResponseRecorder, 10)

	// Request pertama
	req0 := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req0.Header.Set("Content-Type", "application/json")
	req0.Header.Set("Authorization", "Bearer "+tokenMhs)
	req0.Header.Set("Idempotency-Key", key)
	w0 := httptest.NewRecorder()
	h.router.ServeHTTP(w0, req0)
	results[0] = w0

	// Request 1-9 dijalankan sequential setelah request 0 selesai (retry).
	for i := 1; i < 10; i++ {
		req := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tokenMhs)
		req.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.router.ServeHTTP(w, req)
		results[i] = w
	}

	// Semua harus berhasil: winner 201, loser replay 201.
	var sukses int
	var idPertama string
	for i, w := range results {
		if w.Code == http.StatusCreated {
			sukses++
			replayed := w.Header().Get("Idempotency-Replayed")
			if i == 0 && replayed == "true" {
				t.Errorf("request 0 tidak boleh replay")
			}
			if i > 0 && replayed != "true" {
				t.Errorf("request %d harus replay", i)
			}
			var resp map[string]string
			json.Unmarshal(w.Body.Bytes(), &resp)
			if idPertama == "" {
				idPertama = resp["id"]
			} else if resp["id"] != idPertama {
				t.Errorf("booking id berbeda: %s vs %s", resp["id"], idPertama)
			}
		} else {
			t.Errorf("request %d gagal: status=%d, body=%s", i, w.Code, w.Body.String())
		}
	}

	if sukses != 10 {
		t.Errorf("sukses = %d, mau 10", sukses)
	}

	// Hanya ada 1 baris booking.
	if n := h.hitungBooking(slot); n != 1 {
		t.Errorf("booking = %d, mau 1", n)
	}
}

func TestCreateBooking_TanpaIdempotencyKeyParalel(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	mhs := make([]string, 2)
	for i := range mhs {
		_, mhs[i] = h.buatMahasiswa()
	}
	slot := h.buatSlot(dosen, 72*time.Hour)

	reqBody := map[string]string{"slot_id": slot, "topic": "Bimbingan skripsi", "description": ""}
	body, _ := json.Marshal(reqBody)

	mulai := make(chan struct{})
	var wg sync.WaitGroup
	hasil := make([]*httptest.ResponseRecorder, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-mulai

			req := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+mhs[idx])
			w := httptest.NewRecorder()
			h.router.ServeHTTP(w, req)

			hasil[idx] = w
		}(i)
	}

	close(mulai)
	wg.Wait()

	var sukses, konflik int
	for _, w := range hasil {
		if w.Code == http.StatusCreated {
			sukses++
		} else if w.Code == http.StatusConflict {
			konflik++
		}
	}

	if sukses != 1 {
		t.Errorf("sukses = %d, mau 1", sukses)
	}
	if konflik != 1 {
		t.Errorf("konflik = %d, mau 1", konflik)
	}

	if n := h.hitungBooking(slot); n != 1 {
		t.Errorf("booking = %d, mau 1", n)
	}
}

func TestCreateBooking_SlotTerlaluDekat(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	_, tokenMhs := h.buatMahasiswa()
	// Slot mulai 30 menit dari sekarang — kurang dari minLeadMinutes (60).
	slot := h.buatSlot(dosen, 30*time.Minute)

	reqBody := map[string]string{"slot_id": slot, "topic": "Bimbingan skripsi", "description": ""}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, mau 422", w.Code)
	}

	var errResp map[string]map[string]string
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp["error"]["code"] != "SLOT_TOO_SOON" {
		t.Errorf("code = %q, mau SLOT_TOO_SOON", errResp["error"]["code"])
	}
}

func TestCreateBooking_SlotDitarik(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	_, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 72*time.Hour)

	poolUji.Exec(context.Background(),
		`UPDATE slots SET withdrawn_at = now() WHERE id = $1::uuid`, slot)

	reqBody := map[string]string{"slot_id": slot, "topic": "Bimbingan", "description": ""}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, mau 409", w.Code)
	}

	var errResp map[string]map[string]string
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp["error"]["code"] != "SLOT_WITHDRAWN" {
		t.Errorf("code = %q, mau SLOT_WITHDRAWN", errResp["error"]["code"])
	}
}

func TestCreateBooking_SlotSudahDipesan(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	_, tokenMhs1 := h.buatMahasiswa()
	_, tokenMhs2 := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 72*time.Hour)

	reqBody := map[string]string{"slot_id": slot, "topic": "Bimbingan 1", "description": ""}
	body, _ := json.Marshal(reqBody)
	req1 := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+tokenMhs1)
	w1 := httptest.NewRecorder()
	h.router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("request pertama gagal: %d", w1.Code)
	}

	req2 := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+tokenMhs2)
	w2 := httptest.NewRecorder()
	h.router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Errorf("status = %d, mau 409", w2.Code)
	}

	var errResp map[string]map[string]string
	json.Unmarshal(w2.Body.Bytes(), &errResp)
	if errResp["error"]["code"] != "SLOT_ALREADY_BOOKED" {
		t.Errorf("code = %q, mau SLOT_ALREADY_BOOKED", errResp["error"]["code"])
	}
}

func TestCreateBooking_Batas3Aktif(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	mhsID, tokenMhs := h.buatMahasiswa()
	h.buat3BookingAktif(mhsID, dosen)

	slot := h.buatSlot(dosen, 100*time.Hour)

	reqBody := map[string]string{"slot_id": slot, "topic": "Booking ke-4", "description": ""}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, mau 409", w.Code)
	}

	var errResp map[string]map[string]string
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp["error"]["code"] != "BOOKING_LIMIT_REACHED" {
		t.Errorf("code = %q, mau BOOKING_LIMIT_REACHED", errResp["error"]["code"])
	}
}

func TestCreateBooking_BodyStudentIDDitolak(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	_, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 72*time.Hour)

	type reqWithStudentID struct {
		SlotID    string `json:"slot_id"`
		Topic     string `json:"topic"`
		StudentID string `json:"student_id"`
	}
	reqBody := reqWithStudentID{SlotID: slot, Topic: "Bimbingan", StudentID: "fake-id"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, mau 400", w.Code)
	}
}

func TestCreateBooking_DosenDitolak(t *testing.T) {
	h := buatServerUji(t)
	dosen, tokenDosen := h.buatDosen()
	slot := h.buatSlot(dosen, 72*time.Hour)

	reqBody := map[string]string{"slot_id": slot, "topic": "Bimbingan", "description": ""}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenDosen)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, mau 403", w.Code)
	}
}

func TestListBookings_MahasiswaHanyaLihatMiliknya(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	mhs1ID, tokenMhs1 := h.buatMahasiswa()
	mhs2ID, tokenMhs2 := h.buatMahasiswa()
	slot1 := h.buatSlot(dosen, 24*time.Hour)
	slot2 := h.buatSlot(dosen, 48*time.Hour)

	_, _, _ = booking.NewService(poolUji, 60).Create(context.Background(), booking.CreateInput{
		SlotID: slot1, StudentID: mhs1ID, Topic: "Milik mhs1",
	})
	_, _, _ = booking.NewService(poolUji, 60).Create(context.Background(), booking.CreateInput{
		SlotID: slot2, StudentID: mhs2ID, Topic: "Milik mhs2",
	})

	req1 := httptest.NewRequest("GET", "/api/v1/bookings", nil)
	req1.Header.Set("Authorization", "Bearer "+tokenMhs1)
	w1 := httptest.NewRecorder()
	h.router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("mhs1 list gagal: %d", w1.Code)
	}

	var resp1 map[string]any
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	bookings1 := resp1["bookings"].([]any)
	if len(bookings1) != 1 {
		t.Errorf("mhs1 lihat %d booking, mau 1", len(bookings1))
	}

	req2 := httptest.NewRequest("GET", "/api/v1/bookings", nil)
	req2.Header.Set("Authorization", "Bearer "+tokenMhs2)
	w2 := httptest.NewRecorder()
	h.router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("mhs2 list gagal: %d", w2.Code)
	}

	var resp2 map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp2)
	bookings2 := resp2["bookings"].([]any)
	if len(bookings2) != 1 {
		t.Errorf("mhs2 lihat %d booking, mau 1", len(bookings2))
	}
}

func TestListBookings_DosenLihatDiSlotnya(t *testing.T) {
	h := buatServerUji(t)
	dosen, tokenDosen := h.buatDosen()
	mhs1ID, _ := h.buatMahasiswa()
	mhs2ID, _ := h.buatMahasiswa()
	slot1 := h.buatSlot(dosen, 24*time.Hour)
	slot2 := h.buatSlot(dosen, 48*time.Hour)

	_, _, _ = booking.NewService(poolUji, 60).Create(context.Background(), booking.CreateInput{
		SlotID: slot1, StudentID: mhs1ID, Topic: "Bimbingan 1",
	})
	_, _, _ = booking.NewService(poolUji, 60).Create(context.Background(), booking.CreateInput{
		SlotID: slot2, StudentID: mhs2ID, Topic: "Bimbingan 2",
	})

	req := httptest.NewRequest("GET", "/api/v1/bookings", nil)
	req.Header.Set("Authorization", "Bearer "+tokenDosen)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("dosen list gagal: %d", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	bookings := resp["bookings"].([]any)
	if len(bookings) != 2 {
		t.Errorf("dosen lihat %d booking, mau 2", len(bookings))
	}
}

func TestGetBooking_MilikOrangLain(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	mhs1ID, _ := h.buatMahasiswa()
	_, tokenMhs2 := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 24*time.Hour)

	b, _, err := booking.NewService(poolUji, 60).Create(context.Background(), booking.CreateInput{
		SlotID: slot, StudentID: mhs1ID, Topic: "Bimbingan",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/bookings/"+b.ID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenMhs2)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, mau 404", w.Code)
	}
}

func TestGetBooking_Success(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	mhsID, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 24*time.Hour)

	b, _, err := booking.NewService(poolUji, 60).Create(context.Background(), booking.CreateInput{
		SlotID: slot, StudentID: mhsID, Topic: "Bimbingan skripsi",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/bookings/"+b.ID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET booking gagal: %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["id"] != b.ID {
		t.Errorf("id = %v, mau %s", resp["id"], b.ID)
	}
	if resp["topic"] != "Bimbingan skripsi" {
		t.Errorf("topic = %v, mau 'Bimbingan skripsi'", resp["topic"])
	}
}
