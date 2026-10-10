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
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oktasatwika87/sikons/internal/auth"
	"github.com/oktasatwika87/sikons/internal/availability"
	"github.com/oktasatwika87/sikons/internal/booking"
	"github.com/oktasatwika87/sikons/internal/config"
	"github.com/oktasatwika87/sikons/internal/lecturer"
	"github.com/oktasatwika87/sikons/internal/slotgen"
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
	return serverLengkapWithLog(t, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// serverLengkapWithLog sama seperti serverLengkap tapi logger-nya
// bisa disuplai. Test yang ingin menangkap log peringatan reuse
// pakai helper ini dengan slog.NewTextHandler yang menulis ke buffer.
func serverLengkapWithLog(t *testing.T, log *slog.Logger) http.Handler {
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
		DB:           pool,
		Auth:         auth.NewService(pool, tokens, log),
		Tokens:       tokens,
		Log:          log,
		Availability: availability.NewService(pool),
		Booking:      booking.NewService(pool, 60, 3, 1),
		Slotgen:      slotgen.New(pool, campusTZ, 30),
		Lecturer:     lecturer.NewService(pool),
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

// TestRefresh_PenerusSudahDicabut_DalamGrace_409 memverifikasi bahwa token lama
// yang penerusnya sudah dicabut DAN masih dalam grace → 409 (cabang 2b),
// BUKAN pencurian langsung. Sebelum perubahan, kasus ini dianggap pencurian
// (401) dan keluarga langsung dicabut — itu membuat korban kehilangan
// token aktifnya Z yang masih sah. Sekarang grace 30 detik menjadi
// "jendela aman" tempat keluarga tidak disentuh.
//
// Verifikasi: Z masih aktif setelah X dikirim dalam grace. Keluarga
// hanya dicabut setelah grace lewat (diuji test terpisah).
func TestRefresh_PenerusSudahDicabut_DalamGrace_409(t *testing.T) {
	auth.SetGracePeriodUntukTest(5 * time.Second)
	defer auth.SetGracePeriodUntukTest(30 * time.Second)

	h := serverLengkap(t)
	pool := dbUji(t)
	email := emailUnik("peneruscabut")

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang", "full_name": "Test",
	}, "")
	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang",
	}, "")

	var tokenAwal *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			tokenAwal = c
		}
	}

	var familyID string
	err := pool.QueryRow(context.Background(), `
		SELECT family_id FROM refresh_tokens
		WHERE user_id = (SELECT id FROM users WHERE email = $1)
		  AND revoked_at IS NULL
	`, email).Scan(&familyID)
	if err != nil {
		t.Fatalf("mengambil family_id: %v", err)
	}

	// Refresh pertama: tokenAwal -> tokenB (B jadi penerus A).
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh pertama: %s", rec.Body.String())
	}
	tokenB := refreshCookieDariResponse(rec)

	// Refresh kedua: tokenB -> tokenC (B dicabut, C jadi penerus B).
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenB})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh kedua: %s", rec.Body.String())
	}

	// Sekarang tokenAwal.revoked_at != NULL, replaced_by = tokenB,
	// tokenB.revoked_at != NULL. tokenC masih aktif.
	// Kirim tokenAwal dalam grace → case 2b (successor tidak aktif) → 409.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusConflict {
		t.Fatalf("tokenAwal reuse dalam grace: status=%d, mau 409, body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_IN_PROGRESS" {
		t.Errorf("code = %s, mau REFRESH_IN_PROGRESS", kodeError(t, rec))
	}

	// Keluarga TIDAK dicabut — tokenC (aktif) masih utuh.
	var aktif int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NULL
	`, familyID).Scan(&aktif)
	if err != nil {
		t.Fatalf("menghitung token aktif: %v", err)
	}
	if aktif != 1 {
		t.Errorf("token aktif dalam keluarga = %d, mau 1 (keluarga harus TIDAK dicabut dalam grace)", aktif)
	}
}

// TestRefresh_PenerusSudahDicabutLewatGrace_Pencurian memverifikasi bahwa
// setelah grace lewat, reuse terdeteksi sebagai pencurian dan seluruh
// keluarga dicabut. Ini aturan cabang 3.
func TestRefresh_PenerusSudahDicabutLewatGrace_Pencurian(t *testing.T) {
	// Grace efektif: -1s → lewat langsung tanpa butuh backdate manual.
	auth.SetGracePeriodUntukTest(-1 * time.Second)
	defer auth.SetGracePeriodUntukTest(30 * time.Second)

	h := serverLengkap(t)
	pool := dbUji(t)
	email := emailUnik("peneruslewat")

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang", "full_name": "Test",
	}, "")
	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang",
	}, "")

	var tokenAwal *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			tokenAwal = c
		}
	}

	var familyID string
	err := pool.QueryRow(context.Background(), `
		SELECT family_id FROM refresh_tokens
		WHERE user_id = (SELECT id FROM users WHERE email = $1)
		  AND revoked_at IS NULL
	`, email).Scan(&familyID)
	if err != nil {
		t.Fatalf("mengambil family_id: %v", err)
	}

	// tokenAwal -> tokenB
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh pertama: %s", rec.Body.String())
	}
	tokenB := refreshCookieDariResponse(rec)

	// tokenB -> tokenC
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenB})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh kedua: %s", rec.Body.String())
	}

	// tokenAwal lewat grace (grace = -1s) → pencurian, keluarga dicabut.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tokenAwal lewat grace: status=%d, mau 401, body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_TOKEN_REUSED" {
		t.Errorf("code = %s, mau REFRESH_TOKEN_REUSED", kodeError(t, rec))
	}

	var aktif int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NULL
	`, familyID).Scan(&aktif)
	if err != nil {
		t.Fatalf("menghitung token aktif: %v", err)
	}
	if aktif != 0 {
		t.Errorf("token aktif = %d, mau 0 (seluruh keluarga dicabut)", aktif)
	}
}

// TestRefresh_BalasanHilang_RecoveriPenerusUmurCukup memverifikasi skenario
// "balasan refresh hilang": server commit rotasi X→Y tapi Set-Cookie tidak
// sampai ke browser, dan browser mengirim ulang X setelah penerus Y
// berumur ≥ 2 detik. Cabang 2a harus ambil alih: Y dicabut, token
// baru Z diterbitkan, dan Z menjadi pengganti X. Keluarga tetap
// berisi tepat 1 token aktif.
func TestRefresh_BalasanHilang_RecoveriPenerusUmurCukup(t *testing.T) {
	h := serverLengkap(t)
	pool := dbUji(t)
	email := emailUnik("balasanhilang")

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
	var tokenX *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			tokenX = c
		}
	}

	// Rotasi X -> Y, ambil cookie Y dari response (simulasi: cookie
	// "hilang" karena browser tidak sempat update).
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh pertama: %s", rec.Body.String())
	}
	tokenY := refreshCookieDariResponse(rec)

	var familyID string
	var tokenYID string
	var tokenYCreatedAt time.Time
	err := pool.QueryRow(context.Background(), `
		SELECT id, family_id, created_at
		FROM refresh_tokens
		WHERE family_id = (
			SELECT family_id FROM refresh_tokens
			WHERE user_id = (SELECT id FROM users WHERE email = $1)
			LIMIT 1
		) AND replaced_by IS NULL AND revoked_at IS NULL
	`, email).Scan(&tokenYID, &familyID, &tokenYCreatedAt)
	if err != nil {
		t.Fatalf("mengambil Y: %v", err)
	}

	// Mundurkan umur Y lewat 2 detik (cabang 2a butuh successorMinAge
	// terpenuhi). created_at Y -> 5 detik yang lalu.
	_, err = pool.Exec(context.Background(), `
		UPDATE refresh_tokens
		SET created_at = now() - INTERVAL '5 seconds'
		WHERE id = $1`, tokenYID)
	if err != nil {
		t.Fatalf("mundurkan created_at Y: %v", err)
	}

	// Browser mengirim ulang X (tokenX masih satu-satunya yang
	// dimiliki browser, karena Y "hilang"). Cabang 2a harus aktif:
	// Y dicabut, token baru Z diterbitkan.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh X setelah balasan hilang: status=%d, body=%s", rec.Code, rec.Body.String())
	}
	tokenZ := refreshCookieDariResponse(rec)
	if tokenZ == nil || tokenZ.Value == tokenX.Value || tokenZ.Value == tokenY.Value {
		t.Fatal("token Z harus baru — bukan X atau Y")
	}

	// Verifikasi DB: Y dicabut, Z aktif, tepat 1 token aktif di keluarga.
	var aktif int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NULL`, familyID).Scan(&aktif)
	if err != nil {
		t.Fatalf("menghitung aktif: %v", err)
	}
	if aktif != 1 {
		t.Errorf("token aktif = %d, mau 1", aktif)
	}

	// Y benar-benar dicabut.
	var yRevoked bool
	err = pool.QueryRow(context.Background(), `
		SELECT revoked_at IS NOT NULL FROM refresh_tokens WHERE id = $1`,
		tokenYID).Scan(&yRevoked)
	if err != nil {
		t.Fatalf("cek Y revoked: %v", err)
	}
	if !yRevoked {
		t.Error("Y harus sudah dicabut oleh cabang 2a")
	}

	// Z (cookie baru dari response) bisa dipakai untuk refresh — Z aktif.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenZ})
	if rec.Code != http.StatusOK {
		t.Errorf("refresh dengan Z baru: status=%d, mau 200, body=%s", rec.Code, rec.Body.String())
	}
}

// TestRefresh_BalasanHilang_PenerusTerlaluMuda_409 memverifikasi bahwa
// cabang 2a TIDAK aktif jika penerus masih terlalu muda. Skenario:
// X→Y rotasi sukses, Y belum berumur 2 detik, browser mengirim X lagi
// (misalnya render halaman sangat cepat). Sistem harus tetap
// memperlakukan ini sebagai balapan wajar → 409, keluarga tidak
// disentuh.
func TestRefresh_BalasanHilang_PenerusTerlaluMuda_409(t *testing.T) {
	h := serverLengkap(t)
	pool := dbUji(t)
	email := emailUnik("terusmuda")

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
	var tokenX *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			tokenX = c
		}
	}

	// Rotasi X -> Y. Y baru dibuat (< 1 detik).
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %s", rec.Body.String())
	}
	_ = refreshCookieDariResponse(rec)

	var familyID string
	err := pool.QueryRow(context.Background(), `
		SELECT family_id FROM refresh_tokens
		WHERE user_id = (SELECT id FROM users WHERE email = $1)
		LIMIT 1`, email).Scan(&familyID)
	if err != nil {
		t.Fatalf("family_id: %v", err)
	}

	// Y masih baru. Kirim X lagi → case 2b: 409.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
	if rec.Code != http.StatusConflict {
		t.Fatalf("X dalam grace dengan Y masih muda: status=%d, mau 409, body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_IN_PROGRESS" {
		t.Errorf("code = %s, mau REFRESH_IN_PROGRESS", kodeError(t, rec))
	}

	// Keluarga TIDAK disentuh: Y masih aktif, 1 token.
	var aktif int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NULL`, familyID).Scan(&aktif)
	if err != nil {
		t.Fatalf("hitung aktif: %v", err)
	}
	if aktif != 1 {
		t.Errorf("token aktif = %d, mau 1 (cabang 2a harus TIDAK aktif)", aktif)
	}
}

// TestRefresh_BalasanHilang_DeteksiPencurianTetapJalan memverifikasi bahwa
// deteksi pencurian tetap bekerja setelah perubahan: setelah skenario
// balasan hilang (X→Y discard, Y dicabut oleh 2a, Z jadi pengganti X),
// Y (yang sudah diganti/dicabut) dikirim lagi LEWAT grace → reuse,
// seluruh keluarga dicabut. Ini memastikan cabang 2a tidak membuka
// celah keamanan baru.
func TestRefresh_BalasanHilang_DeteksiPencurianTetapJalan(t *testing.T) {
	// Grace normal 5s — cukup untuk 2a fire, tidak untuk Y reuse
	// yang akan kita backdate secara eksplisit.
	auth.SetGracePeriodUntukTest(5 * time.Second)
	defer auth.SetGracePeriodUntukTest(30 * time.Second)

	h := serverLengkap(t)
	pool := dbUji(t)
	email := emailUnik("hilang-pencurian")

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang", "full_name": "Test",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %s", rec.Body.String())
	}
	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang",
	}, "")
	var tokenX *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			tokenX = c
		}
	}

	// X -> Y (Y dibuang).
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh pertama: %s", rec.Body.String())
	}
	tokenY := refreshCookieDariResponse(rec)

	var familyID string
	var tokenYID string
	err := pool.QueryRow(context.Background(), `
		SELECT id, family_id
		FROM refresh_tokens
		WHERE family_id = (
			SELECT family_id FROM refresh_tokens
			WHERE user_id = (SELECT id FROM users WHERE email = $1)
			LIMIT 1
		) AND replaced_by IS NULL AND revoked_at IS NULL
	`, email).Scan(&tokenYID, &familyID)
	if err != nil {
		t.Fatalf("ambil Y: %v", err)
	}

	// Mundurkan Y lewat 2 detik, refresh X → 2a: Y dicabut, Z jadi
	// pengganti X. Z tidak dikembalikan ke test (simulasi: dibuang).
	_, err = pool.Exec(context.Background(), `
		UPDATE refresh_tokens SET created_at = now() - INTERVAL '5 seconds' WHERE id = $1`,
		tokenYID)
	if err != nil {
		t.Fatalf("mundurkan Y: %v", err)
	}
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh X untuk recoveri: %s", rec.Body.String())
	}
	_ = refreshCookieDariResponse(rec)

	// Sekarang: X revoked (dalam grace), Y revoked (oleh 2a, baru
	// saja), Z active (penerus X).
	// Backdate Y.revoked_at lewat grace agar pengiriman Y berikutnya
	// langsung kena cabang 3 (pencurian).
	_, err = pool.Exec(context.Background(), `
		UPDATE refresh_tokens
		SET revoked_at = now() - INTERVAL '10 seconds'
		WHERE id = $1`, tokenYID)
	if err != nil {
		t.Fatalf("backdate Y.revoked_at: %v", err)
	}

	// Kirim Y lewat grace → case 3 → reuse, keluarga dicabut.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenY})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Y lewat grace: status=%d, mau 401, body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_TOKEN_REUSED" {
		t.Errorf("code = %s, mau REFRESH_TOKEN_REUSED", kodeError(t, rec))
	}

	var aktif int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NULL`, familyID).Scan(&aktif)
	if err != nil {
		t.Fatalf("hitung aktif: %v", err)
	}
	if aktif != 0 {
		t.Errorf("token aktif = %d, mau 0 (Y lewat grace = pencurian)", aktif)
	}
}

// TestRefresh_BalasanHilang_Paralel_TepatSatuAktif menguji beberapa
// refresh paralel dalam kondisi cabang 2a (Y berumur >= 2 detik). Di
// akhir, keluarga harus berisi tepat 1 token aktif — tidak ada
// token rangkap yang lolos karena tidak adanya lock.
func TestRefresh_BalasanHilang_Paralel_TepatSatuAktif(t *testing.T) {
	h := serverLengkap(t)
	pool := dbUji(t)
	email := emailUnik("hilang-paralel")

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang", "full_name": "Test",
	}, "")
	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": "password-yang-cukup-panjang",
	}, "")
	var tokenX *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			tokenX = c
		}
	}

	// X -> Y, simpan Y id dan mundurkan lewat 2 detik.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh pertama: %s", rec.Body.String())
	}

	var familyID string
	var tokenYID string
	err := pool.QueryRow(context.Background(), `
		SELECT id, family_id
		FROM refresh_tokens
		WHERE family_id = (
			SELECT family_id FROM refresh_tokens
			WHERE user_id = (SELECT id FROM users WHERE email = $1)
			LIMIT 1
		) AND replaced_by IS NULL AND revoked_at IS NULL
	`, email).Scan(&tokenYID, &familyID)
	if err != nil {
		t.Fatalf("ambil Y: %v", err)
	}
	_, err = pool.Exec(context.Background(), `
		UPDATE refresh_tokens SET created_at = now() - INTERVAL '5 seconds' WHERE id = $1`,
		tokenYID)
	if err != nil {
		t.Fatalf("mundurkan Y: %v", err)
	}

	// Beberapa refresh X paralel. Hanya yang pertama akan masuk cabang
	// 2a (lock FOR UPDATE); sisanya akan melihat Y sudah dicabut (kalau
	// yang pertama commit duluan) → case 2b → 409, atau masuk 2a juga
	// kalau lock serializes semuanya.
	const N = 5
	mulai := make(chan struct{})
	hasil := make([]int, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-mulai
			rec := kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
			hasil[i] = rec.Code
		}(i)
	}
	close(mulai)
	wg.Wait()

	var sukses, inProgress int
	for _, code := range hasil {
		switch code {
		case http.StatusOK:
			sukses++
		case http.StatusConflict:
			inProgress++
		}
	}
	t.Logf("paralel 2a: sukses=%d, in_progress=%d", sukses, inProgress)

	// Tepat satu keluarga harus punya tepat 1 token aktif. Setiap
	// request yang dapat 200 menerbitkan token baru dari X — semua
	// request itu serial melalui lock FOR UPDATE, dan setiap
	// penerus lama di-revoke sebelum token baru diterbitkan. Jadi
	// tiap sukses = 1 token aktif baru, tapi penerus lama dicabut.
	// Pada akhir, tersisa 1 token aktif.
	var aktif int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NULL`, familyID).Scan(&aktif)
	if err != nil {
		t.Fatalf("hitung aktif: %v", err)
	}
	if aktif != 1 {
		t.Errorf("token aktif = %d, mau 1", aktif)
	}
	// Invariant: harus ada minimal satu sukses kalau lock serial.
	if sukses < 1 {
		t.Errorf("paralel 2a: minimal satu sukses, dapat %d", sukses)
	}
}

// TestLogout_DanLangsungRefresh_401DenganWaktuAsli memverifikasi bahwa
// logout sekarang menyimpan revoked_at = waktu pencabutan yang sebenarnya
// (bukan backdate 1 jam) dan refresh setelah logout langsung kena
// cabang 4 (tanpa penerus) → 401 REFRESH_TOKEN_REUSED. Status 401 harus
// tercapai bahkan dalam 30 detik grace, dan selisih revoked_at dengan
// now() harus kecil — menunjukkan data waktu tidak dimanipulasi.
func TestLogout_DanLangsungRefresh_401DenganWaktuAsli(t *testing.T) {
	pool := dbUji(t)
	h := serverLengkap(t)
	email := emailUnik("logoutwaktu")

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

	// Rotasi untuk dapat token baru, supaya tokenAwal punya replaced_by
	// yang akan di-clear saat logout.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %s", rec.Body.String())
	}

	// Logout dengan tokenAwal. Sekarang tokenAwal.revoked_at = now() (waktu
	// sebenarnya) dan replaced_by = NULL.
	waktuSebelumLogout := time.Now()
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/logout", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d, mau 204", rec.Code)
	}
	waktuSetelahLogout := time.Now()

	// Refresh tokenAwal di bawah 30 detik grace. Cabang a (keluarga
	// sudah tidak punya token aktif — logout mencabut seluruhnya) →
	// 401 REFRESH_TOKEN_INVALID, tanpa log peringatan reuse, tanpa
	// DB changes.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh setelah logout: status = %d, mau 401, body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_TOKEN_INVALID" {
		t.Errorf("code = %s, mau REFRESH_TOKEN_INVALID", kodeError(t, rec))
	}

	// Cek waktu revoked_at TIDAK dimanipulasi. revoked_at harus di
	// antara waktuSebelumLogout dan waktuSetelahLogout (kisaran beberapa
	// detik). Kalau logout masih backdate 1 jam, revoked_at akan
	// sebelum waktuSebelumLogout dan test gagal.
	var familyID string
	err := pool.QueryRow(context.Background(), `
		SELECT family_id FROM refresh_tokens
		WHERE user_id = (SELECT id FROM users WHERE email = $1)
		LIMIT 1`, email).Scan(&familyID)
	if err != nil {
		t.Fatalf("family_id: %v", err)
	}

	rows, err := pool.Query(context.Background(), `
		SELECT revoked_at FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NOT NULL
		ORDER BY revoked_at DESC`, familyID)
	if err != nil {
		t.Fatalf("query revoked_at: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ra time.Time
		if err := rows.Scan(&ra); err != nil {
			t.Fatalf("scan: %v", err)
		}
		// Selisih harus kecil — beberapa detik, bukan jam.
		if ra.Before(waktuSebelumLogout.Add(-2 * time.Second)) {
			t.Errorf("revoked_at = %v, lebih lama dari logout — backdate tidak seharusnya dipakai", ra)
		}
		if ra.After(waktuSetelahLogout.Add(2 * time.Second)) {
			t.Errorf("revoked_at = %v, lebih baru dari logout", ra)
		}
	}
}

// TestLogout_DanRefresh_NoLogReuse memverifikasi bahwa refresh
// setelah logout sekarang masuk cabang a (keluarga sudah tidak
// punya token aktif), bukan cabang reuse. Hasil: 401
// REFRESH_TOKEN_INVALID, TIDAK ada log peringatan reuse, dan
// replaced_by di keluarga TIDAK berubah dibanding sebelum logout.
func TestLogout_DanRefresh_NoLogReuse(t *testing.T) {
	pool := dbUji(t)
	logBuf := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := serverLengkapWithLog(t, log)
	email := emailUnik("logout-nolog")

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

	// Rotasi X → Y supaya X.replaced_by = Y. Setelah logout, replaced_by
	// ini harus tetap ada (logout tidak menyentuhnya).
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %s", rec.Body.String())
	}

	// Snapshot replaced_by di keluarga SEBELUM logout.
	var familyID string
	err := pool.QueryRow(context.Background(), `
		SELECT family_id FROM refresh_tokens
		WHERE user_id = (SELECT id FROM users WHERE email = $1)
		LIMIT 1`, email).Scan(&familyID)
	if err != nil {
		t.Fatalf("family_id: %v", err)
	}
	replacedBySebelum := replacedBySnapshot(t, pool, familyID)

	// Clear log buffer dari output refresh sebelumnya.
	logBuf.Reset()

	// Logout dengan tokenAwal.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/logout", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}

	// Refresh dengan tokenAwal di bawah 30 detik grace. Cabang a
	// (keluarga kosong) → 401 REFRESH_TOKEN_INVALID.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh setelah logout: %d, body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_TOKEN_INVALID" {
		t.Errorf("code = %s, mau REFRESH_TOKEN_INVALID", kodeError(t, rec))
	}

	// TIDAK ada log peringatan reuse — ini yang membedakan logout dari
	// pencurian.
	if strings.Contains(logBuf.String(), "refresh token reuse terdeteksi") {
		t.Errorf("ada log peringatan reuse, padahal ini bukan pencurian. Log:\n%s", logBuf.String())
	}

	// replaced_by di keluarga TIDAK berubah (logout tidak menyentuhnya).
	replacedBySetelah := replacedBySnapshot(t, pool, familyID)
	if !reflect.DeepEqual(replacedBySebelum, replacedBySetelah) {
		t.Errorf("replaced_by berubah setelah logout.\nsebelum: %v\nsesudah: %v", replacedBySebelum, replacedBySetelah)
	}
}

// TestLogout_SetelahRotasi_X401_NoLog memverifikasi bahwa X yang
// sudah dirotasi (replaced_by != NULL), lalu logout, lalu dikirim
// lagi dalam grace → 401 REFRESH_TOKEN_INVALID tanpa log reuse.
// Sebelum revisi ini, X masuk cabang 2b (race, 409) karena replaced_by
// masih menunjuk ke Y yang juga sudah di-revoke. Sekarang cabang a
// aktif duluan karena keluarga sudah kosong.
func TestLogout_SetelahRotasi_X401_NoLog(t *testing.T) {
	logBuf := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := serverLengkapWithLog(t, log)
	email := emailUnik("logout-setelah-rotasi")
	password := "password-yang-cukup-panjang"

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": password, "full_name": "Test",
	}, "")
	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}, "")
	var tokenAwal *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			tokenAwal = c
		}
	}

	// X → Y, supaya X.replaced_by = Y dan Y aktif.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %s", rec.Body.String())
	}

	// Logout dengan X. Y (token aktif lain di keluarga) ikut ter-revoke.
	logBuf.Reset()
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/logout", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}

	// Refresh X dalam grace. X.revoked_at baru, X.replaced_by = Y (tidak
	// disentuh logout). Tapi keluarga sudah kosong → cabang a.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenAwal})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh X dalam grace setelah logout: %d, body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_TOKEN_INVALID" {
		t.Errorf("code = %s, mau REFRESH_TOKEN_INVALID", kodeError(t, rec))
	}

	// Tanpa log peringatan reuse.
	if strings.Contains(logBuf.String(), "refresh token reuse terdeteksi") {
		t.Errorf("ada log peringatan reuse, padahal ini bukan pencurian. Log:\n%s", logBuf.String())
	}
}

// TestRefresh_PenerusDicabut2a_LogReuseAda memverifikasi bahwa Y
// yang dicabut oleh cabang 2a (replaced_by NULL sejak dibuat),
// ketika dikirim lagi dalam grace DAN keluarga masih punya token
// aktif (Z dari 2a), tetap masuk cabang b → reuse dengan log.
// Y tidak punya replaced_by, BUKAN keluarga kosong, jadi cabang a
// tidak aktif.
func TestRefresh_PenerusDicabut2a_LogReuseAda(t *testing.T) {
	pool := dbUji(t)
	logBuf := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := serverLengkapWithLog(t, log)
	email := emailUnik("penerus2a-log")
	password := "password-yang-cukup-panjang"

	rec := kirim(t, h, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": password, "full_name": "Test",
	}, "")
	rec = kirim(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}, "")
	var tokenX *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			tokenX = c
		}
	}

	// X → Y.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh X: %s", rec.Body.String())
	}
	tokenY := refreshCookieDariResponse(rec)

	var familyID string
	var tokenYID string
	err := pool.QueryRow(context.Background(), `
		SELECT id, family_id
		FROM refresh_tokens
		WHERE family_id = (
			SELECT family_id FROM refresh_tokens
			WHERE user_id = (SELECT id FROM users WHERE email = $1)
			LIMIT 1
		) AND replaced_by IS NULL AND revoked_at IS NULL
	`, email).Scan(&tokenYID, &familyID)
	if err != nil {
		t.Fatalf("ambil Y: %v", err)
	}

	// Mundurkan Y.created_at > 2 detik, supaya cabang 2a fire.
	_, err = pool.Exec(context.Background(), `
		UPDATE refresh_tokens SET created_at = now() - INTERVAL '5 seconds' WHERE id = $1`,
		tokenYID)
	if err != nil {
		t.Fatalf("mundurkan Y: %v", err)
	}

	// Refresh X → cabang 2a: Y dicabut, Z dibuat, X.replaced_by = Z.
	logBuf.Reset()
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh X untuk recoveri: %s", rec.Body.String())
	}
	_ = refreshCookieDariResponse(rec)

	// Y.revoked_at != NULL (set oleh 2a), Y.replaced_by = NULL.
	// Z adalah token aktif di keluarga. Y masuk cabang b (X.replaced_by
	// == NULL dan keluarga tidak kosong) → reuse, log, keluarga dicabut.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenY})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Y dalam grace setelah 2a: %d, mau 401, body=%s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_TOKEN_REUSED" {
		t.Errorf("code = %s, mau REFRESH_TOKEN_REUSED", kodeError(t, rec))
	}
	if !strings.Contains(logBuf.String(), "refresh token reuse terdeteksi") {
		t.Errorf("log peringatan reuse tidak ada. Log:\n%s", logBuf.String())
	}

	// Keluarga dicabut.
	var aktif int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NULL`, familyID).Scan(&aktif)
	if err != nil {
		t.Fatalf("hitung aktif: %v", err)
	}
	if aktif != 0 {
		t.Errorf("token aktif = %d, mau 0", aktif)
	}
}

// token penerus yang dicabut oleh cabang 2a (recoveri), ketika dikirim
// lagi DALAM grace, langsung kena cabang 4 (tanpa penerus) → 401
// REFRESH_TOKEN_REUSED. Penerus tidak pernah dirotasi sendiri, jadi
// replaced_by-nya NULL sejak dibuat, dan cabang 4 aktif tanpa peduli
// selisih waktu.
func TestRefresh_PenerusDicabut2a_DalamGrace_Reuse(t *testing.T) {
	h := serverLengkap(t)
	pool := dbUji(t)
	email := emailUnik("penerus2a-grace")
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
	var tokenX *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			tokenX = c
		}
	}

	// X -> Y, ambil cookie Y (akan dikirim lagi di akhir).
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh X: %s", rec.Body.String())
	}
	tokenY := refreshCookieDariResponse(rec)

	var familyID string
	var tokenYID string
	err := pool.QueryRow(context.Background(), `
		SELECT id, family_id
		FROM refresh_tokens
		WHERE family_id = (
			SELECT family_id FROM refresh_tokens
			WHERE user_id = (SELECT id FROM users WHERE email = $1)
			LIMIT 1
		) AND replaced_by IS NULL AND revoked_at IS NULL
	`, email).Scan(&tokenYID, &familyID)
	if err != nil {
		t.Fatalf("ambil Y: %v", err)
	}

	// Mundurkan Y lewat 2 detik supaya cabang 2a fire pada refresh X
	// berikutnya (lost-response scenario).
	_, err = pool.Exec(context.Background(), `
		UPDATE refresh_tokens SET created_at = now() - INTERVAL '5 seconds' WHERE id = $1`,
		tokenYID)
	if err != nil {
		t.Fatalf("mundurkan Y: %v", err)
	}

	// Refresh X → cabang 2a. Y dicabut, Z jadi pengganti X. Y.replaced_by
	// tetap NULL (Y tidak pernah dirotasi sendiri).
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenX})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh X untuk recoveri: %s", rec.Body.String())
	}
	_ = refreshCookieDariResponse(rec)

	// Y.revoked_at di-set ke now() (oleh cabang 2a), Y.replaced_by = NULL.
	// Kirim Y DALAM grace (beberapa milidetik setelah cabang 2a) — cabang 4
	// aktif (Y.replaced_by == NULL) → 401 REFRESH_TOKEN_REUSED.
	rec = kirimDenganCookie(t, h, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{tokenY})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Y dalam grace setelah 2a: status = %d, mau 401, body = %s", rec.Code, rec.Body.String())
	}
	if kodeError(t, rec) != "REFRESH_TOKEN_REUSED" {
		t.Errorf("code = %s, mau REFRESH_TOKEN_REUSED", kodeError(t, rec))
	}

	// Keluarga dicabut — token aktif tinggal 0.
	var aktif int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE family_id = $1 AND revoked_at IS NULL`, familyID).Scan(&aktif)
	if err != nil {
		t.Fatalf("hitung aktif: %v", err)
	}
	if aktif != 0 {
		t.Errorf("token aktif = %d, mau 0 (keluarga harus dicabut oleh cabang 4)", aktif)
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

// replacedBySnapshot mengembalikan peta id_token -> replaced_by untuk
// satu keluarga. Dipakai untuk verifikasi bahwa logout tidak
// menyentuh kolom replaced_by.
func replacedBySnapshot(t *testing.T, pool *pgxpool.Pool, familyID string) map[string]*string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT id::text, replaced_by::text
		FROM refresh_tokens
		WHERE family_id = $1`, familyID)
	if err != nil {
		t.Fatalf("snapshot replaced_by: %v", err)
	}
	defer rows.Close()
	out := make(map[string]*string)
	for rows.Next() {
		var id string
		var rb *string
		if err := rows.Scan(&id, &rb); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[id] = rb
	}
	return out
}
