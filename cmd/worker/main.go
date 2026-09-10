// Command worker adalah background process untuk mengirim reminder email.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oktasatwika/sikons/internal/config"
	"github.com/oktasatwika/sikons/internal/db"
	"github.com/oktasatwika/sikons/internal/notifier"
	"github.com/oktasatwika/sikons/internal/reminder"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker berhenti", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg)
	slog.SetDefault(log)

	// Validasi bahwa ResendAPIKey tidak kosong.
	// cmd/api boleh tanpa ini (tidak mengirim email), tapi worker harus punya.
	if cfg.ResendAPIKey == "" {
		return errors.New("RESEND_API_KEY belum diisi — worker tidak bisa mengirim email tanpa API key")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Info("terhubung ke database")

	// Bangun notifier dengan konfigurasi dari env.
	sender := notifier.ResendSender{
		APIKey: cfg.ResendAPIKey,
		From:   cfg.EmailFrom,
	}

	// Bangun reminder service.
	reminderSvc := reminder.New(
		pool,
		&sender,
		cfg.CampusTZ,
		cfg.ReminderPollInterval,
		cfg.ReminderMaxAttempts,
		log,
	)

	log.Info("worker siap",
		"poll_interval", cfg.ReminderPollInterval,
		"max_attempts", cfg.ReminderMaxAttempts,
	)

	// Jalankan worker. Ini memblokir sampai ctx dibatalkan.
	if err := reminderSvc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	log.Info("worker berhenti dengan bersih")
	return nil
}

func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}

	if cfg.IsDevelopment() {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

// Timeout untuk graceful shutdown.
const shutdownTimeout = 15 * time.Second
