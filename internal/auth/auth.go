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
	ErrEmailTaken         = errors.New("email sudah terdaftar")
	ErrInvalidCredentials = errors.New("email atau password salah")
	ErrAccountInactive    = errors.New("akun dinonaktifkan")
	ErrPasswordTooShort   = errors.New("password minimal 8 karakter")
	ErrPasswordTooLong    = errors.New("password maksimal 72 karakter")
	ErrTokenInvalid       = errors.New("token tidak valid")
	ErrTokenExpired       = errors.New("token kedaluwarsa")
	ErrSecretTooShort     = errors.New("JWT_SECRET minimal 32 karakter")
)

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
