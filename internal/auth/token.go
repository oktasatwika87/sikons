package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	issuer              = "sikons"
	minSecretBytes      = 32
	DefaultAccessTTL    = 15 * time.Minute
	metodePenandatangan = "HS256"
)

// klaim yang kita tulis ke dalam token. RegisteredClaims menyediakan field
// standar (sub, exp, iat, iss); Role adalah tambahan kita sendiri.
type klaim struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// TokenIssuer menerbitkan dan memeriksa access token.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
	// jam bisa diganti saat test, supaya kedaluwarsa bisa diuji tanpa
	// benar-benar menunggu 15 menit.
	jam func() time.Time
}

func NewTokenIssuer(secret string, ttl time.Duration) (*TokenIssuer, error) {
	// HS256 mengambil kekuatannya dari panjang kunci. Secret pendek seperti
	// "rahasia" bisa ditebak offline dalam hitungan detik, dan siapa pun yang
	// menebaknya bisa menerbitkan token sebagai admin.
	if len(secret) < minSecretBytes {
		return nil, ErrSecretTooShort
	}
	if ttl <= 0 {
		ttl = DefaultAccessTTL
	}
	return &TokenIssuer{secret: []byte(secret), ttl: ttl, jam: time.Now}, nil
}

// Issue menerbitkan access token beserta waktu kedaluwarsanya.
func (i *TokenIssuer) Issue(id Identity) (string, time.Time, error) {
	sekarang := i.jam()
	kedaluwarsa := sekarang.Add(i.ttl)

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, klaim{
		Role: id.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   id.UserID,
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(sekarang),
			ExpiresAt: jwt.NewNumericDate(kedaluwarsa),
		},
	})

	s, err := t.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("menandatangani token: %w", err)
	}
	return s, kedaluwarsa, nil
}

// Parse memeriksa tanda tangan dan masa berlaku token.
func (i *TokenIssuer) Parse(tokenString string) (Identity, error) {
	var c klaim

	_, err := jwt.ParseWithClaims(tokenString, &c,
		func(t *jwt.Token) (any, error) { return i.secret, nil },

		// INI BARIS PALING PENTING DI FILE INI.
		//
		// Tanpa WithValidMethods, penyerang bisa mengirim token dengan header
		// alg "none" — token tanpa tanda tangan sama sekali — dan sebagian
		// implementasi akan menerimanya. Varian lain: token yang dibuat dengan
		// HS256 memakai PUBLIC KEY RSA kita sebagai secret, kalau kita
		// menerima RS256 dan HS256 sekaligus.
		//
		// Aturannya: JANGAN PERNAH biarkan token yang menentukan algoritma apa
		// yang dipakai untuk memeriksanya. Server yang memutuskan.
		jwt.WithValidMethods([]string{metodePenandatangan}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(i.jam),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Identity{}, ErrTokenExpired
		}
		return Identity{}, ErrTokenInvalid
	}

	if c.Subject == "" || c.Role == "" {
		return Identity{}, ErrTokenInvalid
	}
	return Identity{UserID: c.Subject, Role: c.Role}, nil
}

// SetJamUntukTest mengganti sumber waktu penerbit token.
//
// Hanya untuk test — namanya sengaja dibuat panjang dan canggung supaya tidak
// ada yang memakainya di kode produksi tanpa sadar. Alternatifnya adalah
// mengekspor field jam, tapi field yang diekspor mengundang siapa saja
// mengubahnya kapan saja.
func (i *TokenIssuer) SetJamUntukTest(jam func() time.Time) { i.jam = jam }
