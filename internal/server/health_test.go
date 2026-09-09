package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oktasatwika/sikons/internal/config"
)

// dbPalsu memenuhi interface DB. Tidak ada library mocking, tidak ada code
// generation — cukup struct kecil dengan method yang sama. Inilah untungnya
// interface yang sempit.
type dbPalsu struct{ err error }

func (d dbPalsu) Ping(context.Context) error { return d.err }

func serverUji(db DB) *Server {
	return New(config.Config{Env: "test"}, Deps{
		DB:  db,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func panggil(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// db sengaja nil: kalau /healthz suatu saat diam-diam menyentuh database,
// test ini akan panic. Jadi test ini bukan cuma memeriksa status 200, tapi
// juga menjaga keputusan "liveness tidak boleh bergantung pada database".
func TestHealthzTidakMenyentuhDatabase(t *testing.T) {
	rec := panggil(t, serverUji(nil), "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("response bukan JSON yang valid: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf(`status = %q, mau "ok"`, body["status"])
	}
}

func TestReadyzOKSaatDatabaseSehat(t *testing.T) {
	rec := panggil(t, serverUji(dbPalsu{err: nil}), "/readyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau %d", rec.Code, http.StatusOK)
	}
}

func TestReadyzBalas503SaatDatabaseMati(t *testing.T) {
	rec := panggil(t, serverUji(dbPalsu{err: errors.New("koneksi ditolak")}), "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, mau %d", rec.Code, http.StatusServiceUnavailable)
	}

	var got struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("body bukan JSON: %v", err)
	}
	// Bentuk amplop error harus sama di seluruh API, termasuk di endpoint
	// infrastruktur seperti ini.
	if got.Error.Code != "DATABASE_UNAVAILABLE" {
		t.Errorf("code = %q, mau DATABASE_UNAVAILABLE", got.Error.Code)
	}
}

func TestRouteTidakDikenalBalas404(t *testing.T) {
	rec := panggil(t, serverUji(nil), "/tidak-ada")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, mau %d", rec.Code, http.StatusNotFound)
	}
}
