package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const secretUji = "secret-uji-yang-panjangnya-lebih-dari-32-karakter"

func TestNewTokenIssuer_SecretPendekDitolak(t *testing.T) {
	_, err := NewTokenIssuer("pendek", time.Minute)
	if !errors.Is(err, ErrSecretTooShort) {
		t.Fatalf("err = %v, mau ErrSecretTooShort", err)
	}
}

func TestIssueLaluParse(t *testing.T) {
	i, err := NewTokenIssuer(secretUji, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	asal := Identity{UserID: "11111111-1111-1111-1111-111111111111", Role: RoleStudent}
	token, kedaluwarsa, err := i.Issue(asal)
	if err != nil {
		t.Fatal(err)
	}
	if !kedaluwarsa.After(time.Now()) {
		t.Error("kedaluwarsa seharusnya di masa depan")
	}

	hasil, err := i.Parse(token)
	if err != nil {
		t.Fatalf("parse gagal: %v", err)
	}
	if hasil != asal {
		t.Errorf("hasil = %+v, mau %+v", hasil, asal)
	}
}

func TestParse_TokenKedaluwarsa(t *testing.T) {
	i, err := NewTokenIssuer(secretUji, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// Jam palsu: token diterbitkan "satu jam yang lalu" dengan masa berlaku
	// 15 menit. Tidak perlu menunggu sungguhan — inilah gunanya jam bisa
	// disuntik alih-alih memanggil time.Now() langsung di dalam kode.
	satuJamLalu := time.Now().Add(-time.Hour)
	i.jam = func() time.Time { return satuJamLalu }
	token, _, err := i.Issue(Identity{UserID: "abc", Role: RoleStudent})
	if err != nil {
		t.Fatal(err)
	}

	i.jam = time.Now
	if _, err := i.Parse(token); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("err = %v, mau ErrTokenExpired", err)
	}
}

func TestParse_SecretBerbedaDitolak(t *testing.T) {
	penerbit, _ := NewTokenIssuer(secretUji, time.Minute)
	token, _, err := penerbit.Issue(Identity{UserID: "abc", Role: RoleStudent})
	if err != nil {
		t.Fatal(err)
	}

	penyerang, _ := NewTokenIssuer("secret-lain-yang-juga-lebih-dari-32-karakter", time.Minute)
	if _, err := penyerang.Parse(token); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("err = %v, mau ErrTokenInvalid", err)
	}
}

func TestParse_TandaTanganDiubahDitolak(t *testing.T) {
	i, _ := NewTokenIssuer(secretUji, time.Minute)
	token, _, err := i.Issue(Identity{UserID: "abc", Role: RoleStudent})
	if err != nil {
		t.Fatal(err)
	}

	rusak := token[:len(token)-3] + "AAA"
	if _, err := i.Parse(rusak); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("err = %v, mau ErrTokenInvalid", err)
	}
}

// TEST PALING PENTING DI FILE INI.
//
// Serangan "alg: none": penyerang menyusun sendiri token berisi role admin,
// lalu menyetel header algoritmanya menjadi "none" sehingga tidak ada tanda
// tangan yang perlu dipalsukan. Implementasi JWT yang mempercayai header
// token untuk memilih algoritma verifikasi akan menerimanya mentah-mentah.
//
// Yang menahannya adalah jwt.WithValidMethods di Parse.
func TestParse_TokenTanpaTandaTanganDitolak(t *testing.T) {
	i, _ := NewTokenIssuer(secretUji, time.Minute)

	jahat := jwt.NewWithClaims(jwt.SigningMethodNone, klaim{
		Role: RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "penyerang",
			Issuer:    issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	token, err := jahat.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := i.Parse(token); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("TOKEN TANPA TANDA TANGAN DITERIMA (err = %v) — ini lubang keamanan", err)
	}
}

func TestParse_IssuerLainDitolak(t *testing.T) {
	i, _ := NewTokenIssuer(secretUji, time.Minute)

	asing := jwt.NewWithClaims(jwt.SigningMethodHS256, klaim{
		Role: RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "abc",
			Issuer:    "aplikasi-lain",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	token, err := asing.SignedString([]byte(secretUji))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := i.Parse(token); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("err = %v, mau ErrTokenInvalid", err)
	}
}
