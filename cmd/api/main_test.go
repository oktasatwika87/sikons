package main

import (
	"os"
	"strings"
	"testing"
)

// TestAPITanpaJWTSecret_Ditolak memastikan API menolak start ketika JWT_SECRET kosong.
func TestAPITanpaJWTSecret_Ditolak(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL kosong, test dilewati")
	}

	orig := map[string]string{
		"DATABASE_URL": os.Getenv("DATABASE_URL"),
		"JWT_SECRET":   os.Getenv("JWT_SECRET"),
	}
	os.Setenv("DATABASE_URL", dsn)
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

	err := run()
	if err == nil {
		t.Fatal("run() seharusnya gagal karena JWT_SECRET kosong")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("error = %q, seharusnya menyebutkan JWT_SECRET", err.Error())
	}
}
