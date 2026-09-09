// Package auth menangani identitas: password, token, dan siapa-boleh-apa.
package auth

import (
	"context"
	"errors"
	"time"
)

// Peran yang dikenal sistem. Dibuat konstanta supaya salah ketik ketahuan saat
// compile, bukan saat seseorang diam-diam kehilangan akses.
const (
	RoleStudent  = "student"
	RoleLecturer = "lecturer"
	RoleAdmin    = "admin"
)

var (
	ErrEmailTaken          = errors.New("email sudah terdaftar")
	ErrInvalidCredentials  = errors.New("email atau password salah")
	ErrAccountInactive     = errors.New("akun dinonaktifkan")
	ErrPasswordTooShort    = errors.New("password minimal 8 karakter")
	ErrPasswordTooLong     = errors.New("password maksimal 72 karakter")
	ErrTokenInvalid        = errors.New("token tidak valid")
	ErrTokenExpired        = errors.New("token kedaluwarsa")
	ErrSecretTooShort      = errors.New("JWT_SECRET minimal 32 karakter")
	ErrRefreshTokenInvalid = errors.New("refresh token tidak valid")
	ErrRefreshTokenExpired = errors.New("refresh token kedaluwarsa")
	ErrRefreshTokenReused  = errors.New("refresh token sudah dipakai — kemungkinan dicuri")
	ErrRefreshInProgress   = errors.New("refresh token sedang diproses")
)

// RefreshGracePeriod adalah window waktu di mana reuse token oleh request yang
// kalah balapan dianggap wajar, bukan pencurian. Selama penerus token itu belum
// dicabut dan masih dalam grace period, sistem menganggap kedua request adalah
// dari sesi yang sama (misalnya: tab browser berbeda).
var RefreshGracePeriod = 30 * time.Second

// gracePeriod adalah variabel yang bisa di-overwrite oleh test.
// Pola sama dengan bcryptCost.
var gracePeriod = RefreshGracePeriod

// SetGracePeriodUntukTest menimpa grace period untuk keperluan test.
// Harus dipanggil di awal setiap test yang butuh nilai non-standar.
func SetGracePeriodUntukTest(d time.Duration) {
	gracePeriod = d
}

// ResetGracePeriodMengambang mengembalikan grace period ke nilai default.
// Test yang memanggil SetGracePeriodUntukTest bertanggung jawab memanggil ini
// di deferred cleanup — pola yang sama dengan SetBcryptCostUntukTest.
func ResetGracePeriodMengambang() {
	gracePeriod = RefreshGracePeriod
}

// Identity adalah siapa si pemanggil, hasil pembacaan token.
// Sengaja hanya berisi dua hal — apa pun selain ini harus diambil dari
// database, karena isi token tidak bisa dipercaya untuk data yang berubah.
type Identity struct {
	UserID string
	Role   string
}

type User struct {
	ID             string
	Email          string
	FullName       string
	Role           string
	IdentityNumber *string
	IsActive       bool
	CreatedAt      time.Time
}

// ---------------------------------------------------------------- context

// Tipe kunci sendiri, tidak diekspor. Kalau kuncinya berupa string biasa
// (misalnya "identity"), package lain bisa tidak sengaja memakai string yang
// sama dan saling menimpa nilai di context yang sama. Tipe privat membuat
// tabrakan itu mustahil secara kompilasi.
type ctxKey struct{}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// IdentityFrom mengembalikan identitas pemanggil, dan false kalau request ini
// belum melewati middleware autentikasi.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}
