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

	if skipIfNoDB {
		t.Skip("TEST_DATABASE_URL kosong")
	}

	cfg := cfgUji
	if cfg.CampusTZ == nil {
		cfg.CampusTZ = time.Local
	}

	tokens, err := auth.NewTokenIssuer("test-secret-key-panjang-32-karakter", 15*time.Minute)
	if err != nil {
		t.Fatalf("membuat token issuer: %v", err)
	}

	srv := New(cfg, Deps{
		DB:      poolUji,
		Auth:    auth.NewService(poolUji, tokens, slog.New(slog.NewTextHandler(io.Discard, nil))),
		Tokens:  tokens,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Booking: booking.NewService(poolUji, cfg.BookingMinLeadMin, 3, cfg.ReminderLeadHours),
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

func (h *bookingTestHelper) buatAdmin() (id, token string) {
	h.t.Helper()
	var idAdm string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ('admin-' || gen_random_uuid()::text || '@uji.local', 'x', 'Admin Uji', 'admin')
		RETURNING id::text`,
	).Scan(&idAdm)
	if err != nil {
		h.t.Fatalf("membuat admin: %v", err)
	}

	accessToken, _, err := h.server.tokens.Issue(auth.Identity{UserID: idAdm, Role: "admin"})
	if err != nil {
		h.t.Fatalf("membuat access token: %v", err)
	}
	return idAdm, accessToken
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
		_, err := booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
			SlotID: slot, StudentID: mhsID, Topic: "Bimbingan",
		})
		if err != nil {
			h.t.Fatalf("membuat booking ke-%d: %v", i+1, err)
		}
	}
}

// buatBooking langsung INSERT booking tanpa melewati Create (yang punya minLeadMinutes).
func (h *bookingTestHelper) buatBooking(mhsID, slotID string) string {
	h.t.Helper()
	var id string
	err := poolUji.QueryRow(context.Background(), `
		INSERT INTO bookings (slot_id, student_id, topic, status)
		VALUES ($1::uuid, $2::uuid, 'Test', 'confirmed')
		RETURNING id::text`, slotID, mhsID).Scan(&id)
	if err != nil {
		h.t.Fatalf("membuat booking: %v", err)
	}
	// Set slot ke booked.
	poolUji.Exec(context.Background(),
		`UPDATE slots SET status = 'booked' WHERE id = $1::uuid`, slotID)
	return id
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

// TestCreateBooking_ConcurrencyDenganIdempotencyKey menguji bahwa 10 retry
// IDENTIK (satu mahasiswa, satu idempotency key) yang masuk bersamaan semuanya
// diselesaikan dengan benar: satu pemenang menyimpan booking, sisanya me-replay
// booking yang sama.
//
// Catatan penting kenapa ini beda dari test paralel tanpa key: idempotency key
// punya scope (user_id, key). Kalau setiap goroutine memakai key berbeda,
// mereka adalah request BUKAN kembar menurut mekanisme idempotency —
// mereka akan saling berebut slot. Test ini baru bermakna kalau semua retry
// memakai key yang PERSIS SAMA.
//
// Pola barrier `<-mulai` + `close(mulai)` dipakai supaya semua goroutine
// dilepas serentak. Tanpa barrier, goroutine pertama selesai sebelum yang
// kedua sempat dibuat, dan yang diuji bukan race yang sebenarnya.
func TestCreateBooking_ConcurrencyDenganIdempotencyKey(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	_, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 72*time.Hour)

	key := "concurrent-test-key-" + fmt.Sprint(time.Now().UnixNano())
	reqBody := map[string]string{"slot_id": slot, "topic": "Bimbingan skripsi", "description": ""}
	body, _ := json.Marshal(reqBody)

	const jumlah = 10
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mulai := make(chan struct{})
	// Tiap goroutine menulis ke indeksnya sendiri -> tidak ada shared state,
	// aman tanpa mutex. go test -race akan membuktikan klaim ini.
	hasil := make([]*httptest.ResponseRecorder, jumlah)

	var wg sync.WaitGroup
	for i := 0; i < jumlah; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-mulai

			req := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
			req = req.WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+tokenMhs)
			req.Header.Set("Idempotency-Key", key)
			w := httptest.NewRecorder()
			h.router.ServeHTTP(w, req)
			hasil[idx] = w
		}(i)
	}

	close(mulai)
	wg.Wait()

	// Semua 10 response harus 201, id harus identik, dan TEPAT SATU tanpa
	// header Idempotency-Replayed.
	var idPertama string
	var replayCount int
	for i, w := range hasil {
		if w.Code != http.StatusCreated {
			t.Errorf("request %d: status = %d, body = %s", i, w.Code, w.Body.String())
			continue
		}
		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Errorf("request %d: body bukan JSON: %v", i, err)
			continue
		}
		if resp["id"] == "" {
			t.Errorf("request %d: id kosong", i)
			continue
		}
		if idPertama == "" {
			idPertama = resp["id"]
		} else if resp["id"] != idPertama {
			t.Errorf("request %d: id = %s, mau %s", i, resp["id"], idPertama)
		}
		if w.Header().Get("Idempotency-Replayed") == "true" {
			replayCount++
		}
	}

	// Tepat satu yang TIDAK replay (= yang pertama menang).
	if replayCount != jumlah-1 {
		t.Errorf("replay = %d, mau %d (satu pemenang, sisanya replay)", replayCount, jumlah-1)
	}

	// Satu baris di bookings.
	if n := h.hitungBooking(slot); n != 1 {
		t.Errorf("booking = %d, mau 1", n)
	}
}

// TestCreateBooking_ReplayMengembalikanBookingPertamaBukanYangTerbaru memastikan
// sumber kebenaran replay adalah response_body yang tersimpan di
// idempotency_keys, BUKAN query ulang ke tabel bookings.
//
// Skenario:
//
//  1. booking dengan key K -> dapat id A
//  2. cancel A lewat SQL
//  3. booking slot yang sama TANPA key -> dapat id B
//  4. ulang request dengan key K -> HARUS dapat id A, bukan id B
//
// Kalau jalur replay memilih query ulang `WHERE slot_id=X AND student_id=Y`,
// langkah 4 akan mengembalikan id B — bug yang persis yang harus dicegah.
func TestCreateBooking_ReplayMengembalikanBookingPertamaBukanYangTerbaru(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	idUser, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 72*time.Hour)

	key := "cancel-rebook-key-" + fmt.Sprint(time.Now().UnixNano())
	reqBody := map[string]string{"slot_id": slot, "topic": "Bimbingan skripsi", "description": ""}
	body, _ := json.Marshal(reqBody)

	// 1. Booking pertama dengan key K.
	req1 := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+tokenMhs)
	req1.Header.Set("Idempotency-Key", key)
	w1 := httptest.NewRecorder()
	h.router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("booking pertama: status = %d, body = %s", w1.Code, w1.Body.String())
	}
	var resp1 map[string]string
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	idA := resp1["id"]
	if idA == "" {
		t.Fatal("booking pertama tidak mengembalikan id")
	}

	// 2. Cancel booking pertama lewat SQL. cancelled_by ambil id mahasiswa.
	if _, err := poolUji.Exec(context.Background(), `
		UPDATE bookings
		SET status = 'cancelled',
		    cancelled_at = now(),
		    cancelled_by = $2::uuid
		WHERE id = $1::uuid`, idA, idUser); err != nil {
		t.Fatalf("membatalkan booking: %v", err)
	}

	// Reset slot supaya bisa dipesan ulang.
	if _, err := poolUji.Exec(context.Background(),
		`UPDATE slots SET status = 'open' WHERE id = $1::uuid`, slot); err != nil {
		t.Fatalf("reset slot: %v", err)
	}

	// 3. Booking ulang TANPA idempotency key -> id baru B.
	req2Body := map[string]string{"slot_id": slot, "topic": "Bimbingan skripsi", "description": ""}
	body2, _ := json.Marshal(req2Body)
	req2 := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+tokenMhs)
	// TIDAK ADA Idempotency-Key.
	w2 := httptest.NewRecorder()
	h.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("booking kedua: status = %d, body = %s", w2.Code, w2.Body.String())
	}
	var resp2 map[string]string
	json.Unmarshal(w2.Body.Bytes(), &resp2)
	idB := resp2["id"]
	if idB == "" || idB == idA {
		t.Fatalf("booking kedua harus dapat id berbeda: idA=%s idB=%s", idA, idB)
	}

	// 4. Replay key K -> HARUS dapat id A.
	req3 := httptest.NewRequest("POST", "/api/v1/bookings", bytes.NewReader(body))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+tokenMhs)
	req3.Header.Set("Idempotency-Key", key)
	w3 := httptest.NewRecorder()
	h.router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusCreated {
		t.Fatalf("replay: status = %d, body = %s", w3.Code, w3.Body.String())
	}
	if w3.Header().Get("Idempotency-Replayed") != "true" {
		t.Errorf("replay tanpa header Idempotency-Replayed")
	}
	var resp3 map[string]string
	json.Unmarshal(w3.Body.Bytes(), &resp3)
	if resp3["id"] != idA {
		t.Errorf("replay mengembalikan id = %s, mau %s (booking pertama, BUKAN yang terbaru %s)",
			resp3["id"], idA, idB)
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

	_, _ = booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
		SlotID: slot1, StudentID: mhs1ID, Topic: "Milik mhs1",
	})
	_, _ = booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
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

	_, _ = booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
		SlotID: slot1, StudentID: mhs1ID, Topic: "Bimbingan 1",
	})
	_, _ = booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
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

	b, err := booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
		SlotID: slot, StudentID: mhs1ID, Topic: "Bimbingan",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/bookings/"+b.Booking.ID, nil)
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

	b, err := booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
		SlotID: slot, StudentID: mhsID, Topic: "Bimbingan skripsi",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/bookings/"+b.Booking.ID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET booking gagal: %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["id"] != b.Booking.ID {
		t.Errorf("id = %v, mau %s", resp["id"], b.Booking.ID)
	}
	if resp["topic"] != "Bimbingan skripsi" {
		t.Errorf("topic = %v, mau 'Bimbingan skripsi'", resp["topic"])
	}
}

func TestGetBooking_AdminDitolak(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	mhsID, _ := h.buatMahasiswa()
	_, tokenAdmin := h.buatAdmin()
	slot := h.buatSlot(dosen, 24*time.Hour)

	b, err := booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
		SlotID: slot, StudentID: mhsID, Topic: "Bimbingan",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/bookings/"+b.Booking.ID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenAdmin)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, mau 404", w.Code)
	}
}

// ---------------------------------------------------------------- test cancel HTTP

func TestCancelBooking_HappyPath(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	mhsID, tokenMhs := h.buatMahasiswa()
	// Slot 5 jam dari sekarang — jauh dari H-3.
	slot := h.buatSlot(dosen, 5*time.Hour)

	b, err := booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
		SlotID: slot, StudentID: mhsID, Topic: "Test cancel",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	reqBody := map[string]string{"reason": "Tidak jadi"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PATCH", "/api/v1/bookings/"+b.Booking.ID+"/cancel",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, mau 200. body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "cancelled" {
		t.Errorf("status = %v, mau cancelled", resp["status"])
	}
}

func TestCancelBooking_OrangLain(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	mhs1ID, _ := h.buatMahasiswa()
	_, tokenMhs2 := h.buatMahasiswa()
	slot := h.buatSlot(dosen, 5*time.Hour)

	b, err := booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
		SlotID: slot, StudentID: mhs1ID, Topic: "Milik mhs1",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	req := httptest.NewRequest("PATCH", "/api/v1/bookings/"+b.Booking.ID+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+tokenMhs2)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	// Harus 404, bukan 403 — tidak membocorkan keberadaan booking orang lain.
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, mau 404", w.Code)
	}
}

func TestCancelBooking_TerlaluDekat(t *testing.T) {
	h := buatServerUji(t)
	dosen, _ := h.buatDosen()
	mhsID, tokenMhs := h.buatMahasiswa()
	// Slot 2 jam dari sekarang — kurang dari H-3.
	slot := h.buatSlot(dosen, 2*time.Hour)

	b, err := booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
		SlotID: slot, StudentID: mhsID, Topic: "Terlalu dekat",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	req := httptest.NewRequest("PATCH", "/api/v1/bookings/"+b.Booking.ID+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, mau 422", w.Code)
	}
	var errResp map[string]map[string]string
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp["error"]["code"] != "CANCEL_TOO_LATE" {
		t.Errorf("code = %q, mau CANCEL_TOO_LATE", errResp["error"]["code"])
	}
}

func TestCancelBooking_DosenDitolak(t *testing.T) {
	h := buatServerUji(t)
	dosenID, tokenDosen := h.buatDosen()
	mhsID, _ := h.buatMahasiswa()
	slot := h.buatSlot(dosenID, 5*time.Hour)

	b, err := booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
		SlotID: slot, StudentID: mhsID, Topic: "Test",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	req := httptest.NewRequest("PATCH", "/api/v1/bookings/"+b.Booking.ID+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+tokenDosen)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	// Harus 403 — route cancel dilindungi requireRole(auth.RoleStudent).
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, mau 403", w.Code)
	}
}

// ---------------------------------------------------------------- test complete HTTP

func TestCompleteBooking_HappyPath(t *testing.T) {
	h := buatServerUji(t)
	dosenID, tokenDosen := h.buatDosen()
	mhsID, _ := h.buatMahasiswa()
	// Slot di masa lalu — insert booking langsung.
	slot := h.buatSlot(dosenID, -5*time.Hour)
	bookingID := h.buatBooking(mhsID, slot)

	reqBody := map[string]string{"lecturer_note": "Sesi produktif"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PATCH", "/api/v1/bookings/"+bookingID+"/complete",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenDosen)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, mau 200. body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "completed" {
		t.Errorf("status = %v, mau completed", resp["status"])
	}
}

func TestCompleteBooking_MahasiswaDitolak(t *testing.T) {
	h := buatServerUji(t)
	dosenID, _ := h.buatDosen()
	mhsID, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosenID, -5*time.Hour)
	bookingID := h.buatBooking(mhsID, slot)

	req := httptest.NewRequest("PATCH", "/api/v1/bookings/"+bookingID+"/complete", nil)
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	// Harus 403 — route complete dilindungi requireRole(auth.RoleLecturer).
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, mau 403", w.Code)
	}
}

func TestCompleteBooking_DosenLain(t *testing.T) {
	h := buatServerUji(t)
	dosen1ID, _ := h.buatDosen()
	_, tokenDosen2 := h.buatDosen()
	mhsID, _ := h.buatMahasiswa()
	slot := h.buatSlot(dosen1ID, -5*time.Hour)
	bookingID := h.buatBooking(mhsID, slot)

	req := httptest.NewRequest("PATCH", "/api/v1/bookings/"+bookingID+"/complete", nil)
	req.Header.Set("Authorization", "Bearer "+tokenDosen2)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	// Harus 404 — dosen2 bukan pemilik slot.
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, mau 404", w.Code)
	}
}

func TestCompleteBooking_SebelumStart(t *testing.T) {
	h := buatServerUji(t)
	dosenID, tokenDosen := h.buatDosen()
	mhsID, _ := h.buatMahasiswa()
	// Slot di masa depan — belum dimulai.
	slot := h.buatSlot(dosenID, 5*time.Hour)

	b, err := booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
		SlotID: slot, StudentID: mhsID, Topic: "Test",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	req := httptest.NewRequest("PATCH", "/api/v1/bookings/"+b.Booking.ID+"/complete", nil)
	req.Header.Set("Authorization", "Bearer "+tokenDosen)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, mau 422", w.Code)
	}
	var errResp map[string]map[string]string
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp["error"]["code"] != "SESSION_NOT_STARTED" {
		t.Errorf("code = %q, mau SESSION_NOT_STARTED", errResp["error"]["code"])
	}
}

// ---------------------------------------------------------------- test no-show HTTP

func TestNoShowBooking_HappyPath(t *testing.T) {
	h := buatServerUji(t)
	dosenID, tokenDosen := h.buatDosen()
	mhsID, _ := h.buatMahasiswa()
	slot := h.buatSlot(dosenID, -5*time.Hour)
	bookingID := h.buatBooking(mhsID, slot)

	req := httptest.NewRequest("PATCH", "/api/v1/bookings/"+bookingID+"/no-show", nil)
	req.Header.Set("Authorization", "Bearer "+tokenDosen)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, mau 200. body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "no_show" {
		t.Errorf("status = %v, mau no_show", resp["status"])
	}
}

func TestNoShowBooking_MahasiswaDitolak(t *testing.T) {
	h := buatServerUji(t)
	dosenID, _ := h.buatDosen()
	mhsID, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosenID, -5*time.Hour)
	bookingID := h.buatBooking(mhsID, slot)

	req := httptest.NewRequest("PATCH", "/api/v1/bookings/"+bookingID+"/no-show", nil)
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, mau 403", w.Code)
	}
}

// TestLecturerNoteMunculDiGetBooking menguji alur lengkap:
// 1. booking diselesaikan dosen dengan lecturer_note
// 2. GET /bookings sebagai mahasiswa harus mengembalikan lecturer_note
// 3. GET /bookings sebagai dosen juga harus mengembalikan lecturer_note
func TestLecturerNoteMunculDiGetBooking(t *testing.T) {
	h := buatServerUji(t)
	dosenID, tokenDosen := h.buatDosen()
	mhsID, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosenID, -5*time.Hour)
	bookingID := h.buatBooking(mhsID, slot)

	// Selesaikan booking dengan catatan.
	note := "Sesi produktif, mahasiswa perlu follow-up minggu depan"
	reqBody := map[string]string{"lecturer_note": note}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PATCH", "/api/v1/bookings/"+bookingID+"/complete",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenDosen)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("complete gagal: %d", w.Code)
	}

	// GET sebagai mahasiswa.
	reqMhs := httptest.NewRequest("GET", "/api/v1/bookings", nil)
	reqMhs.Header.Set("Authorization", "Bearer "+tokenMhs)
	wMhs := httptest.NewRecorder()
	h.router.ServeHTTP(wMhs, reqMhs)
	if wMhs.Code != http.StatusOK {
		t.Fatalf("mhs get gagal: %d", wMhs.Code)
	}
	var respMhs map[string]any
	json.Unmarshal(wMhs.Body.Bytes(), &respMhs)
	bookingsMhs := respMhs["bookings"].([]any)
	var foundMhs bool
	for _, b := range bookingsMhs {
		bm := b.(map[string]any)
		if bm["id"] == bookingID {
			foundMhs = true
			if bm["lecturer_note"] != note {
				t.Errorf("mhs: lecturer_note = %q, mau %q", bm["lecturer_note"], note)
			}
		}
	}
	if !foundMhs {
		t.Error("mhs: booking tidak ditemukan di daftar")
	}

	// GET sebagai dosen.
	reqDosen := httptest.NewRequest("GET", "/api/v1/bookings", nil)
	reqDosen.Header.Set("Authorization", "Bearer "+tokenDosen)
	wDosen := httptest.NewRecorder()
	h.router.ServeHTTP(wDosen, reqDosen)
	if wDosen.Code != http.StatusOK {
		t.Fatalf("dosen get gagal: %d", wDosen.Code)
	}
	var respDosen map[string]any
	json.Unmarshal(wDosen.Body.Bytes(), &respDosen)
	bookingsDosen := respDosen["bookings"].([]any)
	var foundDosen bool
	for _, b := range bookingsDosen {
		bm := b.(map[string]any)
		if bm["id"] == bookingID {
			foundDosen = true
			if bm["lecturer_note"] != note {
				t.Errorf("dosen: lecturer_note = %q, mau %q", bm["lecturer_note"], note)
			}
		}
	}
	if !foundDosen {
		t.Error("dosen: booking tidak ditemukan di daftar")
	}
}

// TestLecturerNoteKosongUntukNonCompleted memastikan lecturer_note tidak muncul
// di response untuk booking yang belum completed.
func TestLecturerNoteKosongUntukNonCompleted(t *testing.T) {
	h := buatServerUji(t)
	dosenID, _ := h.buatDosen()
	mhsID, tokenMhs := h.buatMahasiswa()
	slot := h.buatSlot(dosenID, 24*time.Hour)

	b, err := booking.NewService(poolUji, 60, 3, 1).Create(context.Background(), booking.CreateInput{
		SlotID: slot, StudentID: mhsID, Topic: "Bimbingan",
	})
	if err != nil {
		t.Fatalf("booking gagal: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/bookings", nil)
	req.Header.Set("Authorization", "Bearer "+tokenMhs)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get gagal: %d", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	bookings := resp["bookings"].([]any)
	for _, bk := range bookings {
		bm := bk.(map[string]any)
		if bm["id"] == b.Booking.ID {
			if _, ada := bm["lecturer_note"]; ada {
				t.Error("lecturer_note tidak boleh ada di booking yang belum completed")
			}
		}
	}
}
