package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool   *pgxpool.Pool
	issuer *TokenIssuer
	log    *slog.Logger
}

func NewService(pool *pgxpool.Pool, issuer *TokenIssuer, log *slog.Logger) *Service {
	Hangatkan()
	return &Service{pool: pool, issuer: issuer, log: log}
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
// Semua operasi (SELECT FOR UPDATE, revoke, INSERT+UPDATE token baru) berada
// di dalam SATU transaksi eksplisit. Ini wajib: kalau terpisah, row lock
// dilepas saat implicit commit, dan dua request berbarengan bisa sama-sama
// melihat token "belum dicabut", sama-sama membuat token baru, hasilnya 2
// token aktif dalam satu family.
//
// Deteksi reuse dua lapis:
//
//  1. Kalau token belum di-revoke: rotasi biasa, buat token baru.
//     Kalau dua request berbarengan, yang pertama menang, yang kedua
//     melihat token sudah di-revoke oleh si pemenang.
//
//  2. Token sudah di-revoke:
//     - Ambil juga replaced_by di SELECT yang sama.
//     - Cek penerus: kalau ada, revoked_at-nya apa?
//     - BALAPAN WAJAR: penerus ada AND revoked_at-nya NULL
//     AND now() - revoked_at_token_ini <= grace period
//     → JANGAN cabut family, kembalikan ErrRefreshInProgress
//     - PENCURIAN: selain itu
//     → Cabut seluruh family, commit, log Warn, kembalikan
//     ErrRefreshTokenReused
//
// Commit WAJIB saat pencurian terdeteksi — pencabutannya harus tersimpan,
// bukan di-rollback. ErrRefreshTokenReused juga di-log di level Warn
// dengan user_id dan family_id untuk keperluan audit keamanan.
//
// CATATAN TTL: 30 hari sliding. Alternatifnya absolute cap (30 hari dari login
// pertama, tidak peduli berapa kali di-refresh). Sliding dipilih karena lebih
// user-friendly — selama aktif, sesi tidak pernah kedaluwarsa tanpa sebab.
func (s *Service) RotateRefreshToken(ctx context.Context, plaintext, userAgent string) (string, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil {
		return "", "", ErrRefreshTokenInvalid
	}
	hash := sha256.Sum256(decoded)

	// Mulai transaksi eksplisit SEBELUM SELECT. Ini yang membedakan perbaikan
	// ini dari versi sebelumnya: tanpa transaksi di sini, SELECT FOR UPDATE
	// berjalan di implicit transaction yang langsung commit saat selesai,
	// melepas lock sebelum INSERT+UPDATE berikutnya.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("memulai transaksi: %w", err)
	}
	defer tx.Rollback(ctx)

	// Sekalian ambil replaced_by di SELECT yang sama — dipakai untuk deteksi
	// balapan wajar vs pencurian.
	var rowID, familyID, userID string
	var revokedAt *time.Time
	var expiresAt time.Time
	var replacedBy *string
	err = tx.QueryRow(ctx, `
		SELECT id, family_id, user_id, revoked_at, expires_at, replaced_by
		FROM refresh_tokens
		WHERE token_hash = $1
		FOR UPDATE`,
		hash[:],
	).Scan(&rowID, &familyID, &userID, &revokedAt, &expiresAt, &replacedBy)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrRefreshTokenInvalid
	}
	if err != nil {
		return "", "", fmt.Errorf("mencari token: %w", err)
	}

	if revokedAt != nil {
		// Token sudah di-revoke — cek apakah ini balapan wajar atau pencurian.
		if replacedBy != nil {
			// Ada penerus — cek apakah penerus masih aktif dan dalam grace period.
			var penerusRevokedAt *time.Time
			err = tx.QueryRow(ctx, `
				SELECT revoked_at FROM refresh_tokens WHERE id = $1`,
				*replacedBy,
			).Scan(&penerusRevokedAt)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return "", "", fmt.Errorf("membaca penerus token: %w", err)
			}

			// BALAPAN WAJAR: penerus ada, revoked_at-nya NULL, dan masih dalam grace period.
			if penerusRevokedAt == nil {
				selisih := time.Since(*revokedAt)
				if selisih <= gracePeriod {
					// Balapan wajar — JANGAN cabut family, jangan commit.
					// Kembalikan ErrRefreshInProgress agar caller tahu untuk retry.
					return "", "", ErrRefreshInProgress
				}
			}
		}

		// PENCURIAN: penerus tidak ada, penerus sudah dicabut, atau sudah lewat grace.
		// Cabut seluruh family.
		_, err := tx.Exec(ctx, `
			UPDATE refresh_tokens
			SET revoked_at = now()
			WHERE family_id = $1 AND revoked_at IS NULL`,
			familyID,
		)
		if err != nil {
			return "", "", fmt.Errorf("mencabut family saat reuse: %w", err)
		}
		// Commit WAJIB: pencabutannya harus permanen, bukan di-rollback.
		// Ini yang membedakan reuse detection yang berfungsi dari yang
		// menyesatkan — tanpa commit, database mengira tidak terjadi apa-apa.
		if err := tx.Commit(ctx); err != nil {
			return "", "", fmt.Errorf("commit pencabutan family saat reuse: %w", err)
		}
		s.log.Warn("refresh token reuse terdeteksi",
			slog.String("user_id", userID),
			slog.String("family_id", familyID))
		return "", "", ErrRefreshTokenReused
	}

	// Periksa apakah sudah lewat expires_at
	if time.Now().After(expiresAt) {
		return "", "", ErrRefreshTokenExpired
	}

	// Buat token baru dengan family_id yang SAMA, dapat ID-nya via RETURNING.
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("membuat token: %w", err)
	}
	hashBaru := sha256.Sum256(bytes)
	plaintextBaru := base64.RawURLEncoding.EncodeToString(bytes)
	expiresAtBaru := time.Now().Add(RefreshTokenTTL)

	var idBaru string
	err = tx.QueryRow(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		userID, hashBaru[:], familyID, expiresAtBaru, userAgent,
	).Scan(&idBaru)
	if err != nil {
		return "", "", fmt.Errorf("menyimpan token baru: %w", err)
	}

	// Update token lama: tandai revoked_at dan replaced_by.
	_, err = tx.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now(), replaced_by = $1
		WHERE id = $2`,
		idBaru, rowID,
	)
	if err != nil {
		return "", "", fmt.Errorf("menandai token dirotasi: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("commit transaksi: %w", err)
	}

	return plaintextBaru, userID, nil
}

// RevokeRefreshToken mencabut seluruh family dari token yang diberikan.
// Dipakai saat logout. Cukup SATU statement dengan subquery — tidak perlu
// SELECT terpisah.
func (s *Service) RevokeRefreshToken(ctx context.Context, plaintext string) error {
	decoded, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil {
		return nil // Token tidak valid — logout tetap idempoten
	}
	hash := sha256.Sum256(decoded)

	_, err = s.pool.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)
		  AND revoked_at IS NULL`,
		hash[:],
	)
	if err != nil {
		return fmt.Errorf("mencabut family token: %w", err)
	}
	return nil
}
