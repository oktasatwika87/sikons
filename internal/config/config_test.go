package config

import (
	"os"
	"testing"
)

func TestLoad_DatabaseURLKosong_Ditolak(t *testing.T) {
	// Pastikan DATABASE_URL dan JWT_SECRET tidak ada di env.
	orig := map[string]string{
		"DATABASE_URL": os.Getenv("DATABASE_URL"),
		"JWT_SECRET":   os.Getenv("JWT_SECRET"),
	}
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("JWT_SECRET")
	defer func() {
		for k, v := range orig {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("Load() seharusnya gagal karena DATABASE_URL kosong")
	}
}

func TestLoad_JWTSecretKosong_TetapBerhasil(t *testing.T) {
	// Pastikan DATABASE_URL ada tapi JWT_SECRET kosong.
	orig := map[string]string{
		"DATABASE_URL": os.Getenv("DATABASE_URL"),
		"JWT_SECRET":   os.Getenv("JWT_SECRET"),
	}
	os.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test?sslmode=disable")
	os.Unsetenv("JWT_SECRET")
	defer func() {
		for k, v := range orig {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	_, err := Load()
	if err != nil {
		t.Fatalf("Load() seharusnya berhasil tanpa JWT_SECRET, tapi error: %v", err)
	}
}
