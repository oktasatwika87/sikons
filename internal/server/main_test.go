package server

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/oktasatwika/sikons/internal/auth"
	"github.com/oktasatwika/sikons/internal/config"
)

// TestMain di package ini:
// 1. Set bcrypt cost ke minimum untuk speed di test.
// 2. Setup koneksi database untuk test integrasi.

var poolUji *pgxpool.Pool
var cfgUji config.Config

// testLogger implements *slog.Logger untuk test.
var logUji = newTestLogger()

func newTestLogger() *testLogger {
	return &testLogger{}
}

type testLogger struct{}

func (l *testLogger) Debug(msg string, args ...any) {}
func (l *testLogger) Info(msg string, args ...any)  {}
func (l *testLogger) Warn(msg string, args ...any)  {}
func (l *testLogger) Error(msg string, args ...any) {}
func (l *testLogger) With(args ...any) *testLogger  { return l }

func TestMain(m *testing.M) {
	auth.SetBcryptCostUntukTest(bcrypt.MinCost)

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		fmt.Println("TEST_DATABASE_URL kosong — test integrasi server dilewati")
		os.Exit(m.Run())
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = config.Config{
			CampusTZ:          time.Local,
			SlotHorizonDays:   30,
			BookingMinLeadMin: 60,
		}
	} else {
		if cfg.CampusTZ == nil {
			cfg.CampusTZ = time.Local
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolUji, err = pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Println("tidak bisa terhubung ke database uji:", err)
		os.Exit(1)
	}
	if err := poolUji.Ping(ctx); err != nil {
		fmt.Println("database uji tidak merespons:", err)
		os.Exit(1)
	}

	cfgUji = cfg
	kode := m.Run()
	poolUji.Close()
	os.Exit(kode)
}
