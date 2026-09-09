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
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oktasatwika/sikons/internal/auth"
	"github.com/oktasatwika/sikons/internal/config"
)

// Test di file ini butuh Postgres sungguhan. Kalau TEST_DATABASE_URL kosong,
// masing-masing test memanggil t.Skip — BUKAN os.Exit di TestMain, karena file
// tetangga di package ini berisi test yang tidak butuh database sama sekali
// dan harus tetap jalan.
var (
	sekali sync.Once
	poolDB *pgxpool.Pool
)

func dbUji(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL kosong — test integrasi dilewati")
	}

	sekali.Do(func() {
		ctx, batal := context.WithTimeout(context.Background(), 10*time.Second)
		defer batal()
		p, err := pgxpool.New(ctx, dsn)
		if err != nil {
			return
		}
		if err := p.Ping(ctx); err != nil {
			p.Close()
			return
		}
		poolDB = p
	})

	if poolDB == nil {
		t.Fatal("tidak bisa terhubung ke TEST_DATABASE_URL")
	}
	return poolDB
}

// Tidak ada TRUNCATE di file ini, dan itu disengaja.
//
// `go test ./...` menjalankan test antar-package SECARA PARALEL. TRUNCATE di
// sini pernah menghapus data test package booking di tengah jalan, dan
// gejalanya menyesatkan total: booking gagal dengan "mahasiswa tidak
// ditemukan" padahal mahasiswanya baru saja dibuat.
//
// Gantinya: setiap test memakai email yang unik, jadi tidak ada test yang
// perlu tahu atau peduli isi tabel sebelum dia jalan.
var penghitungUnik atomic.Int64

func emailUnik(nama string) string {
	return fmt.Sprintf("%s-%d-%d@uji.local", nama, time.Now().UnixNano(), penghitungUnik.Add(1))
}

// serverLengkap merakit server dengan auth.Service sungguhan di atas database
// uji — persis seperti yang dirakit main.go, supaya yang diuji memang jalur
// yang benar-benar dipakai di produksi.
func serverLengkap(t *testing.T) http.Handler {
	t.Helper()
	pool := dbUji(t)

	tokens, err := auth.NewTokenIssuer(secretUji, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	return New(config.Config{Env: "test"}, Deps{
		DB:     pool,
		Auth:   auth.NewService(pool, tokens),
		Tokens: tokens,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).Routes()
}

func kirim(t *testing.T, h http.Handler, metode, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()

	var r *http.Request
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(metode, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(metode, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestAlurDaftarLoginLaluMe(t *testing.T) {
	h := serverLengkap(t)

	// Sengaja ada huruf besar, untuk membuktikan email dinormalkan.
	emailAsli := "Ani-" + fmt.Sprint(time.Now().UnixNano()) + "@Kampus.ac.id"

	// --- daftar
	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email":           emailAsli,
		"password":        "password-yang-cukup-panjang",
		"full_name":       "Ani Rahmawati",
		"identity_number": "NIM-" + fmt.Sprint(time.Now().UnixNano()),
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var daftar struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&daftar); err != nil {
		t.Fatal(err)
	}
	// Email dinormalkan jadi huruf kecil, supaya "Ani@" dan "ani@" adalah satu
	// akun yang sama, bukan dua.
	if daftar.User.Email != strings.ToLower(emailAsli) {
		t.Errorf("email = %q, mau %q", daftar.User.Email, strings.ToLower(emailAsli))
	}
	if daftar.User.Role != auth.RoleStudent {
		t.Errorf("role = %q, mau student", daftar.User.Role)
	}

	// --- login, dengan kapitalisasi email yang berbeda lagi
	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    strings.ToUpper(emailAsli), // kapitalisasi berbeda lagi
		"password": "password-yang-cukup-panjang",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var masuk struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&masuk); err != nil {
		t.Fatal(err)
	}
	if masuk.AccessToken == "" {
		t.Fatal("access_token kosong")
	}

	// Response login tidak boleh membawa hash password dalam bentuk apa pun.
	if bytes.Contains(rec.Body.Bytes(), []byte("password_hash")) {
		t.Error("response login mengandung password_hash")
	}

	// --- /me pakai token barusan
	rec = kirim(t, h, http.MethodGet, "/api/v1/me", nil, masuk.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var saya struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&saya); err != nil {
		t.Fatal(err)
	}
	if saya.User.ID != daftar.User.ID {
		t.Errorf("/me mengembalikan user yang berbeda: %q vs %q", saya.User.ID, daftar.User.ID)
	}
}

// INI TEST KEAMANAN PALING PENTING DI FILE INI.
//
// Kalau seseorang bisa mendaftar sebagai 'lecturer' atau 'admin', dia bisa
// membuka jadwal konsultasi atas nama dosen. Dua lapis menahannya:
// DisallowUnknownFields menolak field `role` yang tidak dikenal, dan seandainya
// itu dilonggarkan suatu hari, Register tetap memaku perannya jadi 'student'.
func TestRegister_TidakBisaMenyetelRoleSendiri(t *testing.T) {
	h := serverLengkap(t)

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email":     emailUnik("penyusup"),
		"password":  "password-yang-cukup-panjang",
		"full_name": "Penyusup",
		"role":      "admin",
	}, "")

	if rec.Code == http.StatusCreated {
		t.Fatal("field role diterima saat register — akun bisa naik pangkat sendiri")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, mau 400", rec.Code)
	}
}

func TestRegister_EmailSudahTerdaftar(t *testing.T) {
	h := serverLengkap(t)
	body := map[string]string{
		"email":     emailUnik("kembar"),
		"password":  "password-yang-cukup-panjang",
		"full_name": "Orang Pertama",
	}

	if rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", body, ""); rec.Code != http.StatusCreated {
		t.Fatalf("register pertama gagal: %s", rec.Body.String())
	}

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", body, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, mau 409", rec.Code)
	}
	if kode := kodeError(t, rec); kode != "EMAIL_TAKEN" {
		t.Errorf("code = %q, mau EMAIL_TAKEN", kode)
	}
}

func TestRegister_PasswordTerlaluPendek(t *testing.T) {
	h := serverLengkap(t)

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email":     emailUnik("pendek"),
		"password":  "abc",
		"full_name": "Pendek",
	}, "")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, mau 422", rec.Code)
	}
}

// Login yang gagal harus memberi jawaban yang PERSIS SAMA, baik email itu
// terdaftar maupun tidak. Kalau berbeda, halaman login berubah jadi alat untuk
// mendata siapa saja yang punya akun di sistem ini.
func TestLogin_GagalTidakMembocorkanApakahEmailTerdaftar(t *testing.T) {
	h := serverLengkap(t)

	emailAda := emailUnik("ada")

	if rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email":     emailAda,
		"password":  "password-yang-cukup-panjang",
		"full_name": "Ada",
	}, ""); rec.Code != http.StatusCreated {
		t.Fatalf("register gagal: %s", rec.Body.String())
	}

	// email terdaftar, password salah
	a := kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": emailAda, "password": "password-yang-salah-sekali",
	}, "")
	// email tidak terdaftar sama sekali
	b := kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": emailUnik("tidakada"), "password": "password-yang-salah-sekali",
	}, "")

	if a.Code != http.StatusUnauthorized || b.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d dan %d, dua-duanya mau 401", a.Code, b.Code)
	}
	if a.Body.String() != b.Body.String() {
		t.Errorf("response berbeda — membocorkan email mana yang terdaftar:\n  terdaftar: %s\n  tidak ada: %s",
			a.Body.String(), b.Body.String())
	}
}

func TestMe_TanpaTokenDitolak(t *testing.T) {
	h := serverLengkap(t)

	rec := kirim(t, h, http.MethodGet, "/api/v1/me", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, mau 401", rec.Code)
	}
}
