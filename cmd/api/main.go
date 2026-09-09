// Command api adalah entrypoint HTTP server SIKONS.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oktasatwika/sikons/internal/auth"
	"github.com/oktasatwika/sikons/internal/availability"
	"github.com/oktasatwika/sikons/internal/booking"
	"github.com/oktasatwika/sikons/internal/config"
	"github.com/oktasatwika/sikons/internal/db"
	"github.com/oktasatwika/sikons/internal/server"
	"github.com/oktasatwika/sikons/internal/slotgen"
)

func main() {
	if err := run(); err != nil {
		slog.Error("aplikasi berhenti", "err", err)
		os.Exit(1)
	}
}

// Kenapa ada run() dan main() cuma memanggilnya:
//
// os.Exit() TIDAK menjalankan defer. Kalau main() penuh dengan os.Exit(1) di
// tengah-tengah, koneksi database tidak sempat ditutup dan server tidak sempat
// pamit. Dengan memindahkan semuanya ke run() yang mengembalikan error,
// setiap defer di dalamnya dijamin jalan dulu sebelum proses benar-benar mati.
// Ini pola yang sangat umum di Go dan layak kamu pertahankan.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg)
	slog.SetDefault(log)

	// signal.NotifyContext memberi context yang otomatis dibatalkan saat
	// proses menerima Ctrl+C (SIGINT) atau `docker stop` (SIGTERM).
	//
	// context.Context di Go adalah cara membawa sinyal "berhenti" menembus
	// batas fungsi. Nanti setiap query database menerima ctx ini juga, jadi
	// saat kamu tekan Ctrl+C, query yang sedang berjalan ikut dibatalkan
	// alih-alih menggantung.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Info("terhubung ke database")

	tokens, err := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTokenTTL)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr: ":" + cfg.HTTPPort,
		Handler: server.New(cfg, server.Deps{
			Pool:         pool,
			Auth:         auth.NewService(pool, tokens, log),
			Tokens:       tokens,
			Log:          log,
			Availability: availability.NewService(pool),
			Booking:      booking.NewService(pool, cfg.BookingMinLeadMin),
			Slotgen:      slotgen.New(pool, cfg.CampusTZ, cfg.SlotHorizonDays),
		}).Routes(),

		// Tanpa timeout ini, satu klien yang membuka koneksi lalu diam saja
		// bisa menahan resource selamanya. Default net/http adalah "tanpa
		// batas", jadi ini WAJIB diisi sendiri untuk server yang menghadap
		// internet.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Server dijalankan di goroutine terpisah supaya fungsi ini bisa lanjut
	// menunggu sinyal berhenti. `go` di depan pemanggilan fungsi = jalankan
	// bersamaan, jangan tunggu selesai.
	//
	// Errornya dikirim lewat channel, bukan disimpan di variabel biasa.
	// Variabel biasa yang ditulis satu goroutine dan dibaca goroutine lain
	// adalah data race — channel yang membuat serah-terimanya aman.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("server berjalan", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// select menunggu beberapa channel sekaligus, dan melanjutkan pada yang
	// pertama siap. Di sini: entah servernya mati sendiri karena error, atau
	// kita yang menerima sinyal berhenti.
	select {
	case err := <-serverErr:
		return err

	case <-ctx.Done():
		log.Info("sinyal berhenti diterima, menutup server dengan rapi")

		// Context BARU dengan batas waktunya sendiri. Tidak boleh menurunkan
		// dari ctx, karena ctx sudah terlanjur dibatalkan — shutdown-nya akan
		// langsung menyerah tanpa menunggu request yang sedang berjalan.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// Shutdown berhenti menerima koneksi baru, tapi memberi kesempatan
		// request yang sedang diproses untuk selesai. Inilah bedanya deploy
		// yang mulus dengan deploy yang memutus request orang di tengah jalan.
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}

		log.Info("server berhenti dengan bersih")
		return nil
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}

	// Development: teks berwarna-warni yang enak dibaca manusia.
	// Production: JSON, supaya bisa dicari dan disaring oleh mesin.
	if cfg.IsDevelopment() {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
