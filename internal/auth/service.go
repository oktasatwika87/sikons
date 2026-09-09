package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	User           *User
	AccessToken    string
	RefreshToken   string
	ExpiresAt      time.Time
	RefreshExpires time.Time
}

// Login memeriksa kredensial lalu menerbitkan access token dan refresh token.
func (s *Service) Login(ctx context.Context, emailInput, password, userAgent string) (*LoginResult, error) {
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

	accessToken, kedaluwarsa, err := s.issuer.Issue(Identity{UserID: u.ID, Role: u.Role})
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.IssueRefreshToken(ctx, u.ID, userAgent)
	if err != nil {
		return nil, err
	}

	return &LoginResult{
		User:           &u,
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		ExpiresAt:      kedaluwarsa,
		RefreshExpires: time.Now().Add(RefreshTokenTTL),
	}, nil
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

// ---------------------------------------------------------------- refresh token

const RefreshTokenTTL = 30 * 24 * time.Hour // 30 hari, sliding

// IssueRefreshToken membuat token baru dengan family_id baru. Plaintext dikembalikan
// ke caller (untuk disetel di cookie), SHA-256 hash-nya disimpan ke database.
//
// Tidak pakai bcrypt: 32 byte acak dari crypto/rand punya ~256 bit entropi,
// tidak bisa ditebak, jadi hash cepat sudah cukup. bcrypt dirancang untuk
// password entropi-rendah; di sini justru memperlambat operasi yang mestinya
// cepat (tiap refresh harus secepat mungkin).
func (s *Service) IssueRefreshToken(ctx context.Context, userID, userAgent string) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("membuat token: %w", err)
	}
	hash := sha256.Sum256(bytes)

	encoded := base64.RawURLEncoding.EncodeToString(bytes)
	expiresAt := time.Now().Add(RefreshTokenTTL)

	result, err := s.pool.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at, user_agent)
		VALUES ($1, $2, gen_random_uuid(), $3, $4)`,
		userID, hash[:], expiresAt, userAgent,
	)
	if err != nil {
		return "", fmt.Errorf("menyimpan refresh token: %w", err)
	}
	if result.RowsAffected() != 1 {
		return "", fmt.Errorf("menyimpan refresh token: rows affected = %d", result.RowsAffected())
	}

	return encoded, nil
}

// RotateRefreshToken memutar token dalam satu transaksi.
//
// Deteksi reuse: kalau token sudah di-revoke, seluruh family dicabut dan
// ErrRefreshTokenReused dikembalikan. Ini berarti token itu pernah dicuri dan
// dipakai oleh penyerang — satu-satunya respons aman adalah mencabut semuanya.
//
// FOR UPDATE pada SELECT:WAJIB. Tanpa itu, dua tab yang merefresh bersamaan
// akan sama-sama menemukan token yang belum di-revoke, dan sama-sama membuat
// token baru. Kedua token baru valid, tapi yang pertama melakukan refresh
// mencuri token kedua yang belum selesai dibuat. FOR UPDATE mengunci baris
// sehingga transaksi kedua harus menunggu sampai yang pertama commit, lalu
// melihat tokennya sudah di-revoke dan gagal dengan benar.
//
// CATATAN TTL: 30 hari sliding. Alternatifnya absolute cap (30 hari dari login
// pertama, tidak peduli berapa kali di-refresh). Sliding dipilih karena lebih
// user-friendly — selama aktif, sesi tidak pernah kedaluwarsa tanpa sebab.
func (s *Service) RotateRefreshToken(ctx context.Context, plaintext, userAgent string) (string, error) {
	// Decode base64, lalu hash bytes-nya (bukan UTF-8 string)
	decoded, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil {
		return "", ErrRefreshTokenInvalid
	}
	hash := sha256.Sum256(decoded)

	var rowID, familyID, userID string
	var revokedAt *time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT id, family_id, user_id, revoked_at
		FROM refresh_tokens
		WHERE token_hash = $1
		FOR UPDATE`,
		hash[:],
	).Scan(&rowID, &familyID, &userID, &revokedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRefreshTokenInvalid
	}
	if err != nil {
		return "", fmt.Errorf("mencari token: %w", err)
	}

	if revokedAt != nil {
		// Token sudah di-revoke — deteksi reuse. Cabut SELURUH family
		// karena penyerang yang merefresh token curian akan membuat token baru
		// yang juga sudah dirottasi oleh pemilik sah. Yang aman: cabut semua.
		_, err := s.pool.Exec(ctx, `
			UPDATE refresh_tokens
			SET revoked_at = now()
			WHERE family_id = $1 AND revoked_at IS NULL`,
			familyID,
		)
		if err != nil {
			return "", fmt.Errorf("mencabut family saat reuse: %w", err)
		}
		// Log level Warn, bukan Error — ini bukan bug sistem, tapi aktivitas
		// mencurigakan yang perlu diselidiki (misalnya log aggregation).
		return "", ErrRefreshTokenReused
	}

	// Periksa apakah sudah lewat expires_at
	var expiresAt time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT expires_at FROM refresh_tokens WHERE id = $1`,
		rowID,
	).Scan(&expiresAt)
	if err != nil {
		return "", fmt.Errorf("membaca kedaluwarsa token: %w", err)
	}
	if time.Now().After(expiresAt) {
		return "", ErrRefreshTokenExpired
	}

	// Mulai transaksi eksplisit agar atomis.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("memulai transaksi: %w", err)
	}
	defer tx.Rollback(ctx)

	// Buat token baru dengan family_id yang SAMA, dapat ID-nya via RETURNING.
	plaintextBaru, err := func() (string, error) {
		bytes := make([]byte, 32)
		if _, err := rand.Read(bytes); err != nil {
			return "", fmt.Errorf("membuat token: %w", err)
		}
		hashBaru := sha256.Sum256(bytes)
		encoded := base64.RawURLEncoding.EncodeToString(bytes)
		expiresAtBaru := time.Now().Add(RefreshTokenTTL)

		var idBaru string
		err := tx.QueryRow(ctx, `
			INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at, user_agent)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id`,
			userID, hashBaru[:], familyID, expiresAtBaru, userAgent,
		).Scan(&idBaru)
		if err != nil {
			return "", fmt.Errorf("menyimpan token baru: %w", err)
		}

		// Update token lama: tandai revoked_at dan replaced_by.
		_, err = tx.Exec(ctx, `
			UPDATE refresh_tokens
			SET revoked_at = now(), replaced_by = $1
			WHERE id = $2`,
			idBaru, rowID,
		)
		if err != nil {
			return "", fmt.Errorf("menandai token dirotasi: %w", err)
		}

		return encoded, nil
	}()
	if err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit transaksi: %w", err)
	}

	return plaintextBaru, nil
}

// RevokeRefreshToken mencabut seluruh family dari token yang diberikan.
// Dipakai saat logout.
func (s *Service) RevokeRefreshToken(ctx context.Context, plaintext string) error {
	decoded, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil {
		return nil // Token tidak valid — logout tetap idempoten
	}
	hash := sha256.Sum256(decoded)

	var familyID string
	err = s.pool.QueryRow(ctx, `
		SELECT family_id FROM refresh_tokens WHERE token_hash = $1`,
		hash[:],
	).Scan(&familyID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Token tidak ada — logout tetap berhasil (idempoten)
		return nil
	}
	if err != nil {
		return fmt.Errorf("mencari family token: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = now()
		WHERE family_id = $1 AND revoked_at IS NULL`,
		familyID,
	)
	if err != nil {
		return fmt.Errorf("mencabut family token: %w", err)
	}

	return nil
}

// UserIDDariRefreshToken membaca user_id dari baris refresh token.
// Dipakai saat refresh untuk mendapat identity tanpa harus minta password lagi.
func (s *Service) UserIDDariRefreshToken(ctx context.Context, plaintext string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil {
		return "", ErrRefreshTokenInvalid
	}
	hash := sha256.Sum256(decoded)

	var userID string
	err = s.pool.QueryRow(ctx, `
		SELECT user_id FROM refresh_tokens WHERE token_hash = $1 AND revoked_at IS NULL`,
		hash[:],
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRefreshTokenInvalid
	}
	if err != nil {
		return "", fmt.Errorf("mencari user dari refresh token: %w", err)
	}

	return userID, nil
}
