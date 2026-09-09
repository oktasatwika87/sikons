// Package server merakit router dan menyimpan dependency yang dipakai handler.
package server

import (
	"log/slog"
	"net/http"
	"time"

	"context"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/oktasatwika/sikons/internal/config"
)

// Server memegang semua dependency handler. Ini pola dependency injection
// paling sederhana di Go: tidak ada framework, tidak ada container, tidak ada
// variabel global. Handler adalah method dari struct ini, jadi otomatis
// kebagian akses ke db dan log.
//
// Kenapa bukan variabel global seperti `var DB *pgxpool.Pool`? Karena begitu
// pakai global, test tidak bisa lagi menjalankan dua konfigurasi berbeda
// secara paralel, dan tidak ada yang bisa menukar db dengan versi palsu.
type Server struct {
	cfg config.Config
	db  DB
	log *slog.Logger
}

// DB sengaja didefinisikan di SINI, di package yang MEMAKAINYA, bukan di
// package db yang menyediakannya. Ini kebiasaan Go yang berbeda dari Java/C#:
// interface milik konsumen, bukan produsen.
//
// Akibatnya bagus dua-duanya:
//   - package server tidak perlu mengimpor pgx sama sekali
//   - test bisa menyodorkan DB palsu yang sengaja gagal, tanpa Postgres
//
// *pgxpool.Pool memenuhi interface ini tanpa perlu mendeklarasikan apa pun —
// di Go, sebuah tipe memenuhi interface cukup dengan punya method-nya.
// Interface ini akan tumbuh seiring kebutuhan, tapi tetap sesempit mungkin:
// makin banyak method di interface, makin susah dibuat palsunya saat test.
type DB interface {
	Ping(ctx context.Context) error
}

func New(cfg config.Config, db DB, log *slog.Logger) *Server {
	return &Server{cfg: cfg, db: db, log: log}
}

// Routes mengembalikan http.Handler, bukan *chi.Mux.
//
// http.Handler adalah interface dengan satu method: ServeHTTP. Mengembalikan
// interface, bukan tipe konkretnya, berarti pemanggil tidak bisa bergantung
// pada chi secara diam-diam — kalau suatu saat router-nya diganti, tidak ada
// kode lain yang ikut berubah. Ini kebiasaan yang sangat khas Go: terima
// interface, kembalikan struct — kecuali saat interface-nya memang kontrak
// yang ingin kamu tegakkan, seperti di sini.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.requestLogger)
	// Recoverer menangkap panic di dalam handler supaya satu bug tidak
	// mematikan seluruh proses. Diletakkan SETELAH logger supaya panic-nya
	// tetap tercatat lengkap dengan request id-nya.
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	r.Route("/api/v1", func(r chi.Router) {
		// Diisi mulai M2 (auth), M3 (booking).
	})

	return r
}
