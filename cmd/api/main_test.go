package main

import (
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oktasatwika87/sikons/internal/auth"
	"github.com/oktasatwika87/sikons/internal/config"
)

// Test ini tidak butuh database: yang dicek cuma bahwa newDeps mengisi
// SEMUA field server.Deps. Kalau suatu saat ada service baru di Deps tapi
// lupa dirakit di sini, test ini yang gagal, bukan request pertama di
// produksi yang panic karena nil pointer.
func TestNewDeps_SemuaFieldTerisi(t *testing.T) {
	tokens, err := auth.NewTokenIssuer(strings.Repeat("r", 32), time.Minute)
	if err != nil {
		t.Fatalf("membuat token issuer: %v", err)
	}
	cfg := config.Config{CampusTZ: time.UTC, SlotHorizonDays: 30}

	deps := newDeps(cfg, &pgxpool.Pool{}, tokens, slog.Default())

	v := reflect.ValueOf(deps)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Pointer, reflect.Interface:
			if f.IsNil() {
				t.Errorf("server.Deps.%s tidak diisi di newDeps", v.Type().Field(i).Name)
			}
		}
	}
}
