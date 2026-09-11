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

	"github.com/oktasatwika87/sikons/internal/config"
)

// dbPalsu memenuhi interface DB. Tidak ada library mocking, tidak ada code
// generation — cukup struct kecil dengan method yang sama.
type dbPalsu struct{ err error }

func (d dbPalsu) Ping(context.Context) error { return d.err }

// rowPalsu dipakai oleh test yang ingin mensimulasikan satu query gagal.
// Tanpa rowPalsu, test healthz akan butuh Postgres hidup hanya untuk
// memvalidasi respons error. Itu beban yang tidak perlu.
type rowPalsu struct{ err error }

func (r rowPalsu) Scan(_ ...any) error { return r.err }

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

// /healthz tidak menyentuh database: ini pilihan desain, bukan kebetulan.
// Kalau /healthz suatu saat diam-diam menyentuh database, test ini akan panic.
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

// rowPalsu mensimulasikan query yang gagal Scan. Test ini memastikan kode
// path "baris tidak bisa dibaca" tidak crash diam-diam.
func TestRowPalsu_MengembalikanErrornyaSendiri(t *testing.T) {
	ingin := errors.New("baris tidak bisa dibaca")
	r := rowPalsu{err: ingin}
	if err := r.Scan(); !errors.Is(err, ingin) {
		t.Errorf("Scan = %v, mau %v", err, ingin)
	}
}
