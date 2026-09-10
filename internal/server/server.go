// Package server merakit router dan menyimpan dependency yang dipakai handler.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/oktasatwika/sikons/internal/auth"
	"github.com/oktasatwika/sikons/internal/availability"
	"github.com/oktasatwika/sikons/internal/booking"
	"github.com/oktasatwika/sikons/internal/config"
	"github.com/oktasatwika/sikons/internal/lecturer"
	"github.com/oktasatwika/sikons/internal/slotgen"
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
	cfg          config.Config
	db           DB
	auth         *auth.Service
	tokens       *auth.TokenIssuer
	log          *slog.Logger
	availability *availability.Service
	booking      *booking.Service
	slotgen      *slotgen.Service
	lecturer     *lecturer.Service
	slotHorizon  int // hari, untuk rekonsiliasi
}

// Deps dikumpulkan dalam satu struct, bukan dijadikan parameter berjejer.
//
// Alasannya praktis: tiap milestone menambah satu dependency baru (booking di
// M3, notifikasi di M4). Dengan parameter berjejer, setiap penambahan mengubah
// signature New dan memaksa semua pemanggil — termasuk setiap test — ikut
// diedit. Dengan struct, penambahan field tidak merusak apa pun yang sudah ada.
type Deps struct {
	DB           DB
	Auth         *auth.Service
	Tokens       *auth.TokenIssuer
	Log          *slog.Logger
	Availability *availability.Service
	Booking      *booking.Service
	Slotgen      *slotgen.Service
	Lecturer     *lecturer.Service
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

func New(cfg config.Config, deps Deps) *Server {
	return &Server{
		cfg:          cfg,
		db:           deps.DB,
		auth:         deps.Auth,
		tokens:       deps.Tokens,
		log:          deps.Log,
		availability: deps.Availability,
		booking:      deps.Booking,
		slotgen:      deps.Slotgen,
		lecturer:     deps.Lecturer,
		slotHorizon:  cfg.SlotHorizonDays,
	}
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

	// CORS harus paling awal — sebelum middleware lain yang bisa mengirim header.
	// Dipakai untuk mengizinkan Next.js (M5) mengakses API dari origin berbeda.
	corsHandler := cors.Handler(cors.Options{
		AllowedOrigins:   s.cfg.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "Idempotency-Key"},
		AllowCredentials: true,
		MaxAge:           300, // browser cache preflight 5 menit
	})
	r.Use(corsHandler)

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
		// Terbuka untuk umum.
		r.Post("/auth/register", s.handleRegister)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/refresh", s.handleRefresh)
		r.Post("/auth/logout", s.handleLogout)

		// Browsing dosen — publik karena:
		// - Tidak ada data sensitif yang terbuka.
		// - Portfolio butuh bisa dilihat recruiter sebelum login.
		// - Kalender slot pun tidak membocorkan data booking mahasiswa.
		r.Get("/lecturers", s.handleListLecturers)
		r.Get("/lecturers/{id}", s.handleGetLecturer)
		r.Get("/lecturers/{id}/slots", s.handleGetLecturerSlots)

		// Butuh access token. Group membuat middleware hanya berlaku untuk
		// rute di dalamnya — rute publik di atas tidak ikut terkena.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/me", s.handleMe)

			// Aturan ketersediaan — milik dosen, butuh role lecturer.
			r.Group(func(r chi.Router) {
				r.Use(s.requireRole(auth.RoleLecturer))
				r.Post("/availability-rules", s.handleCreateAvailabilityRule)
				r.Get("/availability-rules", s.handleListAvailabilityRules)
				r.Patch("/availability-rules/{id}", s.handleUpdateAvailabilityRule)
				r.Delete("/availability-rules/{id}", s.handleDeleteAvailabilityRule)
				r.Post("/availability-exceptions", s.handleCreateAvailabilityException)
				r.Get("/availability-exceptions", s.handleListAvailabilityExceptions)
				r.Delete("/availability-exceptions/{id}", s.handleDeleteAvailabilityException)
			})

			// Booking — mahasiswa membuat, semua peran melihat.
			r.Group(func(r chi.Router) {
				r.Use(s.requireRole(auth.RoleStudent))
				r.Post("/bookings", s.handleCreateBooking)
				r.Patch("/bookings/{id}/cancel", s.handleCancelBooking)
			})
			r.Get("/bookings", s.handleListBookings)
			r.Get("/bookings/{id}", s.handleGetBooking)
			// Dosen hanya: complete dan no-show.
			r.Group(func(r chi.Router) {
				r.Use(s.requireRole(auth.RoleLecturer))
				r.Patch("/bookings/{id}/complete", s.handleCompleteBooking)
				r.Patch("/bookings/{id}/no-show", s.handleNoShowBooking)
			})
		})
	})

	return r
}
