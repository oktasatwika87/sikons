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
// 'admin'. Tidak ada validasi di frontend yang bisa mencegah itu: penyerang
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
	// dibalik, orang yang menebak email bisa tahu akun itu ada tapi nonaktif;
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
// Aturan saat ini:
//  1. X aktif -> rotasi X ke Y. Simpan X.replaced_by = Y.
//  2. X sudah dicabut KARENA ROTASI (X.replaced_by != NULL): masih
//     dalam grace period:
//     a. Penerus (Y) masih aktif DAN umurnya >= RefreshSuccessorMinAge
//     (default 2 detik): cabut Y, terbitkan token baru Z dari X,
//     catat Z sebagai pengganti X. Balas 200. Ini memperbaiki skenario
//     "balasan refresh hilang": server commit tapi cookie baru tidak
//     sampai ke browser, dan browser mengirim ulang token lama di
//     page load berikutnya.
//     b. Selain itu (penerus sudah dicabut, atau penerus masih terlalu
//     muda): 409 REFRESH_IN_PROGRESS. Pemenangnya akan menyelesaikan
//     rotasi dengan benar.
//  3. X sudah dicabut KARENA ROTASI dan lewat grace -> reuse. Cabut
//     seluruh keluarga.
//  4. X sudah dicabut TANPA PENERUS (X.replaced_by == NULL);
//     perlakukan sama seperti lewat grace, apapun selisih waktunya.
//     Ini mencakup:
//     - Token yang dicabut oleh RevokeRefreshToken (logout) yang
//     juga membersihkan replaced_by. Tanpa perlakuan ini, user
//     yang logout baru saja masih bisa pakai token yang sudah
//     dirotasi selama 30 detik.
//     - Token penerus yang dicabut oleh cabang 2a (recoveri). Penerus
//     tidak pernah dirotasi, jadi replaced_by-nya NULL sejak
//     dibuat. Saat dikirim ulang harus dianggap pencurian, bukan
//     balapan wajar.
//
// Commit WAJIB saat pencurian terdeteksi: pencabutannya harus tersimpan,
// bukan di-rollback. ErrRefreshTokenReused juga di-log di level Warn
// dengan user_id dan family_id untuk keperluan audit keamanan.
//
// CATATAN TTL: 30 hari sliding. Alternatifnya absolute cap (30 hari dari login
// pertama, tidak peduli berapa kali di-refresh). Sliding dipilih karena lebih
// user-friendly: selama aktif, sesi tidak pernah kedaluwarsa tanpa sebab.
//
// INVARIAN: pada setiap saat, maksimal SATU token aktif per keluarga.
// Dilindungi oleh FOR UPDATE pada baris X + INSERT/UPDATE dalam transaksi
// yang sama. Pada kasus 2a, penerus lama di-revoke sebelum token baru
// diterbitkan, jadi keluarga tetap punya tepat satu token aktif.
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

	// Sekalian ambil replaced_by di SELECT yang sama: dipakai untuk deteksi
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
		// Token sudah dicabut. Urutan cek (lihat juga doc comment
		// fungsi): keluarga kosong dulu, baru cek turunan dari X.

		// a. Keluarga X sudah tidak punya token aktif sama sekali
		// (logout, atau sudah dicabut karena reuse). Balas 401
		// REFRESH_TOKEN_INVALID, tanpa log peringatan reuse, tanpa
		// menyentuh DB lagi. defer Rollback sudah menjamin tidak ada
		// perubahan yang bocor.
		var aktif int
		err := tx.QueryRow(ctx, `
			SELECT count(*) FROM refresh_tokens
			WHERE family_id = $1 AND revoked_at IS NULL`,
			familyID,
		).Scan(&aktif)
		if err != nil {
			return "", "", fmt.Errorf("menghitung token aktif: %w", err)
		}
		if aktif == 0 {
			return "", "", ErrRefreshTokenInvalid
		}

		// b. X tidak punya penerus. Keluarga masih punya token aktif
		// (Z atau penerus lain), tapi X jelas bukan bagian dari
		// rotasi normal: penerus Y-nya pernah dirotasi tanpa
		// diganti, atau Y dicabut oleh cabang 2a. Apapun
		// skenarionya, ini pencurian.
		if replacedBy == nil {
			return s.familyRevokeAndCommit(ctx, tx, familyID, userID)
		}

		selisihGrace := time.Since(*revokedAt)

		// d. Lewat grace -> pencurian. Cabut seluruh keluarga.
		if selisihGrace > gracePeriod {
			return s.familyRevokeAndCommit(ctx, tx, familyID, userID)
		}

		// Masih dalam grace. Cek penerus untuk kasus 2a.
		if replacedBy != nil {
			var penerusRevokedAt *time.Time
			var penerusCreatedAt time.Time
			err = tx.QueryRow(ctx, `
				SELECT created_at, revoked_at FROM refresh_tokens WHERE id = $1`,
				*replacedBy,
			).Scan(&penerusCreatedAt, &penerusRevokedAt)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return "", "", fmt.Errorf("membaca penerus token: %w", err)
			}

			// Kasus 2a: penerus masih aktif DAN umurnya cukup -> balasan
			// refresh sebelumnya kemungkinan hilang. Cabut penerus, terbitkan
			// token baru yang menjadi pengganti X.
			if penerusRevokedAt == nil && time.Since(penerusCreatedAt) >= successorMinAge {
				newPlaintext, newID, err := s.issueNewInTx(ctx, tx, userID, familyID, userAgent)
				if err != nil {
					return "", "", err
				}
				// Tandai penerus lama di-revoke (untuk invariants DB).
				_, err = tx.Exec(ctx, `
					UPDATE refresh_tokens
					SET revoked_at = now()
					WHERE id = $1`,
					*replacedBy,
				)
				if err != nil {
					return "", "", fmt.Errorf("mencabut penerus saat recoveri: %w", err)
				}
				// X.replaced_by menunjuk ke token baru. Penerus lama (Y)
				// TIDAK kita isi replaced_by: biarkan NULL, sama seperti
				// Y dibuat tanpa successor oleh rotasi normal. Dengan begitu
				// Y yang dikirim ulang setelah grace jatuh ke "no successor
				// past grace" -> reuse.
				_, err = tx.Exec(ctx, `
					UPDATE refresh_tokens
					SET replaced_by = $1
					WHERE id = $2`,
					newID, rowID,
				)
				if err != nil {
					return "", "", fmt.Errorf("memperbarui replaced_by X: %w", err)
				}
				if err := tx.Commit(ctx); err != nil {
					return "", "", fmt.Errorf("commit recoveri: %w", err)
				}
				return newPlaintext, userID, nil
			}
		}

		// Kasus 2b: tidak ada penerus, atau penerus sudah dicabut, atau
		// penerus masih terlalu muda. Balapan wajar: JANGAN cabut family,
		// JANGAN commit. Kembalikan ErrRefreshInProgress agar caller retry.
		return "", "", ErrRefreshInProgress
	}

	// Periksa apakah sudah lewat expires_at
	if time.Now().After(expiresAt) {
		return "", "", ErrRefreshTokenExpired
	}

	plaintextBaru, idBaru, err := s.issueNewInTx(ctx, tx, userID, familyID, userAgent)
	if err != nil {
		return "", "", err
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

// issueNewInTx membuat token refresh baru di dalam transaksi yang sudah
// berjalan. Dipakai oleh alur rotasi normal dan oleh alur recoveri (kasus 2a).
// Tidak commit: caller yang memutuskan kapan COMMIT.
func (s *Service) issueNewInTx(ctx context.Context, tx pgx.Tx, userID, familyID, userAgent string) (string, string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("membuat token: %w", err)
	}
	hashBaru := sha256.Sum256(bytes)
	plaintextBaru := base64.RawURLEncoding.EncodeToString(bytes)
	expiresAtBaru := time.Now().Add(RefreshTokenTTL)

	var idBaru string
	err := tx.QueryRow(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		userID, hashBaru[:], familyID, expiresAtBaru, userAgent,
	).Scan(&idBaru)
	if err != nil {
		return "", "", fmt.Errorf("menyimpan token baru: %w", err)
	}
	return plaintextBaru, idBaru, nil
}

// familyRevokeAndCommit mencabut seluruh keluarga dan melakukan COMMIT.
// Dipakai oleh cabang 3 (lewat grace) dan cabang 4 (tanpa penerus).
// Commit eksplisit di sini: pencabutannya harus permanen, bukan
// di-rollback. Setelah selesai, log peringatan reuse dicatat untuk
// audit keamanan.
func (s *Service) familyRevokeAndCommit(ctx context.Context, tx pgx.Tx, familyID, userID string) (string, string, error) {
	_, err := tx.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE family_id = $1 AND revoked_at IS NULL`,
		familyID,
	)
	if err != nil {
		return "", "", fmt.Errorf("mencabut family saat reuse: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("commit pencabutan family saat reuse: %w", err)
	}
	s.log.Warn("refresh token reuse terdeteksi",
		slog.String("user_id", userID),
		slog.String("family_id", familyID))
	return "", "", ErrRefreshTokenReused
}

// RevokeRefreshToken mencabut seluruh keluarga dari token yang diberikan.
// Dipakai saat logout. Satu statement dengan subquery: tidak perlu
// SELECT terpisah.
//
// Hanya `revoked_at` yang diubah, jadi data waktu pencabutan di DB
// adalah waktu yang sebenarnya. `replaced_by` TIDAK disentuh: logika
// "keluarga sudah tidak punya token aktif" di RotateRefreshToken
// (cabang a) mendeteksi efek logout dari sini, bukan dari
// memanipulasi `replaced_by` untuk mengarahkan logika.
func (s *Service) RevokeRefreshToken(ctx context.Context, plaintext string) error {
	decoded, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil {
		return nil // Token tidak valid: logout tetap idempoten
	}
	hash := sha256.Sum256(decoded)

	_, err = s.pool.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)`,
		hash[:],
	)
	if err != nil {
		return fmt.Errorf("mencabut family token: %w", err)
	}
	return nil
}
