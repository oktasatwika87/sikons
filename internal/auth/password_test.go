package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPassword_HasilnyaBerbedaTiapKali(t *testing.T) {
	const p = "rahasia-banget"

	h1, err := HashPassword(p)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashPassword(p)
	if err != nil {
		t.Fatal(err)
	}

	// Password yang sama menghasilkan hash yang berbeda karena bcrypt menyelipkan
	// salt acak di setiap hash. Ini yang membuat rainbow table tidak berguna:
	// penyerang tidak bisa menghitung sekali lalu memakainya untuk semua orang.
	if h1 == h2 {
		t.Fatal("dua hash dari password yang sama ternyata identik — salt-nya tidak bekerja")
	}

	// Dan keduanya tetap cocok dengan password aslinya, karena salt-nya ikut
	// tersimpan di dalam string hash itu sendiri.
	if err := VerifyPassword(h1, p); err != nil {
		t.Errorf("hash pertama tidak cocok: %v", err)
	}
	if err := VerifyPassword(h2, p); err != nil {
		t.Errorf("hash kedua tidak cocok: %v", err)
	}
}

func TestVerifyPassword_PasswordSalah(t *testing.T) {
	h, err := HashPassword("password-yang-benar")
	if err != nil {
		t.Fatal(err)
	}

	err = VerifyPassword(h, "password-yang-salah")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, mau ErrInvalidCredentials", err)
	}
}

func TestHashPassword_TerlaluPendek(t *testing.T) {
	_, err := HashPassword("pendek")
	if !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("err = %v, mau ErrPasswordTooShort", err)
	}
}

// bcrypt DIAM-DIAM memotong di 72 byte. Tanpa penolakan eksplisit, dua password
// berbeda yang 72 byte pertamanya sama akan sama-sama bisa dipakai login.
// Test ini yang menjaga penolakan itu tetap ada.
func TestHashPassword_LebihDari72ByteDitolak(t *testing.T) {
	_, err := HashPassword(strings.Repeat("a", 73))
	if !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("err = %v, mau ErrPasswordTooLong", err)
	}
}

// Memastikan hash umpan yang dipakai untuk menyamakan waktu response memang
// hash bcrypt yang valid. Kalau formatnya rusak, bcrypt gagal seketika dan
// perlindungan terhadap kebocoran lewat waktu response ikut hilang tanpa suara.
func TestHashUmpanValid(t *testing.T) {
	if err := VerifyPassword(string(hashUmpan()), "apa saja"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("hash umpan tidak berperilaku seperti hash bcrypt normal: %v", err)
	}
}
