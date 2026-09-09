package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool   *pgxpool.Pool
	issuer *TokenIssuer
}

func NewService(pool *pgxpool.Pool, issuer *TokenIssuer) *Service {
	// Hitung hash umpan sekarang, bukan nanti saat login gagal pertama kali.
	Hangatkan()
	return &Service{pool: pool, issuer: issuer}
}

type RegisterInput struct {
	Email          string
	Password       string
	FullName       string
	IdentityNumber string
}

// Register membuat akun mahasiswa baru.
//
// PERANNYA DIPAKU DI SINI, TIDAK DIAMBIL DARI INPUT.
//
// Kalau role bisa dikirim dari body request, siapa pun bisa mendaftar sebagai
// 'lecturer' lalu membuka jadwal konsultasi atas nama dosen, atau sebagai
// 'admin'. Tidak ada validasi di frontend yang bisa mencegah itu — penyerang
// tidak memakai frontend-mu. Akun dosen dan admin dibuat lewat jalur terpisah
// yang sudah terautentikasi.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*User, error) {
	email := normalkanEmail(in.Email)
	if email == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("email tidak valid")
	}
	if strings.TrimSpace(in.FullName) == "" {
		return nil, fmt.Errorf("nama lengkap wajib diisi")
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	var nomor *string
	if n := strings.TrimSpace(in.IdentityNumber); n != "" {
		nomor = &n
	}

	var u User
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, full_name, role, identity_number)
		VALUES ($1, $2, $3, 'student', $4)
		RETURNING id::text, email, full_name, role::text, identity_number, is_active, created_at`,
		email, hash, strings.TrimSpace(in.FullName), nomor,
	).Scan(&u.ID, &u.Email, &u.FullName, &u.Role, &u.IdentityNumber, &u.IsActive, &u.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case "users_email_key":
				return nil, ErrEmailTaken
			case "users_identity_number_key":
				return nil, fmt.Errorf("NIM sudah terdaftar")
			}
		}
		return nil, fmt.Errorf("menyimpan user: %w", err)
	}

	return &u, nil
}

type LoginResult struct {
	User        *User
	AccessToken string
	ExpiresAt   time.Time
}

// Login memeriksa kredensial lalu menerbitkan access token.
func (s *Service) Login(ctx context.Context, emailInput, password string) (*LoginResult, error) {
	email := normalkanEmail(emailInput)

	var u User
	var hash string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, email, password_hash, full_name, role::text,
		       identity_number, is_active, created_at
		FROM users WHERE lower(email) = $1`,
		email,
	).Scan(&u.ID, &u.Email, &hash, &u.FullName, &u.Role,
		&u.IdentityNumber, &u.IsActive, &u.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		// Tetap membakar waktu setara satu verifikasi bcrypt, lalu balas dengan
		// error yang PERSIS SAMA seperti password salah. Dua-duanya perlu:
		// pesan yang sama menutup kebocoran lewat isi response, waktu yang sama
		// menutup kebocoran lewat lamanya response.
		BuangWaktuSetaraVerifikasi()
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("mencari user: %w", err)
	}

	if err := VerifyPassword(hash, password); err != nil {
		return nil, err
	}

	// Pemeriksaan is_active dilakukan SETELAH password diverifikasi. Kalau
	// dibalik, orang yang menebak email bisa tahu akun itu ada tapi nonaktif —
	// bocor lagi informasinya.
	if !u.IsActive {
		return nil, ErrAccountInactive
	}

	token, kedaluwarsa, err := s.issuer.Issue(Identity{UserID: u.ID, Role: u.Role})
	if err != nil {
		return nil, err
	}

	return &LoginResult{User: &u, AccessToken: token, ExpiresAt: kedaluwarsa}, nil
}

// ByID dipakai endpoint /me: token cuma menyimpan id dan role, sisanya selalu
// dibaca ulang dari database. Data di dalam token adalah foto lama; kalau nama
// atau status aktif berubah, tokennya tidak ikut berubah.
func (s *Service) ByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, email, full_name, role::text, identity_number, is_active, created_at
		FROM users WHERE id = $1::uuid`, id,
	).Scan(&u.ID, &u.Email, &u.FullName, &u.Role, &u.IdentityNumber, &u.IsActive, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("membaca user: %w", err)
	}
	return &u, nil
}

func normalkanEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
