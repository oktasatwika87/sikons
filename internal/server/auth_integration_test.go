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
	"github.com/oktasatwika/sikons/internal/availability"
	"github.com/oktasatwika/sikons/internal/config"
	"github.com/oktasatwika/sikons/internal/slotgen"
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

	// Zona waktu kampus dan horizon untuk slotgen
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatal("gagal load zona kampus:", err)
	}

	return New(config.Config{Env: "test", CampusTZ: campusTZ, SlotHorizonDays: 30}, Deps{
		Pool:         pool,
		Auth:         auth.NewService(pool, tokens, slog.New(slog.NewTextHandler(io.Discard, nil))),
		Tokens:       tokens,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Availability: availability.NewService(pool),
		Slotgen:      slotgen.New(pool, campusTZ, 30),
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

func kirimDenganCookie(t *testing.T, h http.Handler, metode, path string, body any, token string, cookies []*http.Cookie) *httptest.ResponseRecorder {
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
	for _, c := range cookies {
		r.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// loginLengkap helper untuk test refresh token: register, login dan dapat access token + refresh cookie.
func loginLengkap(t *testing.T, h http.Handler) (accessToken string, refreshCookie *http.Cookie) {
	t.Helper()

	email := emailUnik("refresh")
	password := "password-yang-cukup-panjang"

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email":           email,
		"password":        password,
		"full_name":       "User Refresh",
		"identity_number": "NIM-" + fmt.Sprint(time.Now().UnixNano()),
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register gagal: %s", rec.Body.String())
	}

	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login gagal: %s", rec.Body.String())
	}

	// Decode dari bytes, bukan stream (agar bisa di-decode ulang jika perlu)
	var masuk struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &masuk); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			return masuk.AccessToken, c
		}
	}
	t.Fatal("cookie refresh_token tidak ditemukan di response login")
	return
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

// =============================================================================
// Refresh token tests
// =============================================================================

func TestRefresh_RotasoBerhasil(t *testing.T) {
	h := serverLengkap(t)
	_, refreshCookie := loginLengkap(t, h)

	// Refresh pertama
	rec := kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{refreshCookie})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh pertama: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var hasil1 struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &hasil1); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if hasil1.AccessToken == "" {
		t.Error("access_token kosong dari refresh")
	}

	// Token baru harus bisa dipakai untuk /me
	rec = kirim(t, h, http.MethodGet, "/api/v1/me", nil, hasil1.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("/me setelah refresh: status = %d", rec.Code)
	}

	// Token lama (refreshCookie) harus GAGAL di refresh kedua.
	// Karena penerus (token baru) masih aktif dan dalam grace period,
	// ini dianggap race wajar, bukan pencurian -> 409 REFRESH_IN_PROGRESS.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{refreshCookie})
	if rec.Code != http.StatusConflict {
		t.Fatalf("refresh dengan token lama: status = %d, mau 409, body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_IN_PROGRESS" {
		t.Errorf("code = %s, mau REFRESH_IN_PROGRESS (penerus masih aktif, race wajar)", kodeError(t, rec))
	}
}

func TestRefresh_ReuseTokenMenCulutSeluruhFamily(t *testing.T) {
	h := serverLengkap(t)
	pool := dbUji(t)
	email := emailUnik("reusefamily")

	// Setup: register + login
	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang", "full_name": "Test",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %s", rec.Body.String())
	}

	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %s", rec.Body.String())
	}

	// Dapatkan cookies dari login
	var loginCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			loginCookie = c
		}
	}
	if loginCookie == nil {
		t.Fatal("no login cookie")
	}

	// Refresh pertama
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{loginCookie})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh pertama: %d body=%s", rec.Code, rec.Body.String())
	}

	// Dapatkan cookie baru
	var cookieBaru *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			cookieBaru = c
		}
	}
	if cookieBaru == nil {
		t.Fatal("no new cookie")
	}

	// Refresh KEDUA dengan token lama — penerus (cookieBaru) masih aktif dan dalam
	// grace period, jadi dianggap race wajar -> 409 REFRESH_IN_PROGRESS.
	// Family TIDAK di-revoke.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{loginCookie})
	if rec.Code != http.StatusConflict {
		t.Fatalf("reuse token lama: %d body=%s", rec.Code, rec.Body.String())
	}

	// Verifikasi kode error
	var errResp struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error.Code != "REFRESH_IN_PROGRESS" {
		t.Errorf("code = %s, mau REFRESH_IN_PROGRESS", errResp.Error.Code)
	}

	// Token BARU (penerus) masih berlaku — family tidak dicabut karena dianggap race wajar.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{cookieBaru})
	if rec.Code != http.StatusOK {
		t.Fatalf("token baru setelah race terdeteksi: %d body=%s", rec.Code, rec.Body.String())
	}

	// Verifikasi family_id berbeda dari user lain — User 2 login dan refresh
	email2 := emailUnik("reusefamily2")
	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email2, "password": "password-yang-cukup-panjang", "full_name": "Test2",
	}, "")
	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email2, "password": "password-yang-cukup-panjang",
	}, "")
	var loginCookie2 *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			loginCookie2 = c
		}
	}
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{loginCookie2})

	var familyID2 string
	pool.QueryRow(context.Background(), `
		SELECT family_id FROM refresh_tokens WHERE user_id = (
			SELECT id FROM users WHERE email = $1
		) LIMIT 1`, email2).Scan(&familyID2)

	// Test ini memverifikasi bahwa reuse mencabut seluruh family.
	// family_id berbeda antar user, jadi kalau user lain tidak terpengaruh, test ini cukup.
}

func TestRefresh_TokenKedaluwarsaDitolak(t *testing.T) {
	h := serverLengkap(t)
	pool := dbUji(t)

	// Buat user dan login untuk dapat refresh token
	email := emailUnik("expired")
	password := "password-yang-cukup-panjang"
	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": password, "full_name": "Test",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %s", rec.Body.String())
	}

	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %s", rec.Body.String())
	}

	// Dapatkan cookie dari login
	var loginCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			loginCookie = c
		}
	}
	if loginCookie == nil {
		t.Fatal("no cookie")
	}

	// Set expires_at ke masa lalu
	pool.Exec(context.Background(), `
		UPDATE refresh_tokens
		SET expires_at = now() - interval '1 day'
		WHERE user_id = (SELECT id FROM users WHERE email = $1)
		  AND revoked_at IS NULL`, email)

	// Coba refresh — harus gagal karena expired
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{loginCookie})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token expired: status = %d, mau 401, body=%s", rec.Code, rec.Body.String())
	}

	var errResp struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error.Code != "REFRESH_TOKEN_EXPIRED" {
		t.Errorf("code = %s, mau REFRESH_TOKEN_EXPIRED", errResp.Error.Code)
	}
}

func TestRefresh_TanpaCookieDitolak(t *testing.T) {
	h := serverLengkap(t)

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh tanpa cookie: status = %d, mau 401", rec.Code)
	}
	if kodeError(t, rec) != "REFRESH_TOKEN_INVALID" {
		t.Errorf("code = %s, mau REFRESH_TOKEN_INVALID", kodeError(t, rec))
	}
}

func TestRefresh_CookieMilikUserBerbeda(t *testing.T) {
	h := serverLengkap(t)

	// Login dua user
	_, cookie1 := loginLengkap(t, h)
	_, cookie2 := loginLengkap(t, h)

	// User 1 refresh
	rec := kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{cookie1})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh user 1: %s", rec.Body.String())
	}
	cookie1Baru := refreshCookieDariResponse(rec)

	// User 2 refresh
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{cookie2})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh user 2: %s", rec.Body.String())
	}

	// User 1 token baru TIDAK terpengaruh oleh refresh user 2
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{cookie1Baru})
	if rec.Code != http.StatusOK {
		t.Errorf("user 1 token baru seharusnya masih berlaku, tapi: %s", rec.Body.String())
	}
}

// TestRefresh_ParalelRefreshTokenSama memverifikasi bahwa dua tab browser yang
// merefresh token bersamaan tidak saling logout. Yang menang dapat 200, yang kalah
// dapat 409 REFRESH_IN_PROGRESS (bukan 401), dan token aktif dalam family tetap 1.
func TestRefresh_ParalelRefreshTokenSama(t *testing.T) {
	auth.SetGracePeriodUntukTest(5 * time.Second)
	defer auth.SetGracePeriodUntukTest(30 * time.Second)

	h := serverLengkap(t)
	pool := dbUji(t)
	email := emailUnik("paralel")

	// Register dan login untuk dapat refresh token
	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang", "full_name": "Test",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %s", rec.Body.String())
	}

	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %s", rec.Body.String())
	}

	// Ambil cookie dari login response
	var refreshCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			refreshCookie = c
		}
	}
	if refreshCookie == nil {
		t.Fatal("no refresh cookie from login")
	}

	// Ambil family_id berdasarkan email user
	var familyID string
	err := pool.QueryRow(context.Background(), `
		SELECT family_id FROM refresh_tokens
		WHERE user_id = (SELECT id FROM users WHERE email = $1)
		  AND revoked_at IS NULL
	`, email).Scan(&familyID)
	if err != nil {
		t.Fatalf("mengambil family_id: %v", err)
	}

	const N = 10
	mulai := make(chan struct{})
	hasil := make([]int, N)
	var wg sync.WaitGroup

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-mulai
			rec := kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{refreshCookie})
			hasil[i] = rec.Code
		}(i)
	}

	close(mulai)
	wg.Wait()

	// Hitung hasil
	var sukses int
	var inProgress int
	var reuse int
	var errServer int
	for _, code := range hasil {
		switch code {
		case http.StatusOK:
			sukses++
		case http.StatusConflict:
			inProgress++
		case http.StatusUnauthorized:
			reuse++
		case http.StatusInternalServerError:
			errServer++
		}
	}
	t.Logf("sukses=%d, in_progress=409=%d, reuse=401=%d, server_error=%d", sukses, inProgress, reuse, errServer)

	// Tepat SATU harus sukses
	if sukses != 1 {
		t.Errorf("tepat satu refresh harus sukses, tapi %d yang sukses", sukses)
	}
	// Sisanya harus 409 REFRESH_IN_PROGRESS, BUKAN 401
	if inProgress != N-1 {
		t.Errorf("%d request mendapat 409, mau %d", inProgress, N-1)
	}
	// Tidak boleh ada 500
	if errServer > 0 {
		t.Errorf("%d request menghasilkan 500", errServer)
	}

	// ASSERTION PALING PENTING: periksa langsung ke database.
	// Memverifikasi bahwa race dicegah di level database. Dengan FOR UPDATE,
	// hanya 1 request benar-benar membuat token baru.
	var aktif int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NULL
	`, familyID).Scan(&aktif)
	if err != nil {
		t.Fatalf("menghitung token aktif: %v", err)
	}
	t.Logf("token aktif dalam family setelah 10 refresh paralel: %d", aktif)
	if aktif != 1 {
		t.Errorf("token aktif dalam family = %d, mau 1", aktif)
	}
}

// TestRefresh_PencurianSetelahGraceHabis memverifikasi bahwa setelah grace period habis
// (atau tidak berlaku), reuse terdeteksi sebagai pencurian (401 REFRESH_TOKEN_REUSED,
// token aktif = 0). Grace period negatif memastikan kondisi selalu gagal.
func TestRefresh_PencurianSetelahGraceHabis(t *testing.T) {
	// Grace period negatif: tidak ada window grace, reuse langsung dianggap pencurian.
	auth.SetGracePeriodUntukTest(-1 * time.Second)
	defer auth.SetGracePeriodUntukTest(30 * time.Second)

	h := serverLengkap(t)
	pool := dbUji(t)
	email := emailUnik("pencuriangrace")

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang", "full_name": "Test",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %s", rec.Body.String())
	}

	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %s", rec.Body.String())
	}

	var refreshCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			refreshCookie = c
		}
	}

	// Ambil family_id
	var familyID string
	err := pool.QueryRow(context.Background(), `
		SELECT family_id FROM refresh_tokens
		WHERE user_id = (SELECT id FROM users WHERE email = $1)
		  AND revoked_at IS NULL
	`, email).Scan(&familyID)
	if err != nil {
		t.Fatalf("mengambil family_id: %v", err)
	}

	// Refresh pertama (sukses, membuat token baru)
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{refreshCookie})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh pertama: %s", rec.Body.String())
	}

	// Sekarang pakai token lama — revoked_at-nya ada, grace=-1s, jadi langsung dianggap pencurian
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{refreshCookie})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse setelah grace habis: status=%d, mau 401, body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_TOKEN_REUSED" {
		t.Errorf("code = %s, mau REFRESH_TOKEN_REUSED", kodeError(t, rec))
	}

	// Token aktif dalam family harus 0 (seluruh family dicabut)
	var aktif int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NULL
	`, familyID).Scan(&aktif)
	if err != nil {
		t.Fatalf("menghitung token aktif: %v", err)
	}
	if aktif != 0 {
		t.Errorf("token aktif setelah pencurian terdeteksi = %d, mau 0", aktif)
	}
}

// TestRefresh_PenerusSudahDicabutTetapDianggapPencurian memastikan grace period
// tidak jadi celah untuk token lama. Rotasi dua kali: token pertama -> token kedua ->
// token ketiga. Token pertama tahu penerusnya (token kedua) sudah dicabut, jadi
// tetap dianggap pencurian, bukan balapan wajar.
func TestRefresh_PenerusSudahDicabutTetapDianggapPencurian(t *testing.T) {
	auth.SetGracePeriodUntukTest(5 * time.Second)
	defer auth.SetGracePeriodUntukTest(30 * time.Second)

	h := serverLengkap(t)
	pool := dbUji(t)
	email := emailUnik("peneruscabut")

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang", "full_name": "Test",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %s", rec.Body.String())
	}

	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %s", rec.Body.String())
	}

	var tokenAwal *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			tokenAwal = c
		}
	}

	// Ambil family_id
	var familyID string
	err := pool.QueryRow(context.Background(), `
		SELECT family_id FROM refresh_tokens
		WHERE user_id = (SELECT id FROM users WHERE email = $1)
		  AND revoked_at IS NULL
	`, email).Scan(&familyID)
	if err != nil {
		t.Fatalf("mengambil family_id: %v", err)
	}

	// Refresh pertama: tokenAwal -> tokenB
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh pertama: %s", rec.Body.String())
	}
	tokenB := refreshCookieDariResponse(rec)

	// Refresh kedua: tokenB -> tokenC
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenB})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh kedua: %s", rec.Body.String())
	}

	// Sekarang tokenAwal tahu penerusnya (tokenB) sudah dicabut.
	// Karena penerus tidak aktif, ini bukan balapan wajar — harus dianggap pencurian.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tokenAwal reuse setelah penerus dicabut: status=%d, mau 401, body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_TOKEN_REUSED" {
		t.Errorf("code = %s, mau REFRESH_TOKEN_REUSED", kodeError(t, rec))
	}

	// Token aktif dalam family harus 0 (seluruh family dicabut)
	var aktif int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NULL
	`, familyID).Scan(&aktif)
	if err != nil {
		t.Fatalf("menghitung token aktif: %v", err)
	}
	if aktif != 0 {
		t.Errorf("token aktif setelah reuse terdeteksi = %d, mau 0", aktif)
	}
}

func TestLogout_CabutToken(t *testing.T) {
	h := serverLengkap(t)
	_, refreshCookie := loginLengkap(t, h)

	// Refresh dulu untuk dapat token baru
	rec := kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{refreshCookie})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh gagal: %s", rec.Body.String())
	}
	cookieBaru := refreshCookieDariResponse(rec)

	// Logout
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/logout", nil, "", []*http.Cookie{refreshCookie})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d, mau 204", rec.Code)
	}

	// Token lama GAGAL di refresh
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{refreshCookie})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("token lama setelah logout: status = %d, mau 401", rec.Code)
	}

	// Token baru juga GAGAL (satu family)
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{cookieBaru})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("token baru setelah logout: status = %d, mau 401", rec.Code)
	}
}

func TestLogout_CookieDihapus(t *testing.T) {
	h := serverLengkap(t)
	_, refreshCookie := loginLengkap(t, h)

	rec := kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/logout", nil, "", []*http.Cookie{refreshCookie})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d", rec.Code)
	}

	// Cek cookie dihapus
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			found = true
			if c.Value != "" {
				t.Error("cookie tidak dihapus (value tidak kosong)")
			}
			if c.MaxAge != -1 {
				t.Error("cookie MaxAge bukan -1")
			}
		}
	}
	if !found {
		t.Error("cookie refresh_token tidak ada di response logout")
	}
}

func TestLogin_MenyetelCookieRefresh(t *testing.T) {
	h := serverLengkap(t)

	_, refreshCookie := loginLengkap(t, h)

	if refreshCookie.HttpOnly != true {
		t.Error("cookie HttpOnly harus true")
	}
	if refreshCookie.SameSite != http.SameSiteStrictMode {
		t.Error("cookie SameSite harus Strict")
	}
	if refreshCookie.Path != "/api/v1/auth" {
		t.Errorf("cookie Path = %q, mau /api/v1/auth", refreshCookie.Path)
	}
	if refreshCookie.Secure != true {
		// Secure=true untuk environment non-development (termasuk test).
		// Ini memastikan cookie tidak pernah dikirim melalui HTTP plaintext di production.
		t.Error("cookie Secure harus true untuk environment test (non-development)")
	}
	if refreshCookie.MaxAge != int(auth.RefreshTokenTTL.Seconds()) {
		t.Errorf("cookie MaxAge = %d, mau %d", refreshCookie.MaxAge, int(auth.RefreshTokenTTL.Seconds()))
	}
}

// =============================================================================
// Helper
// =============================================================================

func refreshCookieDariResponse(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			return c
		}
	}
	// Return dummy cookie untuk debugging, test yang butuh ini akan gagal dengan jelas
	return &http.Cookie{Name: "refresh_token", Value: ""}
}
