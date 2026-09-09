package booking

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service memakai *pgxpool.Pool secara langsung, BUKAN lewat interface.
//
// Ini kebalikan dari yang saya lakukan di package server, dan itu disengaja.
// Aturannya: pakai interface untuk hal yang ingin kamu PALSUKAN, pakai yang
// asli untuk hal yang ingin kamu BUKTIKAN. Yang sedang diuji di sini adalah
// perilaku penguncian baris Postgres — database palsu tidak membuktikan
// apa pun tentang itu.
type Service struct {
	pool           *pgxpool.Pool
	maxActive      int
	minLeadMinutes int
}

func NewService(pool *pgxpool.Pool, minLeadMinutes int) *Service {
	return &Service{
		pool:           pool,
		maxActive:      DefaultMaxActiveBookings,
		minLeadMinutes: minLeadMinutes,
	}
}

// Create memesan satu slot untuk satu mahasiswa.
//
// Urutan di dalam transaction TIDAK BOLEH diubah. Berikut alasannya:
//
// LANGKAH 0 (idempotency — SEBELUM kunci apapun):
//   Klaim idempotency key SEBELUM kunci baris. Kalau diletakkan di akhir,
//   request kembar sudah keburu kalah di kunci slot dan menerima
//   ErrSlotAlreadyBooked — persis yang mau dicegah.
//
//   ON CONFLICT DO NOTHING terhadap transaksi kembar yang belum commit membuat
//   Postgres MENUNGGU, bukan mengembalikan nol baris. Transaction yang menang
//   commit duluan, transaction yang kalah bangun dari wait dan INSERT-nya
//   mendapat constraint violation -> dapat nol baris -> masuk ke replay path.
//   Ini yang menyerialkan request kembar tanpa perlu lock terpisah.
//
//   Satu transaction: crash di tengah = rollback = key hilang = retry bersih.
//   Pola dua fase (INSERT klaim, COMMIT, baru INSERT booking) meninggalkan
//   klaim yatim kalau crash sebelum booking disimpan; dua transaction juga
//   menambah latency dan kompleksitas reaper untuk klaim basi.
//
// LANGKAH 1 — kunci baris mahasiswa.
// LANGKAH 2 — kunci baris slot.
// LANGKAH 3 — hitung booking aktif.
// LANGKAH 4 — simpan booking.
// LANGKAH 5 — tandai slot booked.
// LANGKAH 6 — tandai idempotency selesai.
//
// Urutan 1 lalu 2 selalu sama untuk SEMUA pemanggil. Itulah yang mencegah
// deadlock: deadlock terjadi kalau dua transaksi mengambil dua kunci yang sama
// dalam urutan terbalik, dan di sini urutan terbalik tidak pernah mungkin.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Booking, bool /*replay*/, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("memulai transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// LANGKAH 0 — klaim idempotency key.
	//
	// Kalau key kosong, lewati seluruh mekanisme ini.
	var replay bool
	if in.IdempotencyKey != "" {
		var keyAda bool
		err = tx.QueryRow(ctx, `
			INSERT INTO idempotency_keys (user_id, key, endpoint, request_hash)
			VALUES ($1::uuid, $2, '/api/v1/bookings', $3)
			ON CONFLICT (user_id, key) DO NOTHING
			RETURNING true`,
			in.StudentID, in.IdempotencyKey, in.RequestHash,
		).Scan(&keyAda)
		if errors.Is(err, pgx.ErrNoRows) {
			// Key sudah ada. Cek apakah isinya sama.
			var storedEndpoint, storedHash string
			err = tx.QueryRow(ctx, `
				SELECT endpoint, request_hash
				FROM idempotency_keys
				WHERE user_id = $1::uuid AND key = $2`,
				in.StudentID, in.IdempotencyKey,
			).Scan(&storedEndpoint, &storedHash)
			if err != nil {
				return nil, false, fmt.Errorf("membaca idempotency key: %w", err)
			}
			// endpoint harus sama — key yang sama tidak boleh dipakai di endpoint lain.
			if storedEndpoint != "/api/v1/bookings" {
				return nil, false, ErrIdempotencyReused
			}
			// request_hash berbeda = key dipakai ulang dengan isi berbeda.
			if storedHash != in.RequestHash {
				return nil, false, ErrIdempotencyReused
			}
			// Hash sama: ini replay. Ambil booking yang sudah ada dari database.
			// Sumber kebenaran adalah tabel bookings, bukan response_body.
			var bookingID string
			err = tx.QueryRow(ctx, `
				SELECT id::text FROM bookings
				WHERE slot_id = $1::uuid AND student_id = $2::uuid
				ORDER BY created_at DESC LIMIT 1`,
				in.SlotID, in.StudentID,
			).Scan(&bookingID)
			if err != nil {
				return nil, false, fmt.Errorf("booking tidak ditemukan untuk replay: %w", err)
			}

			// Buat objek Booking dengan ID yang sudah ada.
			return &Booking{ID: bookingID}, true, nil
		} else if err != nil {
			return nil, false, fmt.Errorf("menklaim idempotency key: %w", err)
		}
		// keyAda == true: klaim berhasil, lanjut ke langkah 1.
	}

	// LANGKAH 1 — kunci baris mahasiswa.
	//
	// Tiga hal dicek sekaligus di sini:
	//   - baris ada
	//   - is_active = true  (akun tidak dinonaktifkan)
	//   - role = 'student' (bukan dosen/admin)
	// Semua dalam satu FOR NO KEY UPDATE sehingga konsisten dan efisien.
	//
	// FOR NO KEY UPDATE, bukan FOR UPDATE. Bedanya halus tapi nyata: saat
	// menyimpan booking, Postgres otomatis mengambil kunci FOR KEY SHARE pada
	// baris users karena ada foreign key student_id. FOR UPDATE berbenturan
	// dengan FOR KEY SHARE, jadi memakainya akan memblokir transaksi lain yang
	// sebenarnya tidak ada urusan dengan mahasiswa ini. FOR NO KEY UPDATE tetap
	// berbenturan dengan sesama FOR NO KEY UPDATE — yang persis kita butuhkan
	// untuk menserialkan booking milik mahasiswa yang sama — tanpa mengganggu
	// pemeriksaan foreign key milik orang lain.
	//
	// ErrStudentNotFound sekarang berarti: token menunjuk user yang sudah
	// tidak ada, nonaktif, atau bukan mahasiswa. Di HTTP layer, ini dikembalikan
	// sebagai 401 (token valid tapi akun tidak valid).
	var studentOK bool
	err = tx.QueryRow(ctx, `
		SELECT true FROM users
		WHERE id = $1::uuid AND is_active AND role = 'student'
		FOR NO KEY UPDATE`,
		in.StudentID,
	).Scan(&studentOK)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrStudentNotFound
	}
	if !studentOK {
		return nil, false, ErrStudentNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("mengunci baris mahasiswa: %w", err)
	}

	// LANGKAH 2 — kunci baris slot.
	//
	// Tiga hal dicek sekaligus: status, withdrawn, dan jarak tempuh.
	// Pemeriksaan jarak tempuh pakai now() Postgres, BUKAN time.Now() Go —
	// sumber jam yang berbeda (app server vs database) bisa menghasilkan
	// perbedaan hasil saat beban tinggi atau zona waktu tidak sinkron.
	//
	// IF dengan make_interval(), BUKAN parameter Go, menjamin kondisi ini
	// selalu dievaluasi oleh Postgres pada saat execution, bukan planning.
	var status string
	var withdrawn bool
	var tooSoon bool
	err = tx.QueryRow(ctx, `
		SELECT status::text,
		       withdrawn_at IS NOT NULL,
		       start_at <= now() + make_interval(mins => $2)
		FROM slots WHERE id = $1::uuid FOR UPDATE`,
		in.SlotID, s.minLeadMinutes,
	).Scan(&status, &withdrawn, &tooSoon)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrSlotNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("mengunci baris slot: %w", err)
	}
	if withdrawn {
		return nil, false, ErrSlotWithdrawn
	}
	if tooSoon {
		return nil, false, ErrSlotTooSoon
	}
	if status != "open" {
		return nil, false, ErrSlotAlreadyBooked
	}

	// LANGKAH 3 — hitung booking aktif milik mahasiswa ini.
	//
	// Hanya yang belum lewat yang dihitung: konsultasi minggu lalu tidak boleh
	// menahan jatah minggu ini.
	var aktif int
	err = tx.QueryRow(ctx, `
		SELECT count(*)
		FROM bookings b
		JOIN slots s ON s.id = b.slot_id
		WHERE b.student_id = $1::uuid
		  AND b.status = 'confirmed'
		  AND s.start_at > now()`,
		in.StudentID,
	).Scan(&aktif)
	if err != nil {
		return nil, false, fmt.Errorf("menghitung booking aktif: %w", err)
	}
	if aktif >= s.maxActive {
		return nil, false, ErrLimitReached
	}

	// LANGKAH 4 — simpan booking.
	//
	// Secara logika, sampai di sini slot sudah dikunci dan statusnya sudah
	// dipastikan 'open', jadi INSERT ini tidak mungkin bentrok. Penanganan
	// error 23505 di bawah tetap ditulis sebagai JARING PENGAMAN: kalau suatu
	// hari ada kode lain yang menyimpan booking tanpa melewati langkah 2 —
	// migrasi data, skrip perbaikan, endpoint baru yang ditulis buru-buru —
	// partial unique index one_active_booking_per_slot yang akan menghentikannya,
	// dan pengguna tetap menerima 409 yang benar, bukan 500.
	var b Booking
	var deskripsi *string
	if in.Description != "" {
		deskripsi = &in.Description
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO bookings (slot_id, student_id, topic, description)
		VALUES ($1::uuid, $2::uuid, $3, $4)
		RETURNING id::text, slot_id::text, student_id::text,
		          topic, coalesce(description, ''), status::text, created_at`,
		in.SlotID, in.StudentID, in.Topic, deskripsi,
	).Scan(&b.ID, &b.SlotID, &b.StudentID, &b.Topic, &b.Description, &b.Status, &b.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "one_active_booking_per_slot" {
			return nil, false, ErrSlotAlreadyBooked
		}
		return nil, false, fmt.Errorf("menyimpan booking: %w", err)
	}

	// LANGKAH 5 — tandai slotnya terpakai.
	//
	// `AND status = 'open'` sebenarnya sudah dijamin oleh langkah 2. Ditulis
	// tetap, lalu jumlah baris terdampak diperiksa: kalau suatu saat asumsinya
	// runtuh, transaction ini gagal dengan berisik alih-alih diam-diam
	// meninggalkan slot dan booking dalam keadaan tidak sinkron.
	ct, err := tx.Exec(ctx,
		`UPDATE slots SET status = 'booked' WHERE id = $1::uuid AND status = 'open'`,
		in.SlotID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("menandai slot terpakai: %w", err)
	}
	if ct.RowsAffected() != 1 {
		return nil, false, fmt.Errorf("slot %s gagal ditandai terpakai: %d baris terdampak",
			in.SlotID, ct.RowsAffected())
	}

	// LANGKAH 6 — tandai idempotency selesai.
	//
	// completed_at selalu terisi pada baris yang terlihat transaksi lain,
	// karena baris yang belum selesai belum di-commit.
	if in.IdempotencyKey != "" {
		_, err = tx.Exec(ctx, `
			UPDATE idempotency_keys
			SET status_code = 201,
			    response_body = jsonb_build_object('id', $1::text),
			    completed_at = now()
			WHERE user_id = $2::uuid AND key = $3`,
			b.ID, in.StudentID, in.IdempotencyKey,
		)
		if err != nil {
			return nil, false, fmt.Errorf("menandai idempotency selesai: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit: %w", err)
	}

	return &b, replay, nil
}

// GetByID mengambil satu booking.
func (s *Service) GetByID(ctx context.Context, bookingID string) (*Booking, error) {
	var b Booking
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, slot_id::text, student_id::text,
		       topic, coalesce(description, ''), status::text, created_at
		FROM bookings WHERE id = $1::uuid`,
		bookingID,
	).Scan(&b.ID, &b.SlotID, &b.StudentID, &b.Topic, &b.Description, &b.Status, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSlotNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mengambil booking: %w", err)
	}
	return &b, nil
}

// ListByStudent mengambil booking milik satu mahasiswa dengan pagination.
func (s *Service) ListByStudent(ctx context.Context, studentID, status string, page, perPage int) ([]Booking, int, error) {
	return listBookings(ctx, s.pool, "student", studentID, status, page, perPage)
}

// ListByLecturer mengambil booking di slot milik satu dosen dengan pagination.
func (s *Service) ListByLecturer(ctx context.Context, lecturerID, status string, page, perPage int) ([]Booking, int, error) {
	return listBookings(ctx, s.pool, "lecturer", lecturerID, status, page, perPage)
}

func listBookings(ctx context.Context, pool *pgxpool.Pool, role, userID, status string, page, perPage int) ([]Booking, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}
	offset := (page - 1) * perPage

	var args []any
	var argIdx int

	// Bangun where clause berdasarkan peran.
	var whereClause string
	var orderBy string
	switch role {
	case "student":
		args = append(args, userID)
		argIdx = 2
		whereClause = "b.student_id = $1"
		orderBy = "b.created_at DESC"
	case "lecturer":
		args = append(args, userID)
		argIdx = 2
		whereClause = "sl.lecturer_id = $1"
		orderBy = "sl.start_at DESC"
	}

	if status != "" {
		whereClause += fmt.Sprintf(" AND b.status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}

	// Hitung total.
	var total int
	countQuery := fmt.Sprintf(`SELECT count(*) FROM bookings b JOIN slots sl ON sl.id = b.slot_id WHERE %s`, whereClause)
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("menghitung booking: %w", err)
	}

	// Ambil halaman.
	args = append(args, perPage, offset)
	rows, err := pool.Query(ctx, fmt.Sprintf(`
		SELECT b.id::text, b.slot_id::text, b.student_id::text,
		       b.topic, coalesce(b.description, ''), b.status::text, b.created_at
		FROM bookings b
		JOIN slots sl ON sl.id = b.slot_id
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`,
		whereClause, orderBy, argIdx, argIdx+1),
		args...)
	if err != nil {
		return nil, 0, fmt.Errorf("mengambil booking: %w", err)
	}
	defer rows.Close()

	var bookings []Booking
	for rows.Next() {
		var b Booking
		if err := rows.Scan(&b.ID, &b.SlotID, &b.StudentID, &b.Topic, &b.Description, &b.Status, &b.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("membaca booking: %w", err)
		}
		bookings = append(bookings, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("membaca hasil booking: %w", err)
	}

	return bookings, total, nil
}

// ComputeRequestHash menghitung SHA-256 hex dari field request yang sudah
// diurutkan secara deterministik: slot_id + newline + topic + newline + description.
func ComputeRequestHash(slotID, topic, description string) string {
	data := slotID + "\n" + topic + "\n" + description
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}
