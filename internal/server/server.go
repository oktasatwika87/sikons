// Package server merakit router dan menyimpan dependency yang dipakai handler.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"

	"github.com/oktasatwika/sikons/internal/auth"
	"github.com/oktasatwika/sikons/internal/availability"
	"github.com/oktasatwika/sikons/internal/booking"
	"github.com/oktasatwika/sikons/internal/config"
	"github.com/oktasatwika/sikons/internal/slotgen"
)

// Pool mendefinisikan akses database yang dipakai di package ini.
// *pgxpool.Pool dan *pgxpool.Tx memenuhi interface ini.
type Pool interface {
	Ping(ctx context.Context) error
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Server memegang semua dependency handler. Ini pola dependency injection
// paling sederhana di Go: tidak ada framework, tidak ada container, tidak ada
// variabel global. Handler adalah method dari struct ini, jadi otomatis
// kebagian akses ke db dan log.
type Server struct {
	cfg          config.Config
	db           Pool
	auth         *auth.Service
	tokens       *auth.TokenIssuer
	log          *slog.Logger
	availability *availability.Service
	booking      *booking.Service
	slotgen      *slotgen.Service
	slotHorizon  int // hari, untuk rekonsiliasi
}

// Deps dikumpulkan dalam satu struct, bukan dijadikan parameter berjejer.
type Deps struct {
	Pool         Pool
	Auth         *auth.Service
	Tokens       *auth.TokenIssuer
	Log          *slog.Logger
	Availability *availability.Service
	Booking      *booking.Service
	Slotgen      *slotgen.Service
}

func New(cfg config.Config, deps Deps) *Server {
	return &Server{
		cfg:          cfg,
		db:           deps.Pool,
		auth:         deps.Auth,
		tokens:       deps.Tokens,
		log:          deps.Log,
		availability: deps.Availability,
		booking:      deps.Booking,
		slotgen:      deps.Slotgen,
		slotHorizon:  cfg.SlotHorizonDays,
	}
}

// Routes mengembalikan http.Handler, bukan *chi.Mux.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", s.handleRegister)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/refresh", s.handleRefresh)
		r.Post("/auth/logout", s.handleLogout)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/me", s.handleMe)

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

			r.Group(func(r chi.Router) {
				r.Use(s.requireRole(auth.RoleStudent))
				r.Post("/bookings", s.handleCreateBooking)
			})
			r.Get("/bookings", s.handleListBookings)
			r.Get("/bookings/{id}", s.handleGetBooking)
		})
	})

	return r
}
