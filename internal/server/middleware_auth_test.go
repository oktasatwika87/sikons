package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/oktasatwika87/sikons/internal/auth"
	"github.com/oktasatwika87/sikons/internal/config"
)

const secretUji = "secret-uji-yang-panjangnya-lebih-dari-32-karakter"

// Test di file ini TIDAK butuh database. Yang diuji adalah middleware, dan
// middleware tidak menyentuh database sama sekali — dia hanya membaca header
// lalu memverifikasi tanda tangan token.
func serverToken(t *testing.T) (*Server, *auth.TokenIssuer) {
	t.Helper()
	tokens, err := auth.NewTokenIssuer(secretUji, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := New(config.Config{Env: "test"}, Deps{
		Tokens: tokens,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return s, tokens
}

// Router mini khusus test, bukan s.Routes(). Alasannya: rute /me yang asli
// memanggil auth.Service yang butuh database, sedangkan yang ingin diuji di
// sini cuma middleware-nya.
func routerUji(s *Server, middleware ...func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(s.requireAuth)
	for _, m := range middleware {
		r.Use(m)
	}
	r.Get("/rahasia", func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.IdentityFrom(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]string{
			"user_id": id.UserID, "role": id.Role,
		})
	})
	return r
}

func panggilDenganHeader(h http.Handler, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/rahasia", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func kodeError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("body bukan JSON: %v", err)
	}
	return body.Error.Code
}

func TestRequireAuth_TanpaHeaderDitolak(t *testing.T) {
	s, _ := serverToken(t)
	rec := panggilDenganHeader(routerUji(s), "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, mau 401", rec.Code)
	}
}

// Bentuk-bentuk header yang gampang lolos kalau pemisahannya asal
// strings.TrimPrefix.
func TestRequireAuth_HeaderBerformatSalahDitolak(t *testing.T) {
	s, tokens := serverToken(t)
	token, _, err := tokens.Issue(auth.Identity{UserID: "abc", Role: auth.RoleStudent})
	if err != nil {
		t.Fatal(err)
	}

	kasus := map[string]string{
		"tanpa skema":        token,
		"skema salah":        "Basic " + token,
		"tanpa spasi":        "Bearer" + token,
		"bearer tanpa token": "Bearer",
		"kelebihan potongan": "Bearer " + token + " tambahan",
	}

	for nama, header := range kasus {
		t.Run(nama, func(t *testing.T) {
			rec := panggilDenganHeader(routerUji(s), header)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, mau 401", rec.Code)
			}
		})
	}
}

func TestRequireAuth_TokenValidDiteruskan(t *testing.T) {
	s, tokens := serverToken(t)
	token, _, err := tokens.Issue(auth.Identity{UserID: "user-123", Role: auth.RoleLecturer})
	if err != nil {
		t.Fatal(err)
	}

	rec := panggilDenganHeader(routerUji(s), "Bearer "+token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// Membuktikan identitasnya benar-benar sampai ke handler lewat context.
	if body["user_id"] != "user-123" || body["role"] != auth.RoleLecturer {
		t.Errorf("identitas di context = %+v", body)
	}
}

// Token kedaluwarsa harus dibedakan dari token tidak valid, supaya frontend
// tahu kapan cukup menukar refresh token diam-diam dan kapan harus melempar
// user ke halaman login.
func TestRequireAuth_TokenKedaluwarsaPunyaKodeSendiri(t *testing.T) {
	s, _ := serverToken(t)

	kedaluwarsa, err := auth.NewTokenIssuer(secretUji, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	kedaluwarsa.SetJamUntukTest(func() time.Time { return time.Now().Add(-time.Hour) })
	token, _, err := kedaluwarsa.Issue(auth.Identity{UserID: "abc", Role: auth.RoleStudent})
	if err != nil {
		t.Fatal(err)
	}

	rec := panggilDenganHeader(routerUji(s), "Bearer "+token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, mau 401", rec.Code)
	}
	if kode := kodeError(t, rec); kode != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, mau TOKEN_EXPIRED", kode)
	}
}

func TestRequireRole_PeranTidakCocokDapat403(t *testing.T) {
	s, tokens := serverToken(t)
	token, _, err := tokens.Issue(auth.Identity{UserID: "abc", Role: auth.RoleStudent})
	if err != nil {
		t.Fatal(err)
	}

	h := routerUji(s, s.requireRole(auth.RoleLecturer, auth.RoleAdmin))
	rec := panggilDenganHeader(h, "Bearer "+token)

	// 403, bukan 401: sistem tahu persis siapa dia, cuma tidak berhak.
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, mau 403", rec.Code)
	}
	if kode := kodeError(t, rec); kode != "FORBIDDEN" {
		t.Errorf("code = %q, mau FORBIDDEN", kode)
	}
}

func TestRequireRole_PeranCocokDiteruskan(t *testing.T) {
	s, tokens := serverToken(t)
	token, _, err := tokens.Issue(auth.Identity{UserID: "abc", Role: auth.RoleAdmin})
	if err != nil {
		t.Fatal(err)
	}

	h := routerUji(s, s.requireRole(auth.RoleLecturer, auth.RoleAdmin))
	rec := panggilDenganHeader(h, "Bearer "+token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", rec.Code)
	}
}
